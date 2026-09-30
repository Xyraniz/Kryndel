package kry

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateASTLimitsRejectsExceededInstructionLimit(t *testing.T) {
	p, _ := testProgram(t, "let value: Int = 1\n")
	limits := DefaultLimits()
	limits.MaxInstructions = 1
	if diagnostic := ValidateASTLimits(p, limits); diagnostic == nil || !strings.Contains(diagnostic.Message, "IR instruction limit exceeded") {
		t.Fatalf("expected instruction limit rejection, got %v", diagnostic)
	}
}

func TestValidateASTLimitsRejectsExceededNestingLimit(t *testing.T) {
	p, _ := testProgram(t, "let value: Int = 1 + 2\n")
	limits := DefaultLimits()
	limits.MaxNesting = 1
	if diagnostic := ValidateASTLimits(p, limits); diagnostic == nil || !strings.Contains(diagnostic.Message, "IR nesting limit exceeded") {
		t.Fatalf("expected nesting limit rejection, got %v", diagnostic)
	}
}

func TestCompileMIRRoundTripsToCanonicalKIR(t *testing.T) {
	program, checker := testProgram(t, "fn main() -> Nil { println(40 + 2) }\n")
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile checked source to MIR: %v", err)
	}
	encoded, err := mir.MarshalKIR()
	if err != nil {
		t.Fatalf("marshal MIR: %v", err)
	}
	decoded, err := DecodeMIR(encoded, DefaultLimits())
	if err != nil {
		t.Fatalf("decode canonical KIR as MIR: %v", err)
	}
	roundTrip, err := decoded.MarshalKIR()
	if err != nil {
		t.Fatalf("marshal decoded MIR: %v", err)
	}
	if !bytes.Equal(encoded, roundTrip) {
		t.Fatal("in-memory and decoded MIR did not preserve canonical KIR bytes")
	}
}

func TestCompileMIRRejectsCheckerForDifferentProgram(t *testing.T) {
	program, _ := testProgram(t, "let value: Int = 1\n")
	_, checker := testProgram(t, "let value: Int = 2\n")
	if _, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "does not describe") {
		t.Fatalf("expected mismatched checker to be rejected, got %v", err)
	}
}

func TestDecodedMIRRunsDirectlyInInterpreterAndPreservesLocations(t *testing.T) {
	program, checker := testProgram(t, "fn main() -> Nil {\n    println(42)\n}\n")
	compiled, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile MIR: %v", err)
	}
	encoded, err := compiled.MarshalKIR()
	if err != nil {
		t.Fatalf("marshal MIR: %v", err)
	}
	decoded, err := DecodeMIR(encoded, DefaultLimits())
	if err != nil {
		t.Fatalf("decode MIR: %v", err)
	}
	if decoded.hasSourceContext || decoded.visibilityScopes != nil {
		t.Fatal("decoded MIR unexpectedly retained non-serialized source context")
	}
	runtime, diagnostic := newRuntimeFromMIR(decoded, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatalf("prepare runtime from decoded MIR: %s", diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run decoded MIR: %s", diagnostic.Message)
	}
	if output.String() != "42\n" {
		t.Fatalf("decoded MIR output = %q, want %q", output.String(), "42\n")
	}

	badProgram, badChecker := testProgram(t, "fn main() -> Nil {\n    let zero: Int = 0\n    println(42 / zero)\n}\n")
	badMIR, err := CompileMIR(badProgram, badChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile diagnostic MIR: %v", err)
	}
	encoded, err = badMIR.MarshalKIR()
	if err != nil {
		t.Fatalf("marshal diagnostic MIR: %v", err)
	}
	decoded, err = DecodeMIR(encoded, DefaultLimits())
	if err != nil {
		t.Fatalf("decode diagnostic MIR: %v", err)
	}
	astRuntime, astDiagnostic := newRuntimeFromProgram(badProgram, badChecker, DefaultLimits(), Sandbox{}, nil)
	if astDiagnostic != nil {
		t.Fatalf("prepare AST diagnostic oracle: %s", astDiagnostic.Message)
	}
	wantDiagnostic := astRuntime.run()
	if wantDiagnostic == nil {
		t.Fatal("AST diagnostic oracle unexpectedly succeeded")
	}
	runtime, diagnostic = newRuntimeFromMIR(decoded, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatalf("prepare diagnostic runtime from decoded MIR: %s", diagnostic.Message)
	}
	diagnostic = runtime.run()
	if diagnostic == nil || diagnostic.Source != badProgram.Source.Name || diagnostic.Line != wantDiagnostic.Line || diagnostic.Column != wantDiagnostic.Column {
		t.Fatalf("decoded MIR diagnostic location = %#v, want %s:%d:%d", diagnostic, badProgram.Source.Name, wantDiagnostic.Line, wantDiagnostic.Column)
	}
}

func TestCompiledMIRDoesNotRetainMutableFrontendTree(t *testing.T) {
	program, checker := testProgram(t, "println(42)\n")
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile MIR: %v", err)
	}
	program.Statements = nil
	program.Functions = nil
	program.Source.Text = "invalid source after MIR compilation"
	program.Source.VisibilityScope = "mutated-scope"

	runtime, diagnostic := newRuntimeFromMIR(mir, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatalf("prepare runtime from immutable MIR: %s", diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run MIR snapshot: %s", diagnostic.Message)
	}
	if got := output.String(); got != "42\n" {
		t.Fatalf("runtime used mutated source AST instead of compiled MIR: output %q", got)
	}
}

func TestCompiledMIRSnapshotsSourceForRuntimeDiagnostics(t *testing.T) {
	source := "let divisor: Int = 0\nprintln(10 / divisor)\n"
	program, checker := testProgram(t, source)
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile MIR: %v", err)
	}
	originalName := program.Source.Name
	program.Source.Text = "mutated source"
	program.Source.VisibilityScope = "mutated-scope"

	runtime, diagnostic := newRuntimeFromMIR(mir, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatalf("prepare runtime from MIR: %s", diagnostic.Message)
	}
	diagnostic = runtime.run()
	if diagnostic == nil || diagnostic.Category != CatRuntime {
		t.Fatalf("expected runtime division diagnostic, got %#v", diagnostic)
	}
	if diagnostic.Source != originalName || diagnostic.Text != source {
		t.Fatalf("diagnostic source context = %q/%q, want %q/%q", diagnostic.Source, diagnostic.Text, originalName, source)
	}
}

func TestValidatedMIRIsOnlyInputToRuntimeAndNativeLowerers(t *testing.T) {
	program, checker := testProgram(t, "fn main() -> Nil { println(42); return nil }\n")
	linuxMIR, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile Linux MIR: %v", err)
	}
	windowsMIR, err := CompileMIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile Windows MIR: %v", err)
	}

	program.Statements = nil
	program.Functions = nil
	program.Source.Text = "mutated after CompileMIR"
	checker.Env = nil

	runtime, diagnostic := newRuntimeFromMIR(linuxMIR, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatalf("prepare interpreter from MIR: %s", diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.run(); diagnostic != nil || output.String() != "42\n" {
		t.Fatalf("MIR interpreter output/diagnostic = %q/%v", output.String(), diagnostic)
	}

	cSource, err := generateCFromValidatedKIR(linuxMIR, linuxMIR.limits, false)
	if err != nil || cSource == "" {
		t.Fatalf("C AOT did not lower the validated MIR: output length=%d error=%v", len(cSource), err)
	}
	elf, err := buildDirectKIRELF(linuxMIR, linuxMIR.limits, linuxMIR.sources)
	if err != nil {
		t.Fatalf("direct ELF did not lower the validated MIR: %v", err)
	}
	if _, err := InspectNative(elf); err != nil {
		t.Fatalf("direct ELF from validated MIR is invalid: %v", err)
	}
	pe, err := lowerDirectPEKIR(windowsMIR, windowsMIR.limits)
	if err != nil {
		t.Fatalf("direct PE did not lower the validated MIR: %v", err)
	}
	if _, err := InspectNative(pe); err != nil {
		t.Fatalf("direct PE from validated MIR is invalid: %v", err)
	}
}

func TestEngineValidatesIRForPortableArtifacts(t *testing.T) {
	p, _ := testProgram(t, "let value: Int = 1\n")
	data, diagnostic := BuildArtifact(p, t.TempDir())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	path := filepath.Join(t.TempDir(), "program.kexe")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	engine.Limits.MaxInstructions = 1
	if _, _, diagnostic := engine.CheckPath(path); diagnostic == nil || !strings.Contains(diagnostic.Message, "IR instruction limit exceeded") {
		t.Fatalf("expected portable artifact to use IR validation, got %v", diagnostic)
	}
}
