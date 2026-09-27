package kry

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func runOutputSemantics(t *testing.T, source string, outputLimit int64) (string, *Diagnostic, string, string, int) {
	t.Helper()
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT output differential tests require linux/amd64")
	}

	limits := DefaultLimits()
	limits.MaxOutputBytes = outputLimit
	program, diagnostic := Parse(&Source{Name: "output.kry", Text: source}, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}

	var interpreted bytes.Buffer
	runtimeValue, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	runtimeValue.output = &interpreted
	interpreterDiagnostic := runtimeValue.run()

	data, err := BuildNative(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	binary := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(binary, data, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary)
	var nativeOut, nativeErr bytes.Buffer
	cmd.Stdout = &nativeOut
	cmd.Stderr = &nativeErr
	nativeStatus := 0
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			nativeStatus = exit.ExitCode()
		} else {
			t.Fatalf("C AOT execution failed: %v", err)
		}
	}
	return interpreted.String(), interpreterDiagnostic, nativeOut.String(), nativeErr.String(), nativeStatus
}

func TestPrintAndPrintlnRespectExactOutputLimitAcrossInterpreterAndCAOT(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		limit       int64
		wantOutput  string
		wantFailure bool
	}{
		{name: "print exact limit excludes newline", source: "fn main() -> Nil { print(\"x\"); return nil }\n", limit: 1, wantOutput: "x"},
		{name: "println exact limit includes newline", source: "fn main() -> Nil { println(\"x\"); return nil }\n", limit: 2, wantOutput: "x\n"},
		{name: "println newline exceeds limit", source: "fn main() -> Nil { println(\"x\"); return nil }\n", limit: 1, wantFailure: true},
		{name: "limit is cumulative", source: "fn main() -> Nil { print(\"a\"); println(\"b\"); return nil }\n", limit: 2, wantOutput: "a", wantFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interpreted, diagnostic, nativeOut, nativeErr, nativeStatus := runOutputSemantics(t, test.source, test.limit)
			if test.wantFailure {
				if diagnostic == nil || diagnostic.Category != CatResource || diagnostic.Message != "output limit exceeded" {
					t.Fatalf("interpreter diagnostic = %#v, want resource output limit exceeded", diagnostic)
				}
				if interpreted != test.wantOutput || nativeOut != test.wantOutput || nativeStatus != 1 || nativeErr != "kryndel: output limit exceeded\n" {
					t.Fatalf("limit failure differs: interpreter output %q, C AOT stdout=%q stderr=%q status=%d", interpreted, nativeOut, nativeErr, nativeStatus)
				}
				return
			}
			if diagnostic != nil || interpreted != test.wantOutput {
				t.Fatalf("interpreter output/diagnostic = %q / %#v, want %q / nil", interpreted, diagnostic, test.wantOutput)
			}
			if nativeStatus != 0 || nativeOut != test.wantOutput || nativeErr != "" {
				t.Fatalf("C AOT output differs: stdout=%q stderr=%q status=%d, want %q / empty / 0", nativeOut, nativeErr, nativeStatus, test.wantOutput)
			}
		})
	}
}

type failedOutputWriter struct{}

func (failedOutputWriter) Write([]byte) (int, error) { return 0, errors.New("closed output") }

func TestInterpreterPrintReportsInjectedWriterFailure(t *testing.T) {
	limits := DefaultLimits()
	source := &Source{Name: "failed-output.kry", Text: "fn main() -> Nil { println(\"x\"); return nil }\n"}
	program, diagnostic := Parse(source, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	runtimeValue, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	runtimeValue.output = failedOutputWriter{}

	diagnostic = runtimeValue.run()
	if diagnostic == nil || diagnostic.Category != CatIO || diagnostic.Message != "stream failure" || diagnostic.Source != source.Name || diagnostic.Line != 1 || diagnostic.Column == 0 {
		t.Fatalf("writer failure diagnostic = %#v, want io stream failure from %s", diagnostic, source.Name)
	}
}

func TestCAOTPrintReportsStreamFailure(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT stream failure test requires linux/amd64")
	}
	limits := DefaultLimits()
	source := &Source{Name: "failed-output.kry", Text: "fn main() -> Nil { println(\"x\"); return nil }\n"}
	program, diagnostic := Parse(source, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	data, err := BuildNative(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(binary, data, 0o700); err != nil {
		t.Fatal(err)
	}
	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer full.Close()
	cmd := exec.Command(binary)
	cmd.Stdout = full
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || stderr.String() != "kryndel: stream failure\n" {
		t.Fatalf("C AOT stream failure = err %v, stderr %q; want exit 1 and stream diagnostic", err, stderr.String())
	}
}
