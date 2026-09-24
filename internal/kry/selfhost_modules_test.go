package kry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStage37SelfHostedSourceModules(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	program, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compile := func(source string) (string, *Diagnostic) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "module-output")
		r, d := NewRuntimeWithArgs(program, checker, DefaultLimits(), Sandbox{}, []string{source, output})
		if d != nil {
			return output, d
		}
		return output, r.run()
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "modules_stage37", "main.kry")
	output, d := compile(fixture)
	if d != nil {
		t.Fatalf("multi-file compiler failed: %s", d.Message)
	}
	artifact, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertLinuxAMD64ELF(t, artifact, "stage37 imported module output")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		if err := os.Chmod(output, 0o700); err != nil {
			t.Fatal(err)
		}
		got, err := exec.Command(output).CombinedOutput()
		if err != nil || string(got) != "42\n" {
			t.Fatalf("imported module output = %q, error = %v; want 42", got, err)
		}
	}
	t.Run("import keyword inside a string is ordinary text", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(path, []byte("let phrase: String = \"import\"\nprintln(phrase)\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output, d := compile(path)
		if d != nil {
			t.Fatalf("string containing import was treated as a directive: %s", d.Message)
		}
		if _, err := os.ReadFile(output); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("private function stays in its module", func(t *testing.T) {
		path := filepath.Join(root, "..", "..", "selfhost", "fixtures", "modules_stage37", "private_call.kry")
		// Keep the negative input beside its import without changing fixture files.
		path = filepath.Join(t.TempDir(), "main.kry")
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), "numbers.kry"), []byte("fn secret() -> Int { return 99 }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("import \"numbers\"\nprintln(secret())\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(path)
		if d == nil || !strings.Contains(d.Message, "unknown function 'secret'") {
			t.Fatalf("private function diagnostic = %v", d)
		}
	})
	t.Run("brace characters in strings do not hide later imports", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "main.kry")
		mainSource := "fn marker() -> Int { println(\"{\"); return 1 }\nimport \"numbers\"\nprintln(marker() + answer())\n"
		if err := os.WriteFile(path, []byte(mainSource), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "numbers.kry"), []byte("pub fn answer() -> Int { return 41 }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output, d := compile(path)
		if d != nil {
			t.Fatalf("import after brace inside a string failed: %s", d.Message)
		}
		artifact, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		assertLinuxAMD64ELF(t, artifact, "brace-in-string module output")
		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			if err := os.Chmod(output, 0o700); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(output).CombinedOutput()
			if err != nil || string(got) != "{\n42\n" {
				t.Fatalf("brace-in-string module output = %q, error = %v; want { then 42", got, err)
			}
		}
	})
	t.Run("private helpers can have the same name in separate modules", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":  "import \"left\"\nimport \"right\"\nprintln(left_value() + right_value())\n",
			"left.kry":  "fn secret() -> Int { return 40 }\npub fn left_value() -> Int { return secret() }\n",
			"right.kry": "fn secret() -> Int { return 2 }\npub fn right_value() -> Int { return secret() }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		output, d := compile(path)
		if d != nil {
			t.Fatalf("independent private helpers failed to compile: %s", d.Message)
		}
		artifact, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		assertLinuxAMD64ELF(t, artifact, "independent private module helpers")
		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			if err := os.Chmod(output, 0o700); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(output).CombinedOutput()
			if err != nil || string(got) != "42\n" {
				t.Fatalf("private module helper output = %q, error = %v; want 42", got, err)
			}
		}
	})
	t.Run("transitive exports are not imported implicitly", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry": "import \"api\"\nprintln(hidden())\n",
			"api.kry":  "import \"dep\"\npub fn visible() -> Int { return hidden() }\n",
			"dep.kry":  "pub fn hidden() -> Int { return 42 }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, d := compile(path)
		if d == nil || !strings.Contains(d.Message, "unknown function 'hidden'") {
			t.Fatalf("transitive export diagnostic = %v; want hidden to remain unavailable", d)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.kry")
		if err := os.WriteFile(path, []byte("import \"b\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "b.kry"), []byte("import \"a\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(path)
		if d == nil || !strings.Contains(d.Message, "import cycle") {
			t.Fatalf("cycle diagnostic = %v", d)
		}
	})
	t.Run("unsafe imports", func(t *testing.T) {
		for _, name := range []string{"../outside", "/absolute", "sub/file", "other.kry"} {
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "main.kry")
				if err := os.WriteFile(path, []byte("import \""+name+"\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, d := compile(path)
				if d == nil || !strings.Contains(d.Message, "unsafe module path") {
					t.Fatalf("unsafe import %q diagnostic = %v", name, d)
				}
			})
		}
	})
	t.Run("duplicate function names", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(path, []byte("import \"left\"\nimport \"right\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"left", "right"} {
			if err := os.WriteFile(filepath.Join(dir, name+".kry"), []byte("pub fn collide() -> Int { return 1 }\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, d := compile(path)
		if d == nil || !strings.Contains(d.Message, "duplicate visible function 'collide'") {
			t.Fatalf("duplicate name diagnostic = %v", d)
		}
	})
}
