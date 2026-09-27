package kry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSelfhostDynamicPEPropagatesFFIResults(t *testing.T) {
	const source = `
fn process_id(symbol: FFISymbol) -> Result[Int, String] {
    let probe: Int = ffi_call(symbol, "u()", [])?
    return ffi_call(symbol, "u()", [])
}

fn missing_symbol(library: FFILibrary) -> Result[Int, String] {
    let symbol: FFISymbol = ffi_symbol(library, "KryndelDefinitelyMissingSymbol")?
    return ffi_call(symbol, "u()", [])
}

fn main() -> Nil {
    ffi_thread_pin()
    let library: FFILibrary = result_unwrap(ffi_library_open("kernel32.dll"))
    let symbol: FFISymbol = result_unwrap(ffi_symbol(library, "GetCurrentProcessId"))
    let success: Result[Int, String] = process_id(symbol)
    println(is_ok(success))
    println(result_unwrap(success) > 0)

    let failure: Result[Int, String] = missing_symbol(library)
    println(is_err(failure))
    ffi_library_close(library)
    ffi_thread_unpin()
}
`
	image, err := runSelfhostPEBackend(t, source)
	if err != nil {
		t.Fatalf("selfhost PE backend rejected Result propagation: %v", err)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native Result propagation execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "selfhost-propagate.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("run generated Result propagation PE: %v; output: %q", err, output)
	}
	lines := strings.Fields(string(output))
	want := []string{"true", "true", "true"}
	if len(lines) != len(want) {
		t.Fatalf("unexpected Result propagation output %q", output)
	}
	for index, want := range want {
		if lines[index] != want {
			t.Fatalf("propagation output line %d = %q, want %q; full output %q", index+1, lines[index], want, output)
		}
	}
}

func TestSelfhostDynamicPEPropagatesOptionValues(t *testing.T) {
	const source = `
fn increment_if_present(values: Map[String, Int]) -> Option[Int] {
    let value: Int = map_get(values, "primary")?
    return some(value + 1)
}

fn main() -> Nil {
    let present: Map[String, Int] = {"primary": 41}
    let missing: Map[String, Int] = {}
    let success: Option[Int] = increment_if_present(present)
    let failure: Option[Int] = increment_if_present(missing)
    println(is_some(success))
    println(unwrap_or(success, -1))
    println(is_none(failure))
    println(unwrap_or(failure, -1))
}
`
	image, err := runSelfhostPEBackend(t, source)
	if err != nil {
		t.Fatalf("selfhost PE backend rejected Option propagation: %v", err)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native Option propagation execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "selfhost-option-propagate.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("run generated Option propagation PE: %v; output: %q", err, output)
	}
	lines := strings.Fields(string(output))
	want := []string{"true", "42", "true", "-1"}
	if len(lines) != len(want) {
		t.Fatalf("unexpected Option propagation output %q", output)
	}
	for index, value := range want {
		if lines[index] != value {
			t.Fatalf("propagation output line %d = %q, want %q; full output %q", index+1, lines[index], value, output)
		}
	}
}
