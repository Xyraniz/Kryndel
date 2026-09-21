package kry

import (
	"encoding/json"
	"strconv"
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
