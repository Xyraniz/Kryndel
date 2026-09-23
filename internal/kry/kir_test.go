package kry

import (
	"bytes"
	"encoding/json"
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

func TestKIRRejectsMalformedTreesAndResourceLimits(t *testing.T) {
	p, c := testProgram(t, "let value: Int = 1\n")
	data, err := EmitKIR(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		want   string
		mutate func(*KIRDocument)
		limits func(*Limits, []byte)
	}{
		{
			name: "unknown expression kind",
			want: "unknown expression kind",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0].Init.Kind = "mystery"
			},
		},
		{
			name: "missing checked type",
			want: "has no checked type",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0].Init.Type = ""
			},
		},
		{
			name: "unknown call target",
			want: "undeclared function",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0].Init = &KIRExpr{Kind: "call", Type: "Int", Name: "missing", CallTarget: "function:missing"}
			},
		},
		{
			name: "call name and target mismatch",
			want: "does not match its target",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0].Init = &KIRExpr{Kind: "call", Type: "Int", Name: "wrong", CallTarget: "builtin:len"}
			},
		},
		{
			name: "mismatched map entries",
			want: "mismatched keys and values",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0].Init = &KIRExpr{Kind: "map", Type: "Map[String, Int]", MapKeys: []*KIRExpr{{Kind: "string", Type: "String", String: "a"}}}
			},
		},
		{
			name: "mismatched struct fields",
			want: "mismatched fields and values",
			mutate: func(doc *KIRDocument) {
				doc.Structs = append(doc.Structs, &KIRStruct{Name: "Pair", Fields: []*KIRField{{Name: "left", Type: "Int"}}})
				doc.Statements[0].Init = &KIRExpr{Kind: "struct", Type: "Pair", StructName: "Pair", Fields: []string{"left"}}
			},
		},
		{
			name: "match without arms",
			want: "match statement has no arms",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0] = &KIRStmt{Kind: "match", Scrutinee: &KIRExpr{Kind: "int", Type: "Int"}}
			},
		},
		{
			name: "non-binding assignment target",
			want: "assignment target is not a binding",
			mutate: func(doc *KIRDocument) {
				doc.Statements[0] = &KIRStmt{
					Kind:   "assign",
					Target: &KIRExpr{Kind: "int", Type: "Int"},
					Value:  &KIRExpr{Kind: "int", Type: "Int"},
				}
			},
		},
		{
			name: "nesting limit",
			want: "tree depth exceeds configured nesting limit",
			limits: func(limits *Limits, _ []byte) {
				limits.MaxNesting = 1
			},
		},
		{
			name: "node limit",
			want: "node count exceeds configured limit",
			limits: func(limits *Limits, _ []byte) {
				limits.MaxASTNodes = 1
			},
		},
		{
			name: "JSON byte limit",
			want: "configured JSON limit",
			limits: func(limits *Limits, encoded []byte) {
				limits.MaxJSONBytes = len(encoded) - 1
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc KIRDocument
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if tt.mutate != nil {
				tt.mutate(&doc)
			}
			encoded, err := json.Marshal(&doc)
			if err != nil {
				t.Fatal(err)
			}
			limits := DefaultLimits()
			if tt.limits != nil {
				tt.limits(&limits, encoded)
			}
			if _, err := DecodeKIR(encoded, limits); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLLVMEmissionDoesNotReturnFakeIR(t *testing.T) {
	if data, err := EmitLLVMIR(nil, NativeTarget{}); err == nil || data != nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("LLVM emitter must fail honestly, data=%q err=%v", data, err)
	}
}
