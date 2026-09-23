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
	if _, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), `builtin "datetime_now" is not listed as supported by the elf-direct backend`) {
		t.Fatalf("expected direct backend subset diagnostic, got %v", err)
	}
}

func TestDirectELFLowersAssignmentsAndControlFlow(t *testing.T) {
	p, c := testProgram(t, `
let mut index: Int = 0
let message: String = "loop"
while index < 3 {
    println(message)
    println(index)
    index = index + 1
}
if index == 3 {
    println("done")
} else {
    println("wrong")
}
println("after")
`)
	data, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectNative(data); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "control-flow-program")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("dynamic direct ELF failed to execute: %v", err)
	}
	if string(out) != "loop\n0\nloop\n1\nloop\n2\ndone\nafter\n" {
		t.Fatalf("unexpected dynamic direct ELF output %q", out)
	}
}

func TestDirectELFLowersImmutableArrayRuntime(t *testing.T) {
	p, c := testProgram(t, `
fn make_values(start: Int, count: Int) -> Array[Int] {
    let mut values: Array[Int] = []
    let mut index: Int = 0
    while index < count {
        values = array_push(values, start + index)
        index = index + 1
    }
    return values
}
fn main() -> Nil {
    let left: Array[Int] = [10, 20]
    let right: Array[Int] = [30]
    let joined: Array[Int] = left + right
    println(len(joined))
    println(joined[1])
    println(array_push(joined, 40)[3])
    println(make_values(5, 3)[2])
    return nil
}
`)
	data, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectNative(data); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "array-runtime-program")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("array runtime direct ELF failed to execute: %v", err)
	}
	if string(out) != "3\n20\n40\n7\n" {
		t.Fatalf("unexpected array runtime output %q", out)
	}
}

func TestDirectELFLowersTopLevelArrayRuntime(t *testing.T) {
	p, c := testProgram(t, `
let values: Array[Int] = [1, 2]
println(len(values))
println(values[0])
`)
	data, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "top-level-array-program")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("top-level array direct ELF failed to execute: %v", err)
	}
	if string(out) != "2\n1\n" {
		t.Fatalf("unexpected top-level array output %q", out)
	}
}

func TestDirectELFLowersOptionAndResultRuntime(t *testing.T) {
	p, c := testProgram(t, `
let empty: Option[Int] = none()
let present: Option[Int] = some(41)
println(is_none(empty))
println(is_some(present))
println(unwrap_or(empty, 7))
println(unwrap_or(present, 7))
let success: Result[Int, String] = ok(42)
let failure: Result[Int, String] = err("bad")
println(is_ok(success))
println(is_err(failure))
println(result_unwrap(success))
println(unwrap_or(result_error(failure), "fallback"))
println(unwrap_or(array_get([7, 8], 4), 99))
`)
	data, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "option-result-program")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("option/result direct ELF failed to execute: %v", err)
	}
	if string(out) != "true\ntrue\n7\n41\ntrue\ntrue\n42\nbad\n99\n" {
		t.Fatalf("unexpected option/result output %q", out)
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
