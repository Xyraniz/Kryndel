package kry

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
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

func TestEngineFallsBackForUnsupportedArrayWithCapturedClosure(t *testing.T) {
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
	if _, err := executeKIRSubset(document, DefaultLimits(), kirSourceMap(program)); !errors.Is(err, errKIRSubsetUnsupported) {
		t.Fatalf("captured closure KIR error = %v, want the explicit unsupported-subset sentinel", err)
	}

	path := filepath.Join(t.TempDir(), "closure.kry")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	output, diagnostic := captureEngineRun(t, func() *Diagnostic {
		_, diagnostic := NewEngine().RunPath(path)
		return diagnostic
	})
	if diagnostic != nil || output != "43\n" {
		t.Fatalf("AST fallback result = output %q, diagnostic %#v; want 43 and no diagnostic", output, diagnostic)
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
