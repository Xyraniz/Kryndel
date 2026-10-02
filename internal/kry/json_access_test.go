package kry

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestKryndelCanTraverseJSONWithoutLosingUInt64(t *testing.T) {
	src := `
fn main() -> Result[Json, String] {
    let doc: Json = json_parse("{\"kind\":\"array\",\"items\":[42,\"ok\"],\"big\":18446744073709551615,\"flag\":true,\"nothing\":null}")?
    assert_eq(json_kind(doc), "object")
    assert_eq(is_ok(json_object_get(doc, "items")), true)
    assert_eq(is_ok(json_array_len(doc)), false)
    assert_eq(is_ok(json_uint(doc)), false)
    assert_eq(is_ok(json_bool(doc)), false)
    assert_eq(json_is_null(doc), false)
    let kind: String = result_unwrap(json_string(result_unwrap(json_object_get(doc, "kind"))))
    assert_eq(kind, "array")
    return ok(doc)
}
main()
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("JSON traversal failed: %s", d.Message)
	}
}

func TestJSONAccessorsReturnTypedErrors(t *testing.T) {
	src := `
fn main() -> Result[Json, String] {
    let value: Json = json_parse("[1]")?
    assert_eq(is_err(json_object_get(value, "missing")), true)
    assert_eq(is_err(json_int(value)), true)
    assert_eq(is_err(json_array_get(value, 2)), true)
    return ok(value)
}
main()
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("typed JSON errors failed: %s", d.Message)
	}
}

func TestJSONNestedAccessSerializesOnlyWhenObserved(t *testing.T) {
	src := `
fn main() -> Result[Nil, String] {
    let document: Json = json_parse("{\"nested\":{\"values\":[1,{\"ok\":true}]}}")?
    let nested: Json = result_unwrap(json_object_get(document, "nested"))
    let values: Json = result_unwrap(json_object_get(nested, "values"))
    let second: Json = result_unwrap(json_array_get(values, 1))
    assert_eq(json_stringify(nested), "{\"values\":[1,{\"ok\":true}]}")
    assert_eq(str(second), "{\"ok\":true}")
    assert_eq(second, json_parse("{\"ok\":true}")?)
    return ok(nil)
}
main()
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("nested JSON access changed serialization, display, or equality: %s", d.Message)
	}
}

func TestJSONObjectKeysAreSortedAndRejectNonObjects(t *testing.T) {
	src := `
fn main() -> Result[Nil, String] {
    let value: Json = json_parse("{\"z\":1,\"é\":2,\"a\":3,\"💩\":4,\"z\":5}")?
    let keys: Array[String] = json_object_keys(value)?
    assert_eq(unwrap_or(array_get(keys, 0), ""), "a")
    assert_eq(unwrap_or(array_get(keys, 1), ""), "z")
    assert_eq(unwrap_or(array_get(keys, 2), ""), "é")
    assert_eq(unwrap_or(array_get(keys, 3), ""), "💩")
    assert_eq(is_err(json_object_keys(json_parse("[]")?)), true)
    return ok(nil)
}
main()
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("JSON object keys failed: %s", d.Message)
	}
}

func TestJSONObjectKeysCheckerRequiresJson(t *testing.T) {
	source := `fn invalid(value: String) -> Result[Array[String], String] {
    return json_object_keys(value)
}
fn main() -> Nil { return nil }
`
	program, diagnostic := Parse(&Source{Name: "json-object-keys-type.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "json_object_keys expects Json") {
		t.Fatalf("expected Json parameter diagnostic, got %#v", diagnostic)
	}
}
func TestJSONObjectKeysRespectArrayElementLimit(t *testing.T) {
	src := `
fn main() -> Result[Nil, String] {
    let within: Json = json_parse("{\"f\":1,\"e\":2,\"d\":3,\"c\":4,\"b\":5,\"a\":6}")?
    assert_eq(is_ok(json_object_keys(within)), true)
    let over: Json = json_parse("{\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5,\"f\":6,\"g\":7}")?
    assert_eq(is_err(json_object_keys(over)), true)
    assert_eq(unwrap_or(result_error(json_object_keys(over)), "missing error"), "JSON object key count exceeds configured limit")
    return ok(nil)
}
main()
	`
	p, c := testProgram(t, src)
	limits := DefaultLimits()
	limits.MaxArrayElements = 6
	r, d := NewRuntime(p, c, limits, Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("JSON object key limit was not enforced: %s", d.Message)
	}
}

func TestJSONObjectKeysAllowed(t *testing.T) {
	source := `
fn main() -> Result[Nil, String] {
    let object: Json = json_parse("{\"kind\":\"binary\",\"left\":1,\"right\":2}")?
    assert_eq(json_object_keys_allowed(object, "|kind|left|right|"), true)
    assert_eq(json_object_keys_allowed(object, "|kind|left|"), false)
    assert_eq(json_object_keys_allowed(object, "kind|left|right|"), false)
    assert_eq(json_object_keys_allowed(object, "|kind||left|right|"), false)
    assert_eq(json_object_keys_allowed(json_parse("{}")?, ""), true)
    assert_eq(json_object_keys_allowed(json_parse("{\"x\":1}")?, ""), false)
    assert_eq(json_object_keys_allowed(json_parse("[]")?, "|length|"), false)
    assert_eq(json_object_keys_allowed(json_parse("{\"é\":1}")?, "|é|"), true)
    return ok(nil)
}
main()
`
	p, c := testProgram(t, source)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("JSON key allowlist validation failed: %s", d.Message)
	}
}

func TestJSONObjectKeysAllowedCheckerRequiresJsonAndString(t *testing.T) {
	source := `fn invalid(value: String) -> Bool { return json_object_keys_allowed(value, "|key|") }
fn main() -> Nil { return nil }
`
	program, diagnostic := Parse(&Source{Name: "json-object-keys-allowed-type.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "json_object_keys_allowed expects Json and String") {
		t.Fatalf("expected Json parameter diagnostic, got %#v", diagnostic)
	}
}

func TestJSONObjectKeysAllowedKIRExpressionShapeCorpus(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(`{"kind":"call","source":"selfhost/source_kir_compiler.kry","line":81,"column":12,"type":"Result[Json,String]","name":"json_parse","call_target":"builtin:json_parse","builtin_id":"json_parse","args":[],"items":[],"left":null,"right":null,"operand":null,"base":null,"receiver":null,"callee":null,"lambda":null,"map_keys":[],"values":[]}`), &raw); err != nil {
		t.Fatal(err)
	}
	allowed := "|kind|source|line|column|type|name|call_target|builtin_id|args|items|left|right|operand|base|receiver|callee|lambda|map_keys|values|"
	if !jsonObjectKeysAllowed(raw, allowed) {
		t.Fatal("valid compiler KIR expression keys were rejected")
	}
	raw["unknown_extension"] = true
	if jsonObjectKeysAllowed(raw, allowed) {
		t.Fatal("unknown compiler KIR expression key was accepted")
	}
}

func TestJSONObjectFieldsEmptyExcept(t *testing.T) {
	source := `
fn main() -> Result[Nil, String] {
    let object: Json = json_parse("{\"kind\":\"int\",\"left\":null,\"args\":[],\"items\":[]}")?
    assert_eq(json_object_fields_empty_except(object, "|left|args|items|", "|kind|", "|args|items|"), true)
    assert_eq(json_object_fields_empty_except(object, "|left|args|items|", "|kind|", "|args|"), false)
    let nonempty_child: Json = json_parse("{\"kind\":\"int\",\"left\":1}")?
    assert_eq(json_object_fields_empty_except(nonempty_child, "|left|", "|kind|", ""), false)
    let null_child: Json = json_parse("{\"kind\":\"int\",\"left\":null}")?
    assert_eq(json_object_fields_empty_except(null_child, "|left|", "|kind|", ""), true)
    assert_eq(json_object_fields_empty_except(json_parse("[]")?, "|left|", "|kind|", ""), false)
    assert_eq(json_object_fields_empty_except(object, "|left|args|items|", "kind", "|args|items|"), false)
    return ok(nil)
}
main()
`
	p, c := testProgram(t, source)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("JSON empty-field checks failed: %s", d.Message)
	}
}

func TestJSONParseUsesDedicatedDocumentLimit(t *testing.T) {
	src := `
fn main() -> Nil {
    let parsed: Result[Json, String] = json_parse("{\"a\":1}")
    assert_eq(is_err(parsed), true)
    assert_eq(unwrap_or(result_error(parsed), "missing error"), "JSON input exceeds configured limit")
    return nil
}
main()
`
	p, c := testProgram(t, src)
	limits := DefaultLimits()
	limits.MaxJSONBytes = 4
	r, d := NewRuntime(p, c, limits, Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("JSON document limit was not reported explicitly: %s", d.Message)
	}
}

func TestResultErrorReadsFailuresWithoutUnwrapping(t *testing.T) {
	src := `
fn main() -> Result[Nil, String] {
    let failure: Result[Int, String] = err("not found")
    let success: Result[Int, String] = ok(7)
    assert_eq(is_some(result_error(failure)), true)
    assert_eq(unwrap_or(result_error(failure), "fallback"), "not found")
    assert_eq(is_none(result_error(success)), true)
    return ok(nil)
}
main()
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("result_error failed: %s", d.Message)
	}
}

func TestArraySetCopiesAndChecksBounds(t *testing.T) {
	src := `
fn main() -> Result[Array[Int], String] {
    let original: Array[Int] = [1, 2, 3]
    let replaced: Array[Int] = result_unwrap(array_set(original, 1, 9))
    assert_eq(unwrap_or(array_get(original, 1), 0), 2)
    assert_eq(unwrap_or(array_get(replaced, 1), 0), 9)
    assert_eq(is_err(array_set(original, 3, 9)), true)
    return ok(replaced)
}
main()
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("array_set failed: %s", d.Message)
	}
}

func TestJSONNumberHelpersPreserveUInt64Precision(t *testing.T) {
	raw, err := decodeJSONNode("18446744073709551615")
	if err != nil {
		t.Fatal(err)
	}
	n, ok := raw.(json.Number)
	if !ok || n.String() != "18446744073709551615" {
		t.Fatalf("large JSON number was changed: %#v", raw)
	}
	got, err := strconv.ParseUint(n.String(), 10, 64)
	if err != nil || got != ^uint64(0) {
		t.Fatalf("large JSON number did not round-trip as UInt64: %d %v", got, err)
	}
}
