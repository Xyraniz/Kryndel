package kry

import (
	"os/exec"
	"strings"
	"testing"
)

const defaultArgumentScopeProgram = `fn choose(first: Int, second: Int = first) -> Int {
    return second
}
fn main() -> Nil {
    let first: Int = 99
    println(choose(4))
    return nil
}`

func checkedDefaultArgumentProgram(t *testing.T) (*Program, *Checker) {
	t.Helper()
	program, d := Parse(&Source{Name: "default-scope.kry", Text: defaultArgumentScopeProgram}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	return program, checker
}

func TestCBackendEvaluatesDefaultWithEarlierParameter(t *testing.T) {
	program, checker := checkedDefaultArgumentProgram(t)
	generated, err := GenerateC(program, checker)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated, "KValue second = (_argc > 1) ? k_args[1] : first;") {
		t.Fatal("C backend did not evaluate the default after binding the earlier parameter")
	}
	if !strings.Contains(generated, "k_argc = 1; memcpy(k_args, _frame, sizeof(KValue)*1);") {
		t.Fatal("C backend did not pass the explicit argument count")
	}
}

func TestCBackendCompilesDefaultArgumentProgramWhenCompilerAvailable(t *testing.T) {
	target := NativeTarget{OS: "windows", Arch: "amd64"}
	compiler, err := compilerFor(target)
	if err != nil {
		t.Skipf("Windows C compiler is unavailable: %v", err)
	}
	if _, err := exec.LookPath(compiler.program); err != nil {
		t.Skipf("Windows C compiler %q is unavailable: %v", compiler.program, err)
	}
	program, checker := checkedDefaultArgumentProgram(t)
	if _, err := BuildNative(program, checker, target, "exe"); err != nil {
		t.Fatal(err)
	}
}

func TestDirectELFRejectsOmittedDefaultArguments(t *testing.T) {
	program, checker := checkedDefaultArgumentProgram(t)
	if _, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "does not support omitted default arguments") {
		t.Fatalf("expected explicit unsupported-default diagnostic, got %v", err)
	}
}
