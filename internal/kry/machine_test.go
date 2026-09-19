package kry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirectELFEmitsRunnableMachineCodeForStaticOutput(t *testing.T) {
	p, c := testProgram(t, `let prefix: String = "direct "
println(prefix + str(40 + 2))
`)
	data, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < elfCodeOffset || string(data[:4]) != "\x7fELF" {
		t.Fatal("direct backend did not emit ELF64 bytes")
	}
	if _, err := InspectNative(data); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "direct-program")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("direct ELF failed to execute: %v", err)
	}
	if string(out) != "direct 42\n" {
		t.Fatalf("unexpected direct ELF output %q", out)
	}
}

func TestDirectELFRejectsDynamicConstructs(t *testing.T) {
	p, c := testProgram(t, "let value: String = datetime_now()\nprintln(value)\n")
	if _, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "compile-time") {
		t.Fatalf("expected direct backend subset diagnostic, got %v", err)
	}
}

func TestRuntimeReceivesExplicitProgramArguments(t *testing.T) {
	p, c := testProgram(t, `let args: Array[String] = process_args()
assert_eq(args[0], "input.kir")
assert_eq(args[1], "output")
`)
	r, d := NewRuntimeWithArgs(p, c, DefaultLimits(), Sandbox{}, []string{"input.kir", "output"})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("process_args failed: %s", d.Message)
	}
}
