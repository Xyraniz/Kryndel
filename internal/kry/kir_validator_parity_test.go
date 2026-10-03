package kry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGoAndSelfhostValidatorsAgreeOnCoreKIRCorpus(t *testing.T) {
	limits := DefaultLimits()
	encoded := emitValidatorCorpusKIR(t, limits, "validator-parity.kry", `
fn identity(value: Int) -> Int { return value }
fn main() -> Int { return identity(1) + identity(2) }
`)
	var base KIRDocument
	if err := json.Unmarshal(encoded, &base); err != nil {
		t.Fatalf("decode emitted validator corpus KIR: %v", err)
	}
	main := findKIRFunction(&base, "main")
	if main == nil || len(main.Body) != 1 || main.Body[0].Return == nil {
		t.Fatal("validator corpus did not emit the expected main return expression")
	}

	valid, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	wrongResult := cloneKIRDocument(t, base)
	findKIRFunction(wrongResult, "main").Body[0].Return.Type = "String"
	wrongResultBytes, err := json.Marshal(wrongResult)
	if err != nil {
		t.Fatal(err)
	}

	wrongCallTarget := cloneKIRDocument(t, base)
	findKIRFunction(wrongCallTarget, "main").Body[0].Return.Left.CallTarget = "function:ghost"
	wrongCallTargetBytes, err := json.Marshal(wrongCallTarget)
	if err != nil {
		t.Fatal(err)
	}

	wrongCallArity := cloneKIRDocument(t, base)
	findKIRFunction(wrongCallArity, "main").Body[0].Return.Left.Args = nil
	wrongCallArityBytes, err := json.Marshal(wrongCallArity)
	if err != nil {
		t.Fatal(err)
	}

	unknownField := bytes.Replace(encoded, []byte(`"format": "kry-ir"`), []byte(`"format": "kry-ir", "unknown": true`), 1)
	if bytes.Equal(unknownField, encoded) {
		t.Fatal("could not add unknown-field case to validator corpus")
	}

	mutableSource := emitValidatorCorpusKIR(t, limits, "validator-mutability.kry", `
fn main() -> Int {
    let mut value: Int = 1
    value = 2
    return value
}
`)
	var immutableAssignment KIRDocument
	if err := json.Unmarshal(mutableSource, &immutableAssignment); err != nil {
		t.Fatalf("decode mutability validator KIR: %v", err)
	}
	mutableMain := findKIRFunction(&immutableAssignment, "main")
	if mutableMain == nil || len(mutableMain.Body) < 2 || mutableMain.Body[0].Binding == nil {
		t.Fatal("mutability validator source did not emit the expected binding")
	}
	mutableMain.Body[0].Binding.Mutable = false
	immutableAssignmentBytes, err := json.Marshal(immutableAssignment)
	if err != nil {
		t.Fatal(err)
	}

	matchSource := emitValidatorCorpusKIR(t, limits, "validator-match.kry", `
enum TrafficLight { Red, Yellow, Green }
fn main() -> Nil {
    match TrafficLight::Red {
        TrafficLight::Red => { println("red") }
        TrafficLight::Yellow => { println("yellow") }
        TrafficLight::Green => { println("green") }
    }
}
`)
	var nonExhaustiveMatch KIRDocument
	if err := json.Unmarshal(matchSource, &nonExhaustiveMatch); err != nil {
		t.Fatalf("decode match validator KIR: %v", err)
	}
	matchMain := findKIRFunction(&nonExhaustiveMatch, "main")
	if matchMain == nil || len(matchMain.Body) != 1 || len(matchMain.Body[0].Arms) != 3 {
		t.Fatal("match validator source did not emit the expected exhaustive match")
	}
	matchMain.Body[0].Arms = matchMain.Body[0].Arms[:2]
	nonExhaustiveMatchBytes, err := json.Marshal(nonExhaustiveMatch)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		encoded []byte
		accept  bool
	}{
		{name: "valid calls and arithmetic", encoded: valid, accept: true},
		{name: "wrong operator result type", encoded: wrongResultBytes},
		{name: "dangling call target", encoded: wrongCallTargetBytes},
		{name: "wrong call arity", encoded: wrongCallArityBytes},
		{name: "unknown document field", encoded: unknownField},
		{name: "immutable assignment", encoded: immutableAssignmentBytes},
		{name: "valid exhaustive enum match", encoded: matchSource, accept: true},
		{name: "non-exhaustive enum match", encoded: nonExhaustiveMatchBytes},
	}

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	probePath := filepath.Join(root, "..", "..", "selfhost", "validated_kir_probe.kry")
	probeProgram, diagnostic := LoadProgram(probePath, limits, "")
	if diagnostic != nil {
		t.Fatalf("load selfhost KIR validator probe: %s", diagnostic.Message)
	}
	probeChecker, diagnostic := Check(probeProgram, limits)
	if diagnostic != nil {
		t.Fatalf("check selfhost KIR validator probe: %s", diagnostic.Message)
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, goErr := DecodeMIR(test.encoded, limits)
			goAccepted := goErr == nil
			if goAccepted != test.accept {
				t.Fatalf("Go validator acceptance = %t, want %t (error: %v)", goAccepted, test.accept, goErr)
			}

			path := filepath.Join(t.TempDir(), "input.kir")
			if err := os.WriteFile(path, test.encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, diagnostic := NewRuntimeWithArgs(probeProgram, probeChecker, limits, Sandbox{}, []string{path})
			if diagnostic != nil {
				t.Fatalf("create probe runtime: %s", diagnostic.Message)
			}
			var output bytes.Buffer
			runtime.output = &output
			if diagnostic := runtime.run(); diagnostic != nil {
				t.Fatalf("selfhost KIR validator probe failed: %s", diagnostic.Message)
			}
			want := "rejected\n"
			if test.accept {
				want = "accepted\n"
			}
			if got := output.String(); got != want {
				t.Fatalf("selfhost validator output = %q, Go accepted=%t, want %q", got, goAccepted, want)
			}
		})
	}
}

func emitValidatorCorpusKIR(t *testing.T, limits Limits, name, source string) []byte {
	t.Helper()
	program, diagnostic := Parse(&Source{Name: name, Text: source}, limits)
	if diagnostic != nil {
		t.Fatalf("parse validator corpus source: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatalf("check validator corpus source: %s", diagnostic.Message)
	}
	encoded, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("emit validator corpus KIR: %v", err)
	}
	return encoded
}

func cloneKIRDocument(t *testing.T, source KIRDocument) *KIRDocument {
	t.Helper()
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var clone KIRDocument
	if err := json.Unmarshal(encoded, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}

func findKIRFunction(document *KIRDocument, name string) *KIRFunction {
	for _, function := range document.Functions {
		if function != nil && function.Name == name {
			return function
		}
	}
	return nil
}
