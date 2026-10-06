package kry

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCompileMIRPreservesUTF8ByteSpansAndStableBindingIDs(t *testing.T) {
	source := "import \"lib/math\"\nenum Sample { First, Second }\nfn main() -> Int {\n    let mut word: String = \"ñ\"\n    word = word + \"!\"\n    return len(word)\n}\n"
	program, checker := testProgram(t, source)
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile checked source: %v", err)
	}
	document, err := mir.arena.toKIRDocument()
	if err != nil {
		t.Fatalf("read typed arena for contract check: %v", err)
	}
	if len(document.Functions) != 1 || document.Functions[0].Span == nil {
		t.Fatalf("function span was not retained: %#v", document.Functions)
	}
	assertSpan := func(label string, span *KIRSourceSpan) {
		t.Helper()
		if span == nil || span.Start < 0 || span.End < span.Start || span.End > len(source) {
			t.Fatalf("%s has an invalid byte range: %#v", label, span)
		}
	}
	if len(document.ImportRecords) != 1 || document.ImportRecords[0].Span == nil || document.ImportRecords[0].Path != "lib/math" {
		t.Fatalf("import source metadata was not retained in the typed arena: %#v", document.ImportRecords)
	}
	if len(document.Enums) != 1 || document.Enums[0].Span == nil || len(document.Enums[0].VariantSpans) != 2 || document.Enums[0].VariantSpans[0] == nil {
		t.Fatalf("enum declaration and variant spans were not retained: %#v", document.Enums)
	}
	assertSpan("enum declaration", document.Enums[0].Span)
	assertSpan("enum variant", document.Enums[0].VariantSpans[0])
	if got := source[document.Enums[0].VariantSpans[0].Start:document.Enums[0].VariantSpans[0].End]; got != "First" {
		t.Fatalf("enum variant span = %q, want First", got)
	}
	functionSpan := document.Functions[0].Span
	assertSpan("function", functionSpan)
	if got := source[functionSpan.Start:functionSpan.End]; !strings.HasPrefix(got, "fn main()") || !strings.HasSuffix(got, "}") {
		t.Fatalf("function span does not cover its full declaration: %q", got)
	}
	if len(document.Functions[0].Body) != 3 {
		t.Fatalf("unexpected function body size: %d", len(document.Functions[0].Body))
	}
	literal := document.Functions[0].Body[0].Init
	if literal == nil || literal.Span == nil {
		t.Fatal("string literal span was not retained")
	}
	assertSpan("string literal", literal.Span)
	if got := source[literal.Span.Start:literal.Span.End]; got != "\"ñ\"" {
		t.Fatalf("UTF-8 literal span = %q, want quoted literal with byte offsets", got)
	}
	if literal.Span.End-literal.Span.Start != len("\"ñ\"") {
		t.Fatalf("UTF-8 span length = %d, want %d source bytes", literal.Span.End-literal.Span.Start, len("\"ñ\""))
	}
	binary := document.Functions[0].Body[1].Value
	if binary == nil || binary.Span == nil {
		t.Fatal("binary expression span was not retained")
	}
	assertSpan("binary expression", binary.Span)
	if got := source[binary.Span.Start:binary.Span.End]; got != "word + \"!\"" {
		t.Fatalf("binary expression span = %q, want its complete source text", got)
	}

	identities := map[string]string{}
	for index, binding := range mir.arena.Bindings {
		if binding.ID == "" || binding.Span == nil {
			t.Fatalf("arena binding %d is missing stable identity or span: %#v", index, binding)
		}
		decoded, err := hex.DecodeString(binding.ID)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("arena binding %d has a malformed SHA-256 identity %q", index, binding.ID)
		}
		if binding.Span.End-binding.Span.Start != len(binding.Name) {
			t.Fatalf("binding %q has a span inconsistent with its source token: %#v", binding.Name, binding.Span)
		}
		identity := kirBindingIdentity(&binding)
		if prior, found := identities[identity]; found && prior != binding.ID {
			t.Fatalf("binding references for %s have different IDs: %s and %s", identity, prior, binding.ID)
		}
		identities[identity] = binding.ID
	}
}

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

func TestValidatedMIRUsesCompleteFlatArena(t *testing.T) {
	program, checker := testProgram(t, `struct Point { x: Int }
enum Switch { On, Off }
fn add(value: Int, extra: Int = 1) -> Int { return value + extra }
fn main() -> Int {
    let point: Point = Point { x: 1 }
    let mut total: Int = point.x
    for item in [2, 3] { total = total + add(item) }
    if total > 3 { total = total + 1 } else { total = total - 1 }
    match Switch::On {
        Switch::On => { return total }
        Switch::Off => { return 0 }
    }
}
`)
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("compile source to validated arena: %v", err)
	}
	if _, found := reflect.TypeOf(ValidatedMIR{}).FieldByName("document"); found {
		t.Fatal("ValidatedMIR still retains a recursive wire document")
	}
	if mir.arena == nil {
		t.Fatal("CompileMIR did not create an arena")
	}
	for label, count := range map[string]int{
		"expressions": len(mir.arena.Expressions), "statements": len(mir.arena.Statements),
		"functions": len(mir.arena.Functions), "parameters": len(mir.arena.Parameters),
		"patterns": len(mir.arena.Patterns), "match arms": len(mir.arena.Arms),
		"bindings": len(mir.arena.Bindings),
	} {
		if count == 0 {
			t.Errorf("arena has no %s", label)
		}
	}
	if err := mir.arena.validateReferences(); err != nil {
		t.Fatalf("valid source produced invalid arena references: %v", err)
	}
	legacyDocument, err := buildKIRDocument(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("build portable KIR reference from checked source: %v", err)
	}
	if err := validateKIRDocument(legacyDocument, checker.Lim); err != nil {
		t.Fatalf("portable reference document from checked source is invalid: %v", err)
	}
	arenaDocument, err := mir.arena.toKIRDocument()
	if err != nil {
		t.Fatalf("materialize arena for source-lowering contract: %v", err)
	}
	if !reflect.DeepEqual(arenaDocument, legacyDocument) {
		want, _ := json.Marshal(legacyDocument)
		got, _ := json.Marshal(arenaDocument)
		at := 0
		for at < len(want) && at < len(got) && want[at] == got[at] {
			at++
		}
		start, end := at-100, at+250
		if start < 0 {
			start = 0
		}
		if end > len(want) {
			end = len(want)
		}
		if end > len(got) {
			end = len(got)
		}
		t.Fatalf("direct source-to-arena lowering changed canonical checked KIR near byte %d; old=%s new=%s", at, want[start:end], got[start:end])
	}

	wire, err := mir.MarshalKIR()
	if err != nil {
		t.Fatalf("serialize arena: %v", err)
	}
	view, err := mir.documentView()
	if err != nil {
		t.Fatalf("make typed round-trip view: %v", err)
	}
	viewWire, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	viewWire = append(viewWire, '\n')
	if !bytes.Equal(wire, viewWire) {
		t.Fatal("arena to wire view lost KIR fields or child/argument order")
	}

	original := append([]byte(nil), wire...)
	view.Functions[0].Body[0].Return.Int = 999
	afterMutation, err := mir.MarshalKIR()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, afterMutation) {
		t.Fatal("mutating a compatibility view changed ValidatedMIR")
	}

	broken := *mir.arena
	broken.ExpressionRefs = append([]MIRIndex(nil), mir.arena.ExpressionRefs...)
	broken.ExpressionRefs[0] = MIRIndex(len(broken.Expressions))
	if err := broken.validateReferences(); err == nil {
		t.Fatal("arena accepted an out-of-range child/argument reference")
	}
}

func TestCompileMIRRejectsCheckerForDifferentProgram(t *testing.T) {
	program, _ := testProgram(t, "let value: Int = 1\n")
	_, checker := testProgram(t, "let value: Int = 2\n")
	if _, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "does not describe") {
		t.Fatalf("expected mismatched checker to be rejected, got %v", err)
	}
}

func TestCompileMIRRejectsCheckerThatDidNotComplete(t *testing.T) {
	program, checker := testProgram(t, "let value: Int = 42\n")
	unvalidated := *checker
	unvalidated.checked = false
	if _, err := CompileMIR(program, &unvalidated, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "checker has not completed successfully") {
		t.Fatalf("CompileMIR accepted an unchecked Checker or returned the wrong error: %v", err)
	}
}

func TestCompileMIRRejectsUnsupportedTargetMetadata(t *testing.T) {
	program, checker := testProgram(t, "println(42)\n")
	if _, err := CompileMIR(program, checker, NativeTarget{OS: "plan9", Arch: "386"}); err == nil || !strings.Contains(err.Error(), "unsupported target plan9-386") {
		t.Fatalf("CompileMIR accepted invalid target metadata or returned the wrong error: %v", err)
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
	astRuntime, astDiagnostic := newASTOracleRuntime(badProgram, badChecker, DefaultLimits(), Sandbox{}, nil)
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
