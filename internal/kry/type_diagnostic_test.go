package kry

import (
	"strings"
	"testing"
)

func TestNonExhaustiveEnumDiagnosticListsMissingVariants(t *testing.T) {
	source := `enum Color { Red, Green, Blue }
let color: Color = Color::Red
match color {
    Color::Red => {}
}
`
	program, diagnostic := Parse(&Source{Name: "enum-diagnostic.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "non-exhaustive match for Color; missing variants: Green, Blue") {
		t.Fatalf("expected the missing enum variants in the diagnostic, got %#v", diagnostic)
	}
}

func TestUnmatchedOverloadDiagnosticListsCandidates(t *testing.T) {
	source := `fn parse(value: Int) -> String { return "integer" }
fn parse(value: Bool) -> String { return "boolean" }
let result: String = parse("text")
`
	program, diagnostic := Parse(&Source{Name: "overload-diagnostic.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "candidates: parse(Bool) -> String; parse(Int) -> String") {
		t.Fatalf("expected candidate signatures in the diagnostic, got %#v", diagnostic)
	}
}
