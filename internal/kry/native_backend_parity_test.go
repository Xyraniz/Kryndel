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

	interpreter := runOptionResultInterpreter(t, source)
	assertOptionResultOutcome(t, "interpreter parity fixture", interpreter, optionResultObservedOutcome{stdout: wantOutput})

	for _, test := range []struct {
		name, format, target, builtin, want string
	}{
		{name: "C AOT Linux", format: "elf", target: "linux-x64", builtin: "str", want: "supported"},
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
			case "windows-x64":
				target = NativeTarget{OS: "windows", Arch: "amd64"}
			}
			if got := nativeBuiltinBackendStatus(test.builtin, test.format, target); got != test.want {
				t.Fatalf("%s capability for %s = %q, want %q", test.format, test.builtin, got, test.want)
			}
		})
	}

	t.Run("C AOT", func(t *testing.T) {
		if runtime.GOARCH != "amd64" || runtime.GOOS != "linux" && runtime.GOOS != "windows" {
			t.Skip("C AOT parity fixture requires Linux or Windows amd64")
		}
		target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
		if !optionResultCompilerAvailable(target) {
			t.Skipf("C compiler for %s-%s is unavailable", target.OS, target.Arch)
		}
		format := "elf"
		if target.OS == "windows" {
			format = "exe"
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
