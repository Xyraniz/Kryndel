package kry

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"debug/pe"
)

func TestSelfhostPEBackendLowersEnumVariants(t *testing.T) {
	image, diagnostic := runSelfhostPEBackend(t, `
enum Mode { Idle, Active }

fn next(mode: Mode) -> Mode {
    if mode == Mode::Idle { return Mode::Active }
    return Mode::Idle
}

fn score(mode: Mode) -> Int {
    if mode == Mode::Active { return 42 }
    return 0
}

fn main() -> Nil {
    assert_eq(score(next(Mode::Idle)), 42)
    assert_eq(score(next(Mode::Active)), 0)
}
`)
	if diagnostic != nil {
		t.Fatalf("selfhost PE backend rejected enum variants: %v", diagnostic)
	}
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("parse enum PE image: %v", err)
	}
	defer file.Close()
	if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("PE machine = %#x, want amd64", file.Machine)
	}
	if _, ok := file.OptionalHeader.(*pe.OptionalHeader64); !ok {
		t.Fatalf("optional header has type %T, want PE32+", file.OptionalHeader)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native enum PE execution requires Windows amd64; PE structure was validated")
	}
	executable := filepath.Join(t.TempDir(), "enum-lowering.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated enum PE failed: %v; output: %s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("enum PE wrote unexpected output %q", output)
	}
}

func TestSelfhostPEBackendPrintsEnumNames(t *testing.T) {
	source := `
enum Mode { Idle, Active, Failed }

fn choose(mode: Mode) -> Mode {
    if mode == Mode::Idle { return Mode::Active }
    return mode
}

fn identity(mode: Mode) -> Mode { return mode }

fn fifth(a: Int, b: Int, c: Int, d: Int, mode: Mode) -> Mode {
    return mode
}

fn main() -> Nil {
    println(Mode::Idle)
    let selected: Mode = choose(Mode::Idle)
    print(selected)
    println(identity(Mode::Failed))
    println(fifth(1, 2, 3, 4, Mode::Active))
}
`
	want := runInterp(t, source)
	image, diagnostic := runSelfhostPEBackend(t, source)
	if diagnostic != nil {
		t.Fatalf("selfhost PE backend rejected enum output: %v", diagnostic)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native enum output requires Windows amd64; PE structure was validated")
	}
	executable := filepath.Join(t.TempDir(), "enum-output.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated enum-output PE failed: %v; output: %s", err, output)
	}
	if string(output) != want {
		t.Fatalf("enum PE output %q differs from interpreter %q", output, want)
	}
}
