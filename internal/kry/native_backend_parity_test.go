package kry

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeBackendParityFixture(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "tests", "conformance", "native-backend-parity.kry")
	sourceBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	wantOutput := "integer=42\n-9223372036854775808\ntrue\nfalse\nfirst\nsecond\n"
	program, checker := parseCheckOptionResult(t, source)
	kir, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kir, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if err := validateKIRDirectELFValueSubset(document); err != nil {
		t.Fatalf("shared parity fixture must use the KIR direct-ELF lowering: %v", err)
	}

	interpreter := runOptionResultInterpreter(t, source)
	assertOptionResultOutcome(t, "interpreter parity fixture", interpreter, optionResultObservedOutcome{stdout: wantOutput})

	for _, test := range []struct {
		name, format, target, builtin, want string
	}{
		{name: "C AOT Linux", format: "elf", target: "linux-x64", builtin: "str", want: "supported"},
		{name: "C AOT macOS x64", format: "macho", target: "darwin-x64", builtin: "str", want: "supported"},
		{name: "C AOT macOS ARM64", format: "macho", target: "darwin-arm64", builtin: "str", want: "supported"},
		{name: "ELF direct str", format: "elf-direct", target: "linux-x64", builtin: "str", want: "partial"},
		{name: "ELF direct print", format: "elf-direct", target: "linux-x64", builtin: "print", want: "partial"},
		{name: "ELF direct println", format: "elf-direct", target: "linux-x64", builtin: "println", want: "partial"},
		{name: "PE direct str", format: "pe-direct", target: "windows-x64", builtin: "str", want: "partial"},
		{name: "PE direct print", format: "pe-direct", target: "windows-x64", builtin: "print", want: "partial"},
		{name: "PE direct println", format: "pe-direct", target: "windows-x64", builtin: "println", want: "partial"},
	} {
		t.Run("status/"+test.name, func(t *testing.T) {
			var target NativeTarget
			switch test.target {
			case "linux-x64":
				target = NativeTarget{OS: "linux", Arch: "amd64"}
			case "darwin-x64":
				target = NativeTarget{OS: "darwin", Arch: "amd64"}
			case "darwin-arm64":
				target = NativeTarget{OS: "darwin", Arch: "arm64"}
			case "windows-x64":
				target = NativeTarget{OS: "windows", Arch: "amd64"}
			}
			if got := nativeBuiltinBackendStatus(test.builtin, test.format, target); got != test.want {
				t.Fatalf("%s capability for %s = %q, want %q", test.format, test.builtin, got, test.want)
			}
		})
	}

	t.Run("C AOT", func(t *testing.T) {
		target, format, targetName, ok := nativeHostCAOTTarget()
		if !ok {
			t.Skip("C AOT parity fixture requires Linux amd64/arm64, Darwin amd64/arm64, or Windows amd64")
		}
		if !optionResultCompilerAvailable(target) {
			t.Skipf("C compiler for %s-%s is unavailable", target.OS, target.Arch)
		}
		if status := nativeBuiltinBackendStatus("str", format, target); status != "supported" {
			t.Fatalf("C AOT capability for str on %s = %q, want supported", targetName, status)
		}
		native := buildAndRunOptionResultNative(t, source, target, format)
		assertOptionResultOutcome(t, "C AOT parity fixture", native, interpreter)
	})

	t.Run("ELF direct", func(t *testing.T) {
		if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
			t.Skip("ELF-direct execution requires Linux amd64")
		}
		native := buildAndRunOptionResultDirectELF(t, source)
		assertOptionResultOutcome(t, "ELF-direct parity fixture", native, interpreter)
	})

	t.Run("PE direct", func(t *testing.T) {
		if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
			t.Skip("PE-direct execution requires Windows amd64")
		}
		interpretedOutput, diagnostic, nativeOutput, nativeStderr, status := runDirectPEBuiltinOutcome(t, source, DefaultLimits())
		if diagnostic != nil || interpretedOutput != wantOutput {
			t.Fatalf("PE fixture interpreter output=%q diagnostic=%#v", interpretedOutput, diagnostic)
		}
		if status != 0 || nativeOutput != interpretedOutput || nativeStderr != "" {
			t.Fatalf("PE-direct fixture differs: interpreter=%q; PE stdout=%q stderr=%q status=%d", interpretedOutput, nativeOutput, nativeStderr, status)
		}
	})
}

func nativeHostCAOTTarget() (NativeTarget, string, string, bool) {
	target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
	archName := "x64"
	if target.Arch == "arm64" {
		archName = "arm64"
	}
	switch target.OS {
	case "linux":
		if target.Arch == "amd64" || target.Arch == "arm64" {
			return target, "elf", "linux-" + archName, true
		}
	case "darwin":
		if target.Arch == "amd64" || target.Arch == "arm64" {
			return target, "macho", "darwin-" + archName, true
		}
	case "windows":
		if target.Arch == "amd64" {
			return target, "exe", "windows-x64", true
		}
	}
	return NativeTarget{}, "", "", false
}
