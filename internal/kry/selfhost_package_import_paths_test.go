package kry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStage37SelfHostedPackageImportPaths(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	program, diagnostic := LoadProgram(compiler, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 180_000
	compile := func(t *testing.T, source string) (string, *Diagnostic) {
		t.Helper()
		output := filepath.Join(t.TempDir(), "package-output")
		r, diagnostic := NewRuntimeWithArgs(program, checker, limits, Sandbox{}, []string{source, output})
		if diagnostic != nil {
			return output, diagnostic
		}
		return output, r.run()
	}
	withWorkingDirectory := func(t *testing.T, directory string) {
		t.Helper()
		previous, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(directory); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chdir(previous); err != nil {
				t.Errorf("restore working directory: %v", err)
			}
		})
	}
	assertOutput := func(t *testing.T, output string, want string, label string) {
		t.Helper()
		artifact, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		assertLinuxAMD64ELF(t, artifact, label)
		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			if err := os.Chmod(output, 0o700); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(output).CombinedOutput()
			if err != nil || string(got) != want {
				t.Fatalf("%s output = %q, error = %v; want %q", label, got, err, want)
			}
		}
	}

	t.Run("module-local package directory takes precedence", func(t *testing.T) {
		project := t.TempDir()
		withWorkingDirectory(t, project)
		files := map[string]string{
			"examples/app/main.kry":    "import \"ui\"\nprintln(ui_answer())\n",
			"examples/app/ui/main.kry": "pub fn ui_answer() -> Int { return 42 }\n",
			"ui/main.kry":              "pub fn ui_answer() -> Int { return 99 }\n",
		}
		for name, contents := range files {
			path := filepath.Join(project, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		output, diagnostic := compile(t, filepath.Join(project, "examples", "app", "main.kry"))
		if diagnostic != nil {
			t.Fatalf("compile module-local package import: %s", diagnostic.Message)
		}
		assertOutput(t, output, "42\n", "module-local package import")
	})

	t.Run("root package fallback keeps nested imports local", func(t *testing.T) {
		project := t.TempDir()
		withWorkingDirectory(t, project)
		files := map[string]string{
			"examples/qt6_widgets.kry":       "import \"packages/qt6\"\nprintln(widget_answer())\n",
			"packages/qt6/main.kry":          "import \"geometry\"\npub fn widget_answer() -> Int { return geometry_answer() }\n",
			"packages/qt6/geometry.kry":      "pub fn geometry_answer() -> Int { return 42 }\n",
			"packages/qt6/geometry/main.kry": "pub fn geometry_answer() -> Int { return 77 }\n",
			"geometry.kry":                   "pub fn geometry_answer() -> Int { return 99 }\n",
		}
		for name, contents := range files {
			path := filepath.Join(project, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		output, diagnostic := compile(t, filepath.Join(project, "examples", "qt6_widgets.kry"))
		if diagnostic != nil {
			t.Fatalf("compile root package fallback: %s", diagnostic.Message)
		}
		assertOutput(t, output, "42\n", "root package fallback with nested local import")
	})

	t.Run("missing import fails closed", func(t *testing.T) {
		project := t.TempDir()
		withWorkingDirectory(t, project)
		source := filepath.Join(project, "examples", "main.kry")
		if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("import \"not_installed\"\nprintln(1)\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, diagnostic := compile(t, source)
		if diagnostic == nil || !strings.Contains(diagnostic.Message, "not_installed/main.kry") {
			t.Fatalf("missing import diagnostic = %v, want a resolver error", diagnostic)
		}
	})
}
