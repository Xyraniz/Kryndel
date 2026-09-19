package kry

import (
	"bytes"
	"os"
	"os/exec"
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

func TestStage5SourceFunctionFrontendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_scalar_functions_stage5.kry")
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
	outputPath := filepath.Join(dir, "source-stage5")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage5 source function compiler failed: %s", d.Message)
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
		t.Fatalf("stage5 source function compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
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

func TestStage6ArrayRuntimeParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "array_runtime_stage6.kry")
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
	kirPath := filepath.Join(dir, "arrays.kir")
	outputPath := filepath.Join(dir, "arrays-stage6")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("array runtime backend failed: %s", d.Message)
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
		t.Fatalf("array runtime backend differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
}

func TestStage7OptionResultRuntimeParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "option_result_runtime_stage7.kry")
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
	kirPath := filepath.Join(dir, "option-result.kir")
	outputPath := filepath.Join(dir, "option-result-stage7")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("Option/Result runtime backend failed: %s", d.Message)
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
		t.Fatalf("Option/Result runtime backend differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "option-result-stage7.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted Option/Result ELF failed to execute: %v", err)
		}
		if string(output) != "true\ntrue\n7\n41\ntrue\ntrue\n42\nbad\n99\n" {
			t.Fatalf("unexpected self-hosted Option/Result output %q", output)
		}
	}
}

func TestStage8SourceOptionResultFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_option_result_stage8.kry")
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
	outputPath := filepath.Join(dir, "source-option-result-stage8")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage8 source Option/Result compiler failed: %s", d.Message)
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
		t.Fatalf("stage8 source Option/Result compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-option-result-stage8.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source Option/Result ELF failed to execute: %v", err)
		}
		if string(output) != "true\ntrue\n7\n41\ntrue\ntrue\n42\nbad\n" {
			t.Fatalf("unexpected self-hosted source Option/Result output %q", output)
		}
	}
}

func TestStage9SourceArrayFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "array_runtime_stage6.kry")
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
	outputPath := filepath.Join(dir, "source-array-stage9")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage9 source Array compiler failed: %s", d.Message)
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
		t.Fatalf("stage9 source Array compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-array-stage9.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source Array ELF failed to execute: %v", err)
		}
		if string(output) != "3\n20\n40\n7\n" {
			t.Fatalf("unexpected self-hosted source Array output %q", output)
		}
	}
}

func TestStage10SourceNestedGenericFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_nested_generics_stage10.kry")
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
	outputPath := filepath.Join(dir, "source-nested-generics-stage10")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage10 source nested generic compiler failed: %s", d.Message)
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
		t.Fatalf("stage10 source nested generic compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-nested-generics-stage10.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source nested generic ELF failed to execute: %v", err)
		}
		if string(output) != "true\n8\ntrue\ntrue\n5\n" {
			t.Fatalf("unexpected self-hosted source nested generic output %q", output)
		}
	}
}

func TestStage11SourceLoopControlFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_loop_control_stage11.kry")
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
	outputPath := filepath.Join(dir, "source-loop-control-stage11")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage11 source loop-control compiler failed: %s", d.Message)
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
		t.Fatalf("stage11 source loop-control compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-loop-control-stage11.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source loop-control ELF failed to execute: %v", err)
		}
		if string(output) != "1\n3\n" {
			t.Fatalf("unexpected self-hosted source loop-control output %q", output)
		}
	}
}

func TestStage12SourcePublicFunctionFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_public_function_stage12.kry")
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
	outputPath := filepath.Join(dir, "source-public-function-stage12")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage12 source public-function compiler failed: %s", d.Message)
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
		t.Fatalf("stage12 source public-function compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-public-function-stage12.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source public-function ELF failed to execute: %v", err)
		}
		if string(output) != "5\n" {
			t.Fatalf("unexpected self-hosted source public-function output %q", output)
		}
	}
}

func TestStage13SourceOpaqueABIFunctionParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_opaque_abi_stage13.kry")
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
	outputPath := filepath.Join(dir, "source-opaque-abi-stage13")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage13 source opaque ABI compiler failed: %s", d.Message)
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
		t.Fatalf("stage13 source opaque ABI compiler differs from direct ELF oracle: got=%d want=%d", len(got), len(want))
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-opaque-abi-stage13.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source opaque ABI ELF failed to execute: %v", err)
		}
		if len(output) != 0 {
			t.Fatalf("unexpected self-hosted source opaque ABI output %q", output)
		}
	}
}

func TestStage14DirectStructAndForParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_struct_loop_stage14.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-struct-loop-stage14")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage14 direct ELF failed to execute: %v", err)
	}
	if string(output) != "48\n" {
		t.Fatalf("unexpected stage14 direct ELF output %q", output)
	}
}
