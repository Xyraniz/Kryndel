package kry

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const optionResultConformanceSourceName = "option_result_conformance.kry"

type optionResultObservedOutcome struct {
	stdout     string
	stderr     string
	exitStatus int
	diagnostic *Diagnostic
}

type optionResultConformanceCase struct {
	name       string
	statement  string
	wantOutput string
}

var optionResultCommonCases = []optionResultConformanceCase{
	{name: "some construction and predicate", statement: `let present: Option[Int] = some(42); println(is_some(present))`, wantOutput: "true\n"},
	{name: "none construction and predicate", statement: `let empty: Option[Int] = none(); println(is_none(empty))`, wantOutput: "true\n"},
	{name: "ok construction and predicate", statement: `let success: Result[Int, String] = ok(42); println(is_ok(success))`, wantOutput: "true\n"},
	{name: "err construction and predicate", statement: `let failure: Result[Int, String] = err("bad"); println(is_err(failure))`, wantOutput: "true\n"},
	{name: "is_some false branch", statement: `let emptyFalse: Option[Int] = none(); println(is_some(emptyFalse))`, wantOutput: "false\n"},
	{name: "is_none false branch", statement: `let presentFalse: Option[Int] = some(42); println(is_none(presentFalse))`, wantOutput: "false\n"},
	{name: "is_ok false branch", statement: `let failureFalse: Result[Int, String] = err("bad"); println(is_ok(failureFalse))`, wantOutput: "false\n"},
	{name: "is_err false branch", statement: `let successFalse: Result[Int, String] = ok(42); println(is_err(successFalse))`, wantOutput: "false\n"},
	{name: "unwrap_or present", statement: `let fallbackPresent: Option[Int] = some(42); println(unwrap_or(fallbackPresent, 7))`, wantOutput: "42\n"},
	{name: "unwrap_or empty", statement: `let fallbackEmpty: Option[Int] = none(); println(unwrap_or(fallbackEmpty, 7))`, wantOutput: "7\n"},
}

var optionResultDirectOnlyCases = []optionResultConformanceCase{
	{name: "result_error on success", statement: `let resultErrorSuccess: Result[Int, String] = ok(42); println(is_none(result_error(resultErrorSuccess)))`, wantOutput: "true\n"},
	{name: "result_error on error", statement: `let resultErrorFailure: Result[Int, String] = err("bad"); println(unwrap_or(result_error(resultErrorFailure), "no error"))`, wantOutput: "bad\n"},
	{name: "result_unwrap success", statement: `let resultUnwrapSuccess: Result[Int, String] = ok(42); println(result_unwrap(resultUnwrapSuccess))`, wantOutput: "42\n"},
}

func TestOptionResultBuiltinDifferentialConformance(t *testing.T) {
	commonSource := optionResultConformanceProgram(optionResultCommonCases, "")
	commonWant := optionResultExpectedOutput(optionResultCommonCases)
	commonInterpreter := runOptionResultInterpreter(t, commonSource)
	assertOptionResultOutcome(t, "interpreter common family", commonInterpreter, optionResultObservedOutcome{
		stdout: commonWant,
	})
	assertOptionResultCaseOutputs(t, "interpreter", optionResultCommonCases, commonInterpreter.stdout)

	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" || runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
		format := "elf"
		targetName := "linux-x64"
		if target.OS == "windows" {
			format = "exe"
			targetName = "windows-x64"
		}

		t.Run("C AOT", func(t *testing.T) {
			for _, builtin := range []string{"some", "none", "ok", "err", "is_some", "is_none", "is_ok", "is_err", "unwrap_or"} {
				if status := optionResultBuiltinStatus(builtin, targetName, "c_aot"); status != "supported" {
					t.Fatalf("common Option/Result builtin %q unexpectedly has C AOT status %q on %s", builtin, status, targetName)
				}
			}
			if !optionResultCompilerAvailable(target) {
				t.Skipf("C AOT differential execution needs the compiler for %s-%s", target.OS, target.Arch)
			}
			native := buildAndRunOptionResultNative(t, commonSource, target, format)
			assertOptionResultOutcome(t, "C AOT common family", native, commonInterpreter)
			assertOptionResultCaseOutputs(t, "C", optionResultCommonCases, native.stdout)
		})

		t.Run("C AOT rejects unsupported Result extractors", func(t *testing.T) {
			for _, builtin := range []string{"result_error", "result_unwrap"} {
				if status := optionResultBuiltinStatus(builtin, targetName, "c_aot"); status != "unsupported" {
					t.Fatalf("expected %s C AOT capability to be unsupported on %s, got %q", builtin, targetName, status)
				}
				source := optionResultProgram([]string{optionResultInvocation(builtin)}, "")
				program, checker := parseCheckOptionResult(t, source)
				_, err := BuildNative(program, checker, target, format)
				want := fmt.Sprintf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", builtin, format, target.OS, target.Arch)
				if err == nil || err.Error() != want {
					t.Errorf("unsupported %s must be rejected with its precise preflight diagnostic: got %v, want %q", builtin, err, want)
				}
			}
		})
	}

	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		t.Run("ELF-direct common and extended family", func(t *testing.T) {
			allCases := append(append([]optionResultConformanceCase(nil), optionResultCommonCases...), optionResultDirectOnlyCases...)
			source := optionResultConformanceProgram(allCases, "")
			want := optionResultExpectedOutput(allCases)
			interpreter := runOptionResultInterpreter(t, source)
			assertOptionResultOutcome(t, "interpreter full successful family", interpreter, optionResultObservedOutcome{stdout: want})
			assertOptionResultCaseOutputs(t, "interpreter", allCases, interpreter.stdout)

			for _, builtin := range []string{"some", "none", "ok", "err", "is_some", "is_none", "is_ok", "is_err", "unwrap_or", "result_error", "result_unwrap"} {
				if status := optionResultBuiltinStatus(builtin, "linux-x64", "elf_direct"); status != "supported" {
					t.Fatalf("expected %s ELF-direct capability to be supported on Linux x64, got %q", builtin, status)
				}
			}
			native := buildAndRunOptionResultDirectELF(t, source)
			assertOptionResultOutcome(t, "ELF-direct full successful family", native, interpreter)
			assertOptionResultCaseOutputs(t, "ELF-direct", allCases, native.stdout)
		})

		t.Run("ELF-direct runtime diagnostic parity", func(t *testing.T) {
			failures := []struct {
				name       string
				errorType  string
				errorValue string
				message    string
			}{
				{name: "String", errorType: "String", errorValue: `"bad"`, message: "bad"},
				{name: "Int", errorType: "Int", errorValue: "-17", message: "-17"},
				{name: "Int minimum", errorType: "Int", errorValue: "-9223372036854775808", message: "-9223372036854775808"},
				{name: "UInt", errorType: "UInt64", errorValue: "u64(42)", message: "42"},
				{name: "Bool", errorType: "Bool", errorValue: "false", message: "false"},
				{name: "Bool true", errorType: "Bool", errorValue: "true", message: "true"},
			}
			for _, failure := range failures {
				t.Run(failure.name, func(t *testing.T) {
					source := optionResultFailureSource(failure.errorType, failure.errorValue)
					interpreter := runOptionResultInterpreter(t, source)
					wantDiagnostic := &Diagnostic{
						Code: "KRY004", Category: CatRuntime, Severity: "error",
						Source: optionResultConformanceSourceName, Line: 4, Column: 26,
						Message: "cannot unwrap error Result: " + failure.message,
						Stack:   []StackFrame{{Function: "main", Source: optionResultConformanceSourceName, Line: 1, Column: 1}},
					}
					assertOptionResultInterpreterDiagnostic(t, interpreter, wantDiagnostic)
					interpreter.stderr = interpreter.diagnostic.Format(false)
					interpreter.exitStatus = 1

					native := buildAndRunOptionResultDirectELF(t, source)
					assertOptionResultProcessOutcome(t, "ELF-direct result_unwrap "+failure.name, native, interpreter)
				})
			}

			t.Run("source location and stack inside function", func(t *testing.T) {
				source := `fn unwrap(value: Result[Int, Int]) -> Int {
    return result_unwrap(value)
}
fn main() -> Nil {
    let failure: Result[Int, Int] = err(-19)
    println("before")
    println(unwrap(failure))
    return nil
}`
				interpreter := runOptionResultInterpreter(t, source)
				wantDiagnostic := &Diagnostic{
					Code: "KRY004", Category: CatRuntime, Severity: "error",
					Source: optionResultConformanceSourceName, Line: 2, Column: 25,
					Message: "cannot unwrap error Result: -19",
					Stack: []StackFrame{
						{Function: "unwrap", Source: optionResultConformanceSourceName, Line: 7, Column: 19},
						{Function: "main", Source: optionResultConformanceSourceName, Line: 4, Column: 1},
					},
				}
				assertOptionResultInterpreterDiagnostic(t, interpreter, wantDiagnostic)
				interpreter.stderr = interpreter.diagnostic.Format(false)
				interpreter.exitStatus = 1

				native := buildAndRunOptionResultDirectELF(t, source)
				assertOptionResultProcessOutcome(t, "ELF-direct result_unwrap from function", native, interpreter)
			})

			t.Run("reject unsupported error payload", func(t *testing.T) {
				source := optionResultFailureSource("Array[Int]", "[1]")
				program, checker := parseCheckOptionResult(t, source)
				_, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
				const want = "direct ELF backend cannot report result_unwrap error payload type Array[Int]; supported error payload types are String, Int, UInt, and Bool"
				if err == nil || err.Error() != want {
					t.Fatalf("unsupported error payload must be rejected explicitly: got %v, want %q", err, want)
				}
			})

			t.Run("multiple helper callsites preserve diagnostic stack", func(t *testing.T) {
				source := `fn unwrap(value: Result[Int, String]) -> Int {
    return result_unwrap(value)
}
fn main() -> Nil {
    let failure: Result[Int, String] = err("nested")
    println(unwrap(failure))
    println(unwrap(failure))
    return nil
}`
				interpreter := runOptionResultInterpreter(t, source)
				native := buildAndRunOptionResultDirectELF(t, source)
				assertOptionResultProcessOutcome(t, "ELF-direct repeated helper result_unwrap", native, interpreter)
			})
		})
	}
}

func optionResultFailureSource(errorType, errorValue string) string {
	return "fn main() -> Nil {\n" +
		"    let failure: Result[Int, " + errorType + "] = err(" + errorValue + ")\n" +
		"    println(\"before\")\n" +
		"    println(result_unwrap(failure))\n" +
		"    return nil\n" +
		"}\n"
}

func assertOptionResultInterpreterDiagnostic(t *testing.T, interpreter optionResultObservedOutcome, want *Diagnostic) {
	t.Helper()
	if interpreter.diagnostic == nil {
		t.Fatal("interpreter accepted result_unwrap on an error Result")
	}
	if !sameOptionResultDiagnostic(interpreter.diagnostic, want) {
		t.Fatalf("interpreter diagnostic changed:\n got: %#v\nwant: %#v", interpreter.diagnostic, want)
	}
}

func assertOptionResultProcessOutcome(t *testing.T, label string, got, want optionResultObservedOutcome) {
	t.Helper()
	if got.stdout != want.stdout || got.stderr != want.stderr || got.exitStatus != want.exitStatus {
		t.Errorf("%s diverged from interpreter:\n  stdout: got %q; want %q\n  stderr: got %q; want %q\n  exit status: got %d; want %d", label, got.stdout, want.stdout, got.stderr, want.stderr, got.exitStatus, want.exitStatus)
	}
	if got.diagnostic != nil {
		t.Errorf("%s unexpectedly reported an in-process Diagnostic from a native executable: %#v", label, got.diagnostic)
	}
}

func optionResultConformanceProgram(cases []optionResultConformanceCase, suffix string) string {
	statements := make([]string, 0, len(cases)+1)
	for _, testCase := range cases {
		statements = append(statements, "    "+testCase.statement)
	}
	if suffix != "" {
		statements = append(statements, "    "+suffix)
	}
	statements = append(statements, "    return nil")
	return "fn main() -> Nil {\n" + strings.Join(statements, "\n") + "\n}\n"
}

func optionResultProgram(statements []string, suffix string) string {
	cases := make([]optionResultConformanceCase, 0, len(statements))
	for i, statement := range statements {
		cases = append(cases, optionResultConformanceCase{name: fmt.Sprintf("statement %d", i), statement: statement})
	}
	return optionResultConformanceProgram(cases, suffix)
}

func optionResultInvocation(builtin string) string {
	switch builtin {
	case "some":
		return `println(is_some(some(1)))`
	case "none":
		return `println(is_none(none()))`
	case "ok":
		return `println(is_ok(ok(1)))`
	case "err":
		return `println(is_err(err("bad")))`
	case "is_some":
		return `println(is_some(some(1)))`
	case "is_none":
		return `println(is_none(none()))`
	case "is_ok":
		return `println(is_ok(ok(1)))`
	case "is_err":
		return `println(is_err(err("bad")))`
	case "unwrap_or":
		return `println(unwrap_or(none(), 1))`
	case "result_error":
		return `println(is_none(result_error(ok(1))))`
	case "result_unwrap":
		return `println(result_unwrap(err("bad")))`
	default:
		panic("missing invocation for " + builtin)
	}
}

func optionResultExpectedOutput(cases []optionResultConformanceCase) string {
	var out strings.Builder
	for _, testCase := range cases {
		out.WriteString(testCase.wantOutput)
	}
	return out.String()
}

func assertOptionResultCaseOutputs(t *testing.T, backend string, cases []optionResultConformanceCase, output string) {
	t.Helper()
	lines := strings.SplitAfter(normalizeOptionResultNewlines(output), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, testCase := range cases {
		t.Run(backend+"/"+testCase.name, func(t *testing.T) {
			if i >= len(lines) {
				t.Fatalf("missing output for case %q: got %q", testCase.name, output)
			}
			got := lines[i]
			if got != testCase.wantOutput {
				t.Errorf("case %q produced %q; want %q", testCase.name, got, testCase.wantOutput)
			}
		})
	}
	if len(lines) > len(cases) {
		t.Errorf("%s emitted %d unexpected output line(s): %q", backend, len(lines)-len(cases), lines[len(cases):])
	}
}

func optionResultBuiltinStatus(builtin, target, backend string) string {
	for _, row := range BuiltinCapabilityMatrix() {
		if row.Target != target || row.Builtin != builtin {
			continue
		}
		switch backend {
		case "c_aot":
			return row.CAOT
		case "elf_direct":
			return row.ELFDirect
		}
	}
	return ""
}

func parseCheckOptionResult(t *testing.T, source string) (*Program, *Checker) {
	t.Helper()
	program, diagnostic := Parse(&Source{Name: optionResultConformanceSourceName, Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("parse failed: %s", diagnostic.Format(false))
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("check failed: %s", diagnostic.Format(false))
	}
	return program, checker
}

func runOptionResultInterpreter(t *testing.T, source string) optionResultObservedOutcome {
	t.Helper()
	program, checker := parseCheckOptionResult(t, source)
	runtimeValue, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("runtime creation failed: %s", diagnostic.Format(false))
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		_ = stdoutRead.Close()
		_ = stdoutWrite.Close()
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutWrite, stderrWrite
	diagnostic = runtimeValue.run()
	_ = stdoutWrite.Close()
	_ = stderrWrite.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	stdout, stdoutErr := ioReadAllAndClose(stdoutRead)
	stderr, stderrErr := ioReadAllAndClose(stderrRead)
	if stdoutErr != nil {
		t.Fatal(stdoutErr)
	}
	if stderrErr != nil {
		t.Fatal(stderrErr)
	}
	outcome := optionResultObservedOutcome{stdout: normalizeOptionResultNewlines(stdout), stderr: normalizeOptionResultNewlines(stderr)}
	if diagnostic != nil {
		outcome.diagnostic = diagnostic
		outcome.exitStatus = 1
		// Runtime.run returns structured diagnostics; the CLI writes this stable
		// rendering to stderr after the call returns.
		outcome.stderr = normalizeOptionResultNewlines(diagnostic.Format(false))
	}
	return outcome
}

func ioReadAllAndClose(file *os.File) (string, error) {
	data, err := io.ReadAll(file)
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return string(data), nil
}

func buildAndRunOptionResultNative(t *testing.T, source string, target NativeTarget, format string) optionResultObservedOutcome {
	t.Helper()
	program, checker := parseCheckOptionResult(t, source)
	data, err := BuildNative(program, checker, target, format)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	extension := ""
	if target.OS == "windows" {
		extension = ".exe"
	}
	path := filepath.Join(t.TempDir(), "option-result"+extension)
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return runOptionResultExecutable(t, path)
}

func buildAndRunOptionResultDirectELF(t *testing.T, source string) optionResultObservedOutcome {
	t.Helper()
	program, checker := parseCheckOptionResult(t, source)
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("direct ELF build failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "option-result-elf")
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return runOptionResultExecutable(t, path)
}

func runOptionResultExecutable(t *testing.T, path string) optionResultObservedOutcome {
	t.Helper()
	command := exec.Command(path)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	outcome := optionResultObservedOutcome{
		stdout: normalizeOptionResultNewlines(stdout.String()),
		stderr: normalizeOptionResultNewlines(stderr.String()),
	}
	if err == nil {
		return outcome
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		outcome.exitStatus = exitError.ExitCode()
		return outcome
	}
	t.Fatalf("cannot run native artifact %q: %v", path, err)
	return outcome
}

func optionResultCompilerAvailable(target NativeTarget) bool {
	compiler, err := compilerFor(target)
	if err != nil {
		return false
	}
	_, err = exec.LookPath(compiler.program)
	return err == nil
}

func normalizeOptionResultNewlines(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func sameOptionResultDiagnostic(got, want *Diagnostic) bool {
	if got == nil || want == nil {
		return got == want
	}
	return got.Code == want.Code && got.Category == want.Category && got.Severity == want.Severity && got.Source == want.Source && got.Line == want.Line && got.Column == want.Column && got.Message == want.Message && reflect.DeepEqual(got.Stack, want.Stack)
}

func assertOptionResultOutcome(t *testing.T, label string, got, want optionResultObservedOutcome) {
	t.Helper()
	if got.stdout != want.stdout || got.stderr != want.stderr || got.exitStatus != want.exitStatus || !sameOptionResultDiagnostic(got.diagnostic, want.diagnostic) {
		t.Errorf("%s diverged from the reference:\n  stdout: got %q; want %q\n  stderr: got %q; want %q\n  exit status: got %d; want %d\n  Diagnostic: got %#v; want %#v", label, got.stdout, want.stdout, got.stderr, want.stderr, got.exitStatus, want.exitStatus, got.diagnostic, want.diagnostic)
	}
}
