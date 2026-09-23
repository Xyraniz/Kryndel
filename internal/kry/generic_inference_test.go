package kry

import (
	"strings"
	"testing"
)

func TestNestedGenericInferenceAndContextualReturnInference(t *testing.T) {
	source := `
fn preserve[T: Copy](value: Option[Array[T]]) -> Option[Array[T]] { return value }
fn relay[U: Copy](value: Option[Array[U]]) -> Option[Array[U]] { return preserve(value) }
fn box[T: Copy](value: T) -> Option[Array[T]] {
    let values: Array[T] = [value]
    return some(values)
}
fn nothing[T: Copy]() -> Option[T] { return none() }
let nested: Option[Array[Int]] = relay(some([1, 2]))
let boxed: Option[Array[Int]] = box(3)
let empty: Option[Int] = nothing()
`
	p, d := Parse(&Source{Name: "nested-generics.kry", Text: source}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatalf("nested generic inference failed: %s", d.Message)
	}
}

func TestGenericInferenceRejectsConflictsAndConstraintViolations(t *testing.T) {
	cases := []struct {
		name, source string
	}{
		{
			name:   "conflicting nested arguments",
			source: `fn inspect[T: Copy](a: Array[T], b: Option[T]) -> Nil { return nil }` + "\n" + `inspect([1], some("wrong"))`,
		},
		{
			name:   "incompatible constraint",
			source: `fn add[T: Numeric](value: T) -> T { return value }` + "\n" + `let result: String = add("not numeric")`,
		},
		{
			name:   "unresolved type parameter",
			source: `fn create[T: Copy]() -> Option[T] { return none() }` + "\n" + `create()`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, d := Parse(&Source{Name: "generic-error.kry", Text: tc.source}, DefaultLimits())
			if d != nil {
				t.Fatalf("parse: %s", d.Message)
			}
			if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, "no overload") {
				t.Fatalf("expected generic rejection, got %#v", d)
			}
		})
	}
}

func TestGenericTypeDepthLimitAppliesDuringInference(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxTypeDepth = 2
	p, d := Parse(&Source{Name: "deep-generic.kry", Text: `fn use[T: Copy](value: Option[Array[Option[T]]]) -> Nil { return nil }`}, lim)
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	if _, d = Check(p, lim); d == nil || !strings.Contains(d.Message, "type depth limit") {
		t.Fatalf("expected type depth resource diagnostic, got %#v", d)
	}
}
