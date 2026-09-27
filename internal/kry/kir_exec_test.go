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
	astRuntime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
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
	kirResult, err := executeKIRSubset(document, limits, kirSourceMap(program))
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

func TestKIRExecutorRejectsUnsupportedNodesAndTypes(t *testing.T) {
	program, checker := testProgram(t, "let values: Array[Int] = [1]\nprintln(values[0])\n")
	kirBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeKIR(kirBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeKIRSubset(document, DefaultLimits(), kirSourceMap(program)); !errors.Is(err, errKIRSubsetUnsupported) {
		t.Fatalf("array program error = %v, want explicit unsupported-subset error", err)
	}
}

func TestKIRExecutorChecksSubsetSemanticsBeyondDecoder(t *testing.T) {
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
	malformed, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeKIR(malformed, DefaultLimits())
	if err != nil {
		t.Fatalf("structural KIR decoder rejected the fixture earlier than expected: %v", err)
	}
	if _, err := executeKIRSubset(decoded, DefaultLimits(), kirSourceMap(program)); err == nil || errors.Is(err, errKIRSubsetUnsupported) {
		t.Fatalf("executor accepted semantically invalid condition type: %v", err)
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
	astRuntime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
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
			astRuntime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
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
			document, err := DecodeKIR(encoded, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			err = validateKIRDirectELFSubset(document)
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
