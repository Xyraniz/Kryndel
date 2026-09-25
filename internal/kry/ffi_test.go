package kry

import (
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestFFIBuiltinsInterpreter(t *testing.T) {
	library := "ucrtbase.dll"
	if runtime.GOOS == "linux" {
		library = "libc.so.6"
	}
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("FFI smoke fixture uses Windows CRT or glibc")
	}
	if runtime.GOOS == "windows" {
		libraryHandle, err := ffiOpenLibrary(library)
		if err != nil {
			t.Fatal(err)
		}
		address, err := ffiLookupSymbol(libraryHandle, "strlen")
		if err != nil {
			t.Fatal(err)
		}
		var strlen func(*byte) uintptr
		purego.RegisterFunc(&strlen, address)
		probe := []byte("hello\x00")
		if got := strlen((*byte)(unsafe.Pointer(&probe[0]))); got != 5 {
			t.Fatalf("direct purego strlen returned %d", got)
		}
		if err := ffiCloseLibrary(libraryHandle); err != nil {
			t.Fatal(err)
		}
	}
	src := `fn main() -> Nil {
    match ffi_library_open("LIBRARY") {
        ok(library) => {
            match ffi_symbol(library, "strlen") {
                ok(symbol) => {
                    let buffer: FFIBuffer = ffi_buffer_new(string_to_bytes("hello"))
                    match ffi_buffer_address(buffer) {
                        ok(address) => {
                            match ffi_call(symbol, "i(p)", [address]) {
                                ok(length) => { println(str(length)) }
                                err(problem) => { println("call err") }
                            }
                        }
                        err(problem) => { println("address err") }
                    }
                    ffi_buffer_close(buffer)
					match ffi_call(symbol, "i(f)", []) {
						ok(value) => { println("bad signature accepted") }
						err(problem) => { println("bad signature rejected") }
                    }
                }
                err(problem) => { println("symbol err") }
            }
            ffi_library_close(library)
        }
        err(problem) => { println("library err") }
    }
    return nil
}
`
	src = strings.ReplaceAll(src, "LIBRARY", library)
	got := runInterpUnrestricted(t, src)
	want := "5\nbad signature rejected\n"
	if got != want {
		t.Fatalf("unexpected FFI output: got %q want %q", got, want)
	}
}
