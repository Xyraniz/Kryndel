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

func TestDirectPEWin64StackArguments(t *testing.T) {
	source := `fn sum8(a: Int, b: Int, c: Int, d: Int, e: Int, f: Int, g: Int, h: Int) -> Int {
    return a + b + c + d + e + f + g + h
}
fn add(a: Int, b: Int) -> Int {
    return a + b
}
fn main() -> Nil {
    println(sum8(1, 2, 3, 4, 5, 6, 7, 8))
    println(sum8(add(1, 0), add(2, 0), add(3, 0), add(4, 0), add(5, 0), add(6, 0), add(7, 0), add(8, 0)))
    return nil
}`
	p, c := testProgram(t, source)
	data, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Go PE parser rejected image: %v", err)
	}
	image.Close()
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("generated PE execution requires native windows-amd64")
	}
	path := filepath.Join(t.TempDir(), "stack-arguments.exe")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("direct PE with stack arguments failed: %v; output: %q", err, output)
	}
	if string(output) != "36\n36\n" {
		t.Fatalf("direct PE output = %q, want %q", output, "36\n36\n")
	}
}
