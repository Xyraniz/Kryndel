package kry

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStage1KryndelBackendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "static_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "input.kir")
	outputPath := filepath.Join(dir, "stage1")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage1 backend failed: %s", d.Message)
	}
	stage1, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stage1, oracle) {
		t.Fatalf("stage1 output differs from direct ELF oracle: stage1=%d oracle=%d", len(stage1), len(oracle))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		if _, err := InspectNative(stage1); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStage1SourceCompilerMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_stage1.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage1")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("source stage1 compiler failed: %s", d.Message)
	}
	stage1, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stage1, oracle) {
		t.Fatalf("source stage1 output differs from direct ELF oracle: stage1=%d oracle=%d", len(stage1), len(oracle))
	}
}

func TestStage2SourceCompilerParsesExpressionsAndEscapes(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_stage2.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage2")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage2 source compiler failed: %s", d.Message)
	}
	stage2, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stage2, oracle) {
		t.Fatalf("source stage2 output differs from direct ELF oracle: stage2=%d oracle=%d", len(stage2), len(oracle))
	}
}

func TestStage2SourceCompilerRejectsUnsupportedSyntax(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_compiler.kry")
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "unsupported.kry")
	outputPath := filepath.Join(dir, "unsupported-output")
	if err := os.WriteFile(inputPath, []byte("while true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{inputPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d == nil {
		t.Fatal("unsupported source syntax was accepted")
	} else if !strings.Contains(d.Message, "unsupported statement") {
		t.Fatalf("unexpected unsupported-syntax diagnostic: %s", d.Message)
	}
}

func TestStage2KryndelDynamicBackendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic.kir")
	outputPath := filepath.Join(dir, "dynamic-stage2")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("dynamic Kryndel backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("dynamic Kryndel backend differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
}

func TestStage3SourceKIRCompilerMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_dynamic_stage3.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage3")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage3 source KIR compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("stage3 source KIR compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
}

func TestStage3SourceKIRCompilerRejectsUnsupportedSyntax(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "unsupported.kry")
	outputPath := filepath.Join(dir, "unsupported-output")
	if err := os.WriteFile(inputPath, []byte("for item in values { println(item) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{inputPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d == nil {
		t.Fatal("unsupported source syntax was accepted")
	} else if !strings.Contains(d.Message, "unsupported statement") {
		t.Fatalf("unexpected unsupported-syntax diagnostic: %s", d.Message)
	}
}

func TestStage4KryndelFunctionBackendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "function_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "functions.kir")
	outputPath := filepath.Join(dir, "functions-stage4")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("function backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("function backend differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
}

func TestStage4ScalarFunctionABIParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "scalar_function_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "scalar-functions.kir")
	outputPath := filepath.Join(dir, "scalar-functions-stage4")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("scalar function backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("scalar function ABI differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
}
