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
