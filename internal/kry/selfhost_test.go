package kry

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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
