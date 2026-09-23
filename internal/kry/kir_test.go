package kry

import (
	"bytes"
	"strings"
	"testing"
)

func TestKIRIsDeterministicAndTyped(t *testing.T) {
	src := `
struct Header { value: UInt16 }
fn add(a: UInt8, b: UInt8 = u8(1)) -> UInt8 { return a + b }
let header: Header = Header{ value: u16(255) }
let answer: UInt8 = add(u8(1)) << 1
`
	p, c := testProgram(t, src)
	target := NativeTarget{OS: "linux", Arch: "amd64"}
	a, err := EmitKIR(p, c, target)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EmitKIR(p, c, target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("KIR emission is not deterministic")
	}
	if !strings.Contains(string(a), `"format": "kry-ir"`) || !strings.Contains(string(a), `"version": 2`) || !strings.Contains(string(a), `"language_version": "1.0.0"`) {
		t.Fatalf("KIR header missing from %s", a[:minInt(len(a), 160)])
	}
	d, err := DecodeKIR(a, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != KIRFormat || d.Version != KIRVersion || d.Target.OS != "linux" || len(d.Functions) != 1 {
		t.Fatalf("unexpected KIR document: %#v", d)
	}
	if d.Statements[1].Init == nil || d.Statements[1].Init.Type != "UInt8" || d.Statements[1].Init.Operator != "<<" {
		t.Fatalf("typed expression was not preserved: %#v", d.Statements[1].Init)
	}
	if d.Functions[0].Params[1].Default == nil || d.Functions[0].Params[1].Default.CallTarget != "builtin:u8" {
		t.Fatalf("builtin call target was not preserved: %#v", d.Functions[0].Params[1].Default)
	}
}

func TestKIRRejectsWrongVersionAndTrailingData(t *testing.T) {
	p, c := testProgram(t, "let x: Int = 1\n")
	data, err := EmitKIR(p, c, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	wrong := bytes.Replace(data, []byte(`"version": 2`), []byte(`"version": 99`), 1)
	if _, err = DecodeKIR(wrong, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected version rejection, got %v", err)
	}
	if _, err = DecodeKIR(append(append([]byte{}, data...), data...), DefaultLimits()); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing-data rejection, got %v", err)
	}
}

func TestKIRV1DefaultsLanguageVersion(t *testing.T) {
	p, c := testProgram(t, "let x: Int = 1\n")
	data, err := EmitKIR(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.Replace(data, []byte(`"version": 2`), []byte(`"version": 1`), 1)
	legacy = bytes.Replace(legacy, []byte("  \"language_version\": \"1.0.0\",\n"), nil, 1)
	doc, err := DecodeKIR(legacy, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.LanguageVersion != LanguageVersion {
		t.Fatalf("legacy KIR compatibility mismatch: version=%d language=%q", doc.Version, doc.LanguageVersion)
	}
}

func TestKIRPreservesUnaryNotOperator(t *testing.T) {
	p, c := testProgram(t, "let negated: Bool = !false\n")
	data, err := EmitKIR(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeKIR(data, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Statements) != 1 || doc.Statements[0].Init == nil || doc.Statements[0].Init.Kind != "unary" || doc.Statements[0].Init.Operator != "!" {
		t.Fatalf("unary not was not preserved in KIR: %#v", doc.Statements)
	}
}

func TestLLVMEmissionDoesNotReturnFakeIR(t *testing.T) {
	if data, err := EmitLLVMIR(nil, NativeTarget{}); err == nil || data != nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("LLVM emitter must fail honestly, data=%q err=%v", data, err)
	}
}
