package kry

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func captureEngineRun(t *testing.T, run func() *Diagnostic) (string, *Diagnostic) {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = previous
		_ = writer.Close()
		_ = reader.Close()
	}()
	diagnostic := run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = previous
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output), diagnostic
}

func runASTPath(t *testing.T, engine *Engine, path string, args []string) (string, *Diagnostic) {
	t.Helper()
	program, checker, diagnostic := engine.CheckPath(path)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime, diagnostic := newASTOracleRuntime(program, checker, engine.Limits, Sandbox{Root: engine.RestrictedRoot, Restricted: engine.RestrictedRoot != ""}, args)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	diagnostic = runtime.run()
	return output.String(), diagnostic
}

func TestInterpreterLowersOnlyFromValidatedMIR(t *testing.T) {
	engine := NewEngine()
	for _, relative := range []string{
		"../../examples/control_flow.kry",
		"../../examples/fibonacci.kry",
		"../../examples/typed_data.kry",
		"../../examples/collections.kry",
		"../../examples/module_demo.kry",
		"../../examples/runtime_polymorphism.kry",
	} {
		t.Run(filepath.Base(relative), func(t *testing.T) {
			path := filepath.Join("..", "..", "examples", filepath.Base(relative))
			astOutput, astDiagnostic := runASTPath(t, engine, path, []string{"argument"})
			mirOutput, mirDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPathWithArgs(path, []string{"argument"})
				return diagnostic
			})
			if !reflect.DeepEqual(mirDiagnostic, astDiagnostic) || mirOutput != astOutput {
				t.Fatalf("MIR interpreter = output %q, diagnostic %#v; AST oracle = output %q, diagnostic %#v", mirOutput, mirDiagnostic, astOutput, astDiagnostic)
			}
		})
	}
}

func runEngineKIRDocument(t *testing.T, engine *Engine, path string) kirExecResult {
	t.Helper()
	program, checker, document, diagnostic := engine.checkPathWithKIR(path)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if document == nil {
		var err error
		document, err = CompileMIR(program, checker, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH})
		if err != nil {
			t.Fatal(err)
		}
	}
	sandbox := Sandbox{Root: engine.RestrictedRoot, Restricted: engine.RestrictedRoot != ""}
	result, err := executeValidatedMIR(document, engine.Limits, kirSourceMap(program), sandbox)
	if err != nil {
		t.Fatalf("Engine KIR document did not execute directly: %v", err)
	}
	return result
}

func TestEngineRunsKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "main.kry")
	artifactPath := filepath.Join(directory, "main.kexe")
	source := `fn main() -> Nil {
    println("hello from KIR")
    return nil
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, diagnostic := DecodeArtifact(artifactBytes, engine.Limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	document, err := DecodeKIR(artifact.KIR, engine.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Target != "portable/any" || document.Target.OS != "portable" || document.Target.Arch != "any" || document.Target.GUI {
		t.Fatalf("portable artifact target = %q and KIR target %#v", artifact.Target, document.Target)
	}

	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			wantOutput, wantDiagnostic := runASTPath(t, engine, path, []string{"arg"})
			gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPathWithArgs(path, []string{"arg"})
				return diagnostic
			})
			if !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) || gotOutput != wantOutput {
				t.Fatalf("Engine result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
			}
			if gotDiagnostic != nil || gotOutput != "hello from KIR\n" {
				t.Fatalf("unexpected KIR run result: output %q, diagnostic %#v", gotOutput, gotDiagnostic)
			}
		})
	}
}

func TestControlFlowExampleRunsThroughCheckedKIR(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "control_flow.kry")
	engine := NewEngine()
	output, diagnostic := captureEngineRun(t, func() *Diagnostic {
		_, diagnostic := engine.RunPath(path)
		return diagnostic
	})
	if diagnostic != nil {
		t.Fatalf("run control_flow.kry: %s", diagnostic.Format(false))
	}
	if output != "12\n[1, 2, 3]\n" {
		t.Fatalf("control_flow.kry output = %q, want %q", output, "12\n[1, 2, 3]\n")
	}
}

func TestEngineRunsUnsignedAndConcatenationKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "unsigned.kry")
	artifactPath := filepath.Join(directory, "unsigned.kexe")
	source := `fn main() -> Nil {
    println(u8(255) + u8(1))
    println(~u16(0))
    println(u16(1) << 15)
    println(u64(1) << 63)
    println(u64(0) - u64(1))
    let values: Array[UInt8] = [u8(1), u8(2)] + [u8(3)]
    println(values)
    let text: String = bytes_to_string(string_to_bytes("a") + string_to_bytes("b"))
    println(text)
    return nil
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			program, checker, document, diagnostic := engine.checkPathWithKIR(path)
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			if document == nil {
				var err error
				document, err = CompileMIR(program, checker, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH})
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := executeValidatedMIR(document, engine.Limits, kirSourceMap(program)); err != nil {
				t.Fatalf("source/artifact KIR fell outside the executable subset: %v", err)
			}
			wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
			gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) || gotOutput != wantOutput {
				t.Fatalf("Engine result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
			}
			if gotDiagnostic != nil {
				t.Fatalf("unexpected KIR run diagnostic: %#v", gotDiagnostic)
			}
			if want := "0\n65535\n32768\n9223372036854775808\n18446744073709551615\n[1, 2, 3]\nab\n"; gotOutput != want {
				t.Fatalf("Engine output = %q, want %q", gotOutput, want)
			}
		})
	}
}

func TestEngineKIRPreservesPartialOutputAndMainDiagnosticStack(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "failure.kry")
	artifactPath := filepath.Join(directory, "failure.kexe")
	source := `fn main() -> Nil {
    print("before:")
    let zero: Int = 0
    println(5 / zero)
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}

	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
			gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if gotOutput != "before:" || wantOutput != gotOutput {
				t.Fatalf("partial output = %q; AST output = %q", gotOutput, wantOutput)
			}
			if gotDiagnostic == nil || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("diagnostic = %#v; AST diagnostic = %#v", gotDiagnostic, wantDiagnostic)
			}
			if gotDiagnostic.Message != "division by zero" || len(gotDiagnostic.Stack) != 1 || gotDiagnostic.Stack[0].Function != "main" {
				t.Fatalf("KIR runtime diagnostic lost its source stack: %#v", gotDiagnostic)
			}
		})
	}
}

func TestEngineRunsArrayWithCapturedClosureKIRForSourceAndKexe(t *testing.T) {
	source := `fn main() -> Nil {
    let mut base: Int = 40
    let add = fn(value: Int) -> Int {
        base = base + value
        return base
    }
    let values: Array[Int] = [3]
    println(add(values[0]))
}
`
	program, checker := testProgram(t, source)
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	kirResult, err := executeKIRSubset(document, DefaultLimits(), kirSourceMap(program))
	if err != nil {
		t.Fatalf("array/captured-closure program did not execute directly from KIR: %v", err)
	}
	if string(kirResult.Output) != "43\n" || kirResult.Diagnostic != nil {
		t.Fatalf("direct KIR result = output %q, diagnostic %#v; want 43 and no diagnostic", kirResult.Output, kirResult.Diagnostic)
	}

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "closure.kry")
	artifactPath := filepath.Join(directory, "closure.kexe")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
			gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if gotOutput != wantOutput || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("Engine KIR result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
			}
			if gotOutput != "43\n" || gotDiagnostic != nil {
				t.Fatalf("unexpected KIR result: output %q, diagnostic %#v", gotOutput, gotDiagnostic)
			}
		})
	}
}

func TestEngineRunsImportedFunctionsFromKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "lib"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "lib", "math.kry"), []byte(`pub fn twice(value: Int) -> Int { return value * 2 }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(directory, "main.kry")
	artifactPath := filepath.Join(directory, "main.kexe")
	source := `import "lib/math"
fn choose(value: Int) -> Int { return value }
fn choose(value: String) -> Int { return 2 }
println(twice(choose(21)))
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
			gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if gotOutput != wantOutput || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("Engine imported result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
			}
			if gotOutput != "42\n" || gotDiagnostic != nil {
				t.Fatalf("unexpected imported KIR result: output %q, diagnostic %#v", gotOutput, gotDiagnostic)
			}
		})
	}
}

func TestEngineRunsMutableCaptureKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "mutable-closure.kry")
	artifactPath := filepath.Join(directory, "mutable-closure.kexe")
	source := `fn main() -> Nil {
    let mut count: Int = 0
    let zero: Int = 0
    let bump = fn() -> Int {
        count = count + 1
        println(count)
        if count == 1 { return count }
        return 10 / zero
    }
    println(bump())
    println(bump())
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}

	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
			gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if gotOutput != wantOutput || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("mutable capture result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
			}
			if gotOutput != "1\n1\n2\n" || gotDiagnostic == nil || gotDiagnostic.Message != "division by zero" {
				t.Fatalf("unexpected mutable capture result: output %q, diagnostic %#v", gotOutput, gotDiagnostic)
			}
		})
	}
}

func TestEngineDoesNotFallbackAfterKIRRuntimeDiagnostic(t *testing.T) {
	source := `print("before")
let zero: Int = 0
println(5 / zero)
`
	path := filepath.Join(t.TempDir(), "runtime-error.kry")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	output, diagnostic := captureEngineRun(t, func() *Diagnostic {
		_, diagnostic := NewEngine().RunPath(path)
		return diagnostic
	})
	if diagnostic == nil || diagnostic.Message != "division by zero" || output != "before" {
		t.Fatalf("KIR runtime failure = output %q, diagnostic %#v; expected one partial write and division diagnostic", output, diagnostic)
	}
	if strings.Count(output, "before") != 1 {
		t.Fatalf("runtime diagnostic caused an AST retry: duplicate output %q", output)
	}
}

func TestEngineRunsGenericKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	successSource := filepath.Join(directory, "generic-success.kry")
	successArtifact := filepath.Join(directory, "generic-success.kexe")
	errorSource := filepath.Join(directory, "generic-error.kry")
	errorArtifact := filepath.Join(directory, "generic-error.kexe")
	success := `struct Box[T: Copy] { value: T }
impl Box[T] {
    fn get() -> T { return self.value }
    fn map[U: Copy](callback: fn(T) -> U) -> Box[U] {
        return Box[U]{value: callback(self.value)}
    }
}

fn make_box[T: Copy](value: T) -> Box[T] { return Box[T]{value: value} }
fn value_or[T: Copy](value: Option[T], fallback: T) -> T {
    match value {
        some(item) => { return item }
        none => { return fallback }
    }
}

fn main() -> Nil {
    let boxed: Box[Int] = make_box(13)
    println(boxed.get())
    let mapped: Box[String] = boxed.map(fn(value: Int) -> String { return "value=" + str(value) })
    println(mapped.value)
    println(value_or(some(17), 0))
    return nil
}
`
	failure := `fn identity[T: Copy](value: T) -> T { return value }
fn force_generic_error[T: Copy](value: T) -> Int {
    let zero: Int = 0
    return 9 / zero
}
fn main() -> Nil {
    print("before:")
    println(force_generic_error(2))
    return nil
}
`
	for path, source := range map[string]string{successSource: success, errorSource: failure} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	engine := NewEngine()
	for source, artifact := range map[string]string{successSource: successArtifact, errorSource: errorArtifact} {
		if diagnostic := engine.BuildPath(source, artifact); diagnostic != nil {
			t.Fatalf("build %s: %s", source, diagnostic.Message)
		}
	}
	for _, scenario := range []struct {
		name, source, artifact, wantOutput, wantMessage string
	}{
		{name: "generic struct and methods", source: successSource, artifact: successArtifact, wantOutput: "13\nvalue=13\n17\n"},
		{name: "generic runtime error", source: errorSource, artifact: errorArtifact, wantOutput: "before:", wantMessage: "division by zero"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, path := range []string{scenario.source, scenario.artifact} {
				t.Run(filepath.Ext(path), func(t *testing.T) {
					wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
					gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
						_, diagnostic := engine.RunPath(path)
						return diagnostic
					})
					if gotOutput != wantOutput || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
						t.Fatalf("Engine KIR result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
					}
					if gotOutput != scenario.wantOutput {
						t.Fatalf("output = %q, want %q", gotOutput, scenario.wantOutput)
					}
					if scenario.wantMessage == "" && gotDiagnostic != nil {
						t.Fatalf("diagnostic = %#v, want none", gotDiagnostic)
					}
					if scenario.wantMessage != "" && (gotDiagnostic == nil || gotDiagnostic.Message != scenario.wantMessage) {
						t.Fatalf("diagnostic = %#v, want %q", gotDiagnostic, scenario.wantMessage)
					}
				})
			}
		})
	}
}

func TestEngineRunsStaticTraitKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "two concrete receivers", source: traitDispatchFixture, want: "north:one\nsouth:two\nnorth:one\nsouth:two\n"},
		{name: "generic struct receivers", source: traitGenericInstantiationFixture, want: "int:7\nstring:ok\n"},
	}
	engine := NewEngine()
	for index, test := range tests {
		sourcePath := filepath.Join(directory, fmt.Sprintf("trait-%d.kry", index))
		artifactPath := filepath.Join(directory, fmt.Sprintf("trait-%d.kexe", index))
		if err := os.WriteFile(sourcePath, []byte(test.source), 0o600); err != nil {
			t.Fatal(err)
		}
		if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
			t.Fatalf("build %s: %s", test.name, diagnostic.Message)
		}
		t.Run(test.name, func(t *testing.T) {
			for _, path := range []string{sourcePath, artifactPath} {
				t.Run(filepath.Ext(path), func(t *testing.T) {
					wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
					gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
						_, diagnostic := engine.RunPath(path)
						return diagnostic
					})
					if gotOutput != wantOutput || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
						t.Fatalf("Engine trait KIR result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
					}
					if gotOutput != test.want || gotDiagnostic != nil {
						t.Fatalf("unexpected trait KIR result: output %q, diagnostic %#v; want %q", gotOutput, gotDiagnostic, test.want)
					}
				})
			}
		})
	}
}

func TestEngineRunsFilesystemKIRInsideRestrictedRootForSourceAndKexe(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "sandbox")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "filesystem.kry")
	artifactPath := filepath.Join(root, "filesystem.kexe")
	source := `fn main() -> Nil {
    let written: Result[Nil, String] = fs_write_text("inside.txt", "confined")
    println(str(written))
    println(str(fs_read_text("inside.txt")))
    println(str(fs_write_text("../escape.txt", "blocked")))
    println(fs_exists("inside.txt"))
    return nil
}
main()
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	engine.RestrictedRoot = root
	wantOutput := "ok(nil)\nok(confined)\nerr(path denied by sandbox)\ntrue\n"
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			result := runEngineKIRDocument(t, engine, path)
			if result.Diagnostic != nil || string(result.Output) != wantOutput {
				t.Fatalf("direct KIR filesystem result = output %q, diagnostic %#v", result.Output, result.Diagnostic)
			}
			if err := os.Remove(filepath.Join(root, "inside.txt")); err != nil {
				t.Fatal(err)
			}

			want, wantDiagnostic := runASTPath(t, engine, path, nil)
			if want != wantOutput || wantDiagnostic != nil {
				t.Fatalf("restricted interpreter result = output %q, diagnostic %#v", want, wantDiagnostic)
			}
			if err := os.Remove(filepath.Join(root, "inside.txt")); err != nil {
				t.Fatal(err)
			}
			got, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if got != want || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("Engine result = output %q, diagnostic %#v; interpreter result = output %q, diagnostic %#v", got, gotDiagnostic, want, wantDiagnostic)
			}
			if _, err := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(err) {
				t.Fatalf("restricted KIR created a file outside its root: %v", err)
			}
		})
	}
}

func TestEngineRunsSQLiteKIRForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "sqlite.kry")
	artifactPath := filepath.Join(directory, "sqlite.kexe")
	source := `fn main() -> Nil {
    let opened: Result[SQLite, String] = sqlite_open(":memory:")
    match opened {
        ok(database) => {
            let created: Result[Int, String] = sqlite_exec(database, "CREATE TABLE users (id INTEGER, name TEXT)")
            let inserted: Result[Int, String] = sqlite_exec(database, "INSERT INTO users VALUES (1, 'Ada'), (2, 'Grace')")
            println(is_ok(created))
            println(is_ok(inserted))
            println(str(sqlite_query(database, "SELECT id, name FROM users ORDER BY id")))
            sqlite_close(database)
        }
        err(problem) => { println(problem) }
    }
    return nil
}
main()
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	wantOutput := "true\ntrue\nok([[1, Ada], [2, Grace]])\n"
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			result := runEngineKIRDocument(t, engine, path)
			if result.Diagnostic != nil || string(result.Output) != wantOutput {
				t.Fatalf("direct KIR SQLite result = output %q, diagnostic %#v", result.Output, result.Diagnostic)
			}
			want, wantDiagnostic := runASTPath(t, engine, path, nil)
			got, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if got != want || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) || got != wantOutput || gotDiagnostic != nil {
				t.Fatalf("Engine SQLite result = output %q, diagnostic %#v; interpreter result = output %q, diagnostic %#v", got, gotDiagnostic, want, wantDiagnostic)
			}
		})
	}
}

func TestEngineClosesLeakedSQLiteKIRHandlesForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "leaked.db")
	sourcePath := filepath.Join(directory, "leaked.kry")
	artifactPath := filepath.Join(directory, "leaked.kexe")
	source := fmt.Sprintf(`fn main() -> Nil {
    match sqlite_open(%q) {
        ok(database) => {
            match sqlite_exec(database, "BEGIN EXCLUSIVE") {
                ok(_) => { println("transaction started") }
                err(problem) => { println(problem) }
            }
        }
        err(problem) => { println(problem) }
    }
    return nil
}
main()
`, databasePath)
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	wantOutput := "transaction started\n"
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			result := runEngineKIRDocument(t, engine, path)
			if result.Diagnostic == nil || result.Diagnostic.Category != CatResource || !strings.Contains(result.Diagnostic.Message, "SQLite") || !strings.Contains(result.Diagnostic.Message, "not closed") || string(result.Output) != wantOutput {
				t.Fatalf("direct KIR leak result = output %q, diagnostic %#v", result.Output, result.Diagnostic)
			}
			if err := os.Remove(databasePath); err != nil {
				t.Fatalf("direct KIR cleanup did not close the SQLite file: %v", err)
			}

			want, wantDiagnostic := runASTPath(t, engine, path, nil)
			if want != wantOutput || wantDiagnostic == nil || wantDiagnostic.Category != CatResource || !strings.Contains(wantDiagnostic.Message, "SQLite") {
				t.Fatalf("interpreter leak result = output %q, diagnostic %#v", want, wantDiagnostic)
			}
			if err := os.Remove(databasePath); err != nil {
				t.Fatalf("interpreter cleanup did not close the SQLite file: %v", err)
			}
			got, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if got != want || !sameRuntimeDiagnostic(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("Engine leak result = output %q, diagnostic %#v; interpreter result = output %q, diagnostic %#v", got, gotDiagnostic, want, wantDiagnostic)
			}
			if err := os.Remove(databasePath); err != nil {
				t.Fatalf("Engine cleanup did not close the SQLite file: %v", err)
			}
		})
	}
}

func TestEngineRestrictedSQLiteKIRDeniesBeforeEvaluatingArgumentsForSourceAndKexe(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "restricted.kry")
	artifactPath := filepath.Join(directory, "restricted.kexe")
	source := `fn mark_argument() -> String {
    let marker: Result[Nil, String] = fs_write_text("argument-evaluated", "yes")
    return "database.sqlite"
}
fn main() -> Nil {
    let opened: Result[SQLite, String] = sqlite_open(mark_argument())
    return nil
}
main()
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	engine.RestrictedRoot = directory
	for _, path := range []string{sourcePath, artifactPath} {
		t.Run(filepath.Ext(path), func(t *testing.T) {
			result := runEngineKIRDocument(t, engine, path)
			if result.Diagnostic == nil || result.Diagnostic.Category != CatRuntime || !strings.Contains(result.Diagnostic.Message, `sqlite_open" is unavailable with --restricted`) {
				t.Fatalf("direct restricted KIR diagnostic = %#v", result.Diagnostic)
			}
			want, wantDiagnostic := runASTPath(t, engine, path, nil)
			got, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
				_, diagnostic := engine.RunPath(path)
				return diagnostic
			})
			if got != want || !sameRuntimeDiagnostic(gotDiagnostic, wantDiagnostic) {
				t.Fatalf("restricted Engine result = output %q, diagnostic %#v; interpreter result = output %q, diagnostic %#v", got, gotDiagnostic, want, wantDiagnostic)
			}
			if _, err := os.Stat(filepath.Join(directory, "argument-evaluated")); !os.IsNotExist(err) {
				t.Fatalf("restricted SQLite evaluated a denied argument: %v", err)
			}
		})
	}
}
