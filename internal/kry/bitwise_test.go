package kry

import (
	"strings"
	"testing"
)

func TestUnsignedBitwiseAndWrappingArithmetic(t *testing.T) {
	src := `
let a: UInt8 = u8(255)
let b: UInt8 = u8(1)
assert_eq(a + b, u8(0))
assert_eq(a - b, u8(254))
assert_eq(a * b, u8(255))
assert_eq((u8(1) << 7), u8(128))
assert_eq((u8(128) >> 7), u8(1))
assert_eq((u8(170) & u8(15)), u8(10))
assert_eq((u8(170) | u8(15)), u8(175))
assert_eq((u8(170) ^ u8(15)), u8(165))
assert_eq(~u8(0), u8(255))
assert_eq(u8_array(bytes_from_u8([u8(65), u8(255)]))[1], u8(255))
let values: Set[UInt8] = |{u8(1), u8(1)}|
assert_eq(len(values), 1)
`
	p, c := testProgram(t, src)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("unsigned runtime failed: %s", d.Message)
	}
}

func TestSignedBitwiseIsRejected(t *testing.T) {
	p, d := Parse(&Source{Name: "signed-bitwise.kry", Text: "let bad: Int = 1 & 1\n"}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, "bitwise") {
		t.Fatalf("expected a clear bitwise type error, got %#v", d)
	}
}

func TestUnsignedShiftRangeIsRuntimeChecked(t *testing.T) {
	p, d := Parse(&Source{Name: "bad-shift.kry", Text: "let bad: UInt8 = u8(1) << 8\n"}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d == nil || !strings.Contains(d.Message, "shift count") {
		t.Fatalf("expected shift count failure, got %#v", d)
	}
}

func TestUnsignedTypeNamesAndConversions(t *testing.T) {
	for _, tc := range []struct {
		name string
		want *Type
	}{
		{"UInt8", TUInt8},
		{"UInt16", TUInt16},
		{"UInt32", TUInt32},
		{"UInt64", TUInt64},
	} {
		src := "let value: " + tc.name + " = u" + strings.TrimPrefix(tc.name, "UInt") + "(1)\n"
		p, d := Parse(&Source{Name: tc.name + ".kry", Text: src}, DefaultLimits())
		if d != nil {
			t.Fatal(d.Message)
		}
		c, d := Check(p, DefaultLimits())
		if d != nil {
			t.Fatal(d.Message)
		}
		if got := p.Statements[0].Init.Type; !typeEqual(got, tc.want) {
			t.Fatalf("%s resolved as %s, want %s", tc.name, got, tc.want)
		}
		_ = c
	}
}
