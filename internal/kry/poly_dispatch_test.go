package kry

import (
	"path/filepath"
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

func TestPolyHandlersAcceptDynamicStringNames(t *testing.T) {
	source := `
fn alpha(value: String) -> String { return "alpha:" + value }
fn beta(value: String) -> String { return "beta:" + value }
let slot: String = "format"
let first: String = "alpha"
let second: String = "beta"
let registered_first = poly_register(slot, first, 10)
let registered_second = poly_register(slot, second, 5)
let moved = poly_reorder(slot, second, first)
let missing: String = "not_registered"
let rejected = poly_register(slot, missing, 1)
`
	p, d := Parse(&Source{Name: "dynamic-handlers.kry", Text: source}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("dynamic String handler names were rejected: %s", d.Message)
	}
	runtime, d := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = runtime.run(); d != nil {
		t.Fatalf("dynamic handler dispatch failed: %s", d.Message)
	}
	entries := runtime.Dispatch["format"]
	if len(entries) != 2 || entries[0].Handler != "beta" || entries[1].Handler != "alpha" {
		t.Fatalf("dynamic registration/reorder produced %#v", entries)
	}
	rejected, ok := runtime.Global.Values["rejected"]
	if !ok || rejected.Value.Kind != VResult || rejected.Value.OK {
		t.Fatalf("unknown dynamic handler should return an error Result, got %#v", rejected)
	}
}

func TestDispatchLibraryExampleRuns(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "dispatch_library.kry")
	p, d := LoadProgram(path, DefaultLimits(), "")
	if d != nil {
		t.Fatalf("dispatch library example did not load: %s", d.Message)
	}
	checker, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("dispatch library example did not check: %s", d.Message)
	}
	runtime, d := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = runtime.run(); d != nil {
		t.Fatalf("dispatch library example failed: %s", d.Message)
	}
	entries := runtime.Dispatch["render"]
	if len(entries) != 2 || entries[0].Handler != "text_handler" || entries[1].Handler != "json_handler" {
		t.Fatalf("dispatch library example produced %#v", entries)
	}
}
