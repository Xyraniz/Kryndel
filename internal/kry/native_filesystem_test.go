package kry

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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

func TestCAOTFilesystemCopyFileMatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	root := t.TempDir()
	program := func(selfPath, sourcePath, nestedPath, linkPath, targetPath string) string {
		return "fn main() -> Nil {\n" +
			"    println(fs_copy_file(" + strconv.Quote(selfPath) + ", " + strconv.Quote(selfPath) + "))\n" +
			"    println(fs_read_text(" + strconv.Quote(selfPath) + "))\n" +
			"    println(fs_copy_file(" + strconv.Quote(sourcePath) + ", " + strconv.Quote(nestedPath) + "))\n" +
			"    println(fs_read_text(" + strconv.Quote(nestedPath) + "))\n" +
			"    println(fs_copy_file(" + strconv.Quote(sourcePath) + ", " + strconv.Quote(linkPath) + "))\n" +
			"    println(fs_read_text(" + strconv.Quote(targetPath) + "))\n" +
			"    println(fs_copy_file(\"bad\\x00source\", \"destination\"))\n" +
			"    println(fs_copy_file(" + strconv.Quote(sourcePath) + ", \"bad\\x00destination\"))\n" +
			"}\n"
	}
	paths := func(name string) [5]string {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		selfPath := filepath.Join(dir, "self.txt")
		sourcePath := filepath.Join(dir, "source.txt")
		nestedPath := filepath.Join(dir, "new", "nested", "copy.txt")
		targetPath := filepath.Join(dir, "target.txt")
		linkPath := filepath.Join(dir, "destination-link")
		for path, content := range map[string]string{selfPath: "self remains intact", sourcePath: "new source content", targetPath: "target remains intact"} {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(targetPath, linkPath); err != nil {
			t.Fatal(err)
		}
		return [5]string{selfPath, sourcePath, nestedPath, linkPath, targetPath}
	}
	interpreterPaths := paths("interpreter")
	interpreted, diagnostic := runInterpreterCapture(t, program(interpreterPaths[0], interpreterPaths[1], interpreterPaths[2], interpreterPaths[3], interpreterPaths[4]))
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	nativePaths := paths("native")
	native, status, err := buildAndRunLinuxELF(t, program(nativePaths[0], nativePaths[1], nativePaths[2], nativePaths[3], nativePaths[4]))
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("fs_copy_file results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for _, path := range []string{interpreterPaths[0], nativePaths[0]} {
		if got, err := os.ReadFile(path); err != nil || string(got) != "self remains intact" {
			t.Errorf("self-copy changed %q: contents=%q err=%v", path, got, err)
		}
	}
	for i, paths := range [][5]string{interpreterPaths, nativePaths} {
		if got, err := os.ReadFile(paths[2]); err != nil || string(got) != "new source content" {
			t.Errorf("nested copy %d is incorrect: contents=%q err=%v", i, got, err)
		}
		if got, err := os.ReadFile(paths[4]); err != nil || string(got) != "target remains intact" {
			t.Errorf("symlink target %d was modified: contents=%q err=%v", i, got, err)
		}
		if info, err := os.Lstat(paths[3]); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("destination symlink %d was replaced: info=%v err=%v", i, info, err)
		}
	}
}

func TestCAOTFilesystemWritesRejectSymlinks(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	root := t.TempDir()
	program := func(textPath, bytesPath, targetPath string) string {
		return "fn main() -> Nil {\n" +
			"    println(fs_write_text(" + strconv.Quote(textPath) + ", \"changed text\"))\n" +
			"    println(fs_write_bytes(" + strconv.Quote(bytesPath) + ", string_to_bytes(\"changed bytes\")))\n" +
			"    println(fs_read_text(" + strconv.Quote(targetPath) + "))\n" +
			"}\n"
	}
	paths := func(name string) [3]string {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		targetPath := filepath.Join(dir, "target.txt")
		textLink := filepath.Join(dir, "text-link")
		bytesLink := filepath.Join(dir, "bytes-link")
		if err := os.WriteFile(targetPath, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, link := range []string{textLink, bytesLink} {
			if err := os.Symlink(targetPath, link); err != nil {
				t.Fatal(err)
			}
		}
		return [3]string{textLink, bytesLink, targetPath}
	}
	interpreterPaths := paths("interpreter")
	interpreted, diagnostic := runInterpreterCapture(t, program(interpreterPaths[0], interpreterPaths[1], interpreterPaths[2]))
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	nativePaths := paths("native")
	native, status, err := buildAndRunLinuxELF(t, program(nativePaths[0], nativePaths[1], nativePaths[2]))
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("filesystem write results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for _, paths := range [][3]string{interpreterPaths, nativePaths} {
		if got, err := os.ReadFile(paths[2]); err != nil || string(got) != "unchanged" {
			t.Errorf("write through a symlink changed its target: contents=%q err=%v", got, err)
		}
		for _, link := range paths[:2] {
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("write replaced symlink %q: info=%v err=%v", link, info, err)
			}
		}
	}
}

func TestCAOTFilesystemMoveFileOverwritesDestination(t *testing.T) {
	if !((runtime.GOOS == "linux" || runtime.GOOS == "windows") && runtime.GOARCH == "amd64") {
		t.Skip("filesystem AOT differential test requires linux/amd64 or windows/amd64")
	}
	root := t.TempDir()
	type movePaths struct {
		source      string
		destination string
	}
	makePaths := func(name string) movePaths {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		paths := movePaths{source: filepath.Join(dir, "source.txt"), destination: filepath.Join(dir, "destination.txt")}
		for path, content := range map[string]string{paths.source: "new source", paths.destination: "old destination"} {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return paths
	}
	program := func(paths movePaths) string {
		return "fn main() -> Nil {\n" +
			"    println(fs_move_file(" + strconv.Quote(paths.source) + ", " + strconv.Quote(paths.destination) + "))\n" +
			"}\n"
	}
	interpreterPaths := makePaths("interpreter")
	interpreted, diagnostic := runInterpreterCapture(t, program(interpreterPaths))
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	nativePaths := makePaths("native")
	native, status, err := buildAndRunNativeAOT(t, program(nativePaths))
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("fs_move_file results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for _, paths := range []movePaths{interpreterPaths, nativePaths} {
		if _, err := os.Stat(paths.source); !os.IsNotExist(err) {
			t.Errorf("source file %q remains after move: %v", paths.source, err)
		}
		if content, err := os.ReadFile(paths.destination); err != nil || string(content) != "new source" {
			t.Errorf("destination file %q contains %q after move: %v", paths.destination, content, err)
		}
	}
}

func TestCAOTFilesystemPathHelpersMatchInterpreter(t *testing.T) {
	if !((runtime.GOOS == "linux" || runtime.GOOS == "windows") && runtime.GOARCH == "amd64") {
		t.Skip("filesystem AOT differential test requires linux/amd64 or windows/amd64")
	}
	root := t.TempDir()
	link := filepath.Join(root, "target-link")
	if runtime.GOOS == "linux" {
		target := filepath.Join(root, "target.txt")
		if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	joined := filepath.Join(root, "file", "leaf")
	absolute := filepath.Join(root, "normalized")
	source := "fn main() -> Nil {\n" +
		"    println(fs_join_path(" + strconv.Quote(root) + ", [\"nested//\", \"..\", \"file\", \".\", \"leaf\"]))\n"
	want := joined + "\n"
	if runtime.GOOS == "linux" {
		source += "    println(fs_absolute_path(" + strconv.Quote(link) + "))\n"
		want += "ok(" + link + ")\n"
	}
	source += "    println(fs_absolute_path(" + strconv.Quote(root+"/missing/../normalized") + "))\n}\n"
	want += "ok(" + absolute + ")\n"
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	if interpreted != want {
		t.Fatalf("unexpected interpreter paths: got %q, want %q", interpreted, want)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("filesystem path helpers differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
}

func TestCAOTFilesystemTempFileMatchesInterpreter(t *testing.T) {
	if !((runtime.GOOS == "linux" || runtime.GOOS == "windows") && runtime.GOARCH == "amd64") {
		t.Skip("filesystem AOT differential test requires linux/amd64 or windows/amd64")
	}
	root := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("TMP", root)
		t.Setenv("TEMP", root)
	} else {
		t.Setenv("TMPDIR", root)
	}
	tempDir := os.TempDir()
	source := "fn main() -> Nil {\n" +
		"    println(fs_temp_dir())\n" +
		"    println(fs_temp_file(\"kryndel-native\"))\n" +
		"}\n"
	parseOutput := func(output string) string {
		t.Helper()
		lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
		if len(lines) != 2 || lines[0] != tempDir || !strings.HasPrefix(lines[1], "ok(") || !strings.HasSuffix(lines[1], ")") {
			t.Fatalf("unexpected temporary file output: %q", output)
		}
		return strings.TrimSuffix(strings.TrimPrefix(lines[1], "ok("), ")")
	}
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	interpreterPath := parseOutput(interpreted)
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 {
		t.Fatalf("C AOT exited with status %d: %q", status, native)
	}
	nativePath := parseOutput(native)
	for _, path := range []string{interpreterPath, nativePath} {
		if filepath.Dir(path) != tempDir || !strings.HasPrefix(filepath.Base(path), "kryndel-native-") {
			t.Errorf("temporary file path %q is outside the requested directory or prefix", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("temporary file %q was not created: %v", path, err)
			continue
		}
		if info.Size() != 0 || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Errorf("temporary file %q has size %d and mode %o, want empty mode 600", path, info.Size(), info.Mode().Perm())
		}
	}
}

func TestCAOTFilesystemRemoveDirAllMatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT filesystem differential test requires linux/amd64")
	}
	root := t.TempDir()
	targets := [2]string{filepath.Join(root, "interpreter-target"), filepath.Join(root, "native-target")}
	links := [2]string{filepath.Join(root, "interpreter-link"), filepath.Join(root, "native-link")}
	files := [2]string{filepath.Join(root, "interpreter-file"), filepath.Join(root, "native-file")}
	missing := filepath.Join(root, "missing")
	blocker := filepath.Join(root, "regular-file")
	errorPath := filepath.Join(blocker, "child")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range targets {
		if err := os.Mkdir(targets[i], 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(targets[i], "keep.txt"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targets[i], links[i]); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(files[i], []byte("remove"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	program := func(file, link string) string {
		return "fn main() -> Nil {\n" +
			"    println(fs_remove_dir_all(" + strconv.Quote(missing) + "))\n" +
			"    println(fs_remove_dir_all(" + strconv.Quote(file) + "))\n" +
			"    println(fs_remove_dir_all(" + strconv.Quote(link) + "))\n" +
			"    println(fs_remove_dir_all(" + strconv.Quote(errorPath) + "))\n" +
			"    println(fs_remove_dir_all(\"bad\\x00path\"))\n" +
			"}\n"
	}
	interpreted, diagnostic := runInterpreterCapture(t, program(files[0], links[0]))
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunLinuxELF(t, program(files[1], links[1]))
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("fs_remove_dir_all differs:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for _, path := range []string{files[0], files[1], links[0], links[1]} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("%q still exists after fs_remove_dir_all: %v", path, err)
		}
	}
	for _, target := range targets {
		if got, err := os.ReadFile(filepath.Join(target, "keep.txt")); err != nil || string(got) != "keep" {
			t.Errorf("symlink target %q was modified: contents=%q err=%v", target, got, err)
		}
	}
}

func TestCAOTFilesystemNULPathsMatchInterpreter(t *testing.T) {
	if !((runtime.GOOS == "linux" || runtime.GOOS == "windows") && runtime.GOARCH == "amd64") {
		t.Skip("filesystem AOT differential test requires linux/amd64 or windows/amd64")
	}
	root := t.TempDir()
	type paths struct {
		directory string
		protected string
		source    string
	}
	makePaths := func(name string) paths {
		t.Helper()
		directory := filepath.Join(root, name)
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		protected := filepath.Join(directory, "protected.txt")
		source := filepath.Join(directory, "source.txt")
		for path, content := range map[string]string{protected: "protected", source: "source"} {
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return paths{directory: directory, protected: protected, source: source}
	}
	program := func(p paths) string {
		badFile := strconv.Quote(p.protected + "\x00suffix")
		badDir := strconv.Quote(p.directory + "\x00suffix")
		return "fn main() -> Nil {\n" +
			"    println(fs_read_text(" + badFile + "))\n" +
			"    println(fs_read_bytes(" + badFile + "))\n" +
			"    println(fs_write_text(" + badFile + ", \"text\"))\n" +
			"    println(fs_write_bytes(" + badFile + ", string_to_bytes(\"bytes\")))\n" +
			"    println(fs_read_dir(" + badDir + "))\n" +
			"    println(fs_create_dir(" + badDir + "))\n" +
			"    println(fs_create_dir_all(" + badDir + "))\n" +
			"    println(fs_remove_file(" + badFile + "))\n" +
			"    println(fs_remove_dir_all(" + badDir + "))\n" +
			"    println(fs_copy_file(" + strconv.Quote(p.source) + ", " + badFile + "))\n" +
			"    println(fs_move_file(" + strconv.Quote(p.source) + ", " + badFile + "))\n" +
			"    println(fs_is_file(" + badFile + "))\n" +
			"    println(fs_is_dir(" + badDir + "))\n" +
			"    println(fs_file_size(" + badFile + "))\n" +
			"    println(fs_file_modified_time(" + badFile + "))\n" +
			"}\n"
	}
	interpreterPaths := makePaths("interpreter")
	interpreterSource := program(interpreterPaths)
	interpreted, diagnostic := runInterpreterCapture(t, interpreterSource)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	nativePaths := makePaths("native")
	nativeSource := program(nativePaths)
	native, status, err := buildAndRunNativeAOT(t, nativeSource)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("NUL path results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for _, paths := range []paths{interpreterPaths, nativePaths} {
		for path, expected := range map[string]string{paths.protected: "protected", paths.source: "source"} {
			if content, err := os.ReadFile(path); err != nil || string(content) != expected {
				t.Errorf("NUL path operation changed %q: contents=%q err=%v", path, content, err)
			}
		}
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
