package kry

import (
	"bytes"
	"encoding/binary"
	"fmt"
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

// BuildNative compiles a checked program into a real, runnable executable. The
// program is lowered to C and compiled with the host C compiler (or a cross
// compiler for Windows targets), producing genuine PE/ELF binaries that execute
// the full checked language rather than a constant-output stub.
func BuildNative(p *Program, c *Checker, target NativeTarget, format string) ([]byte, error) {
	return BuildNativeOpts(p, c, target, format, false)
}

// BuildNativeOpts is BuildNative with an explicit obfuscation flag. When
// obfuscate is true, string literals are masked so their plaintext does not
// appear in the produced executable.
func BuildNativeOpts(p *Program, c *Checker, target NativeTarget, format string, obfuscate bool) ([]byte, error) {
	if p == nil || c == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	var src string
	var err error
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
	case "exe", "pe":
		if target.OS != "windows" {
			return nil, fmt.Errorf("PE output requires a Windows target")
		}
	case "elf":
		if target.OS != "linux" {
			return nil, fmt.Errorf("ELF output requires a Linux target")
		}
	case "macho":
		return nil, fmt.Errorf("Mach-O output requires a Darwin toolchain; use --format=c and a local clang")
	default:
		return nil, fmt.Errorf("unsupported native format %q", format)
	}
	return compileC(src, target)
}

// EmitC returns the generated C source for a checked program.
func EmitC(p *Program, c *Checker) (string, error) { return GenerateC(p, c) }

// compilerFor selects the C compiler and its target flags for a build.
func compilerFor(target NativeTarget) (string, []string, error) {
	if override := os.Getenv("KRY_CC"); override != "" {
		return override, nil, nil
	}
	hostOS, hostArch := runtime.GOOS, runtime.GOARCH
	switch target.OS {
	case "windows":
		if target.Arch != "amd64" {
			return "", nil, fmt.Errorf("no cross compiler configured for windows-%s", target.Arch)
		}
		if hostOS == "windows" {
			return "gcc", nil, nil
		}
		return "x86_64-w64-mingw32-gcc", nil, nil
	case "linux":
		if target.Arch == hostArch && hostOS == "linux" {
			return "cc", nil, nil
		}
		if target.Arch == "amd64" {
			return "x86_64-linux-gnu-gcc", nil, nil
		}
		if target.Arch == "arm64" {
			return "aarch64-linux-gnu-gcc", nil, nil
		}
		return "", nil, fmt.Errorf("no cross compiler configured for linux-%s", target.Arch)
	case "darwin":
		return "", nil, fmt.Errorf("no cross compiler configured for darwin-%s", target.Arch)
	}
	return "", nil, fmt.Errorf("unsupported target %s-%s", target.OS, target.Arch)
}

func compileC(src string, target NativeTarget) ([]byte, error) {
	cc, extra, err := compilerFor(target)
	if err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(cc); err != nil {
		return nil, fmt.Errorf("C compiler %q not found; install it or set KRY_CC", cc)
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
	args = append(extra, args...)
	cmd := exec.Command(cc, args...)
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

func EmitLLVMIR(p *Program, target NativeTarget) []byte {
	name := "kryndel_main"
	return []byte(fmt.Sprintf("; Kryndel checked IR emission for %s/%s\nsource_filename = \"kryndel\"\n\ndefine i32 @%s() {\nentry:\n  ret i32 0\n}\n", target.OS, target.Arch, name))
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
