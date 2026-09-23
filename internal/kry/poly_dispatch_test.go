package kry

import (
	"strings"
	"testing"
)

func TestPolyHandlersRequireUnambiguousStaticStringFunctions(t *testing.T) {
	valid := `
fn prefix(value: String) -> String { return "prefix:" + value }
fn suffix(value: String) -> String { return value + ":suffix" }
let a: Result[Nil, String] = poly_register("format", "prefix", 10)
let b: Result[Nil, String] = poly_register("format", "suffix", 5)
let moved: Result[Nil, String] = poly_reorder("format", "suffix", "prefix")
`
	p, d := Parse(&Source{Name: "handlers.kry", Text: valid}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatalf("valid static handlers were rejected: %s", d.Message)
	}

	cases := []struct {
		name, source, want string
	}{
		{
			name:   "unknown handler",
			source: `let registered = poly_register("format", "missing", 1)`,
			want:   "literal name of one top-level fn(String) -> String handler",
		},
		{
			name:   "wrong signature",
			source: `fn handler(value: Int) -> String { return str(value) }` + "\n" + `let registered = poly_register("format", "handler", 1)`,
			want:   "literal name of one top-level fn(String) -> String handler",
		},
		{
			name:   "overloaded name",
			source: `fn handler(value: String) -> String { return value }` + "\n" + `fn handler(value: Int) -> String { return str(value) }` + "\n" + `let registered = poly_register("format", "handler", 1)`,
			want:   "literal name of one top-level fn(String) -> String handler",
		},
		{
			name:   "dynamic handler name",
			source: `let name: String = "handler"` + "\n" + `fn handler(value: String) -> String { return value }` + "\n" + `let registered = poly_register("format", name, 1)`,
			want:   "literal name of one top-level fn(String) -> String handler",
		},
		{
			name:   "dynamic reorder names",
			source: `fn handler(value: String) -> String { return value }` + "\n" + `let name: String = "handler"` + "\n" + `let moved = poly_reorder("format", name, "handler")`,
			want:   "literal names of top-level fn(String) -> String handlers",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, d := Parse(&Source{Name: "bad-handler.kry", Text: tc.source}, DefaultLimits())
			if d != nil {
				t.Fatal(d.Message)
			}
			if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, tc.want) {
				t.Fatalf("expected static handler diagnostic containing %q, got %#v", tc.want, d)
			}
		})
	}
}
