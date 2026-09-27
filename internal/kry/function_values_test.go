package kry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runFunctionValueSource(t *testing.T, source string) (string, *Program, *Checker) {
	t.Helper()
	program, checker := testProgram(t, source)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic = runtime.run(); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	return output.String(), program, checker
}

const closureConformanceProgram = `
fn increment(value: Int) -> Int { return value + 1 }
fn apply(callback: fn(Int) -> Int, value: Int) -> Int { return callback(value) }
fn make_adder(amount: Int) -> fn(Int) -> Int {
    return fn(value: Int) -> Int { return value + amount }
}
fn make_counter(start: Int) -> fn(Int) -> Int {
    let mut count: Int = start
    return fn(step: Int) -> Int {
        count = count + step
        return count
    }
}
fn make_shadow() -> fn() -> Int {
    let selected: Int = 7
    let read_selected: fn() -> Int = fn() -> Int { return selected }
	if true {
        let selected: Int = 99
        let ignored: Int = selected
    }
    return read_selected
}
fn shadow_parameter(value: Int) -> fn(Int) -> Int {
    return fn(value: Int) -> Int { return value + 1 }
}
let named: fn(Int) -> Int = increment
let add_five: fn(Int) -> Int = make_adder(5)
let counter: fn(Int) -> Int = make_counter(3)
let shadowed: fn() -> Int = make_shadow()
let parameter_shadow: fn(Int) -> Int = shadow_parameter(100)
println(apply(named, 4))
println(apply(add_five, 7))
println(counter(2))
println(counter(4))
println(shadowed())
println(parameter_shadow(2))
`

func TestFunctionValuesClosuresAndKexeExecution(t *testing.T) {
	want := "5\n12\n5\n9\n7\n3\n"
	interpreted, _, _ := runFunctionValueSource(t, closureConformanceProgram)
	if interpreted != want {
		t.Fatalf("interpreter output = %q, want %q", interpreted, want)
	}

	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.kry")
	artifactPath := filepath.Join(root, "program.kexe")
	if err := os.WriteFile(sourcePath, []byte(closureConformanceProgram), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatalf("building .kexe with closures failed: %s", diagnostic.Message)
	}
	program, checker, diagnostic := engine.CheckPath(artifactPath)
	if diagnostic != nil {
		t.Fatalf("checking .kexe with closures failed: %s", diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic = runtime.run(); diagnostic != nil {
		t.Fatalf("executing .kexe with closures failed: %s", diagnostic.Message)
	}
	if output.String() != want {
		t.Fatalf(".kexe output = %q, want %q", output.String(), want)
	}
}

func TestDebuggerExposesClosureLocalsAndCaptures(t *testing.T) {
	source := strings.Join([]string{
		"let mut total: Int = 7",
		"let calculate: fn(Int) -> Int = fn(value: Int) -> Int {",
		"    let doubled: Int = value * 2",
		"    total = total + doubled",
		"    return total",
		"}",
		"let answer: Int = calculate(2)",
	}, "\n") + "\n"
	path := filepath.Join(t.TempDir(), "closure-debug.kry")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	var paused DebugState
	var hit bool
	diagnostic := NewEngine().DebugPathWithArgs(path, nil, Debugger{
		ShouldPause: func(location DebugLocation) bool {
			return location.Function == "<closure>" && location.Line == 5
		},
		OnPause: func(state DebugState) bool {
			paused = state
			hit = true
			return true
		},
	})
	if diagnostic != nil {
		t.Fatalf("debug execution failed: %s", diagnostic.Message)
	}
	if !hit {
		t.Fatal("debugger did not pause at the closure breakpoint")
	}
	if paused.Function != "<closure>" || len(paused.Stack) == 0 || paused.Stack[0].Function != "<closure>" {
		t.Fatalf("closure stack frame missing at pause: %#v", paused)
	}
	locals := make(map[string]DebugVariable, len(paused.Variables))
	for _, variable := range paused.Variables {
		locals[variable.Name] = variable
	}
	for name, want := range map[string]string{"value": "2", "doubled": "4", "total": "11"} {
		if got, ok := locals[name]; !ok || got.Value != want {
			t.Errorf("debugger local %q = %#v, want %q", name, got, want)
		}
	}
	if !locals["total"].Mutable {
		t.Error("captured mutable binding is not marked mutable in debugger locals")
	}
}

func TestKIRPreservesIndirectCallsAndLexicalCaptures(t *testing.T) {
	source := `
fn make_outer(amount: Int) -> fn(Int) -> fn(Int) -> Int {
    let outer: Int = amount
    return fn(value: Int) -> fn(Int) -> Int {
        let local: Int = value
        return fn(step: Int) -> Int { return outer + local + step }
    }
}
let build_inner: fn(Int) -> fn(Int) -> Int = make_outer(10)
let result: Int = build_inner(3)(2)
`
	program, checker := testProgram(t, source)
	data, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(data, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Statements) != 2 {
		t.Fatalf("unexpected top-level statement count: %d", len(document.Statements))
	}
	outer := document.Functions[0].Body[1].Return.Lambda
	if outer == nil || len(outer.Captures) != 1 || outer.Captures[0].Binding.Name != "outer" {
		t.Fatalf("outer lambda metadata is missing its propagated capture: %#v", outer)
	}
	inner := outer.Body[1].Return.Lambda
	if inner == nil || len(inner.Captures) != 2 {
		t.Fatalf("nested closure did not serialize both live captures: %#v", inner)
	}
	if inner.Captures[0].Binding.Name == inner.Captures[1].Binding.Name {
		t.Fatalf("nested closure capture metadata lost distinct lexical bindings: %#v", inner.Captures)
	}
	chainedCall := document.Statements[1].Init
	if chainedCall == nil || chainedCall.Callee == nil || chainedCall.Callee.Kind != "call" {
		t.Fatalf("call of a returned function value was not preserved: %#v", chainedCall)
	}
	if chainedCall.Callee.Callee == nil || chainedCall.Callee.Callee.Kind != "var" || chainedCall.Callee.Callee.Type != "fn(Int) -> fn(Int) -> Int" {
		t.Fatalf("function-valued call target was not preserved: %#v", chainedCall.Callee)
	}

	var mutated KIRDocument
	if err := json.Unmarshal(data, &mutated); err != nil {
		t.Fatal(err)
	}
	mutatedLambda := mutated.Functions[0].Body[1].Return.Lambda
	mutatedLambda.Captures = append(mutatedLambda.Captures, mutatedLambda.Captures[0])
	bad, err := json.Marshal(&mutated)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeKIR(bad, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "capture") {
		t.Fatalf("KIR validator accepted malformed capture metadata, got %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*KIRFunction)
		want   string
	}{
		{
			name: "missing propagated capture",
			mutate: func(function *KIRFunction) {
				function.Captures = nil
			},
			want: "missing capture",
		},
		{
			name: "capture mutability mismatch",
			mutate: func(function *KIRFunction) {
				function.Captures[0].Binding.Mutable = !function.Captures[0].Binding.Mutable
			},
			want: "does not match",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var altered KIRDocument
			if err := json.Unmarshal(data, &altered); err != nil {
				t.Fatal(err)
			}
			test.mutate(altered.Functions[0].Body[1].Return.Lambda)
			malformed, err := json.Marshal(&altered)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeKIR(malformed, DefaultLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("KIR validator accepted malformed capture metadata, got %v", err)
			}
		})
	}
}

func TestFunctionValueTypeAndArityErrors(t *testing.T) {
	cases := []struct {
		name, source, message string
	}{
		{
			name:    "indirect argument type",
			source:  "let callback: fn(Int) -> Int = fn(value: Int) -> Int { return value }\ncallback(\"wrong\")\n",
			message: "function value argument 1 expected Int, found String",
		},
		{
			name:    "indirect arity",
			source:  "let callback: fn(Int) -> Int = fn(value: Int) -> Int { return value }\ncallback()\n",
			message: "function value expects 1 argument(s), got 0",
		},
		{
			name:    "mismatched lambda signature",
			source:  "let callback: fn(Int) -> Int = fn(value: String) -> Int { return 1 }\n",
			message: "closure expected fn(Int) -> Int, found fn(String) -> Int",
		},
		{
			name:    "wrong higher-order function argument",
			source:  "fn invoke(callback: fn(Int) -> Int) -> Int { return callback(1) }\nfn text(value: String) -> Int { return 1 }\ninvoke(text)\n",
			message: "no overload of 'invoke' matches the argument types",
		},
		{
			name:    "function equality is undefined",
			source:  "fn identity(value: Int) -> Int { return value }\nlet callback: fn(Int) -> Int = identity\nlet same: Bool = callback == callback\n",
			message: "function values do not support equality",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			program, diagnostic := Parse(&Source{Name: "function-values.kry", Text: test.source}, DefaultLimits())
			if diagnostic != nil {
				t.Fatalf("parse failed before type checking: %s", diagnostic.Message)
			}
			if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, test.message) {
				t.Fatalf("expected diagnostic containing %q, got %#v", test.message, diagnostic)
			}
		})
	}
}

func TestFunctionValuesAreNotCopyOrConstValues(t *testing.T) {
	program, diagnostic := Parse(&Source{Name: "function-copy.kry", Text: `
let callback: fn(Int) -> Int = fn(value: Int) -> Int { return value }
let channel: Channel[fn(Int) -> Int] = thread_channel()
thread_try_send(channel, callback)
`}, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("parse failed: %s", diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "Copy") {
		t.Fatalf("expected captured functions to be rejected by Copy constraints, got %#v", diagnostic)
	}

	program, diagnostic = Parse(&Source{Name: "function-const.kry", Text: "const CALLBACK: fn(Int) -> Int = fn(value: Int) -> Int { return value }\n"}, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("const parse failed: %s", diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "compile-time expression") {
		t.Fatalf("expected function const rejection, got %#v", diagnostic)
	}
}

func TestNativeBackendsRejectFunctionValuesBeforeEmission(t *testing.T) {
	program, checker := testProgram(t, `
fn apply(callback: fn(Int) -> Int, value: Int) -> Int { return callback(value) }
let callback: fn(Int) -> Int = fn(value: Int) -> Int { return value + 1 }
let result: Int = apply(callback, 4)
`)
	for _, target := range []struct {
		format string
		target NativeTarget
	}{
		{format: "c", target: NativeTarget{OS: "linux", Arch: "amd64"}},
		{format: "elf-direct", target: NativeTarget{OS: "linux", Arch: "amd64"}},
		{format: "pe-direct", target: NativeTarget{OS: "windows", Arch: "amd64"}},
	} {
		t.Run(target.format, func(t *testing.T) {
			_, err := BuildNativeWithPolicyOpts(program, checker, target.target, target.format, false, true)
			if err == nil || !strings.Contains(err.Error(), "does not support function values or closures") {
				t.Fatalf("backend accepted or partially compiled closures: %v", err)
			}
		})
	}
}

func TestFunctionTypeFormatterRoundTrip(t *testing.T) {
	formatted, diagnostic := FormatSource(&Source{Name: "closure-format.kry", Text: `
fn make_adder(amount: Int) -> fn(Int) -> Int {
    return fn(value: Int) -> Int { return amount + value }
}
let add: fn(Int) -> Int = make_adder(1)
`}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	second, diagnostic := FormatSource(&Source{Name: "closure-format.kry", Text: formatted}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if formatted != second {
		t.Fatalf("closure formatting was not idempotent:\n%s\n---\n%s", formatted, second)
	}
}
