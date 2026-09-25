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
	assertModuleOutput := func(t *testing.T, source string, want string, label string) {
		t.Helper()
		output, d := compile(source)
		if d != nil {
			t.Fatalf("%s failed to compile: %s", label, d.Message)
		}
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
	t.Run("public imported struct can be constructed and accessed", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":     "import \"geometry\"\nfn main() -> Nil { let point: Point = translate(Point{x: 40, y: 1}); println(point.x + point.y); return nil }\n",
			"geometry.kry": "pub struct Point { x: Int, y: Int }\npub fn translate(point: Point) -> Point { return Point{x: point.x + 1, y: point.y} }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		assertModuleOutput(t, mainPath, "42\n", "public imported struct")
	})
	t.Run("public imported enum expressions preserve type identity", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":   "import \"colors\"\nfn main() -> Nil { println(score(Color::Blue)); return nil }\n",
			"colors.kry": "pub enum Color { Red, Blue }\npub fn score(color: Color) -> Int { if color == Color::Blue { return 42 } if color != Color::Red { return 0 } return 1 }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		assertModuleOutput(t, mainPath, "42\n", "public imported enum")
	})
	t.Run("enums reject cross-type comparison and arithmetic", func(t *testing.T) {
		for name, source := range map[string]string{
			"cross-type equality": "enum First { A }\nenum Second { B }\nprintln(First::A == Second::B)\n",
			"arithmetic":          "enum First { A }\nprintln(First::A + First::A)\n",
		} {
			t.Run(name, func(t *testing.T) {
				mainPath := filepath.Join(t.TempDir(), "main.kry")
				if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				_, d := compile(mainPath)
				if d == nil {
					t.Fatal("invalid enum operation compiled")
				}
				if name == "cross-type equality" && !strings.Contains(d.Message, "binary operands must have the same type") {
					t.Fatalf("mixed enum equality diagnostic = %v", d)
				}
				if name == "arithmetic" && !strings.Contains(d.Message, "enum values support only equality") {
					t.Fatalf("enum arithmetic diagnostic = %v", d)
				}
			})
		}
	})
	t.Run("same private enum name in different modules has distinct KIR identities", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":  "import \"left\"\nimport \"right\"\nprintln(42)\n",
			"left.kry":  "enum Status { Ready }\nfn local() -> Status { return Status::Ready }\n",
			"right.kry": "enum Status { Busy }\nfn local() -> Status { return Status::Busy }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		assertModuleOutput(t, mainPath, "42\n", "same-named private module enums")
	})
	t.Run("private imported enum type and variant are rejected", func(t *testing.T) {
		for name, source := range map[string]string{
			"type":    "import \"types\"\nlet mode: Secret = Secret::Hidden\n",
			"variant": "import \"types\"\nprintln(Secret::Hidden)\n",
		} {
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				mainPath := filepath.Join(dir, "main.kry")
				if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte("enum Secret { Hidden }\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				_, d := compile(mainPath)
				if d == nil || !strings.Contains(d.Message, "imported enum 'Secret' is private") {
					t.Fatalf("private enum diagnostic = %v", d)
				}
			})
		}
	})
	t.Run("public declarations cannot expose private enum types", func(t *testing.T) {
		for name, module := range map[string]string{
			"struct field":    "enum Secret { Hidden }\npub struct Public { secret: Secret }\n",
			"function result": "enum Secret { Hidden }\npub fn leak() -> Secret { return Secret::Hidden }\n",
		} {
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				mainPath := filepath.Join(dir, "main.kry")
				if err := os.WriteFile(mainPath, []byte("import \"types\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte(module), 0o600); err != nil {
					t.Fatal(err)
				}
				_, d := compile(mainPath)
				if d == nil || !strings.Contains(d.Message, "private") {
					t.Fatalf("private enum exposure diagnostic = %v", d)
				}
			})
		}
	})
	t.Run("internal enum symbol cannot bypass private import visibility", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte("enum Secret { Hidden }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mainPath, []byte("import \"types\"\nprintln(kry_enum_module_0_Secret::Hidden)\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "unknown enum 'kry_enum_module_0_Secret'") {
			t.Fatalf("internal enum symbol bypass diagnostic = %v; want source identifier rejection", d)
		}
	})
	t.Run("duplicate public imported enum names are ambiguous", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":  "import \"left\"\nimport \"right\"\n",
			"left.kry":  "pub enum State { Ready }\n",
			"right.kry": "pub enum State { Busy }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "duplicate visible type 'State'") {
			t.Fatalf("ambiguous enum diagnostic = %v", d)
		}
	})
	t.Run("public struct and enum imports share one visible type namespace", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":   "import \"record\"\nimport \"state\"\n",
			"record.kry": "pub struct Item { value: Int }\n",
			"state.kry":  "pub enum Item { Empty }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "duplicate visible type 'Item'") {
			t.Fatalf("cross-kind type collision diagnostic = %v", d)
		}
	})
	t.Run("unknown enum variants and payload variants are rejected", func(t *testing.T) {
		for name, declaration := range map[string]string{
			"unknown variant": "enum Color { Red }\nprintln(Color::Blue)\n",
			"payload":         "enum Color { Red(Int) }\n",
			"duplicate":       "enum Color { Red, Red }\n",
			"empty":           "enum Color { }\n",
		} {
			t.Run(name, func(t *testing.T) {
				mainPath := filepath.Join(t.TempDir(), "main.kry")
				if err := os.WriteFile(mainPath, []byte(declaration), 0o600); err != nil {
					t.Fatal(err)
				}
				_, d := compile(mainPath)
				if d == nil {
					t.Fatal("invalid enum source compiled")
				}
			})
		}
	})
	t.Run("private imported struct type is rejected", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte("struct Secret { value: Int }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mainPath, []byte("import \"types\"\nlet value: Secret = Secret{value: 1}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "imported struct 'Secret' is private") {
			t.Fatalf("private type diagnostic = %v", d)
		}
	})
	t.Run("internal struct symbol cannot bypass private import visibility", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte("struct Secret { value: Int }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		source := "import \"types\"\nprintln(kry_type_module_0_Secret{value: 1}.value)\n"
		if err := os.WriteFile(mainPath, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "unknown identifier 'kry_type_module_0_Secret'") {
			t.Fatalf("internal symbol bypass diagnostic = %v; want source identifier rejection", d)
		}
	})
	t.Run("public struct cannot expose a private field type", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(mainPath, []byte("import \"types\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		module := "struct Secret { value: Int }\npub struct Public { private secret: Secret }\n"
		if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte(module), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "public struct 'Public' exposes a private field type") {
			t.Fatalf("private field type diagnostic = %v", d)
		}
	})
	t.Run("public function cannot expose a private return type", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		if err := os.WriteFile(mainPath, []byte("import \"types\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		module := "struct Secret { value: Int }\npub fn open() -> Secret { return Secret{value: 1} }\n"
		if err := os.WriteFile(filepath.Join(dir, "types.kry"), []byte(module), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "public function 'open' exposes a private return type") {
			t.Fatalf("private return type diagnostic = %v", d)
		}
	})
	t.Run("private imported struct field is rejected", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":     "import \"geometry\"\nlet point: Point = new_point()\nprintln(point.secret)\n",
			"geometry.kry": "pub struct Point { private secret: Int, value: Int }\npub fn new_point() -> Point { return Point{secret: 40, value: 2} }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "field 'secret' is private") {
			t.Fatalf("private field diagnostic = %v", d)
		}
	})
	t.Run("same private type name in separate modules has distinct KIR identities", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":  "import \"left\"\nimport \"right\"\nprintln(left_value() + right_value())\n",
			"left.kry":  "struct Node { value: Int }\npub fn left_value() -> Int { let node: Node = Node{value: 40}; return node.value }\n",
			"right.kry": "struct Node { value: Int }\npub fn right_value() -> Int { let node: Node = Node{value: 2}; return node.value }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		assertModuleOutput(t, mainPath, "42\n", "same-named private module structs")
	})
	t.Run("duplicate public imported type names are ambiguous", func(t *testing.T) {
		dir := t.TempDir()
		mainPath := filepath.Join(dir, "main.kry")
		files := map[string]string{
			"main.kry":  "import \"left\"\nimport \"right\"\n",
			"left.kry":  "pub struct Point { x: Int }\n",
			"right.kry": "pub struct Point { y: Int }\n",
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "duplicate visible type 'Point'") {
			t.Fatalf("ambiguous type diagnostic = %v", d)
		}
	})
	t.Run("duplicate type declaration in one module is rejected", func(t *testing.T) {
		mainPath := filepath.Join(t.TempDir(), "main.kry")
		if err := os.WriteFile(mainPath, []byte("struct Point { x: Int }\nstruct Point { y: Int }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, d := compile(mainPath)
		if d == nil || !strings.Contains(d.Message, "duplicate type 'Point'") {
			t.Fatalf("duplicate type diagnostic = %v", d)
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
		for _, name := range []string{"../outside", "/absolute", "other.kry"} {
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
