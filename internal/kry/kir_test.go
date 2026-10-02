package kry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKIRImportedProgramIsIndependentOfCheckoutPath(t *testing.T) {
	writeProject := func(root string) string {
		t.Helper()
		lib := filepath.Join(root, "lib")
		if err := os.MkdirAll(lib, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(lib, "math.kry"), []byte("pub fn twice(value: Int) -> Int { return value + value }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		main := filepath.Join(root, "main.kry")
		source := "import \"lib/math\"\nfn choose(value: Int) -> Int { return value }\nfn choose(value: String) -> Int { return 2 }\nlet answer: Int = twice(choose(3))\n"
		if err := os.WriteFile(main, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		return main
	}
	emit := func(path string) []byte {
		t.Helper()
		program, diagnostic := LoadProgram(path, DefaultLimits(), "")
		if diagnostic != nil {
			t.Fatal(diagnostic.Message)
		}
		checker, diagnostic := Check(program, DefaultLimits())
		if diagnostic != nil {
			t.Fatal(diagnostic.Message)
		}
		data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := emit(writeProject(t.TempDir()))
	second := emit(writeProject(t.TempDir()))
	if !bytes.Equal(first, second) {
		t.Fatal("identical imported sources produced different KIR in different checkout paths")
	}
	document, err := DecodeKIR(first, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if document.Module != "main.kry" || document.Source != "main.kry" || len(document.Imports) != 1 || document.Imports[0] != "lib/math" {
		t.Fatalf("KIR lost logical module identity or imports: module=%q source=%q imports=%q", document.Module, document.Source, document.Imports)
	}
	if len(document.Sources) != 2 || document.Sources[0] != "main.kry" || document.Sources[1] != "lib/math.kry" {
		t.Fatalf("KIR has unexpected source names: %q", document.Sources)
	}
}

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
	if !strings.Contains(string(a), `"format": "kry-ir"`) || !strings.Contains(string(a), `"language_version": "1.0.0"`) {
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
	var header map[string]any
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	header["version"] = float64(99)
	wrong, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeKIR(wrong, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected version rejection, got %v", err)
	}
	if _, err = DecodeKIR(append(append([]byte{}, data...), data...), DefaultLimits()); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("expected trailing-data rejection, got %v", err)
	}
}

func TestKIRV1DefaultsLanguageVersion(t *testing.T) {
	legacy := []byte(`{"format":"kry-ir","version":1,"module":"","source":"legacy.kry","target":{"os":"linux","arch":"amd64","gui":false},"imports":[],"sources":[],"structs":[],"enums":[],"functions":[],"statements":[]}`)
	doc, err := DecodeKIR(legacy, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.LanguageVersion != LanguageVersion {
		t.Fatalf("legacy KIR compatibility mismatch: version=%d language=%q", doc.Version, doc.LanguageVersion)
	}
}

func TestKIRV2RemainsReadable(t *testing.T) {
	p, c := testProgram(t, "fn twice(value: Int) -> Int { return value * 2 }\nlet result: Int = twice(4)\n")
	data, err := EmitKIR(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var legacy any
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	var stripV3Fields func(any)
	stripV3Fields = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			for _, key := range []string{"binding", "resolved_binding", "callee", "lambda", "captures"} {
				delete(node, key)
			}
			for _, child := range node {
				stripV3Fields(child)
			}
		case []any:
			for _, child := range node {
				stripV3Fields(child)
			}
		}
	}
	stripV3Fields(legacy)
	legacyDocument := legacy.(map[string]any)
	delete(legacyDocument, "traits")
	delete(legacyDocument, "trait_impls")
	for _, rawFunction := range legacyDocument["functions"].([]any) {
		function := rawFunction.(map[string]any)
		delete(function, "source")
		delete(function, "line")
		delete(function, "column")
		delete(function, "trait")
	}
	legacyDocument["version"] = float64(2)
	v2, err := json.Marshal(legacyDocument)
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(v2, DefaultLimits())
	if err != nil {
		t.Fatalf("KIR v2 compatibility failed: %v", err)
	}
	if document.Version != 2 {
		t.Fatalf("decoded version = %d, want 2", document.Version)
	}
	if len(document.Functions) != 1 || document.Functions[0].Name != "twice" || len(document.Statements) != 1 || document.Statements[0].Name != "result" || document.Statements[0].Init.CallTarget != "function:twice" {
		t.Fatalf("legacy KIR v2 function, variable, or call was not preserved: %#v", document)
	}
}

func TestKIRV4GenericStructRemainsReadable(t *testing.T) {
	program, checker := testProgram(t, `
struct Box[T: Copy] { value: T }
fn identity[T: Copy](value: T) -> T { return value }
let boxed: Box[Int] = Box[Int]{value: identity(7)}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]any
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "traits")
	delete(legacy, "trait_impls")
	for _, rawFunction := range legacy["functions"].([]any) {
		function := rawFunction.(map[string]any)
		delete(function, "source")
		delete(function, "line")
		delete(function, "column")
		delete(function, "trait")
	}
	legacy["version"] = float64(4)
	v4, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(v4, DefaultLimits())
	if err != nil {
		t.Fatalf("KIR v4 generic compatibility failed: %v", err)
	}
	if document.Version != 4 || len(document.Structs) != 1 || len(document.Structs[0].TypeParams) != 1 || len(document.Functions) != 1 || len(document.Statements) != 1 {
		t.Fatalf("KIR v4 generic declarations or executable nodes were not preserved: %#v", document)
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

func TestKIRCallTargetsPreserveResolvedOverloads(t *testing.T) {
	source := `
fn choose(value: Int) -> Int { return value }
fn choose(value: String) -> Int { return 2 }
let integer: Int = choose(1)
let text: Int = choose("two")
`
	p, c := testProgram(t, source)
	data, err := EmitKIR(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeKIR(data, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	first := doc.Statements[0].Init.CallTarget
	second := doc.Statements[1].Init.CallTarget
	if first == second || !strings.HasPrefix(first, "function:choose@") || !strings.HasPrefix(second, "function:choose@") {
		t.Fatalf("overload resolution was not preserved in distinct KIR targets: %q and %q", first, second)
	}
	doc.Statements[0].Init.CallTarget = "function:choose"
	if err := validateKIRDocument(doc, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "unresolved overload") {
		t.Fatalf("expected ambiguous legacy function target rejection, got %v", err)
	}
}

func TestKIRRejectsResolvedFunctionCallSignatureMismatch(t *testing.T) {
	program, checker := testProgram(t, "fn identity(value: Int) -> Int { return value }\nlet result: Int = identity(1)\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		want   string
		mutate func(*KIRExpr)
	}{
		{
			name: "argument type",
			want: `argument 1 has type "String", want "Int"`,
			mutate: func(call *KIRExpr) {
				argument := *call.Args[0]
				argument.Kind = "string"
				argument.Type = "String"
				argument.Int = 0
				argument.String = "wrong"
				argument.Const = nil
				call.Args[0] = &argument
			},
		},
		{
			name:   "result type",
			want:   "has result type",
			mutate: func(call *KIRExpr) { call.Type = "String" },
		},
		{
			name:   "required argument count",
			want:   "expected 1 to 1",
			mutate: func(call *KIRExpr) { call.Args = nil },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			call := document.Statements[0].Init
			test.mutate(call)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR call rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsBuiltinArityMismatch(t *testing.T) {
	program, checker := testProgram(t, "let length: Int = len(\"text\")\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Args = nil
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `builtin "len" has 0 arguments, want 1`) {
		t.Fatalf("expected KIR rejection for builtin arity mismatch, got %v", err)
	}
}

func TestKIRRequiresBuiltinIDInVersionFive(t *testing.T) {
	program, checker := testProgram(t, "let length: Int = len(\"text\")\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.BuiltinID = ""
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "mismatched builtin id") {
		t.Fatalf("expected KIR v5 rejection for missing builtin id, got %v", err)
	}
}

func TestKIRRejectsFunctionDefaultTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, "fn choose(value: Int = 1) -> Int { return value }\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Functions[0].Params[0].Default.Kind = "bool"
	document.Functions[0].Params[0].Default.Type = "Bool"
	document.Functions[0].Params[0].Default.Bool = true
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "default type does not match") {
		t.Fatalf("expected KIR rejection for mismatched parameter default, got %v", err)
	}
}

func TestKIRRejectsForBindingTypeMismatch(t *testing.T) {
	tests := []struct {
		name   string
		source string
		wrong  string
	}{
		{name: "array", source: "for item in [1] {}\n", wrong: "String"},
		{name: "set", source: "for item in |{1}| {}\n", wrong: "String"},
		{name: "string", source: "for item in \"text\" {}\n", wrong: "Int"},
		{name: "bytes", source: "let data: Bytes = bytes([1])\nfor item in data {}\n", wrong: "String"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			loop := document.Statements[len(document.Statements)-1]
			loop.Binding.Type = test.wrong
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "does not match iterator element type") {
				t.Fatalf("expected KIR rejection for mismatched for binding, got %v", err)
			}
		})
	}
}

func TestKIRRejectsInvalidAssignmentSemantics(t *testing.T) {
	program, checker := testProgram(t, "let mut value: Int = 1\nvalue = 2\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	decode := func() *KIRDocument {
		t.Helper()
		var document KIRDocument
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		return &document
	}
	t.Run("value type", func(t *testing.T) {
		document := decode()
		document.Statements[1].Value = &KIRExpr{Kind: "bool", Source: document.Source, Line: 2, Column: 9, Type: "Bool", Bool: true}
		if err := validateKIRDocument(document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "does not match target type") {
			t.Fatalf("expected KIR rejection for mismatched assignment, got %v", err)
		}
	})
	t.Run("immutable target", func(t *testing.T) {
		document := decode()
		target := document.Statements[1].Target
		binding := *target.Binding
		binding.Mutable = false
		target.Binding = &binding
		if err := validateKIRDocument(document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "is immutable") {
			t.Fatalf("expected KIR rejection for immutable assignment target, got %v", err)
		}
	})
}

func TestKIRRejectsLogicalOperatorTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, "let value: Bool = true && false\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Left = &KIRExpr{Kind: "int", Source: document.Source, Line: 1, Column: 21, Type: "Int", Int: 1}
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "requires Bool operands and result") {
		t.Fatalf("expected KIR rejection for ill-typed logical operation, got %v", err)
	}
}

func TestKIRRejectsUnaryOperatorTypeMismatch(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "positive", source: "let value: Int = +1\n", wantErr: "unary operator + requires a numeric"},
		{name: "negative", source: "let value: Int = -1\n", wantErr: "unary operator - requires an Int or Float"},
		{name: "not", source: "let value: Bool = !true\n", wantErr: "unary operator ! requires Bool"},
		{name: "bitwise not", source: "let value: UInt8 = ~u8(1)\n", wantErr: "unary operator ~ requires a UInt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			unary := document.Statements[0].Init
			unary.Operand = &KIRExpr{Kind: "string", Source: document.Source, Line: 1, Column: 1, Type: "String", String: "bad"}
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func TestKIRAcceptsGenericNumericUnaryPlus(t *testing.T) {
	for _, constraint := range []string{"Numeric", "Integer"} {
		t.Run(constraint, func(t *testing.T) {
			program, checker := testProgram(t, "fn positive[T: "+constraint+"](value: T) -> T { return +value }\n")
			if _, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}); err != nil {
				t.Fatalf("KIR rejected a valid generic %s unary plus: %v", constraint, err)
			}
		})
	}
}

func TestKIRRejectsBitwiseAndShiftTypeMismatch(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "bitwise", source: "let value: UInt8 = u8(1) & u8(2)\n", wantErr: "matching UInt operands and result"},
		{name: "shift", source: "let value: UInt8 = u8(1) << 1\n", wantErr: "UInt value, Int count"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			binary := document.Statements[0].Init
			binary.Left = &KIRExpr{Kind: "int", Source: document.Source, Line: 1, Column: 1, Type: "Int", Int: 1}
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func TestKIRRejectsComparisonResultTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, "let value: Bool = 1 == 1\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Type = "Int"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "requires Bool result") {
		t.Fatalf("expected KIR rejection for a comparison with non-Bool result, got %v", err)
	}
}

func TestKIRRejectsInvalidArithmeticOperandTypes(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		mutate  func(*KIRExpr)
		wantErr string
	}{
		{
			name:    "float remainder",
			source:  "let value: Float = 3.0 / 2.0\n",
			mutate:  func(expression *KIRExpr) { expression.Operator = "%" },
			wantErr: "operator % requires Int or UInt",
		},
		{
			name:    "string subtraction",
			source:  "let value: String = \"a\" + \"b\"\n",
			mutate:  func(expression *KIRExpr) { expression.Operator = "-" },
			wantErr: "requires numeric operands",
		},
		{
			name:    "wrong result type",
			source:  "let value: Int = 1 + 2\n",
			mutate:  func(expression *KIRExpr) { expression.Type = "Float" },
			wantErr: "matching operand and result types",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[0].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func TestKIRAcceptsSupportedArithmeticTypeShapes(t *testing.T) {
	source := `let label: String = "a" + "b"
let first: Array[Int] = [1]
let second: Array[Int] = [2]
let joined: Array[Int] = first + second
let first_bytes: Bytes = bytes([65])
let second_bytes: Bytes = bytes([66])
let joined_bytes: Bytes = first_bytes + second_bytes
fn sum[T: Numeric](left: T, right: T) -> T { return left + right }
fn remainder[T: Integer](left: T, right: T) -> T { return left % right }
fn is_less[T: Numeric](left: T, right: T) -> Bool { return left < right }
`
	program, checker := testProgram(t, source)
	if _, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("KIR rejected supported arithmetic operand types: %v", err)
	}
}

func TestKIRRejectsInvalidComparisonOperandTypes(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		mutate  func(*KIRExpr)
		wantErr string
	}{
		{
			name:   "equality operands differ",
			source: "let value: Bool = 1 == 1\n",
			mutate: func(expression *KIRExpr) {
				expression.Right = &KIRExpr{Kind: "string", Source: expression.Source, Line: 1, Column: 1, Type: "String", String: "bad"}
			},
			wantErr: "requires matching operand types",
		},
		{
			name:   "ordered string operands",
			source: "let value: Bool = 1 < 2\n",
			mutate: func(expression *KIRExpr) {
				expression.Left = &KIRExpr{Kind: "string", Source: expression.Source, Line: 1, Column: 1, Type: "String", String: "a"}
				expression.Right = &KIRExpr{Kind: "string", Source: expression.Source, Line: 1, Column: 2, Type: "String", String: "b"}
			},
			wantErr: "requires matching numeric operands",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[0].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.wantErr, err)
			}
		})
	}
}

func TestKIRRejectsFunctionValueEquality(t *testing.T) {
	source := `fn identity(value: Int) -> Int { return value }
let callback: fn(Int) -> Int = identity
let equal: Bool = true == true
`
	program, checker := testProgram(t, source)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	comparison := document.Statements[1].Init
	left := *document.Statements[0].Init
	right := left
	comparison.Left = &left
	comparison.Right = &right
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "does not support function values") {
		t.Fatalf("expected KIR rejection for function equality, got %v", err)
	}
}

func TestKIRRejectsFunctionValueSignatureMismatch(t *testing.T) {
	program, checker := testProgram(t, `fn identity(value: Int) -> Int { return value }
let callback: fn(Int) -> Int = identity
let result: Int = callback(1)
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Type = "fn(String) -> String"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "does not match target signature") {
		t.Fatalf("expected KIR rejection for a mismatched function value signature, got %v", err)
	}
}

func TestKIRRejectsStructFieldValueTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, `struct Person { age: Int }
let person: Person = Person{age: 1}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Values[0] = &KIRExpr{Kind: "string", Type: "String", String: "wrong"}
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `struct field "age" has checked type "String", want "Int"`) {
		t.Fatalf("expected KIR rejection for a mismatched struct field value, got %v", err)
	}
}

func TestKIRRejectsGenericStructFieldValueTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, `struct Box[T: Copy] { value: T }
let boxed: Box[Int] = Box[Int]{value: 1}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Values[0] = &KIRExpr{Kind: "string", Type: "String", String: "wrong"}
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `struct field "value" has checked type "String", want "Int"`) {
		t.Fatalf("expected KIR rejection for a mismatched generic struct field value, got %v", err)
	}
}

func TestKIRRejectsGenericStructConstraintMismatch(t *testing.T) {
	program, checker := testProgram(t, `struct Box[T: Numeric] { value: T }
let boxed: Box[Int] = Box[Int]{value: 1}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.StructType = "Box[String]"
	document.Statements[0].Init.Type = "Box[String]"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `type argument 1 ("String") does not satisfy constraint "Numeric"`) {
		t.Fatalf("expected KIR rejection for an unsatisfied generic struct constraint, got %v", err)
	}
}

func TestKIRRejectsGenericStructConstraintInFunctionSignature(t *testing.T) {
	program, checker := testProgram(t, `struct Box[T: Numeric] { value: T }
fn read(values: Array[Box[Int]]) -> Int { return 1 }
read([Box[Int]{value: 2}])
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Functions[0].Params[0].Type = "Array[Box[String]]"
	document.Functions[0].Params[0].Binding.Type = "Array[Box[String]]"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `type argument 1 ("String") does not satisfy constraint "Numeric"`) {
		t.Fatalf("expected KIR rejection for an invalid generic type in a signature, got %v", err)
	}
}

func TestKIRRejectsNestedStructConstraintInFieldType(t *testing.T) {
	program, checker := testProgram(t, `struct Inner[T: Numeric] { value: T }
struct Outer[T: Numeric] { inner: Inner[T] }
let outer: Outer[Int] = Outer[Int]{inner: Inner[Int]{value: 1}}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Structs[1].TypeParams[0].Constraint = "Copy"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `type argument 1 ("T") does not satisfy constraint "Numeric"`) {
		t.Fatalf("expected KIR rejection for an invalid nested struct constraint, got %v", err)
	}
}

func TestKIRRejectsMissingStructFieldValue(t *testing.T) {
	program, checker := testProgram(t, "struct Person { age: Int }\nlet person: Person = Person{age: 1}\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Init.Values[0] = nil
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `struct field "age" has no value`) {
		t.Fatalf("expected KIR rejection for a missing struct field value, got %v", err)
	}
}

func TestKIRRejectsStructFieldAccessTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, `struct Person { age: Int }
let person: Person = Person{age: 1}
let age: Int = person.age
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[1].Init.Type = "String"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `field expression "age" has checked type "String", want "Int"`) {
		t.Fatalf("expected KIR rejection for a mismatched struct field access type, got %v", err)
	}
}

func TestKIRRejectsGenericStructFieldAccessTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, `struct Box[T: Copy] { value: T }
let box: Box[Int] = Box[Int]{value: 1}
let value: Int = box.value
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[1].Init.Type = "String"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `field expression "value" has checked type "String", want "Int"`) {
		t.Fatalf("expected KIR rejection for a mismatched generic struct field access type, got %v", err)
	}
}

func TestKIRRejectsUnknownOrNonStructFieldAccess(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*KIRExpr)
		want   string
	}{
		{
			name: "unknown field",
			mutate: func(expression *KIRExpr) {
				expression.Field = "missing"
			},
			want: `references unknown field "missing"`,
		},
		{
			name: "non-struct base",
			mutate: func(expression *KIRExpr) {
				expression.Base.Type = "Int"
			},
			want: `base has non-struct checked type "Int"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, `struct Person { age: Int }
let person: Person = Person{age: 1}
let age: Int = person.age
`)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[1].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsInvalidMapLiteralTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*KIRExpr)
		want   string
	}{
		{
			name: "value type",
			mutate: func(expression *KIRExpr) {
				expression.Values[0] = &KIRExpr{Kind: "string", Type: "String", String: "wrong"}
			},
			want: `map value 1 has checked type "String", want "Int"`,
		},
		{
			name: "key type",
			mutate: func(expression *KIRExpr) {
				expression.MapKeys[0] = &KIRExpr{Kind: "int", Type: "Int", Int: 1}
			},
			want: `map key 1 has checked type "Int", want "String"`,
		},
		{
			name: "unsupported key type",
			mutate: func(expression *KIRExpr) {
				expression.Type = "Map[Float, Int]"
			},
			want: `unsupported key type "Float"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, `let values: Map[String, Int] = {"answer": 41}
`)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[0].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsInvalidArrayAndSetElementTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		mutate func(*KIRExpr)
		want   string
	}{
		{
			name:   "array element type",
			source: "let values: Array[Int] = [1, 2]\n",
			mutate: func(expression *KIRExpr) {
				expression.Items[1] = &KIRExpr{Kind: "string", Type: "String", String: "wrong"}
			},
			want: `array element 2 has checked type "String", want "Int"`,
		},
		{
			name:   "set element type",
			source: `let values: Set[String] = |{"ready", "done"}|` + "\n",
			mutate: func(expression *KIRExpr) {
				expression.Items[1] = &KIRExpr{Kind: "int", Type: "Int", Int: 2}
			},
			want: `set element 2 has checked type "Int", want "String"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[0].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsIncompatibleMatchPatterns(t *testing.T) {
	tests := []struct {
		name   string
		source string
		mutate func(*KIRStmt)
		want   string
	}{
		{
			name:   "pattern kind",
			source: "match true { true => {} false => {} }\n",
			mutate: func(statement *KIRStmt) { statement.Arms[0].Pattern.Kind = "int" },
			want:   "int pattern is incompatible",
		},
		{
			name:   "option payload binding",
			source: "let value: Option[Int] = some(1)\nmatch value { some(item) => { println(item) } none => {} }\n",
			mutate: func(statement *KIRStmt) { statement.Arms[0].Pattern.ResolvedBinding.Type = "String" },
			want:   "does not match its checked payload type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			statement := document.Statements[len(document.Statements)-1]
			test.mutate(statement)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsFunctionReturnTypeMismatch(t *testing.T) {
	program, checker := testProgram(t, "fn identity(value: Int) -> Int { return value }\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Functions[0].Body[0].Return = &KIRExpr{Kind: "string", Source: document.Source, Line: 1, Column: 1, Type: "String", String: "wrong"}
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "function expects") {
		t.Fatalf("expected KIR rejection for mismatched function return, got %v", err)
	}
}

func TestKIRAcceptsPropagatingLambdaReturn(t *testing.T) {
	source := `fn make(result: Result[Int, String]) -> fn() -> Result[Int, String] {
    return fn() -> Result[Int, String] {
        let value: Int = result?
        return ok(value)
    }
}
`
	program, checker := testProgram(t, source)
	if _, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("KIR rejected a lambda whose propagation matches its return type: %v", err)
	}
}

func TestKIRRejectsInconsistentDeclarationFlags(t *testing.T) {
	tests := []struct {
		name   string
		source string
		mutate func(*KIRStmt)
		want   string
	}{
		{
			name:   "let marked const",
			source: "let value: Int = 1\n",
			mutate: func(statement *KIRStmt) { statement.Const = true },
			want:   "inconsistent const or mutability flags",
		},
		{
			name:   "const not marked const",
			source: "const value: Int = 1\n",
			mutate: func(statement *KIRStmt) { statement.Const = false },
			want:   "inconsistent const or mutability flags",
		},
		{
			name:   "mutable for binding",
			source: "for item in [1] {}\n",
			mutate: func(statement *KIRStmt) { statement.Mutable = true },
			want:   "cannot be mutable or const",
		},
		{
			name:   "mutable resolved for binding",
			source: "for item in [1] {}\n",
			mutate: func(statement *KIRStmt) { statement.Binding.Mutable = true },
			want:   "binding must be immutable",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[0])
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsLoopControlOutsideLoops(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "break", source: "while false { break }\n", want: "break statement appears outside a loop"},
		{name: "continue", source: "while false { continue }\n", want: "continue statement appears outside a loop"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			document.Statements = []*KIRStmt{document.Statements[0].Body[0]}
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsNonExhaustiveMatch(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "bool", source: "match true { true => {} false => {} }\n"},
		{name: "option", source: "let value: Option[Int] = some(1)\nmatch value { some(item) => {} none => {} }\n"},
		{name: "result", source: "let value: Result[Int, String] = ok(1)\nmatch value { ok(item) => {} err(problem) => {} }\n"},
		{name: "enum", source: "enum State { Ready, Waiting }\nmatch State::Ready { State::Ready => {} State::Waiting => {} }\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			for _, statement := range document.Statements {
				if statement != nil && statement.Kind == "match" {
					statement.Arms = statement.Arms[:len(statement.Arms)-1]
					break
				}
			}
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "non-exhaustive match") {
				t.Fatalf("expected KIR rejection for non-exhaustive match, got %v", err)
			}
		})
	}
}

func TestKIRRejectsGenericArgumentsOutsideDirectFunctionCalls(t *testing.T) {
	program, checker := testProgram(t, "println(1)\n")
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Expr.GenericArguments = []string{"Int"}
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "generic type arguments require a direct function call or function value") {
		t.Fatalf("expected KIR rejection for generic builtin arguments, got %v", err)
	}
}

func TestKIRRejectsInvalidIndexTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		mutate func(*KIRExpr)
		want   string
	}{
		{
			name:   "array index type",
			source: "let values: Array[Int] = [1]\nlet item: Int = values[0]\n",
			mutate: func(expression *KIRExpr) { expression.Left.Type = "String" },
			want:   `checked index type "String", want "Int"`,
		},
		{
			name:   "array result type",
			source: "let values: Array[Int] = [1]\nlet item: Int = values[0]\n",
			mutate: func(expression *KIRExpr) { expression.Type = "String" },
			want:   `checked result type "String", want "Int"`,
		},
		{
			name:   "map index type",
			source: "let values: Map[String, Int] = {\"key\": 1}\nlet item: Int = values[\"key\"]\n",
			mutate: func(expression *KIRExpr) { expression.Left.Type = "Int" },
			want:   `checked index type "Int", want "String"`,
		},
		{
			name:   "map result type",
			source: "let values: Map[String, Int] = {\"key\": 1}\nlet item: Int = values[\"key\"]\n",
			mutate: func(expression *KIRExpr) { expression.Type = "String" },
			want:   `checked result type "String", want "Int"`,
		},
		{
			name:   "non-indexable base",
			source: "let values: Array[Int] = [1]\nlet item: Int = values[0]\n",
			mutate: func(expression *KIRExpr) { expression.Base.Type = "Bool" },
			want:   `non-indexable checked base type "Bool"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[1].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRAcceptsGenericMapIndex(t *testing.T) {
	program, checker := testProgram(t, `fn lookup[K: Copy, V: Copy](values: Map[K, V], key: K) -> V {
    return values[key]
}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeKIR(data, DefaultLimits()); err != nil {
		t.Fatalf("KIR rejected a checked generic Map[K, V] index: %v", err)
	}
}

func TestKIRAcceptsArrayMapKeyIndex(t *testing.T) {
	program, checker := testProgram(t, `fn lookup(values: Map[Array[Int], Int], key: Array[Int]) -> Int {
    return values[key]
}
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeKIR(data, DefaultLimits()); err != nil {
		t.Fatalf("KIR rejected a checked Array key index into Map: %v", err)
	}
}

func TestKIRRejectsInvalidPropagationTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		mutate func(*KIRDocument)
		want   string
	}{
		{
			name:   "result error type",
			source: "fn use(value: Result[Int, String]) -> Result[Int, String] { let inner: Int = value?; return ok(inner) }\n",
			mutate: func(document *KIRDocument) {
				document.Functions[0].Body[0].Init.Operand.Type = "Result[Int, Bool]"
			},
			want: "are incompatible with enclosing return type",
		},
		{
			name:   "payload type",
			source: "fn use(value: Option[Int]) -> Option[Int] { let inner: Int = value?; return some(inner) }\n",
			mutate: func(document *KIRDocument) {
				document.Functions[0].Body[0].Init.Type = "String"
			},
			want: "are incompatible with enclosing return type",
		},
		{
			name:   "outside function",
			source: "let value: Int = 1\n",
			mutate: func(document *KIRDocument) {
				document.Statements[0].Init = &KIRExpr{Kind: "propagate", Type: "Int", Operand: &KIRExpr{Kind: "int", Type: "Int", Int: 1}}
			},
			want: "are incompatible with enclosing return type \"\"",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(&document)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsGenericCallTypeMismatches(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*KIRExpr)
		want   string
	}{
		{
			name:   "argument",
			mutate: func(call *KIRExpr) { call.Args[0].Type = "String" },
			want:   `argument 1 has type "String", want "Int"`,
		},
		{
			name:   "result",
			mutate: func(call *KIRExpr) { call.Type = "String" },
			want:   `has result type "String", want "Int"`,
		},
		{
			name: "substituted generic type",
			mutate: func(call *KIRExpr) {
				call.GenericArguments[0] = "String"
			},
			want: `has result type "Int", want "String"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, `fn identity[T: Copy](value: T) -> T { return value }
let result: Int = identity(7)
`)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document.Statements[0].Init)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRRejectsGenericCallConstraintMismatch(t *testing.T) {
	program, checker := testProgram(t, `fn identity[T: Numeric](value: T) -> T { return value }
identity(7)
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	call := document.Statements[0].Expr
	call.GenericArguments[0] = "String"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `does not satisfy constraint "Numeric"`) {
		t.Fatalf("expected KIR rejection for an unsatisfied generic constraint, got %v", err)
	}
}

func TestKIRRejectsInvalidMethodCallTypes(t *testing.T) {
	for _, test := range []struct {
		name       string
		selectCall func(*KIRDocument) *KIRExpr
		mutate     func(*KIRExpr)
		want       string
	}{
		{
			name: "generic receiver result",
			selectCall: func(document *KIRDocument) *KIRExpr {
				return document.Statements[1].Init
			},
			mutate: func(call *KIRExpr) { call.Type = "String" },
			want:   `has result type "String", want "Int"`,
		},
		{
			name: "generic method argument",
			selectCall: func(document *KIRDocument) *KIRExpr {
				return document.Statements[2].Init
			},
			mutate: func(call *KIRExpr) { call.Args[0].Type = "String" },
			want:   `argument 1 has type "String", want "Int"`,
		},
		{
			name: "receiver type",
			selectCall: func(document *KIRDocument) *KIRExpr {
				return document.Statements[1].Init
			},
			mutate: func(call *KIRExpr) { call.Receiver.Type = "Int" },
			want:   `has receiver type "Int", incompatible with "Box[T]"`,
		},
		{
			name: "receiver generic constraint",
			selectCall: func(document *KIRDocument) *KIRExpr {
				return document.Statements[1].Init
			},
			mutate: func(call *KIRExpr) { call.Receiver.Type = "Box[FFIBuffer]" },
			want:   `type argument 1 ("FFIBuffer") does not satisfy constraint "Copy"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, `struct Box[T: Copy] { value: T }
impl Box[T] {
    fn get() -> T { return self.value }
    fn echo[U: Copy](value: U) -> U { return value }
}
let box: Box[Int] = Box[Int]{value: 1}
let value: Int = box.get()
let echoed: Int = box.echo(2)
`)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			call := test.selectCall(&document)
			test.mutate(call)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRAcceptsConstrainedGenericReceiverMethods(t *testing.T) {
	program, checker := testProgram(t, `struct Number[T: Numeric] { value: T }
impl Number[T] {
    fn add(other: T) -> T { return self.value + other }
}
fn plus[T: Numeric](left: T, right: T) -> T {
    let number: Number[T] = Number[T]{value: left}
    return number.add(right)
}
let result: Int = plus(1, 2)
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeKIR(data, DefaultLimits()); err != nil {
		t.Fatalf("KIR rejected a checked constrained generic receiver method: %v", err)
	}
}

func TestKIRRejectsInvalidGenericReceiverConstraint(t *testing.T) {
	program, checker := testProgram(t, `struct Box[T: Copy] { value: T }
impl Box[T] {
    fn get() -> T { return self.value }
}
let box: Box[Int] = Box[Int]{value: 1}
let value: Int = box.get()
`)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document.Functions[0].Receiver = "Box[FFIBuffer]"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), `type argument 1 ("FFIBuffer") does not satisfy constraint "Copy"`) {
		t.Fatalf("expected KIR rejection for an invalid generic receiver constraint, got %v", err)
	}
}

func TestKIRRejectsGenericFunctionValueMismatches(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*KIRExpr)
		want   string
	}{
		{
			name: "constraint",
			mutate: func(value *KIRExpr) {
				value.GenericArguments[0] = "String"
				value.Type = "fn(String) -> String"
			},
			want: `does not satisfy constraint "Numeric"`,
		},
		{
			name:   "signature",
			mutate: func(value *KIRExpr) { value.Type = "fn(String) -> String" },
			want:   `does not match target signature "fn(Int) -> Int"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, `fn identity[T: Numeric](value: T) -> T { return value }
let callback: fn(Int) -> Int = identity
`)
			data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			var document KIRDocument
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			value := document.Statements[0].Init
			if value == nil || value.Kind != "var" || len(value.GenericArguments) != 1 {
				t.Fatalf("expected an instantiated generic function value, got %#v", value)
			}
			test.mutate(value)
			if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected KIR rejection containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestKIRTypeSubstitutionUsesOneInstantiationMap(t *testing.T) {
	got := substituteKIRType("fn(T, U) -> Map[T, U]", map[string]string{"T": "U", "U": "T"})
	if want := "fn(U, T) -> Map[U, T]"; got != want {
		t.Fatalf("simultaneous generic substitution = %q, want %q", got, want)
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
			name: "literal checked type mismatch",
			want: `int literal has checked type "String", want "Int"`,
			mutate: func(doc *KIRDocument) {
				doc.Statements[0].Init.Type = "String"
			},
		},
		{
			name: "non-bool if condition",
			want: `if condition has checked type "Int", want "Bool"`,
			mutate: func(doc *KIRDocument) {
				doc.Statements[0] = &KIRStmt{
					Kind:   "if",
					Source: doc.Source,
					Line:   1,
					Column: 1,
					Cond:   &KIRExpr{Kind: "var", Source: doc.Source, Line: 1, Column: 1, Type: "Int", Name: "condition", Binding: &KIRBinding{Name: "condition", Type: "Int", Source: doc.Source, Line: 1, Column: 1}},
				}
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
			name: "artifact byte limit",
			want: "configured input limit",
			limits: func(limits *Limits, encoded []byte) {
				limits.MaxArtifactBytes = len(encoded) - 1
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
