package kry

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirectKIRValuesAndArrayLoopsMatchInterpreter(t *testing.T) {
	source := `let values: Array[Int] = [1, 2, 3, 4]
let mut total: Int = 0
for item in values {
    if item == 2 { continue }
    if item == 4 { break }
    total = total + item
}
let appended: Array[Int] = array_push(values, 5)
let joined: Array[Int] = array_concat(appended, [6])
println(total)
println(values[2])
println(appended[4])
println(joined[5])
println(len(array_indices(values)))
println(unwrap_or(array_get(values, 8), 99))
let absent: Option[Int] = none()
let present: Option[Int] = some(41)
println(is_none(absent))
println(is_some(present))
println(unwrap_or(absent, 7))
println(unwrap_or(present, 7))
let success: Result[Int, String] = ok(42)
let failure: Result[Int, String] = err("bad")
println(is_ok(success))
println(is_err(failure))
println(result_unwrap(success))
println(unwrap_or(result_error(failure), "fallback"))
`
	assertDirectKIRMatchesInterpreter(t, source, "direct-kir-values")
}

func TestDirectKIRArrayLoweringPrimitivesMatchInterpreter(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "literal and index", source: "let values: Array[Int] = [1, 2]\nprintln(values[1])\n"},
		{name: "immutable push", source: "let values: Array[Int] = [1, 2]\nlet grown: Array[Int] = array_push(values, 3)\nprintln(grown[2])\n"},
		{name: "concatenation", source: "let values: Array[Int] = [1, 2]\nlet joined: Array[Int] = array_concat(values, [3])\nprintln(joined[2])\n"},
		{name: "indices", source: "let values: Array[Int] = [1, 2]\nlet indexes: Array[Int] = array_indices(values)\nprintln(indexes[1])\n"},
		{name: "for with break and continue", source: "let mut total: Int = 0\nfor item in [1, 2, 3, 4] { if item == 2 { continue }; if item == 4 { break }; total = total + item }\nprintln(total)\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-array-case")
		})
	}
}

func TestDirectKIRArrayIndexErrorsMatchInterpreter(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "out of range",
			source: `let values: Array[Int] = [1]
print("before")
println(values[1])
`,
		},
		{
			name: "negative index",
			source: `let values: Array[Int] = [1]
print("before")
println(values[-1])
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-array-error")
		})
	}
}

func TestDirectKIRResultUnwrapErrorMatchesInterpreter(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "String payload", source: `let failure: Result[Int, String] = err("bad")
print("before")
println(result_unwrap(failure))
`},
		{name: "UInt payload", source: `let failure: Result[Int, UInt64] = err(~u64(0))
println(result_unwrap(failure))
`},
		{name: "helper stack with conversion error", source: `fn parse(value: String) -> Int { return int(value) }
fn main() -> Nil {
    println(parse("not-decimal"))
    return nil
}
`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-result-error")
		})
	}
}

func TestDirectKIRAssertionsMatchInterpreter(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "successful scalar and string assertions",
			source: `assert(true)
assert_eq(41 + 1, 42)
assert_eq("same", "same")
println("passed")
`,
		},
		{
			name: "assert diagnostic",
			source: `println("before")
assert(false)
println("after")
`,
		},
		{
			name: "assert_eq diagnostic",
			source: `println("before")
assert_eq("left", "right")
println("after")
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-assertion")
		})
	}
}

func TestDirectKIRStringOperationsMatchInterpreter(t *testing.T) {
	source := `let text: String = "café 🌿"
println(text + " Kryndel")
println(contains(text, "fé"))
println(contains(text, "🌿"))
println(contains(text, "absent"))
println(starts_with(text, "café"))
println(starts_with(text, "afé"))
println(ends_with(text, "🌿"))
println(ends_with(text, "café"))
println(contains(text, ""))
println(starts_with(text, ""))
println(ends_with(text, ""))
`
	assertDirectKIRMatchesInterpreter(t, source, "direct-kir-string-operations")
}

func TestDirectKIRConversionsMatchInterpreter(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "supported conversions",
			source: `println(int("0"))
println(int("+42"))
println(int("-42"))
println(int("-9223372036854775808"))
println(int(true))
println(int(u16(65535)))
println(u8(u16(255)))
println(u16(65535))
println(u32(4294967295))
println(u64(42))
println(str(false))
`,
		},
		{
			name: "str supported display types",
			source: `fn main() -> Nil {
    println(str(-9223372036854775807 - 1))
    println(str(u8(255)))
    println(str(u16(65535)))
    println(str(u32(4294967295)))
    println(str(u64(42)))
    println(str(true))
    println(str("Kryndel"))
    println(str(nil))
    return nil
}
`,
		},
		{
			name:   "int string must be complete decimal",
			source: "println(int(\"12x\"))\n",
		},
		{
			name:   "int string sign requires digits",
			source: "println(int(\"+\"))\n",
		},
		{
			name:   "int string overflow",
			source: "println(int(\"9223372036854775808\"))\n",
		},
		{
			name:   "unsigned conversion rejects negative Int",
			source: "println(u8(-1))\n",
		},
		{
			name:   "unsigned conversion rejects values above width",
			source: "println(u8(256))\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-conversion")
		})
	}
}

func TestDirectKIRHelperFunctionsMatchInterpreter(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "nested scalar calls",
			source: `fn increment(value: Int) -> Int { return value + 1 }
fn double(value: Int) -> Int { return value * 2 }
fn composed(value: Int) -> Int { return double(increment(value)) }
fn main() -> Nil {
    println(composed(20))
    return nil
}
`,
		},
		{
			name: "repeated helper callsites",
			source: `fn increment(value: Int) -> Int { return value + 1 }
fn choose(first: Bool) -> Int {
    if first { return increment(10) } else { return increment(20) }
}
fn main() -> Nil {
    println(choose(false))
    return nil
}
`,
		},
		{
			name: "array parameters and return values",
			source: `fn selected(values: Array[Int], index: Int) -> Int {
    return values[index]
}
fn main() -> Nil {
    let values: Array[Int] = [11, 22, 33]
    println(selected(values, 1))
    return nil
}
`,
		},
		{
			name: "composite helper return values",
			source: `fn extend(values: Array[Int]) -> Array[Int] { return array_push(values, 4) }
fn present(value: Int) -> Option[Int] { return some(value) }
fn accepted(value: Int) -> Result[Int, String] { return ok(value) }
fn main() -> Nil {
    let values: Array[Int] = extend([1, 2, 3])
    let option: Option[Int] = present(values[3])
    let result: Result[Int, String] = accepted(unwrap_or(option, 0))
    println(result_unwrap(result))
    return nil
}
`,
		},
		{
			name: "string return and shared output",
			source: `fn greet(name: String) -> String { return "hello, " + name }
fn announce(value: String) -> Nil { println(value) }
fn main() -> Nil {
    announce(greet("Kryndel"))
    return nil
}
`,
		},
		{
			name: "assertion diagnostic carries static call stack",
			source: `fn check(value: Bool) -> Nil { assert(value) }
fn main() -> Nil {
    println("before")
    check(false)
    println("after")
    return nil
}
`,
		},
		{
			name: "result diagnostic carries static call stack",
			source: `fn unwrap(value: Result[Int, String]) -> Int { return result_unwrap(value) }
fn main() -> Nil {
    let failure: Result[Int, String] = err("bad")
    println(unwrap(failure))
    return nil
}
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-helper-function")
		})
	}
}

func TestDirectKIRHelperFunctionRepeatedDiagnosticCallsitesMatchInterpreter(t *testing.T) {
	sources := map[string]string{
		"first branch": `fn check(value: Int) -> Int {
    assert(value > 0)
    return value
}
fn choose(first: Bool) -> Int {
    if first { return check(0) } else { return check(-1) }
}
fn main() -> Nil {
    println("before")
    println(choose(true))
    return nil
}
		`,
		"second branch": `fn check(value: Int) -> Int {
    assert(value > 0)
    return value
}
fn choose(first: Bool) -> Int {
    if first { return check(0) } else { return check(-1) }
}
fn main() -> Nil {
    println("before")
    println(choose(false))
    return nil
}
`,
	}
	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			assertDirectKIRMatchesInterpreter(t, source, "direct-kir-repeated-helper-diagnostic")
		})
	}
}

func TestDirectKIRRecursiveFunctionsMatchInterpreter(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		maxCallDepth int
	}{
		{
			name: "non-tail recursion preserves results",
			source: `fn factorial(value: Int) -> Int {
    if value <= 1 { return 1 }
    return value * factorial(value - 1)
}
fn main() -> Nil {
    println(factorial(8))
    return nil
}
`,
		},
		{
			name: "recursive diagnostics include every active frame",
			source: `fn fail() -> Int {
    assert(false)
    return 0
}
fn descend(value: Int) -> Int {
    if value == 0 { return fail() }
    return descend(value - 1) + 1
}
fn main() -> Nil {
    println(descend(4))
    return nil
}
`,
		},
		{
			name: "tail recursion reuses one activation",
			source: `fn sum(value: Int, total: Int) -> Int {
    if value == 0 { return total }
    return sum(value - 1, total + value)
}
fn main() -> Nil {
    println(sum(5000, 0))
    return nil
}
`,
		},
		{
			name:         "non-tail recursion enforces depth with active stack",
			maxCallDepth: 4,
			source: `fn recurse(value: Int) -> Int {
    if value == 0 { return 0 }
    return recurse(value - 1) + 1
}
fn main() -> Nil {
    println(recurse(12))
    return nil
}
`,
		},
		{
			name:         "tail recursion enforces executor depth semantics",
			maxCallDepth: 2,
			source: `fn recurse(value: Int) -> Int {
    if value == 0 { return 0 }
    return recurse(value - 1)
}
fn main() -> Nil {
    println(recurse(12))
    return nil
}
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.maxCallDepth == 0 {
				assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-recursion")
				return
			}
			_, checker := testProgram(t, test.source)
			limits := checker.Env.Lim
			limits.MaxCallDepth = test.maxCallDepth
			assertDirectKIRMatchesInterpreter(t, test.source, "direct-kir-recursion-depth", limits)
		})
	}
}

func TestDirectKIRPreflightRejectsAlteredUnsignedCastType(t *testing.T) {
	program, checker := testProgram(t, "println(u8(1))\n")
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, checker.Env.Lim)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Statements) != 1 || document.Statements[0].Expr == nil || len(document.Statements[0].Expr.Args) != 1 {
		t.Fatal("test KIR has no expected println(u8(...)) call")
	}
	cast := document.Statements[0].Expr.Args[0]
	if cast == nil || cast.Kind != "call" || cast.Name != "u8" {
		t.Fatal("test KIR has no expected u8(...) expression")
	}
	cast.Type = "UInt16"
	err = validateKIRDirectELFValueSubset(document)
	if !errors.Is(err, errKIRSubsetUnsupported) || !strings.Contains(err.Error(), `builtin "u8"`) {
		t.Fatalf("altered u8 return type should be rejected by direct lowering preflight, got %v", err)
	}
}

func assertDirectKIRMatchesInterpreter(t *testing.T, source, artifactName string, requestedLimits ...Limits) {
	t.Helper()
	program, checker := testProgram(t, source)
	limits := checker.Env.Lim
	if len(requestedLimits) > 0 {
		limits = requestedLimits[0]
		checker.Lim = limits
		checker.Env.Lim = limits
	}
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateKIRDirectELFValueSubset(document); err != nil {
		t.Fatalf("direct KIR value preflight rejected a supported test: %v", err)
	}
	image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("public direct ELF KIR dispatch failed: %v", err)
	}
	if _, err := InspectNative(image); err != nil {
		t.Fatalf("direct KIR ELF image is invalid: %v", err)
	}

	var astOutput bytes.Buffer
	astRuntime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	astRuntime.output = &astOutput
	astDiagnostic := astRuntime.run()

	kirResult, err := executeKIRSubset(document, limits, kirSourceMap(program))
	if err != nil {
		t.Fatalf("KIR interpreter rejected a direct backend test: %v", err)
	}
	if !bytes.Equal(kirResult.Output, astOutput.Bytes()) || !sameRuntimeDiagnostic(kirResult.Diagnostic, astDiagnostic) {
		t.Fatalf("KIR interpreter differs from AST: output %q/%q diagnostic %#v/%#v", kirResult.Output, astOutput.Bytes(), kirResult.Diagnostic, astDiagnostic)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("generated ELF execution is verified on Linux/amd64 hosts")
	}

	path := filepath.Join(t.TempDir(), artifactName)
	if err := os.WriteFile(path, image, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(path)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	wantStatus := 0
	if astDiagnostic != nil {
		wantStatus = 1
	}
	status := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("direct ELF failed to run: %v", runErr)
		}
		status = exitErr.ExitCode()
	}
	wantStderr := ""
	if astDiagnostic != nil {
		wantStderr = astDiagnostic.Format(false)
	}
	if stdout.String() != astOutput.String() || stderr.String() != wantStderr || status != wantStatus {
		t.Fatalf("direct KIR ELF stdout/stderr/status = %q/%q/%d (run error %v), want AST %q/%q/%d", stdout.String(), stderr.String(), status, runErr, astOutput.String(), wantStderr, wantStatus)
	}
}

func TestDirectELFKIRProcessArgsMatchesInterpreter(t *testing.T) {
	source := `let args: Array[String] = process_args()
println(len(args))
for argument in args { println(argument) }
`
	program, checker := testProgram(t, source)
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, checker.Env.Lim)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateKIRDirectELFValueSubset(document); err != nil {
		t.Fatalf("direct KIR preflight rejected process_args: %v", err)
	}
	if err := validateKIRExecSubset(document); !errors.Is(err, errKIRSubsetUnsupported) || !strings.Contains(err.Error(), "host effect") {
		t.Fatalf("KIR executor unexpectedly accepted process_args: %v", err)
	}
	image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("direct ELF KIR lowering rejected process_args: %v", err)
	}
	if _, err := InspectNative(image); err != nil {
		t.Fatalf("process_args produced an invalid ELF image: %v", err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("generated ELF execution is verified on Linux/amd64 hosts")
	}

	path := filepath.Join(t.TempDir(), "direct-process-args")
	if err := os.WriteFile(path, image, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		arguments []string
	}{
		{name: "empty", arguments: nil},
		{name: "spaces and UTF-8", arguments: []string{"alpha", "two words", "á"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			arguments := test.arguments
			interpreter, diagnostic := NewRuntimeWithArgs(program, checker, checker.Env.Lim, Sandbox{}, arguments)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			var expected bytes.Buffer
			interpreter.output = &expected
			wantDiagnostic := interpreter.run()

			command := exec.Command(path, arguments...)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			runErr := command.Run()
			status := 0
			if runErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) {
					t.Fatalf("generated ELF failed: %v", runErr)
				}
				status = exitErr.ExitCode()
			}
			wantStatus, wantStderr := 0, ""
			if wantDiagnostic != nil {
				wantStatus, wantStderr = 1, wantDiagnostic.Format(false)
			}
			if stdout.String() != expected.String() || stderr.String() != wantStderr || status != wantStatus {
				t.Fatalf("direct ELF stdout/stderr/status = %q/%q/%d, want %q/%q/%d", stdout.String(), stderr.String(), status, expected.String(), wantStderr, wantStatus)
			}
		})
	}
}

func TestDirectELFKIRArraySliceMatchesInterpreter(t *testing.T) {
	source := `let values: Array[Int] = [10, 20, 30, 40]
let middle: Array[Int] = array_slice(values, 1, 2)
println(len(middle))
println(unwrap_or(array_get(middle, 0), -1))
println(unwrap_or(array_get(middle, 1), -1))
let empty: Array[Int] = array_slice(values, 4, 0)
println(len(empty))
`
	assertDirectKIRMatchesInterpreter(t, source, "direct-array-slice")
}

func TestDirectELFKIRArraySliceRejectsInvalidRanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		start  string
		length string
	}{
		{name: "negative start", start: "-1", length: "0"},
		{name: "negative length", start: "0", length: "-1"},
		{name: "start after end", start: "5", length: "0"},
		{name: "range exceeds end", start: "4", length: "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "let values: Array[Int] = [10, 20, 30, 40]\n" +
				"let invalid: Array[Int] = array_slice(values, " + test.start + ", " + test.length + ")\n" +
				"println(len(invalid))\n"
			assertDirectKIRMatchesInterpreter(t, source, "direct-array-slice-invalid")
		})
	}
}

func TestDirectELFKIRArraySetMatchesInterpreter(t *testing.T) {
	source := `let values: Array[Int] = [10, 20, 30]
let updated: Array[Int] = result_unwrap(array_set(values, 1, 99))
println(len(updated))
println(unwrap_or(array_get(updated, 0), -1))
println(unwrap_or(array_get(updated, 1), -1))
println(unwrap_or(array_get(updated, 2), -1))
`
	assertDirectKIRMatchesInterpreter(t, source, "direct-array-set")
}

func TestDirectELFKIRArraySetOutOfBoundsMatchesInterpreter(t *testing.T) {
	source := `let values: Array[Int] = [10, 20, 30]
let invalid: Array[Int] = result_unwrap(array_set(values, 3, 99))
println(len(invalid))
`
	assertDirectKIRMatchesInterpreter(t, source, "direct-array-set-out-of-bounds")
}

func TestDirectELFKIRStringCharsMatchesInterpreter(t *testing.T) {
	source := `let chars: Array[String] = string_chars("aá🙂")
println(len(chars))
println(unwrap_or(array_get(chars, 0), ""))
println(unwrap_or(array_get(chars, 1), ""))
println(unwrap_or(array_get(chars, 2), ""))
`
	assertDirectKIRMatchesInterpreter(t, source, "direct-string-chars")
}
