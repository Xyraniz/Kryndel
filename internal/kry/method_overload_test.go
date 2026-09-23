package kry

import (
	"strings"
	"testing"
)

func TestOverloadedMethodsResolveInCheckerKIRAndInterpreter(t *testing.T) {
	source := `
struct Counter { base: Int }
impl Counter {
    fn read(extra: Int) -> Int { return self.base + extra }
    fn read(extra: String) -> Int { return self.base + len(extra) }
}
let counter: Counter = Counter{base: 40}
assert_eq(counter.read(2), 42)
assert_eq(counter.read("xx"), 42)
`
	p, checker := testProgram(t, source)
	firstCall := p.Statements[1].Expr.Args[0]
	secondCall := p.Statements[2].Expr.Args[0]
	if firstCall.Function == nil || secondCall.Function == nil || firstCall.Function == secondCall.Function {
		t.Fatalf("checker did not preserve separate method overloads: first=%#v second=%#v", firstCall.Function, secondCall.Function)
	}
	if got := typeSpecString(firstCall.Function.Params[0].Type); got != "Int" {
		t.Fatalf("first call resolved to parameter type %q, want Int", got)
	}
	if got := typeSpecString(secondCall.Function.Params[0].Type); got != "String" {
		t.Fatalf("second call resolved to parameter type %q, want String", got)
	}

	kirBytes, err := EmitKIR(p, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeKIR(kirBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	firstTarget := doc.Statements[1].Expr.Args[0].CallTarget
	secondTarget := doc.Statements[2].Expr.Args[0].CallTarget
	if firstTarget == secondTarget || !strings.HasPrefix(firstTarget, "function:read@") || !strings.HasPrefix(secondTarget, "function:read@") {
		t.Fatalf("KIR did not preserve method overload targets: %q and %q", firstTarget, secondTarget)
	}

	runtimeValue, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if diagnostic = runtimeValue.run(); diagnostic != nil {
		t.Fatalf("interpreter did not execute the checker-selected methods: %s", diagnostic.Message)
	}
}

func TestOverloadedMethodRejectsUnmatchedArguments(t *testing.T) {
	source := `
struct Counter { base: Int }
impl Counter {
    fn read(extra: Int) -> Int { return self.base + extra }
    fn read(extra: String) -> Int { return self.base + len(extra) }
}
let counter: Counter = Counter{base: 40}
counter.read(true)
`
	p, diagnostic := Parse(&Source{Name: "method-overload.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic = Check(p, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "no overload of method 'read'") {
		t.Fatalf("expected no-matching-method-overload diagnostic, got %#v", diagnostic)
	}
}
