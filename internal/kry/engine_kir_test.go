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
	runtime, diagnostic := NewRuntimeWithArgs(program, checker, engine.Limits, Sandbox{Root: engine.RestrictedRoot, Restricted: engine.RestrictedRoot != ""}, args)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	diagnostic = runtime.run()
	return output.String(), diagnostic
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
				kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH})
				if err != nil {
					t.Fatal(err)
				}
				document, err = DecodeKIR(kirBytes, engine.Limits)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := executeKIRSubset(document, engine.Limits, kirSourceMap(program)); err != nil {
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
