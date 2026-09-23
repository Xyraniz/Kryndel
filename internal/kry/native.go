package kry

//go:generate go run ../../cmd/kry-capgen

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// NativeTarget describes a machine-code output target.
type NativeTarget struct {
	OS   string
	Arch string
	GUI  bool
}

// NativeBackend describes the implementation and build-time dependencies for
// a requested output format.
type NativeBackend struct {
	Name                      string
	ExternalToolchain         string
	RequiresExternalToolchain bool
}

// NativeCapability is one generated format/target row. Status describes the
// backend's target support; external compiler availability is checked when a
// build is requested.
type NativeCapability struct {
	Format       string `json:"format"`
	Target       string `json:"target"`
	Status       string `json:"status"`
	Backend      string `json:"backend"`
	Toolchain    string `json:"toolchain"`
	FeatureScope string `json:"feature_scope"`
	Reason       string `json:"reason,omitempty"`
}

type nativeCapabilityTarget struct {
	name   string
	target NativeTarget
}

var nativeCapabilityTargets = []nativeCapabilityTarget{
	{name: "linux-x64", target: NativeTarget{OS: "linux", Arch: "amd64"}},
	{name: "linux-arm64", target: NativeTarget{OS: "linux", Arch: "arm64"}},
	{name: "windows-x64", target: NativeTarget{OS: "windows", Arch: "amd64"}},
	{name: "windows-arm64", target: NativeTarget{OS: "windows", Arch: "arm64"}},
	{name: "darwin-x64", target: NativeTarget{OS: "darwin", Arch: "amd64"}},
	{name: "darwin-arm64", target: NativeTarget{OS: "darwin", Arch: "arm64"}},
}

// DescribeNativeBackend returns the effective backend contract for a native
// output format. The description is shared by the CLI and build policy checks
// so output and enforcement cannot drift apart.
func DescribeNativeBackend(format string) (NativeBackend, error) {
	switch format {
	case "elf-direct":
		return NativeBackend{Name: "direct ELF", ExternalToolchain: "none"}, nil
	case "c":
		return NativeBackend{Name: "C source", ExternalToolchain: "none (source only)"}, nil
	case "exe", "pe", "elf":
		return NativeBackend{Name: "C AOT", ExternalToolchain: "external C compiler", RequiresExternalToolchain: true}, nil
	case "macho":
		return NativeBackend{}, fmt.Errorf("Mach-O output is not implemented; use --format=c with a local clang")
	default:
		return NativeBackend{}, fmt.Errorf("unsupported native format %q", format)
	}
}

// NativeCapabilityMatrix builds the canonical target matrix from the same
// target policy used by native output validation. Compiler availability is
// intentionally reported as a build-time dependency, since cross compilers
// vary by host.
func NativeCapabilityMatrix() []NativeCapability {
	formats := []struct {
		name  string
		scope string
	}{
		{name: "elf", scope: "C AOT subset; host integrations without a C runtime implementation are rejected"},
		{name: "elf-direct", scope: "documented direct ELF subset; scalar, string, array, struct, Option/Result, and function slices"},
		{name: "exe", scope: "C AOT subset; host integrations without a C runtime implementation are rejected"},
		{name: "pe", scope: "C AOT subset; host integrations without a C runtime implementation are rejected"},
		{name: "macho", scope: "not implemented"},
	}
	rows := make([]NativeCapability, 0, len(nativeCapabilityTargets)*len(formats)+1)
	for _, format := range formats {
		backend, backendErr := DescribeNativeBackend(format.name)
		for _, target := range nativeCapabilityTargets {
			row := NativeCapability{
				Format:       format.name,
				Target:       target.name,
				Status:       "supported",
				Backend:      backend.Name,
				Toolchain:    backend.ExternalToolchain,
				FeatureScope: format.scope,
			}
			if backendErr != nil {
				row.Status = "unsupported"
				row.Backend = "none"
				row.Toolchain = "none"
				row.Reason = backendErr.Error()
			} else if reason := nativeOutputTargetReason(format.name, target.target); reason != "" {
				row.Status = "unsupported"
				row.Reason = reason
			} else if format.name == "elf-direct" {
				row.Status = "partial"
				row.Toolchain = "none"
			}
			rows = append(rows, row)
		}
	}
	rows = append(rows, NativeCapability{
		Format:       "c",
		Target:       "any",
		Status:       "source-only",
		Backend:      "C source",
		Toolchain:    "none (source generation only)",
		FeatureScope: "generated C subset; this command does not compile the output",
	})
	return rows
}

type cCompiler struct {
	program string
	prefix  []string
	wsl     bool
}

func ParseNativeTarget(raw string) (NativeTarget, error) {
	if raw == "" || raw == "host" {
		return NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}, nil
	}
	var t NativeTarget
	switch raw {
	case "windows-x64":
		t = NativeTarget{OS: "windows", Arch: "amd64"}
	case "windows-arm64":
		t = NativeTarget{OS: "windows", Arch: "arm64"}
	case "linux-x64":
		t = NativeTarget{OS: "linux", Arch: "amd64"}
	case "linux-arm64":
		t = NativeTarget{OS: "linux", Arch: "arm64"}
	case "darwin-x64":
		t = NativeTarget{OS: "darwin", Arch: "amd64"}
	case "darwin-arm64":
		t = NativeTarget{OS: "darwin", Arch: "arm64"}
	default:
		return NativeTarget{}, fmt.Errorf("unsupported native target %q", raw)
	}
	return t, nil
}

// BuildNative compiles a checked program into a runnable executable using the
// requested backend. The "elf-direct" format bypasses C; the "elf", "exe", and
// "pe" formats lower to C and invoke an external C compiler.
func BuildNative(p *Program, c *Checker, target NativeTarget, format string) ([]byte, error) {
	return BuildNativeOpts(p, c, target, format, false)
}

// BuildNativeOpts is BuildNative with an explicit obfuscation flag. When
// obfuscate is true, string literals are masked so their plaintext does not
// appear in the produced executable.
func BuildNativeOpts(p *Program, c *Checker, target NativeTarget, format string, obfuscate bool) ([]byte, error) {
	return BuildNativeWithPolicyOpts(p, c, target, format, obfuscate, false)
}

// nativeExecCommand is isolated for regression tests that prove a rejected
// no-toolchain build never attempts to launch an external compiler.
var nativeExecCommand = exec.Command

// BuildNativeWithPolicyOpts is BuildNativeOpts with an explicit external
// toolchain policy. When noExternalToolchain is true, formats that compile
// generated C are rejected before code generation or process execution.
func BuildNativeWithPolicyOpts(p *Program, c *Checker, target NativeTarget, format string, obfuscate, noExternalToolchain bool) ([]byte, error) {
	if p == nil || c == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	backend, err := DescribeNativeBackend(format)
	if err != nil {
		return nil, err
	}
	if noExternalToolchain && backend.RequiresExternalToolchain {
		return nil, fmt.Errorf("--no-external-toolchain forbids --format=%s: the %s backend requires an external C compiler; use --format=elf-direct for the supported direct ELF backend", format, backend.Name)
	}
	if format == "elf-direct" {
		if err := validateNativeOutputTarget(format, target); err != nil {
			return nil, err
		}
		if obfuscate {
			return nil, fmt.Errorf("direct ELF backend does not support C obfuscation flags")
		}
		return BuildDirectELF(p, c, target)
	}
	if err := validateNativeOutputTarget(format, target); err != nil {
		return nil, err
	}
	if err := validateNativeFeatureSupport(p, c, format, target); err != nil {
		return nil, err
	}
	var src string
	if obfuscate {
		src, err = GenerateCObfuscated(p, c)
	} else {
		src, err = GenerateC(p, c)
	}
	if err != nil {
		return nil, err
	}
	switch format {
	case "c":
		return []byte(src), nil
	case "exe", "pe", "elf":
	case "macho":
		return nil, fmt.Errorf("Mach-O output requires a Darwin toolchain; use --format=c and a local clang")
	default:
		return nil, fmt.Errorf("unsupported native format %q", format)
	}
	return compileC(src, target)
}

func validateNativeOutputTarget(format string, target NativeTarget) error {
	if reason := nativeOutputTargetReason(format, target); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func nativeOutputTargetReason(format string, target NativeTarget) string {
	switch format {
	case "exe", "pe":
		if target.OS != "windows" {
			return "PE output requires a Windows target"
		}
		if target.Arch != "amd64" {
			return "C AOT currently supports Windows amd64 targets only"
		}
	case "elf":
		if target.OS != "linux" {
			return "ELF output requires a Linux target"
		}
		if target.Arch != "amd64" && target.Arch != "arm64" {
			return "C AOT currently supports Linux amd64 and arm64 targets only"
		}
	case "elf-direct":
		if target.OS != "linux" || target.Arch != "amd64" {
			return "direct ELF backend currently supports only linux-amd64"
		}
	}
	return ""
}

// EmitC returns the generated C source for a checked program.
func EmitC(p *Program, c *Checker) (string, error) { return GenerateC(p, c) }

// compilerFor selects the C compiler and its target flags for a build.
func compilerFor(target NativeTarget) (cCompiler, error) {
	if override := os.Getenv("KRY_CC"); override != "" {
		return cCompiler{program: override}, nil
	}
	hostOS, hostArch := runtime.GOOS, runtime.GOARCH
	switch target.OS {
	case "windows":
		if target.Arch != "amd64" {
			return cCompiler{}, fmt.Errorf("no cross compiler configured for windows-%s", target.Arch)
		}
		if hostOS == "windows" {
			return cCompiler{program: "gcc"}, nil
		}
		return cCompiler{program: "x86_64-w64-mingw32-gcc"}, nil
	case "linux":
		if target.Arch == hostArch && hostOS == "linux" {
			return cCompiler{program: "cc"}, nil
		}
		if target.Arch == "amd64" {
			return linuxCrossCompiler("x86_64-linux-gnu-gcc")
		}
		if target.Arch == "arm64" {
			return linuxCrossCompiler("aarch64-linux-gnu-gcc")
		}
		return cCompiler{}, fmt.Errorf("no cross compiler configured for linux-%s", target.Arch)
	case "darwin":
		return cCompiler{}, fmt.Errorf("no cross compiler configured for darwin-%s", target.Arch)
	}
	return cCompiler{}, fmt.Errorf("unsupported target %s-%s", target.OS, target.Arch)
}

func linuxCrossCompiler(name string) (cCompiler, error) {
	if _, err := exec.LookPath(name); err == nil {
		return cCompiler{program: name}, nil
	}
	if runtime.GOOS != "windows" {
		return cCompiler{program: name}, nil
	}
	wsl, err := exec.LookPath("wsl.exe")
	if err != nil {
		return cCompiler{program: name}, nil
	}
	distro := os.Getenv("KRY_WSL_DISTRO")
	if distro == "" {
		distro = "Ubuntu"
	}
	probe := nativeExecCommand(wsl, "-d", distro, "--", name, "--version")
	probe.Stdout = io.Discard
	probe.Stderr = io.Discard
	if err := probe.Run(); err != nil {
		return cCompiler{program: name}, nil
	}
	return cCompiler{program: wsl, prefix: []string{"-d", distro, "--", name}, wsl: true}, nil
}

func compileC(src string, target NativeTarget) ([]byte, error) {
	compiler, err := compilerFor(target)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(compiler.program); err != nil {
		return nil, fmt.Errorf("C compiler %q not found; install it, configure WSL, or set KRY_CC", compiler.program)
	}
	dir, err := os.MkdirTemp("", "kry-native-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cpath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(cpath, []byte(src), 0o644); err != nil {
		return nil, err
	}
	ext := ""
	if target.OS == "windows" {
		ext = ".exe"
	}
	out := filepath.Join(dir, "program"+ext)
	args := []string{"-O2", "-std=c11", "-w", "-o", out, cpath, "-lm"}
	if compiler.wsl {
		args[len(args)-2] = wslPath(cpath)
		args[4] = wslPath(out)
	}
	args = append(append([]string{}, compiler.prefix...), args...)
	cmd := nativeExecCommand(compiler.program, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("C compilation failed: %s", msg)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func wslPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	volume := filepath.VolumeName(abs)
	if len(volume) == 2 && volume[1] == ':' {
		rest := strings.TrimPrefix(abs, volume)
		return "/mnt/" + strings.ToLower(volume[:1]) + strings.ReplaceAll(filepath.ToSlash(rest), "\\", "/")
	}
	return filepath.ToSlash(abs)
}

// EmitLLVMIR is intentionally unavailable until Kryndel has a real lowering
// to LLVM's typed SSA model. The old implementation emitted a valid-looking
// function that always returned zero, which was not an IR representation of
// the checked program and could hide compiler bugs.
func EmitLLVMIR(p *Program, target NativeTarget) ([]byte, error) {
	return nil, fmt.Errorf("LLVM IR emission is not implemented; use --format=kry-ir")
}

func InspectNative(data []byte) (string, error) {
	if len(data) >= 2 && data[0] == 'M' && data[1] == 'Z' {
		if len(data) < 0x40 {
			return "PE: truncated DOS header", fmt.Errorf("truncated DOS header")
		}
		pe := int(binary.LittleEndian.Uint32(data[0x3c:0x40]))
		if pe < 0 || pe+24 > len(data) || !bytes.Equal(data[pe:pe+4], []byte("PE\x00\x00")) {
			return "PE: invalid signature", fmt.Errorf("invalid PE signature")
		}
		machine := binary.LittleEndian.Uint16(data[pe+4 : pe+6])
		sections := binary.LittleEndian.Uint16(data[pe+6 : pe+8])
		var arch string
		switch machine {
		case 0x8664:
			arch = "windows-x64"
		case 0xaa64:
			arch = "windows-arm64"
		default:
			arch = fmt.Sprintf("machine-0x%x", machine)
		}
		return fmt.Sprintf("PE\narchitecture: %s\nsections: %d\n", arch, sections), nil
	}
	if len(data) >= 20 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		if data[4] != 2 || data[5] != 1 {
			return "ELF: unsupported class or endianness", fmt.Errorf("unsupported ELF")
		}
		machine := binary.LittleEndian.Uint16(data[18:20])
		return fmt.Sprintf("ELF64\nmachine: 0x%x\n", machine), nil
	}
	return "unknown binary format", fmt.Errorf("unrecognized executable format")
}
