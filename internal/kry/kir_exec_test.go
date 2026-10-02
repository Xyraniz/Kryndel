package kry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func compareKIRExecutionWithRuntime(t *testing.T, source string, limits Limits) (*Program, *Checker, kirExecResult, *Diagnostic) {
	return compareKIRExecutionWithRuntimeAndSandbox(t, source, limits, Sandbox{})
}

func compareKIRExecutionWithRuntimeAndSandbox(t *testing.T, source string, limits Limits, sandbox Sandbox) (*Program, *Checker, kirExecResult, *Diagnostic) {
	t.Helper()
	program, diagnostic := Parse(&Source{Name: "kir-exec.kry", Text: source}, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	var astOutput bytes.Buffer
	astRuntime, diagnostic := newASTOracleRuntime(program, checker, limits, sandbox, nil)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	astRuntime.output = &astOutput
	astDiagnostic := astRuntime.run()

	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	kirResult, err := executeKIRSubset(document, limits, kirSourceMap(program), sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kirResult.Output, astOutput.Bytes()) {
		t.Fatalf("output differs: KIR %q, AST %q", kirResult.Output, astOutput.Bytes())
	}
	if !sameRuntimeDiagnostic(kirResult.Diagnostic, astDiagnostic) {
		t.Fatalf("diagnostic differs: KIR %#v, AST %#v", kirResult.Diagnostic, astDiagnostic)
	}
	return program, checker, kirResult, astDiagnostic
}

func sameRuntimeDiagnostic(left, right *Diagnostic) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Code == right.Code && left.Category == right.Category && left.Source == right.Source && left.Line == right.Line && left.Column == right.Column && left.Message == right.Message
}

func TestKIRExecutorMatchesRuntimeForScalarControlFlow(t *testing.T) {
	source := `
let mut index: Int = 0
let prefix: String = "item="
while index < 3 {
    print(prefix)
    println(index)
    index = index + 1
}
if index == 3 && true {
    println(str(40 + 2))
} else {
    println("wrong")
}
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if want := "item=0\nitem=1\nitem=2\n42\n"; string(result.Output) != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
}

func TestKIRExecutorMatchesRuntimeForGenericFunctionValues(t *testing.T) {
	source := `
fn identity[T: Copy](value: T) -> T { return value }
fn relay[U: Copy](value: U) -> U { return identity(value) }
println(relay(42))
println(relay("generic"))
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if want := "42\ngeneric\n"; string(result.Output) != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
}

func TestKIRExecutorMatchesRuntimeForPolyDispatch(t *testing.T) {
	source := `
fn prefix(value: String) -> String { return "prefix:" + value }
fn suffix(value: String) -> String { return value + ":suffix" }
let first: Result[Nil, String] = poly_register("format", "prefix", 10)
let second: Result[Nil, String] = poly_register("format", "suffix", 5)
let moved: Result[Nil, String] = poly_reorder("format", "suffix", "prefix")
println(result_unwrap(poly_dispatch("format", "value")))
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if want := "value:suffix\n"; string(result.Output) != want {
		t.Fatalf("poly dispatch output = %q, want %q", result.Output, want)
	}
}

func TestKIRExecutorMatchesRuntimeForWorkerSharedCaptures(t *testing.T) {
	source := `
let shared: Shared[Array[Int]] = shared_new([3, 5, 8])
fn values() -> Array[Int] { return shared_read(shared) }
let thread: Thread[Array[Int]] = thread_spawn("values")
println(thread_join(thread))
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if want := "[3, 5, 8]\n"; string(result.Output) != want {
		t.Fatalf("worker output = %q, want %q", result.Output, want)
	}
}

func TestKIRExecutorWorkerDispatchIsSynchronized(t *testing.T) {
	source := `
fn first(value: String) -> String { return "first:" + value }
fn second(value: String) -> String { return "second:" + value }
fn dispatch_worker() -> Int {
    let registered: Result[Nil, String] = poly_register("slot", "third", 20)
    let mut count: Int = 0
    while count < 10000 {
        let result: Result[String, String] = poly_dispatch("slot", "value")
        count = count + 1
    }
    return count
}
fn third(value: String) -> String { return "third:" + value }
let first_registered: Result[Nil, String] = poly_register("slot", "first", 10)
let second_registered: Result[Nil, String] = poly_register("slot", "second", 5)
let worker: Thread[Int] = thread_spawn("dispatch_worker")
let mut index: Int = 0
while index < 10000 {
    let reordered: Result[Nil, String] = poly_reorder("slot", "first", "second")
    let reordered_back: Result[Nil, String] = poly_reorder("slot", "second", "first")
    index = index + 1
}
println(thread_join(worker))
println(result_unwrap(poly_dispatch("slot", "value")))
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if want := "10000\nthird:value\n"; string(result.Output) != want {
		t.Fatalf("worker dispatch output = %q, want %q", result.Output, want)
	}
}

func TestKIRExecutorMatchesRuntimeForGenericStructsAndMethods(t *testing.T) {
	source := `
struct Box[T: Copy] { value: T }
impl Box[T] {
    fn get() -> T { return self.value }
    fn map[U: Copy](callback: fn(T) -> U) -> Box[U] {
        return Box[U]{value: callback(self.value)}
    }
}
fn make_box[T: Copy](value: T) -> Box[T] { return Box[T]{value: value} }
fn value_or[T: Copy](value: Option[T], fallback: T) -> T {
    match value {
        some(item) => { return item }
        none => { return fallback }
    }
}
let boxed: Box[Int] = make_box(13)
println(boxed.get())
let mapped: Box[String] = boxed.map(fn(value: Int) -> String { return "value=" + str(value) })
println(mapped.value)
println(value_or(some(17), 0))
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if want := "13\nvalue=13\n17\n"; string(result.Output) != want {
		t.Fatalf("output = %q, want %q", result.Output, want)
	}
}

func TestKIRExecutorMatchesRuntimeForStaticTraitDispatch(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "two concrete receivers through a generic bound", source: traitDispatchFixture, want: "north:one\nsouth:two\nnorth:one\nsouth:two\n"},
		{name: "generic struct implementations", source: traitGenericInstantiationFixture, want: "int:7\nstring:ok\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, test.source, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			if got := string(result.Output); got != test.want {
				t.Fatalf("trait dispatch output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestKIRDecodeUsesArtifactLimitWhileJSONBuiltinUsesPayloadLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxJSONBytes = 2
	source := `let parsed: Result[Json, String] = json_parse("{}")
match parsed {
    ok(value) => { println(json_stringify(value)) }
    err(problem) => { println(problem) }
}
`
	program, diagnostic := Parse(&Source{Name: "json-payload-limit.kry", Text: source}, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if len(kirBytes) <= limits.MaxJSONBytes {
		t.Fatalf("fixture KIR size %d must exceed JSON payload limit %d", len(kirBytes), limits.MaxJSONBytes)
	}
	document, err := DecodeKIR(kirBytes, limits)
	if err != nil {
		t.Fatalf("DecodeKIR applied JSON payload limit to KIR document: %v", err)
	}
	result, err := executeKIRSubset(document, limits, kirSourceMap(program))
	if err != nil {
		t.Fatal(err)
	}
	if result.Diagnostic != nil || string(result.Output) != "{}\n" {
		t.Fatalf("KIR JSON payload result = output %q, diagnostic %#v", result.Output, result.Diagnostic)
	}

	tooSmall := limits
	tooSmall.MaxJSONBytes = 1
	_, _, rejected, runtimeDiagnostic := compareKIRExecutionWithRuntime(t, source, tooSmall)
	if runtimeDiagnostic != nil || rejected.Diagnostic != nil || string(rejected.Output) != "JSON input exceeds configured limit\n" {
		t.Fatalf("over-limit JSON payload result = output %q, diagnostic %#v", rejected.Output, rejected.Diagnostic)
	}
}

func TestKIRExecutorMatchesRuntimeForIterationAndDeferredBlocks(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantOutput string
		wantDiag   string
	}{
		{
			name: "for over array with continue and break",
			source: `let mut total: Int = 0
for item in [1, 2, 3, 4] {
    if item == 2 { continue }
    if item == 4 { break }
    total = total + item
}
println(total)
`,
			wantOutput: "4\n",
		},
		{
			name: "for over ordered set",
			source: `let mut total: Int = 0
for item in |{3, 1, 2}| { total = total * 10 + item }
println(total)
`,
			wantOutput: "312\n",
		},
		{
			name: "for over unicode string code points",
			source: `for character in "Aé🙂" { print(character) }
println("")
`,
			wantOutput: "Aé🙂\n",
		},
		{
			name: "for over bytes",
			source: `let data: Bytes = bytes([65, 66])
for octet in data { print(octet) }
println("")
`,
			wantOutput: "6566\n",
		},
		{
			name: "while with continue and break",
			source: `let mut item: Int = 0
let mut total: Int = 0
while item < 5 {
    item = item + 1
    if item == 2 { continue }
    if item == 4 { break }
    total = total + item
}
println(total)
`,
			wantOutput: "4\n",
		},
		{
			name: "defer runs last-in first-out at block exit",
			source: `defer { println("first") }
defer { println("second") }
println("body")
`,
			wantOutput: "body\nsecond\nfirst\n",
		},
		{
			name: "defer runs when a function returns",
			source: `fn work() -> Nil {
    defer { println("deferred") }
    println("body")
    return nil
}
work()
`,
			wantOutput: "body\ndeferred\n",
		},
		{
			name: "unsafe block executes with a nested scope",
			source: `let outside: Int = 41
unsafe {
    let inside: Int = outside + 1
    println(inside)
}
`,
			wantOutput: "42\n",
		},
		{
			name: "deferred diagnostic preserves partial output and location",
			source: `defer { println(1 / 0) }
println("body")
`,
			wantOutput: "body\n",
			wantDiag:   "division by zero",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, test.source, DefaultLimits())
			if string(result.Output) != test.wantOutput {
				t.Fatalf("KIR output = %q, want %q", result.Output, test.wantOutput)
			}
			if test.wantDiag == "" {
				if diagnostic != nil {
					t.Fatalf("runtime diagnostic = %#v, want none", diagnostic)
				}
				return
			}
			if diagnostic == nil || diagnostic.Message != test.wantDiag || result.Diagnostic == nil || !sameDiagnosticStack(result.Diagnostic, diagnostic) {
				t.Fatalf("KIR diagnostic = %#v, AST diagnostic = %#v", result.Diagnostic, diagnostic)
			}
		})
	}
}

func TestKIRExecutorRunsImportedAndOverloadedFunctions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "math.kry"), []byte(`pub fn twice(value: Int) -> Int { return value * 2 }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.kry")
	mainSource := `import "lib/math"
fn choose(value: Int) -> Int { return value }
fn choose(value: String) -> Int { return 2 }
println(twice(choose(21)))
`
	if err := os.WriteFile(mainPath, []byte(mainSource), 0o600); err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(mainPath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	astRuntime, diagnostic := newASTOracleRuntime(program, checker, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	var astOutput bytes.Buffer
	astRuntime.output = &astOutput
	astDiagnostic := astRuntime.run()

	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Imports) != 1 || len(document.Sources) != 2 {
		t.Fatalf("KIR import closure metadata = imports %q, sources %q", document.Imports, document.Sources)
	}
	kirResult, err := executeKIRSubset(document, DefaultLimits(), kirSourceMap(program))
	if err != nil {
		t.Fatalf("executor rejected imported/overloaded function program: %v", err)
	}
	if string(kirResult.Output) != astOutput.String() || !sameDiagnosticStack(kirResult.Diagnostic, astDiagnostic) {
		t.Fatalf("KIR imported result = output %q, diagnostic %#v; AST result = output %q, diagnostic %#v", kirResult.Output, kirResult.Diagnostic, astOutput.String(), astDiagnostic)
	}
	if string(kirResult.Output) != "42\n" || kirResult.Diagnostic != nil {
		t.Fatalf("unexpected imported function result: output %q, diagnostic %#v", kirResult.Output, kirResult.Diagnostic)
	}
}

func TestKIRExecutorMatchesRuntimeDiagnostics(t *testing.T) {
	t.Run("division by zero after prior output", func(t *testing.T) {
		_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, `
let mut divisor: Int = 0
print("before")
println(5 / divisor)
`, DefaultLimits())
		if diagnostic == nil || diagnostic.Category != CatRuntime || diagnostic.Message != "division by zero" || string(result.Output) != "before" {
			t.Fatalf("KIR error result = output %q, diagnostic %#v", result.Output, diagnostic)
		}
	})
	t.Run("output limit", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxOutputBytes = 2
		_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, "print(\"a\")\nprintln(\"b\")\n", limits)
		if diagnostic == nil || diagnostic.Category != CatResource || diagnostic.Message != "output limit exceeded" || string(result.Output) != "a" {
			t.Fatalf("KIR output-limit result = output %q, diagnostic %#v", result.Output, diagnostic)
		}
	})
}

func TestKIRExecutorRunsFunctionValuesAndClosures(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "pass an anonymous closure as an argument",
			source: `fn apply(callback: fn(Int) -> Int, value: Int) -> Int {
    return callback(value)
}
println(apply(fn(value: Int) -> Int { return value + 1 }, 41))
`,
			want: "42\n",
		},
		{
			name: "return a closure that captures a parameter",
			source: `fn make_adder(base: Int) -> fn(Int) -> Int {
    return fn(value: Int) -> Int { return base + value }
}
let add = make_adder(40)
println(add(2))
`,
			want: "42\n",
		},
		{
			name: "mutable capture keeps one shared cell",
			source: `fn make_counter(start: Int) -> fn(Int) -> Int {
    let mut count: Int = start
    return fn(step: Int) -> Int {
        count = count + step
        return count
    }
}
let counter = make_counter(1)
println(counter(2))
println(counter(5))
`,
			want: "3\n8\n",
		},
		{
			name: "named function can be passed as a value",
			source: `fn increment(value: Int) -> Int { return value + 1 }
fn apply(callback: fn(Int) -> Int) -> Int {
    return callback(41)
}
println(apply(increment))
`,
			want: "42\n",
		},
		{
			name: "tail-recursive calls execute through KIR",
			source: `fn countdown(value: Int) -> Int {
    if value == 0 { return 42 }
    return countdown(value - 1)
}
println(countdown(8))
`,
			want: "42\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, test.source, DefaultLimits())
			if diagnostic != nil {
				t.Fatalf("runtime diagnostic = %#v", diagnostic)
			}
			if string(result.Output) != test.want {
				t.Fatalf("KIR output = %q, want %q", result.Output, test.want)
			}
		})
	}
}

func TestKIRExecutorMatchesRuntimeForClosureDiagnosticsAndLimits(t *testing.T) {
	t.Run("error in returned closure preserves source and stack", func(t *testing.T) {
		source := `fn make_divider() -> fn(Int) -> Int {
    return fn(value: Int) -> Int { return 10 / value }
}
let divide = make_divider()
println(divide(0))
`
		_, _, result, runtimeDiagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
		if runtimeDiagnostic == nil || runtimeDiagnostic.Message != "division by zero" {
			t.Fatalf("runtime diagnostic = %#v, want division by zero", runtimeDiagnostic)
		}
		if !sameDiagnosticStack(result.Diagnostic, runtimeDiagnostic) {
			t.Fatalf("KIR stack = %#v, runtime stack = %#v", result.Diagnostic.Stack, runtimeDiagnostic.Stack)
		}
	})
	t.Run("recursive call depth limit", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxCallDepth = 4
		source := `fn descend(value: Int) -> Int {
    if value == 0 { return 0 }
    let next: Int = descend(value - 1)
    return next + 1
}
println(descend(8))
`
		_, _, result, runtimeDiagnostic := compareKIRExecutionWithRuntime(t, source, limits)
		if runtimeDiagnostic == nil || runtimeDiagnostic.Category != CatResource || runtimeDiagnostic.Message != "call depth limit exceeded" {
			t.Fatalf("runtime diagnostic = %#v, want call depth limit", runtimeDiagnostic)
		}
		if !sameDiagnosticStack(result.Diagnostic, runtimeDiagnostic) {
			t.Fatalf("KIR stack = %#v, runtime stack = %#v", result.Diagnostic.Stack, runtimeDiagnostic.Stack)
		}
	})
	t.Run("instruction limit inside function", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxInstructions = 60
		limits.MaxWallTimeMS = 0
		source := `fn count(value: Int) -> Int {
    if value == 0 { return 0 }
    return count(value - 1) + 1
}
println(count(20))
`
		_, _, result, runtimeDiagnostic := compareKIRExecutionWithRuntime(t, source, limits)
		if runtimeDiagnostic == nil || runtimeDiagnostic.Category != CatResource || runtimeDiagnostic.Message != "instruction limit exceeded" {
			t.Fatalf("runtime diagnostic = %#v, want instruction limit", runtimeDiagnostic)
		}
		if !sameDiagnosticStack(result.Diagnostic, runtimeDiagnostic) {
			t.Fatalf("KIR stack = %#v, runtime stack = %#v", result.Diagnostic.Stack, runtimeDiagnostic.Stack)
		}
	})
}

func sameDiagnosticStack(left, right *Diagnostic) bool {
	if !sameRuntimeDiagnostic(left, right) {
		return false
	}
	if left == nil {
		return true
	}
	if len(left.Stack) != len(right.Stack) {
		return false
	}
	for i := range left.Stack {
		if left.Stack[i] != right.Stack[i] {
			return false
		}
	}
	return true
}

func TestKIRExecutorMatchesRuntimeForCollectionsAndNominalValues(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantOutput string
		wantDiag   string
	}{
		{
			name: "arrays nested indexing and immutable append",
			source: `let values: Array[Array[Int]] = [[1, 2], [3, 4]]
let appended: Array[Array[Int]] = array_push(values, [5, 6])
println(values[1][0])
println(appended[2][1])
`,
			wantOutput: "3\n6\n",
		},
		{
			name: "map and set builtins",
			source: `let scores: Map[String, Int] = {"answer": 41}
let changed: Map[String, Int] = map_insert(scores, "answer", 42)
let flags: Set[String] = |{"ready", "ready", "done"}|
println(scores["answer"])
println(changed["answer"])
println(set_contains(flags, "ready"))
println(map_get(changed, "missing"))
`,
			wantOutput: "41\n42\ntrue\nnone\n",
		},
		{
			name: "struct fields enum values and match",
			source: `struct Point { x: Int, name: String }
enum State { Ready, Waiting }
let point: Point = Point{name: "origin", x: 7}
let state: State = State::Waiting
println(point.x)
println(point.name)
match state {
    State::Ready => { println("ready") }
    State::Waiting => { println("waiting") }
}
`,
			wantOutput: "7\norigin\nwaiting\n",
		},
		{
			name: "option result builtins and match payloads",
			source: `let present: Option[Int] = some(42)
let absent: Option[Int] = none()
let failure: Result[Int, String] = err("bad")
println(is_some(present))
println(is_none(absent))
println(result_error(failure))
match present { some(value) => { println(value) } none => { println("empty") } }
match failure { ok(value) => { println(value) } err(problem) => { println(problem) } }
`,
			wantOutput: "true\ntrue\nsome(bad)\n42\nbad\n",
		},
		{
			name: "JSON pure builtins",
			source: `let decoded: Result[Json, String] = json_parse("{\"answer\":42}")
match decoded {
    ok(document) => { println(json_stringify(document)) }
    err(problem) => { println(problem) }
}
`,
			wantOutput: "{\"answer\":42}\n",
		},
		{
			name: "array index diagnostic",
			source: `let values: Array[Int] = [7]
print("before")
println(values[1])
`,
			wantOutput: "before",
			wantDiag:   "array index out of range",
		},
		{
			name: "map lookup diagnostic",
			source: `let values: Map[String, Int] = {"present": 1}
println(values["missing"])
`,
			wantDiag: "map key not found",
		},
		{
			name: "result unwrap diagnostic",
			source: `let failure: Result[Int, String] = err("bad")
println(result_unwrap(failure))
`,
			wantDiag: "cannot unwrap error Result: bad",
		},
		{
			name: "result propagation returns the same error",
			source: `fn inner() -> Result[Int, String] { return err("bad") }
fn outer() -> Result[Int, String] { return inner()? }
match outer() { ok(value) => { println(value) } err(problem) => { println(problem) } }
`,
			wantOutput: "bad\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, test.source, DefaultLimits())
			if test.wantDiag == "" {
				if diagnostic != nil {
					t.Fatalf("diagnostic = %#v", diagnostic)
				}
				if string(result.Output) != test.wantOutput {
					t.Fatalf("KIR output = %q, want %q", result.Output, test.wantOutput)
				}
				return
			}
			if diagnostic == nil || diagnostic.Message != test.wantDiag {
				t.Fatalf("runtime diagnostic = %#v, want %q", diagnostic, test.wantDiag)
			}
			if string(result.Output) != test.wantOutput {
				t.Fatalf("KIR output = %q, want %q", result.Output, test.wantOutput)
			}
		})
	}
}

func TestKIRExecutorMatchesRuntimeForUnsignedValuesAndConcatenation(t *testing.T) {
	source := `fn add[T: Numeric](left: T, right: T) -> T { return left + right }
fn remainder[T: Integer](left: T, right: T) -> T { return left % right }
println(u8(255) + u8(1))
println(u8(0) - u8(1))
println(u8(16) * u8(16))
println(u16(7) / u16(2))
println(u32(10) % u32(4))
println(~u8(0))
println(u8(8) | u8(1))
println(u8(15) & u8(6))
println(u8(8) ^ u8(1))
println(u8(1) << 7)
println(u8(255) >> 1)
println(u16(1) << 15)
println(u32(1) << 31)
println(u64(1) << 63)
println(u64(0) - u64(1))
println(u16(u8(255)))
println(add(u8(255), u8(2)))
println(remainder(u16(13), u16(5)))
println(u8_array(bytes_from_u8([u8(1), u8(255)])))
let max_json: UInt64 = result_unwrap(json_uint(result_unwrap(json_parse("18446744073709551615"))))
println(max_json)
println(u8(2) < u8(3))
let values: Array[UInt8] = [u8(1), u8(2)] + [u8(3)]
println(values)
let text: String = bytes_to_string(string_to_bytes("a") + string_to_bytes("b"))
println(text)
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("runtime diagnostic = %#v", diagnostic)
	}
	want := "0\n255\n0\n3\n2\n255\n9\n6\n9\n128\n127\n32768\n2147483648\n9223372036854775808\n18446744073709551615\n255\n1\n3\n[1, 255]\n18446744073709551615\ntrue\n[1, 2, 3]\nab\n"
	if got := string(result.Output); got != want {
		t.Fatalf("KIR output = %q, want %q", got, want)
	}
}

func TestKIRExecutorMatchesRuntimeForUnsignedAndArrayLimitDiagnostics(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		wantDiag string
		limits   Limits
	}{
		{name: "unsigned conversion below zero", source: "println(u8(-1))\n", wantDiag: "Int is outside unsigned range", limits: DefaultLimits()},
		{name: "unsigned conversion above width", source: "println(u8(256))\n", wantDiag: "value is outside unsigned range", limits: DefaultLimits()},
		{name: "unsigned division by zero", source: "println(u8(1) / u8(0))\n", wantDiag: "division by zero", limits: DefaultLimits()},
		{name: "unsigned remainder by zero", source: "println(u8(1) % u8(0))\n", wantDiag: "remainder by zero", limits: DefaultLimits()},
		{name: "unsigned shift at width", source: "println(u8(1) << 8)\n", wantDiag: "shift count must be between 0 and UInt width minus one", limits: DefaultLimits()},
		{name: "unsigned negative shift", source: "println(u8(1) >> -1)\n", wantDiag: "shift count must be between 0 and UInt width minus one", limits: DefaultLimits()},
		{name: "array concatenation limit", source: "let left: Array[Int] = [1, 2]\nlet right: Array[Int] = [3, 4]\nprintln(left + right)\n", wantDiag: "array size limit exceeded", limits: func() Limits { limits := DefaultLimits(); limits.MaxArrayElements = 3; return limits }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, test.source, test.limits)
			if diagnostic == nil || diagnostic.Message != test.wantDiag {
				t.Fatalf("runtime diagnostic = %#v, want %q", diagnostic, test.wantDiag)
			}
			if !sameRuntimeDiagnostic(result.Diagnostic, diagnostic) {
				t.Fatalf("KIR diagnostic = %#v, runtime diagnostic = %#v", result.Diagnostic, diagnostic)
			}
		})
	}
}

func TestKIRExecutorRejectsUnsupportedEffectsAndTypes(t *testing.T) {
	program, checker := testProgram(t, "let values: Array[Int] = [1]\nprintln(values[0])\n")
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeKIRSubset(document, DefaultLimits(), kirSourceMap(program)); err != nil {
		t.Fatalf("array indexing is now an executable KIR capability: %v", err)
	}
	program, checker = testProgram(t, "println(fs_exists(\"missing\"))\n")
	kirBytes, err = EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err = DecodeKIR(kirBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	result, err := executeKIRSubset(document, DefaultLimits(), kirSourceMap(program))
	if err != nil {
		t.Fatalf("filesystem builtin fell outside the KIR executor: %v", err)
	}
	if result.Diagnostic != nil || string(result.Output) != "false\n" {
		t.Fatalf("filesystem builtin result = output %q, diagnostic %#v; want false and no diagnostic", result.Output, result.Diagnostic)
	}
	for _, builtin := range []Builtin{
		{Name: "unreviewed_fs", Effects: "filesystem"},
		{Name: "unreviewed_database", Effects: "database"},
	} {
		if kirExecBuiltinSupported(builtin) {
			t.Errorf("KIR executor accepted unreviewed builtin %q by effect category", builtin.Name)
		}
	}
}

func TestKIRExecutorMatchesRuntimeForFilesystemBuiltins(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	file := filepath.Join(nested, "input.txt")
	copyPath := filepath.Join(root, "copied.txt")
	movedPath := filepath.Join(root, "moved.txt")
	singleDir := filepath.Join(root, "single")
	binaryPath := filepath.Join(root, "payload.bin")
	dotenvPath := filepath.Join(root, ".env")
	if err := os.WriteFile(dotenvPath, []byte("FIRST=one\nSECOND=two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`fn main() -> Nil {
    let nested: Result[Nil, String] = fs_create_dir_all(%q)
    println(is_ok(nested))
    let single: Result[Nil, String] = fs_create_dir(%q)
    println(is_ok(single))
    let write_text: Result[Nil, String] = fs_write_text(%q, "hello")
    println(is_ok(write_text))
    println(str(fs_read_text(%q)))
    let write_bytes: Result[Nil, String] = fs_write_bytes(%q, bytes([65, 66]))
    println(is_ok(write_bytes))
    println(is_ok(fs_read_bytes(%q)))
    println(fs_exists(%q))
    println(fs_is_file(%q))
    println(fs_is_dir(%q))
    println(is_ok(fs_file_size(%q)))
    println(is_ok(fs_file_modified_time(%q)))
    println(str(fs_read_dir(%q)))
    println(is_ok(fs_copy_file(%q, %q)))
    println(is_ok(fs_move_file(%q, %q)))
    println(fs_exists(%q))
    println(is_ok(fs_absolute_path(%q)))
    println(fs_join_path(%q, ["nested", "joined.txt"]))
    println(str(dotenv_load(%q)))
    println(fs_temp_dir())
    match fs_temp_file("kry-kir-exec") {
        ok(path) => { println(is_ok(fs_remove_file(path))) }
        err(problem) => { println(false) }
    }
    println(is_ok(fs_remove_file(%q)))
    println(is_ok(fs_remove_file(%q)))
    println(is_ok(fs_remove_file(%q)))
    println(is_ok(fs_remove_dir_all(%q)))
    println(is_ok(fs_remove_dir_all(%q)))
    return nil
}
`, nested, singleDir, file, file, binaryPath, binaryPath, file, file, singleDir, file, file, nested, file, copyPath, copyPath, movedPath, movedPath, file, root, dotenvPath, movedPath, file, binaryPath, nested, singleDir)
	_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
	if diagnostic != nil || result.Diagnostic != nil {
		t.Fatalf("filesystem KIR diagnostic = %#v, runtime diagnostic = %#v", result.Diagnostic, diagnostic)
	}
	want := fmt.Sprintf("true\ntrue\ntrue\nok(hello)\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\nok([input.txt])\ntrue\ntrue\ntrue\ntrue\n%s\nok({FIRST: one, SECOND: two})\n%s\ntrue\ntrue\ntrue\ntrue\ntrue\ntrue\n", filepath.Join(root, "nested", "joined.txt"), os.TempDir())
	if string(result.Output) != want {
		t.Fatalf("filesystem KIR output = %q, want %q", result.Output, want)
	}
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatalf("KIR filesystem cleanup left nested directory: %v", err)
	}
	if _, err := os.Stat(singleDir); !os.IsNotExist(err) {
		t.Fatalf("KIR filesystem cleanup left single directory: %v", err)
	}
}

func TestKIRExecutorMatchesRuntimeForSQLiteAndResourceCleanup(t *testing.T) {
	t.Run("database operations", func(t *testing.T) {
		source := `fn main() -> Nil {
    let opened: Result[SQLite, String] = sqlite_open(":memory:")
    match opened {
        ok(database) => {
            let created: Result[Int, String] = sqlite_exec(database, "CREATE TABLE users (id INTEGER, name TEXT)")
            let inserted: Result[Int, String] = sqlite_exec(database, "INSERT INTO users VALUES (1, 'Ada'), (2, 'Grace')")
            println(is_ok(created))
            println(is_ok(inserted))
            println(str(sqlite_query(database, "SELECT id, name FROM users ORDER BY id")))
            sqlite_close(database)
        }
        err(problem) => { println(problem) }
    }
    return nil
}
main()
`
		_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
		if diagnostic != nil || result.Diagnostic != nil || string(result.Output) != "true\ntrue\nok([[1, Ada], [2, Grace]])\n" {
			t.Fatalf("SQLite KIR result = output %q, diagnostic %#v; runtime diagnostic %#v", result.Output, result.Diagnostic, diagnostic)
		}
	})

	t.Run("row limit", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxArrayElements = 2
		source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(database) => {
            println(str(sqlite_query(database, "SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3")))
            sqlite_close(database)
        }
        err(problem) => { println(problem) }
    }
    return nil
}
main()
`
		_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, limits)
		if diagnostic != nil || result.Diagnostic != nil || string(result.Output) != "err(SQLite result exceeds configured row limit)\n" {
			t.Fatalf("SQLite row-limit result = output %q, diagnostic %#v; runtime diagnostic %#v", result.Output, result.Diagnostic, diagnostic)
		}
	})

	t.Run("leaked handle is reported and closed", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "leaked.db")
		source := fmt.Sprintf(`fn main() -> Nil {
    match sqlite_open(%q) {
        ok(database) => {
            match sqlite_exec(database, "BEGIN EXCLUSIVE") {
                ok(_) => { println("transaction started") }
                err(problem) => { println(problem) }
            }
        }
        err(problem) => { println(problem) }
    }
    return nil
}
main()
`, databasePath)
		_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
		if diagnostic == nil || diagnostic.Category != CatResource || !strings.Contains(diagnostic.Message, "SQLite") || !strings.Contains(diagnostic.Message, "not closed") {
			t.Fatalf("runtime leak diagnostic = %#v", diagnostic)
		}
		if !sameRuntimeDiagnostic(result.Diagnostic, diagnostic) || string(result.Output) != "transaction started\n" {
			t.Fatalf("KIR leak result = output %q, diagnostic %#v; runtime output %q, diagnostic %#v", result.Output, result.Diagnostic, "transaction started\n", diagnostic)
		}
		if err := os.Remove(databasePath); err != nil {
			t.Fatalf("KIR cleanup left its SQLite file open: %v", err)
		}
	})

	t.Run("double close error", func(t *testing.T) {
		source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(database) => { sqlite_close(database); sqlite_close(database) }
        err(problem) => { println(problem) }
    }
    return nil
}
main()
`
		_, _, result, diagnostic := compareKIRExecutionWithRuntime(t, source, DefaultLimits())
		if diagnostic == nil || diagnostic.Category != CatRuntime || !strings.Contains(diagnostic.Message, "already closed") || !sameRuntimeDiagnostic(result.Diagnostic, diagnostic) {
			t.Fatalf("SQLite double-close diagnostics differ: KIR %#v, runtime %#v", result.Diagnostic, diagnostic)
		}
	})
}

func TestKIRExecutorUsesSandboxForFilesystemAndSQLitePermissions(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "sandbox")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sandbox := Sandbox{Root: root, Restricted: true}
	escapePath := filepath.Join(parent, "escape.txt")
	source := `println(fs_write_text("inside.txt", "kept inside"))
println(fs_read_text("inside.txt"))
println(fs_read_text("../escape.txt"))
println(fs_write_text("../escape.txt", "must not escape"))
println(fs_absolute_path("../escape.txt"))
println(fs_temp_dir())
println(fs_is_dir("tmp"))
match fs_temp_file("restricted-kir") {
    ok(path) => {
        println(starts_with(path, "tmp/"))
        println(fs_is_file(path))
        println(is_ok(fs_remove_file(path)))
    }
    err(problem) => { println(problem) }
}
println(is_ok(fs_remove_dir_all("tmp")))
`
	_, _, result, diagnostic := compareKIRExecutionWithRuntimeAndSandbox(t, source, DefaultLimits(), sandbox)
	if diagnostic != nil || result.Diagnostic != nil {
		t.Fatalf("restricted filesystem KIR diagnostic = %#v, runtime diagnostic = %#v", result.Diagnostic, diagnostic)
	}
	if want := "ok(nil)\nok(kept inside)\nerr(path denied by sandbox)\nerr(path denied by sandbox)\nerr(path denied by sandbox)\ntmp\ntrue\ntrue\ntrue\ntrue\ntrue\n"; string(result.Output) != want {
		t.Fatalf("restricted filesystem output = %q, want %q", result.Output, want)
	}
	if _, err := os.Stat(escapePath); !os.IsNotExist(err) {
		t.Fatalf("restricted KIR filesystem write escaped its root: %v", err)
	}

	deniedSource := `fn mark_argument() -> String {
    let marker: Result[Nil, String] = fs_write_text("argument-evaluated", "yes")
    return "database.sqlite"
}
fn main() -> Nil {
    let opened: Result[SQLite, String] = sqlite_open(mark_argument())
    return nil
}
main()
`
	_, _, denied, runtimeDiagnostic := compareKIRExecutionWithRuntimeAndSandbox(t, deniedSource, DefaultLimits(), sandbox)
	if runtimeDiagnostic == nil || runtimeDiagnostic.Category != CatRuntime || !strings.Contains(runtimeDiagnostic.Message, `sqlite_open" is unavailable with --restricted`) || !sameRuntimeDiagnostic(denied.Diagnostic, runtimeDiagnostic) {
		t.Fatalf("restricted SQLite diagnostics differ: KIR %#v, runtime %#v", denied.Diagnostic, runtimeDiagnostic)
	}
	if _, err := os.Stat(filepath.Join(root, "argument-evaluated")); !os.IsNotExist(err) {
		t.Fatalf("restricted KIR SQLite evaluated denied builtin arguments: %v", err)
	}
}

func TestKIRConditionSemanticsAreCheckedByDecoderAndExecutor(t *testing.T) {
	program, checker := testProgram(t, "if true { println(\"yes\") }\n")
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(kirBytes, &document); err != nil {
		t.Fatal(err)
	}
	document.Statements[0].Cond.Type = "Int"
	if err := validateKIRDocument(&document, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "condition has checked type") {
		t.Fatalf("KIR decoder accepted a non-Bool condition: %v", err)
	}
	if err := validateKIRExecSubset(&document); err == nil || errors.Is(err, errKIRSubsetUnsupported) {
		t.Fatalf("KIR executor accepted a non-Bool condition: %v", err)
	}
}

func TestKIRExecutorAcceptsDecodedKIRV3(t *testing.T) {
	source := "let mut index: Int = 0\nwhile index < 3 { println(index); index = index + 1 }\nif index == 3 { println(\"done\") }\n"
	program, checker := testProgram(t, source)
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var legacy any
	if err := json.Unmarshal(kirBytes, &legacy); err != nil {
		t.Fatal(err)
	}
	document, ok := legacy.(map[string]any)
	if !ok {
		t.Fatalf("KIR document decoded as %T, want object", legacy)
	}
	document["version"] = float64(3)
	var removeV4Fields func(any)
	removeV4Fields = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			delete(value, "type_params")
			delete(value, "struct_type")
			delete(value, "generic_arguments")
			for _, child := range value {
				removeV4Fields(child)
			}
		case []any:
			for _, child := range value {
				removeV4Fields(child)
			}
		}
	}
	removeV4Fields(document)
	kirV3, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeKIR(kirV3, DefaultLimits())
	if err != nil {
		t.Fatalf("DecodeKIR rejected a compatible v3 scalar program: %v", err)
	}
	if decoded.Version != 3 {
		t.Fatalf("decoded KIR version = %d, want 3", decoded.Version)
	}
	result, err := executeKIRSubset(decoded, DefaultLimits(), kirSourceMap(program))
	if err != nil {
		t.Fatalf("KIR v3 execution failed: %v", err)
	}
	var astOutput bytes.Buffer
	astRuntime, diagnostic := newASTOracleRuntime(program, checker, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	astRuntime.output = &astOutput
	astDiagnostic := astRuntime.run()
	if !bytes.Equal(result.Output, astOutput.Bytes()) || !sameRuntimeDiagnostic(result.Diagnostic, astDiagnostic) {
		t.Fatalf("KIR v3 result output/diagnostic = %q/%#v, want AST %q/%#v", result.Output, result.Diagnostic, astOutput.String(), astDiagnostic)
	}
}

func TestDirectELFLowersKIRControlFlowAndMatchesRuntimeErrors(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		wantOutput      string
		wantStatus      int
		wantError       bool
		runtimeLowering bool
	}{
		{
			name: "loop and branch",
			source: `let mut i: Int = 0
while i < 3 { println(i); i = i + 1 }
if i == 3 { println("done") }
`,
			wantOutput:      "0\n1\n2\ndone\n",
			runtimeLowering: true,
		},
		{
			name:       "runtime failure keeps prior output",
			source:     "let mut divisor: Int = 0\nprint(\"before\")\nprintln(5 / divisor)\n",
			wantOutput: "before",
			wantStatus: 1,
			wantError:  true,
		},
		{
			name:       "checked integer overflow keeps prior output",
			source:     "print(\"before\")\nprintln(9223372036854775807 + 1)\n",
			wantOutput: "before",
			wantStatus: 1,
			wantError:  true,
		},
		{
			name:       "remainder by zero",
			source:     "println(1 % 0)\n",
			wantStatus: 1,
			wantError:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			if test.runtimeLowering && bytes.Contains(image, []byte(test.wantOutput)) {
				t.Fatal("generated ELF embeds the complete output instead of lowering KIR control flow for runtime execution")
			}
			var astOutput bytes.Buffer
			astRuntime, diagnostic := newASTOracleRuntime(program, checker, DefaultLimits(), Sandbox{}, nil)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			astRuntime.output = &astOutput
			astDiagnostic := astRuntime.run()
			if string(astOutput.Bytes()) != test.wantOutput {
				t.Fatalf("AST output = %q, want %q", astOutput.String(), test.wantOutput)
			}
			if (astDiagnostic != nil) != test.wantError {
				t.Fatalf("AST diagnostic = %#v, want error %t", astDiagnostic, test.wantError)
			}
			wantStderr := ""
			if astDiagnostic != nil {
				wantStderr = astDiagnostic.Format(false)
			}
			if _, err := InspectNative(image); err != nil {
				t.Fatalf("invalid ELF image: %v", err)
			}
			if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
				return
			}
			path := filepath.Join(t.TempDir(), "direct-kir-program")
			if err := os.WriteFile(path, image, 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(path)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			runErr := command.Run()
			status := 0
			if runErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) {
					t.Fatalf("ELF execution failed unexpectedly: %v, stdout %q, stderr %q", runErr, stdout.String(), stderr.String())
				}
				status = exitErr.ExitCode()
			}
			if stdout.String() != test.wantOutput || stderr.String() != wantStderr || status != test.wantStatus {
				t.Fatalf("ELF stdout/stderr/status = %q/%q/%d, want %q/%q/%d", stdout.String(), stderr.String(), status, test.wantOutput, wantStderr, test.wantStatus)
			}
		})
	}
}

func TestDirectELFRejectsFunctionsAndClosuresOutsideItsLoweredSubset(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantMessage string
	}{
		{
			name: "helper alongside main",
			source: `fn helper() -> Int { return 42 }
fn main() -> Nil { println(helper()) }
`,
			wantMessage: "helper functions are not lowered",
		},
		{
			name: "capturing lambda in main",
			source: `fn main() -> Nil {
    let base: Int = 40
    let add = fn(value: Int) -> Int { return base + value }
    println(add(2))
}
`,
			wantMessage: "does not lower lambdas or captured bindings",
		},
		{
			name: "function declaration with top-level execution",
			source: `fn helper() -> Int { return 1 }
println(2)
`,
			wantMessage: "does not execute top-level function declarations",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := testProgram(t, test.source)
			encoded, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			mir, err := DecodeMIR(encoded, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			err = validateKIRDirectELFSubset(mir)
			if !errors.Is(err, errKIRSubsetUnsupported) || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("direct KIR ELF validation error = %v, want explicit unsupported error containing %q", err, test.wantMessage)
			}
		})
	}
}

func TestDirectELFKIRExecutionLimitsMatchAST(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		limits        Limits
		want          string
		exactPosition bool
	}{
		{
			name:   "instruction limit source location",
			source: "let mut value: Int = 0\nwhile value < 1000 { println(value); value = value + 1 }\n",
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxInstructions = 40
				limits.MaxWallTimeMS = 0
				return limits
			}(),
			want:          "instruction limit exceeded",
			exactPosition: true,
		},
		{
			name:   "wall-clock limit source location",
			source: "let mut value: Int = 0\nwhile true { value = value + 1 }\n",
			limits: func() Limits {
				limits := DefaultLimits()
				limits.MaxInstructions = 50_000_000
				limits.MaxWallTimeMS = 50
				return limits
			}(),
			want: "wall-clock execution limit exceeded",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, diagnostic := Parse(&Source{Name: "direct-limit.kry", Text: test.source}, test.limits)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			checker, diagnostic := Check(program, test.limits)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			astOutput, astDiagnostic := directRuntimeResult(t, program, checker)
			if astDiagnostic == nil || astDiagnostic.Message != test.want {
				t.Fatalf("AST diagnostic = %#v, want %q", astDiagnostic, test.want)
			}
			image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := InspectNative(image); err != nil {
				t.Fatalf("invalid ELF image: %v", err)
			}
			if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
				return
			}
			path := filepath.Join(t.TempDir(), "direct-kir-limits")
			if err := os.WriteFile(path, image, 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(path)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			runErr := command.Run()
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("ELF error/status = %v/%d, want status 1", runErr, func() int {
					if exitErr != nil {
						return exitErr.ExitCode()
					}
					return 0
				}())
			}
			if stdout.String() != string(astOutput) {
				t.Fatalf("ELF stdout/stderr = %q/%q, want AST %q/%q", stdout.String(), stderr.String(), astOutput, astDiagnostic.Format(false))
			}
			if test.exactPosition && stderr.String() != astDiagnostic.Format(false) {
				t.Fatalf("ELF diagnostic = %q, want exact AST diagnostic %q", stderr.String(), astDiagnostic.Format(false))
			}
			if !test.exactPosition && (!strings.Contains(stderr.String(), astDiagnostic.Message) || !strings.Contains(stderr.String(), fmt.Sprintf("%s:%d:", astDiagnostic.Source, astDiagnostic.Line))) {
				t.Fatalf("ELF diagnostic %q does not preserve AST error and source line (%s:%d): %q", stderr.String(), astDiagnostic.Source, astDiagnostic.Line, astDiagnostic.Message)
			}
		})
	}
}
