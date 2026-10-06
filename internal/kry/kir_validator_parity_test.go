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
	var immutableTarget KIRDocument
	if err := json.Unmarshal(mutableSource, &immutableTarget); err != nil {
		t.Fatalf("decode immutable target validator KIR: %v", err)
	}
	immutableTargetAssignment := findKIRFunction(&immutableTarget, "main").Body[1]
	if immutableTargetAssignment.Target == nil || immutableTargetAssignment.Target.Binding == nil {
		t.Fatal("mutability validator assignment did not emit a resolved target binding")
	}
	immutableTargetAssignment.Target.Binding.Mutable = false
	immutableTargetBytes, err := json.Marshal(immutableTarget)
	if err != nil {
		t.Fatal(err)
	}
	constFlagMismatch := emitValidatorCorpusKIR(t, limits, "validator-const-flags.kry", `
fn main() -> Int {
    let value: Int = 1
    return value
}
`)
	var constFlagDocument KIRDocument
	if err := json.Unmarshal(constFlagMismatch, &constFlagDocument); err != nil {
		t.Fatalf("decode const flag validator KIR: %v", err)
	}
	findKIRFunction(&constFlagDocument, "main").Body[0].Const = true
	constFlagMismatch, err = json.Marshal(constFlagDocument)
	if err != nil {
		t.Fatal(err)
	}

	forSource := emitValidatorCorpusKIR(t, limits, "validator-for-types.kry", `
fn main() -> Nil {
    for item in [1] {}
}
`)
	forMissingFlagsBytes := omitMainStatementFields(t, forSource, "mutable", "const")
	var forBindingMismatch KIRDocument
	if err := json.Unmarshal(forSource, &forBindingMismatch); err != nil {
		t.Fatalf("decode for binding validator KIR: %v", err)
	}
	forStatement := findKIRFunction(&forBindingMismatch, "main").Body[0]
	if forStatement.Kind != "for" || forStatement.Binding == nil || forStatement.Iter == nil {
		t.Fatal("for validator source did not emit the expected checked loop")
	}
	forStatement.Binding.Type = "String"
	forBindingMismatchBytes, err := json.Marshal(forBindingMismatch)
	if err != nil {
		t.Fatal(err)
	}
	var forConstMismatch KIRDocument
	if err := json.Unmarshal(forSource, &forConstMismatch); err != nil {
		t.Fatalf("decode for flags validator KIR: %v", err)
	}
	findKIRFunction(&forConstMismatch, "main").Body[0].Const = true
	forConstMismatchBytes, err := json.Marshal(forConstMismatch)
	if err != nil {
		t.Fatal(err)
	}
	var nonIterableFor KIRDocument
	if err := json.Unmarshal(forSource, &nonIterableFor); err != nil {
		t.Fatalf("decode for iterator validator KIR: %v", err)
	}
	loop := findKIRFunction(&nonIterableFor, "main").Body[0]
	loop.Iter = &KIRExpr{Kind: "int", Source: loop.Iter.Source, Line: loop.Iter.Line, Column: loop.Iter.Column, Type: "Int", Const: &KIRValue{Kind: "int", Int: 1}, Int: 1}
	nonIterableForBytes, err := json.Marshal(nonIterableFor)
	if err != nil {
		t.Fatal(err)
	}
	var assignmentTypeMismatch KIRDocument
	if err := json.Unmarshal(mutableSource, &assignmentTypeMismatch); err != nil {
		t.Fatalf("decode assignment validator KIR: %v", err)
	}
	assignment := findKIRFunction(&assignmentTypeMismatch, "main").Body[1]
	assignment.Value = &KIRExpr{
		Kind: "string", Source: assignment.Value.Source, Line: assignment.Value.Line,
		Column: assignment.Value.Column, Type: "String", Const: &KIRValue{Kind: "string", String: "bad"}, String: "bad",
	}
	assignmentTypeMismatchBytes, err := json.Marshal(assignmentTypeMismatch)
	if err != nil {
		t.Fatal(err)
	}

	ifConditionTypeMismatch := nonBooleanConditionKIR(t, limits, "validator-if-condition.kry", `
fn main() -> Nil {
    let condition: Bool = true
    if condition { println("yes") }
}
`, "if")
	whileConditionTypeMismatch := nonBooleanConditionKIR(t, limits, "validator-while-condition.kry", `
fn main() -> Nil {
    let condition: Bool = true
    while condition { break }
}
`, "while")

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

	wrongReturnType := emitValidatorCorpusKIR(t, limits, "validator-return-type.kry", `
fn main() -> Int { return 1 }
`)
	var wrongReturnDocument KIRDocument
	if err := json.Unmarshal(wrongReturnType, &wrongReturnDocument); err != nil {
		t.Fatalf("decode return type validator KIR: %v", err)
	}
	findKIRFunction(&wrongReturnDocument, "main").Body[0].Return = &KIRExpr{
		Kind: "string", Source: "validator-return-type.kry", Line: 1, Column: 24,
		Type: "String", Const: &KIRValue{Kind: "string", String: "bad"}, String: "bad",
	}
	wrongReturnTypeBytes, err := json.Marshal(wrongReturnDocument)
	if err != nil {
		t.Fatal(err)
	}
	var missingIntReturn KIRDocument
	if err := json.Unmarshal(emitValidatorCorpusKIR(t, limits, "validator-missing-return.kry", `
fn main() -> Int { return 1 }
`), &missingIntReturn); err != nil {
		t.Fatalf("decode missing return validator KIR: %v", err)
	}
	findKIRFunction(&missingIntReturn, "main").Body[0].Return = nil
	missingIntReturnBytes, err := json.Marshal(missingIntReturn)
	if err != nil {
		t.Fatal(err)
	}
	validNilReturn := emitValidatorCorpusKIR(t, limits, "validator-nil-return.kry", `
fn main() -> Nil { return }
`)
	var topLevelReturn KIRDocument
	if err := json.Unmarshal(validNilReturn, &topLevelReturn); err != nil {
		t.Fatalf("decode top-level return validator KIR: %v", err)
	}
	topLevelReturn.Statements = append(topLevelReturn.Statements, &KIRStmt{Kind: "return", Source: topLevelReturn.Source, Line: 1, Column: 1})
	topLevelReturnBytes, err := json.Marshal(topLevelReturn)
	if err != nil {
		t.Fatal(err)
	}
	var breakOutsideLoop KIRDocument
	if err := json.Unmarshal(validNilReturn, &breakOutsideLoop); err != nil {
		t.Fatalf("decode loop control validator KIR: %v", err)
	}
	breakOutsideLoop.Statements = append(breakOutsideLoop.Statements, &KIRStmt{Kind: "break", Source: breakOutsideLoop.Source, Line: 1, Column: 1})
	breakOutsideLoopBytes, err := json.Marshal(breakOutsideLoop)
	if err != nil {
		t.Fatal(err)
	}
	continueOutsideLoop := cloneKIRDocument(t, breakOutsideLoop)
	continueOutsideLoop.Statements[len(continueOutsideLoop.Statements)-1].Kind = "continue"
	continueOutsideLoopBytes, err := json.Marshal(continueOutsideLoop)
	if err != nil {
		t.Fatal(err)
	}
	validBreak := emitValidatorCorpusKIR(t, limits, "validator-valid-break.kry", `
fn main() -> Nil {
    while true { break }
}
`)
	validContinue := emitValidatorCorpusKIR(t, limits, "validator-valid-continue.kry", `
fn main() -> Nil {
    for item in [1] { continue }
}
`)
	validPropagationReturn := emitValidatorCorpusKIR(t, limits, "validator-propagation-return.kry", `
fn unwrap_result(input: Result[Int, String]) -> Result[Int, String] {
    return input?
}
`)
	var incompatiblePropagationDocument KIRDocument
	if err := json.Unmarshal(validPropagationReturn, &incompatiblePropagationDocument); err != nil {
		t.Fatalf("decode propagation return validator KIR: %v", err)
	}
	incompatiblePropagationDocument.Functions[0].Return = "Result[String, String]"
	incompatiblePropagationBytes, err := json.Marshal(incompatiblePropagationDocument)
	if err != nil {
		t.Fatal(err)
	}
	validOptionPropagationReturn := emitValidatorCorpusKIR(t, limits, "validator-option-propagation-return.kry", `
fn unwrap_option(input: Option[Int]) -> Option[Int] {
    return input?
}
`)
	fieldAccessSource := emitValidatorCorpusKIR(t, limits, "validator-field-access.kry", `
struct Point { x: Int }
fn main() -> Int {
    let point: Point = Point { x: 1 }
    return point.x
}
`)
	var unknownFieldAccess KIRDocument
	if err := json.Unmarshal(fieldAccessSource, &unknownFieldAccess); err != nil {
		t.Fatalf("decode field-access validator KIR: %v", err)
	}
	fieldReturn := findKIRFunction(&unknownFieldAccess, "main").Body[1].Return
	if fieldReturn == nil || fieldReturn.Kind != "field" {
		t.Fatal("field-access source did not emit a field expression return")
	}
	fieldReturn.Field = "ghost"
	unknownFieldAccessBytes, err := json.Marshal(unknownFieldAccess)
	if err != nil {
		t.Fatal(err)
	}
	wrongFieldType := cloneKIRDocument(t, unknownFieldAccess)
	findKIRFunction(wrongFieldType, "main").Body[1].Return.Field = "x"
	findKIRFunction(wrongFieldType, "main").Body[1].Return.Type = "String"
	wrongFieldTypeBytes, err := json.Marshal(wrongFieldType)
	if err != nil {
		t.Fatal(err)
	}
	validGenericFieldAccess := emitValidatorCorpusKIR(t, limits, "validator-generic-field-access.kry", `
struct Box[T] { value: T }
fn main() -> Int {
    let item: Box[Int] = Box[Int] { value: 1 }
    return item.value
}
`)
	validNestedGenericFieldAccess := emitValidatorCorpusKIR(t, limits, "validator-nested-generic-field-access.kry", `
struct Wrap[T] { value: Option[T] }
fn unwrap_box(box: Wrap[Array[Int]]) -> Option[Array[Int]] {
    return box.value
}
`)
	validArrayIndex := emitValidatorCorpusKIR(t, limits, "validator-array-index.kry", `
fn first(values: Array[Int]) -> Int {
    return values[0]
}
`)
	var wrongIndexResult KIRDocument
	if err := json.Unmarshal(validArrayIndex, &wrongIndexResult); err != nil {
		t.Fatalf("decode array-index validator KIR: %v", err)
	}
	arrayIndex := findKIRFunction(&wrongIndexResult, "first").Body[0].Return
	if arrayIndex == nil || arrayIndex.Kind != "index" {
		t.Fatal("array-index source did not emit an index expression return")
	}
	arrayIndex.Type = "String"
	wrongIndexResultBytes, err := json.Marshal(wrongIndexResult)
	if err != nil {
		t.Fatal(err)
	}
	var validArrayIndexDocument KIRDocument
	if err := json.Unmarshal(validArrayIndex, &validArrayIndexDocument); err != nil {
		t.Fatalf("decode array-index validator KIR: %v", err)
	}
	nonIndexableBase := cloneKIRDocument(t, validArrayIndexDocument)
	nonIndexableIndex := findKIRFunction(nonIndexableBase, "first").Body[0].Return
	integerLiteral := *nonIndexableIndex.Left
	nonIndexableIndex.Base = &integerLiteral
	nonIndexableBaseBytes, err := json.Marshal(nonIndexableBase)
	if err != nil {
		t.Fatal(err)
	}
	validMapIndex := emitValidatorCorpusKIR(t, limits, "validator-map-index.kry", `
fn lookup(values: Map[String, Int], key: String) -> Int {
    return values[key]
}
`)
	validStringIndex := emitValidatorCorpusKIR(t, limits, "validator-string-index.kry", `
fn first(text: String) -> String {
    return text[0]
}
`)
	validBytesIndex := emitValidatorCorpusKIR(t, limits, "validator-bytes-index.kry", `
fn first(data: Bytes) -> Int {
    return data[0]
}
`)
	validBuiltinArity := emitValidatorCorpusKIR(t, limits, "validator-builtin-arity.kry", `
fn text_length(text: String) -> Int {
    return len(text)
}
`)
	var wrongBuiltinArity KIRDocument
	if err := json.Unmarshal(validBuiltinArity, &wrongBuiltinArity); err != nil {
		t.Fatalf("decode builtin-arity validator KIR: %v", err)
	}
	lenCall := findKIRFunction(&wrongBuiltinArity, "text_length").Body[0].Return
	if lenCall == nil || lenCall.CallTarget != "builtin:len" {
		t.Fatal("builtin-arity source did not emit the len call")
	}
	lenCall.Args = nil
	wrongBuiltinArityBytes, err := json.Marshal(wrongBuiltinArity)
	if err != nil {
		t.Fatal(err)
	}

	var missingV6Span KIRDocument
	if err := json.Unmarshal(valid, &missingV6Span); err != nil {
		t.Fatalf("decode KIR v6 span fixture: %v", err)
	}
	findKIRFunction(&missingV6Span, "main").Span = nil
	missingV6SpanBytes, err := json.Marshal(missingV6Span)
	if err != nil {
		t.Fatal(err)
	}
	var forgedV6BindingID KIRDocument
	if err := json.Unmarshal(mutableSource, &forgedV6BindingID); err != nil {
		t.Fatalf("decode KIR v6 binding fixture: %v", err)
	}
	findKIRFunction(&forgedV6BindingID, "main").Body[0].Binding.ID = "forged-binding-id"
	forgedV6BindingIDBytes, err := json.Marshal(forgedV6BindingID)
	if err != nil {
		t.Fatal(err)
	}
	foreignV6Source := cloneKIRDocument(t, base)
	findKIRFunction(foreignV6Source, "main").Body[0].Return.Source = "outside-source-table.kry"
	foreignV6SourceBytes, err := json.Marshal(foreignV6Source)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		encoded []byte
		accept  bool
	}{
		{name: "valid calls and arithmetic", encoded: valid, accept: true},
		{name: "KIR v6 missing function span", encoded: missingV6SpanBytes},
		{name: "KIR v6 forged binding ID", encoded: forgedV6BindingIDBytes},
		{name: "KIR v6 source outside source table", encoded: foreignV6SourceBytes},
		{name: "wrong operator result type", encoded: wrongResultBytes},
		{name: "dangling call target", encoded: wrongCallTargetBytes},
		{name: "wrong call arity", encoded: wrongCallArityBytes},
		{name: "unknown document field", encoded: unknownField},
		{name: "immutable assignment", encoded: immutableAssignmentBytes},
		{name: "immutable assignment target", encoded: immutableTargetBytes},
		{name: "inconsistent let const flag", encoded: constFlagMismatch},
		{name: "for binding type mismatch", encoded: forBindingMismatchBytes},
		{name: "for const flag", encoded: forConstMismatchBytes},
		{name: "non-iterable for expression", encoded: nonIterableForBytes},
		{name: "for omitted false flags", encoded: forMissingFlagsBytes, accept: true},
		{name: "wrong return type", encoded: wrongReturnTypeBytes},
		{name: "missing non-Nil return value", encoded: missingIntReturnBytes},
		{name: "top-level return", encoded: topLevelReturnBytes},
		{name: "break outside loop", encoded: breakOutsideLoopBytes},
		{name: "continue outside loop", encoded: continueOutsideLoopBytes},
		{name: "valid Nil return", encoded: validNilReturn, accept: true},
		{name: "valid break inside loop", encoded: validBreak, accept: true},
		{name: "valid continue inside loop", encoded: validContinue, accept: true},
		{name: "valid propagated return", encoded: validPropagationReturn, accept: true},
		{name: "incompatible propagated return", encoded: incompatiblePropagationBytes},
		{name: "valid Option propagated return", encoded: validOptionPropagationReturn, accept: true},
		{name: "unknown struct field read", encoded: unknownFieldAccessBytes},
		{name: "struct field read type mismatch", encoded: wrongFieldTypeBytes},
		{name: "valid generic struct field read", encoded: validGenericFieldAccess, accept: true},
		{name: "valid nested generic struct field read", encoded: validNestedGenericFieldAccess, accept: true},
		{name: "valid Array index", encoded: validArrayIndex, accept: true},
		{name: "index result type mismatch", encoded: wrongIndexResultBytes},
		{name: "non-indexable checked base", encoded: nonIndexableBaseBytes},
		{name: "valid Map index", encoded: validMapIndex, accept: true},
		{name: "valid String index", encoded: validStringIndex, accept: true},
		{name: "valid Bytes index", encoded: validBytesIndex, accept: true},
		{name: "valid builtin arity", encoded: validBuiltinArity, accept: true},
		{name: "builtin arity mismatch", encoded: wrongBuiltinArityBytes},
		{name: "assignment type mismatch", encoded: assignmentTypeMismatchBytes},
		{name: "non-Boolean if condition", encoded: ifConditionTypeMismatch},
		{name: "non-Boolean while condition", encoded: whileConditionTypeMismatch},
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
			got := output.String()
			selfhostAccepted := got == "accepted\n"
			if selfhostAccepted != test.accept {
			t.Fatalf("selfhost validator output = %q, selfhost accepted=%t, Go accepted=%t; want both validators to agree", got, selfhostAccepted, goAccepted)
			}
		})
	}
}

func omitMainStatementFields(t *testing.T, encoded []byte, fields ...string) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode KIR for omitted fields: %v", err)
	}
	functions, ok := document["functions"].([]any)
	if !ok {
		t.Fatal("KIR document has no function list")
	}
	for _, rawFunction := range functions {
		function, ok := rawFunction.(map[string]any)
		if !ok || function["name"] != "main" {
			continue
		}
		body, ok := function["body"].([]any)
		if !ok || len(body) == 0 {
			t.Fatal("main function has no statement for omitted-field fixture")
		}
		statement, ok := body[0].(map[string]any)
		if !ok || statement["kind"] != "for" {
			t.Fatal("main function does not start with a for statement")
		}
		for _, field := range fields {
			delete(statement, field)
		}
		malformed, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		return malformed
	}
	t.Fatal("main function not found in KIR document")
	return nil
}

func nonBooleanConditionKIR(t *testing.T, limits Limits, name, source, statementKind string) []byte {
	t.Helper()
	encoded := emitValidatorCorpusKIR(t, limits, name, source)
	var document KIRDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode %s validator KIR: %v", statementKind, err)
	}
	main := findKIRFunction(&document, "main")
	if main == nil || len(main.Body) != 2 || main.Body[0].Binding == nil || main.Body[0].Init == nil || main.Body[1].Kind != statementKind || main.Body[1].Cond == nil {
		t.Fatalf("%s validator source did not emit the expected checked condition", statementKind)
	}
	main.Body[0].Binding.Type = "Int"
	main.Body[0].Init = &KIRExpr{
		Kind: "int", Source: main.Body[0].Init.Source, Line: main.Body[0].Init.Line,
		Column: main.Body[0].Init.Column, Type: "Int", Const: &KIRValue{Kind: "int", Int: 1}, Int: 1,
	}
	main.Body[1].Cond.Type = "Int"
	if main.Body[1].Cond.Binding == nil {
		t.Fatalf("%s condition is not bound to its local declaration", statementKind)
	}
	main.Body[1].Cond.Binding.Type = "Int"
	malformed, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return malformed
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
