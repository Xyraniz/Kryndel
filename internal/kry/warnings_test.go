package kry

import (
	"strings"
	"testing"
)

func TestAnalyzeWarningsForBranchesLoopsMatchesAndUnreachableCode(t *testing.T) {
	source := `
enum Color { Red, Blue }
fn choose() -> Int {
    if true { return 1 } else { return 2 }
    println("unreachable")
}
fn spin() -> Nil {
    while true { continue }
}
let color: Color = Color::Red
match color {
    Color::Red => { println("red") }
    _ => { println("other") }
}
`
	p, d := Parse(&Source{Name: "warnings.kry", Text: source}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatal(d.Message)
	}
	warnings := AnalyzeWarnings(p)
	got := map[string]bool{}
	for _, warning := range warnings {
		if warning.Severity != "warning" || warning.Source != "warnings.kry" {
			t.Fatalf("warning fields are not stable: %#v", warning)
		}
		got[warning.Code] = true
	}
	for _, code := range []string{WarnRedundantMatch, WarnConstantBranch, WarnInfiniteLoop, WarnUnreachable} {
		if !got[code] {
			t.Errorf("missing warning %s in %#v", code, warnings)
		}
	}
	if !strings.Contains(warnings[0].Format(false), "warning[") {
		t.Fatalf("text formatter did not use warning severity: %q", warnings[0].Format(false))
	}
}

func TestAnalyzeWarningsDoesNotWarnForConstantFalseWhenNoLoopOrUnreachable(t *testing.T) {
	p, d := Parse(&Source{Name: "quiet.kry", Text: `fn read() -> Int { if false { return 0 } else { return 1 } }`}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatal(d.Message)
	}
	warnings := AnalyzeWarnings(p)
	if len(warnings) != 1 || warnings[0].Code != WarnConstantBranch {
		t.Fatalf("unexpected warnings: %#v", warnings)
	}
}
