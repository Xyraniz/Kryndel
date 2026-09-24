package kry

import (
	"bytes"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelfhostPEBackendSupportsWin64StackArguments(t *testing.T) {
	image, d := runSelfhostPEBackend(t, `
fn five(a: Int, b: Int, c: Int, d: Int, e: Int) -> Int {
    return a + b * 10 + c * 100 + d * 1000 + e * 10000
}

fn seven(a: Int, b: Int, c: Int, d: Int, e: Int, f: Int, g: Int) -> Int {
    return a + b * 2 + c * 4 + d * 8 + e * 16 + f * 32 + g * 64
}

fn eight(a: Int, b: Int, c: Int, d: Int, e: Int, f: Int, g: Int, h: Int) -> Int {
    return a + b * 2 + c * 4 + d * 8 + e * 16 + f * 32 + g * 64 + h * 128
}

fn main() -> Nil {
    println(five(1, 2, 3, 4, 5))
    println(seven(1, 2, 3, 4, 5, 6, 7))
    println(seven(1, 2, 3, 4, 5, 6, five(1, 2, 3, 4, 5)))
    println(seven(1, five(1, 2, 3, 4, 5), 3, 4, 5, 6, 7))
    println(eight(1, 2, 3, 4, 5, 6, 7, 8))
}
`)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected Win64 stack arguments: %v", d)
	}
	if len(image) < 512 || !bytes.Equal(image[:2], []byte{'M', 'Z'}) {
		t.Fatalf("selfhost backend emitted a truncated PE image (%d bytes)", len(image))
	}
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected stack-argument image: %v", err)
	}
	defer file.Close()
	if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("PE machine = %#x, want amd64", file.Machine)
	}
	optional, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || optional.Magic != 0x20b || optional.Subsystem != 3 {
		t.Fatalf("expected PE32+ console image, got %#v", file.OptionalHeader)
	}
	if file.Section(".text") == nil || file.Section(".pdata") == nil {
		t.Fatalf("stack-argument image is missing code or unwind sections: %v", file.Sections)
	}
	if optional.DataDirectory[3].VirtualAddress != file.Section(".pdata").VirtualAddress || optional.DataDirectory[3].Size < 24 {
		t.Fatalf("missing unwind records for five- and seven-argument functions: %#v", optional.DataDirectory[3])
	}

	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64; PE structure was validated")
	}
	executable := filepath.Join(t.TempDir(), "stack-arguments.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated PE failed: %v; output: %s", err, output)
	}
	const want = "54321\n769\n3476865\n109407\n1793\n"
	if string(output) != want {
		t.Fatalf("unexpected stack-argument PE output %q, want %q", output, want)
	}
}
