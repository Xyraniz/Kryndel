package kry

import (
	"bytes"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This exercises the selfhost PE backend through runSelfhostPEBackend: Go
// emits KIR and interprets kir_backend.kry to produce the PE. It is a native
// Windows execution proof for backend imports, not a Stage2/Stage3 bootstrap
// proof that a generated Kryndel compiler can compile source without Go.
func TestSelfhostDynamicPEFFIRunsWindowsCallsAndBuffers(t *testing.T) {
	const source = `
fn main() -> Nil {
    ffi_thread_pin()
    println(10)
    let allocator_probe: FFIBuffer = ffi_buffer_new(string_to_bytes("allocator"))
    println(11)
    let allocator_address: Int = result_unwrap(ffi_buffer_address(allocator_probe))
    println(allocator_address > 0)
    // Regression: these FFI imports are emitted before u8_array below turns
    // on the optional host-runtime imports.
    ffi_buffer_close(allocator_probe)
    println(12)
    let opened: Result[FFILibrary, String] = ffi_library_open("kernel32.dll")
    println(13)
    println(is_ok(opened))
    let library: FFILibrary = result_unwrap(opened)

    let pid_symbol_result: Result[FFISymbol, String] = ffi_symbol(library, "GetCurrentProcessId")
    println(is_ok(pid_symbol_result))
    let pid_symbol: FFISymbol = result_unwrap(pid_symbol_result)
    let pid_result: Result[Int, String] = ffi_call(pid_symbol, "u()", [])
    println(is_ok(pid_result))
    let pid: Int = result_unwrap(pid_result)
    println(pid)

    let move_symbol_result: Result[FFISymbol, String] = ffi_symbol(library, "RtlMoveMemory")
    println(is_ok(move_symbol_result))
    let move_symbol: FFISymbol = result_unwrap(move_symbol_result)
    let input: FFIBuffer = ffi_buffer_new(string_to_bytes("Hello!"))
    let output: FFIBuffer = result_unwrap(ffi_buffer_new_sized(16))
    let input_address: Int = result_unwrap(ffi_buffer_address(input))
    let output_address: Int = result_unwrap(ffi_buffer_address(output))
    let move_result: Result[Int, String] = ffi_call(move_symbol, "v(p,p,u)", [output_address, input_address, 6])
    println(result_unwrap(move_result))
    let copied: Bytes = result_unwrap(ffi_buffer_read(output))
    let copied_values: Array[UInt8] = u8_array(copied)
    println(copied_values[0])
    println(copied_values[5])
    println(len(copied))

    let device_io_result: Result[FFISymbol, String] = ffi_symbol(library, "DeviceIoControl")
    println(is_ok(device_io_result))
    let device_io: FFISymbol = result_unwrap(device_io_result)
    let eight_arguments: Result[Int, String] = ffi_call(device_io, "i(p,u,p,u,p,u,p,p)", [0, 0, 0, 0, 0, 0, 0, 0])
    println(is_ok(eight_arguments))
    println(result_unwrap(eight_arguments))

    let wrong_values: Array[Int] = [1]
    let wrong_arity: Result[Int, String] = ffi_call(pid_symbol, "u()", wrong_values)
    println(is_err(wrong_arity))

    ffi_buffer_close(input)
    ffi_buffer_close(output)
    println(is_err(ffi_buffer_address(output)))
    ffi_library_close(library)
    ffi_thread_unpin()
}
`
	image, err := runSelfhostPEBackend(t, source)
	if err != nil {
		t.Fatalf("selfhost PE backend rejected the FFI fixture: %v", err)
	}

	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the generated FFI image: %v", err)
	}
	imports, err := file.ImportedSymbols()
	file.Close()
	if err != nil {
		t.Fatalf("read generated PE import table: %v", err)
	}
	for _, want := range []string{
		"LoadLibraryA:KERNEL32.dll",
		"GetProcAddress:KERNEL32.dll",
		"FreeLibrary:KERNEL32.dll",
		"HeapFree:KERNEL32.dll",
	} {
		if !containsString(imports, want) {
			t.Errorf("generated PE imports do not contain %q; imports: %v", want, imports)
		}
	}

	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native FFI PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "selfhost-ffi.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("run generated Windows FFI PE: %v; output: %q", err, output)
	}
	lines := strings.Fields(string(output))
	if len(lines) != 19 {
		t.Fatalf("unexpected FFI probe output %q; want stage markers, PID, buffer values, and arity checks", output)
	}
	pid, parseErr := strconv.ParseUint(lines[8], 10, 32)
	if parseErr != nil || pid == 0 {
		t.Fatalf("GetCurrentProcessId result %q is not a positive PID; output %q", lines[8], output)
	}
	want := []string{"10", "11", "true", "12", "13", "true", "true", "true", "", "true", "0", "72", "33", "16", "true", "true", "0", "true", "true"}
	for index, value := range want {
		if value == "" {
			continue
		}
		if lines[index] != value {
			t.Fatalf("FFI probe output line %d is %q, want %q; full output %q", index, lines[index], value, output)
		}
	}
}

func TestSelfhostDynamicPEFFIRejectsInvalidSignatures(t *testing.T) {
	cases := []struct {
		name        string
		signature   string
		arguments   string
		local       string
		wantMessage string
	}{
		{
			name:        "unsupported token",
			signature:   `"i(f)"`,
			arguments:   `[]`,
			wantMessage: "FFI arguments support only i, u, and p",
		},
		{
			name:        "static arity mismatch",
			signature:   `"i(i)"`,
			arguments:   `[]`,
			wantMessage: "FFI signature declares 1 argument(s), got 0",
		},
		{
			name:        "more than eight arguments",
			signature:   `"i(i,i,i,i,i,i,i,i,i)"`,
			arguments:   `[0, 0, 0, 0, 0, 0, 0, 0, 0]`,
			wantMessage: "FFI calls support at most 8 arguments",
		},
		{
			name:        "nonliteral signature",
			signature:   `signature`,
			arguments:   `[]`,
			local:       `let signature: String = "u()"`,
			wantMessage: "ffi_call signature must be a literal string",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := tc.local
			if local != "" {
				local += "\n"
			}
			source := `fn main() -> Nil {
    let library: FFILibrary = result_unwrap(ffi_library_open("kernel32.dll"))
    let symbol: FFISymbol = result_unwrap(ffi_symbol(library, "GetCurrentProcessId"))
    ` + local + `    let result: Result[Int, String] = ffi_call(symbol, ` + tc.signature + `, ` + tc.arguments + `)
    println(is_ok(result))
    ffi_library_close(library)
}`
			image, err := runSelfhostPEBackend(t, source)
			if err == nil {
				t.Fatalf("invalid signature unexpectedly compiled to %d-byte PE", len(image))
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("diagnostic %q does not contain %q", err, tc.wantMessage)
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
