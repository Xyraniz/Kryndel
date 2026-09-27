package kry

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestCAOTFilesystemIOMatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	root := t.TempDir()
	inputPath := filepath.Join(root, "input.txt")
	if err := os.WriteFile(inputPath, []byte("Kryndel files: é\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	danglingLink := filepath.Join(root, "dangling-link")
	if err := os.Symlink(filepath.Join(root, "missing-target"), danglingLink); err != nil {
		t.Fatal(err)
	}
	program := func(textPath, bytesPath string) string {
		return "fn main() -> Nil {\n" +
			"    println(fs_read_text(" + strconv.Quote(inputPath) + "))\n" +
			"    println(fs_read_bytes(" + strconv.Quote(inputPath) + "))\n" +
			"    println(fs_exists(" + strconv.Quote(inputPath) + "))\n" +
			"    println(fs_exists(" + strconv.Quote(root) + "))\n" +
			"    println(fs_exists(" + strconv.Quote(danglingLink) + "))\n" +
			"    println(fs_exists(" + strconv.Quote(filepath.Join(root, "missing-target")) + "))\n" +
			"    println(fs_write_text(" + strconv.Quote(textPath) + ", \"written: é\\n\"))\n" +
			"    println(fs_write_bytes(" + strconv.Quote(bytesPath) + ", string_to_bytes(\"raw\\x00bytes\")))\n" +
			"}\n"
	}

	interpreterTextPath := filepath.Join(root, "interpreter", "nested", "written.txt")
	interpreterBytesPath := filepath.Join(root, "interpreter", "nested", "written.bin")
	interpreted, diagnostic := runInterpreterCapture(t, program(interpreterTextPath, interpreterBytesPath))
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}

	nativeTextPath := filepath.Join(root, "native", "nested", "written.txt")
	nativeBytesPath := filepath.Join(root, "native", "nested", "written.bin")
	native, status, err := buildAndRunLinuxELF(t, program(nativeTextPath, nativeBytesPath))
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("filesystem results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for path, want := range map[string]string{
		interpreterTextPath:  "written: é\n",
		nativeTextPath:       "written: é\n",
		interpreterBytesPath: "raw\x00bytes",
		nativeBytesPath:      "raw\x00bytes",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("cannot read %q: %v", path, err)
		}
		if string(got) != want {
			t.Errorf("file %q contains %q, want %q", path, got, want)
		}
	}
}

func TestCAOTFilesystemReadDirOrderingMatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	root := t.TempDir()
	for _, name := range []string{"zeta", "éclair", "alpha", "Beta"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := "fn main() -> Nil { println(fs_read_dir(" + strconv.Quote(root) + ")) }\n"
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunLinuxELF(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("directory listings differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
}

func TestCAOTFilesystemNULPathsMatchInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	source := "fn main() -> Nil {\n" +
		"    println(fs_read_text(\"bad\\x00path\"))\n" +
		"    println(fs_read_bytes(\"bad\\x00path\"))\n" +
		"    println(fs_write_text(\"bad\\x00path\", \"text\"))\n" +
		"    println(fs_write_bytes(\"bad\\x00path\", string_to_bytes(\"bytes\")))\n" +
		"}\n"
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunLinuxELF(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("NUL path results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
}

func TestCAOTFilesystemExistsDiagnosticMatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	source := "fn main() -> Nil { println(fs_exists(\"bad\\x00path\")) }\n"
	_, diagnostic := runInterpreterCapture(t, source)
	if diagnostic == nil {
		t.Fatal("interpreter accepted a path containing NUL")
	}
	native, status, err := buildAndRunLinuxELF(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	want := "kryndel: " + diagnostic.Message + "\n"
	if status == 0 || native != want {
		t.Fatalf("filesystem diagnostics differ:\ninterpreter: %q\nC AOT (%d): %q", diagnostic.Message, status, native)
	}
}

func TestCAOTFilesystemInvalidUTF8MatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	inputPath := filepath.Join(t.TempDir(), "invalid-utf8.bin")
	if err := os.WriteFile(inputPath, []byte{0xff, 0xfe, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	source := "fn main() -> Nil {\n" +
		"    println(fs_read_text(" + strconv.Quote(inputPath) + "))\n" +
		"    println(fs_read_bytes(" + strconv.Quote(inputPath) + "))\n" +
		"}\n"
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunLinuxELF(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("invalid UTF-8 results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
}

func TestCAOTFilesystemErrorsMatchInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	missingPath := filepath.Join(t.TempDir(), "missing", "file.txt")
	blockedParent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "fn main() -> Nil {\n" +
		"    println(fs_read_text(" + strconv.Quote(missingPath) + "))\n" +
		"    println(fs_read_bytes(" + strconv.Quote(missingPath) + "))\n" +
		"    println(fs_write_text(" + strconv.Quote(filepath.Join(blockedParent, "child.txt")) + ", \"text\"))\n" +
		"    println(fs_write_bytes(" + strconv.Quote(filepath.Join(blockedParent, "child.bin")) + ", string_to_bytes(\"bytes\")))\n" +
		"}\n"
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunLinuxELF(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("filesystem errors differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
}
