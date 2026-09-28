package kry

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildDirectOutputProgram(t *testing.T, source string, outputLimit int64) (*Program, *Checker) {
	t.Helper()
	limits := DefaultLimits()
	limits.MaxOutputBytes = outputLimit
	program, diagnostic := Parse(&Source{Name: "direct-output.kry", Text: source}, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	return program, checker
}

func writeDirectProgram(t *testing.T, suffix string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "direct-output"+suffix)
	mode := os.FileMode(0o700)
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func directExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	return exitErr.ExitCode(), true
}

func TestDirectELFOutputLimitPreflightsWholePrintln(t *testing.T) {
	program, checker := buildDirectOutputProgram(t, `print("a")
println("b")
`, 2)
	image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectNative(image); err != nil {
		t.Fatalf("direct ELF output image is invalid: %v", err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("generated ELF execution requires native linux-amd64")
	}
	path := writeDirectProgram(t, "", image)
	astOutput, astDiagnostic := directRuntimeResult(t, program, checker)
	if astDiagnostic == nil {
		t.Fatal("AST runtime did not report the configured output limit")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(path)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if status, ok := directExitCode(err); !ok || status != 1 || stdout.String() != string(astOutput) || stderr.String() != astDiagnostic.Format(false) {
		t.Fatalf("accumulated output limit: err=%v stdout=%q stderr=%q, want exit 1 and AST stdout/stderr %q/%q", err, stdout.String(), stderr.String(), astOutput, astDiagnostic.Format(false))
	}
}

func TestDirectELFDynamicPrintKindsCountTheCompleteNewline(t *testing.T) {
	tests := []struct {
		name              string
		source            string
		limit             int64
		wantOutput        string
		wantASTDiagnostic bool
	}{
		{name: "dynamic string", source: "let mut text: String = \"xy\"\nif true { println(text) }\n", limit: 3, wantOutput: "xy\n"},
		{name: "dynamic integer", source: "let mut value: Int = 12\nif true { println(value) }\n", limit: 3, wantOutput: "12\n"},
		{name: "dynamic bool", source: "let mut value: Bool = true\nif true { println(value) }\n", limit: 5, wantOutput: "true\n"},
		{name: "accumulated loop", source: "let mut value: Int = 0\nwhile value < 2 { println(\"x\"); value = value + 1 }\n", limit: 3, wantOutput: "x\n", wantASTDiagnostic: true},
		{name: "counter shared with callee", source: "fn emit() -> Nil { println(\"b\"); return nil }\nfn main() -> Nil { print(\"a\"); emit(); return nil }\n", limit: 2, wantOutput: "a", wantASTDiagnostic: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := buildDirectOutputProgram(t, test.source, test.limit)
			image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := InspectNative(image); err != nil {
				t.Fatalf("direct ELF output image is invalid: %v", err)
			}
			if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
				return
			}
			path := writeDirectProgram(t, "", image)
			wantStderr := ""
			if test.wantASTDiagnostic {
				_, diagnostic := directRuntimeResult(t, program, checker)
				if diagnostic == nil {
					t.Fatal("AST runtime did not report the configured output limit")
				}
				wantStderr = diagnostic.Format(false)
			}
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(path)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err = cmd.Run()
			if test.name == "accumulated loop" || test.name == "counter shared with callee" {
				if status, ok := directExitCode(err); !ok || status != 1 || stdout.String() != test.wantOutput || stderr.String() != wantStderr {
					t.Fatalf("loop output/status = %q/%v stderr=%q, want %q/1/%q", stdout.String(), err, stderr.String(), test.wantOutput, wantStderr)
				}
				return
			}
			if err != nil || stdout.String() != test.wantOutput || stderr.Len() != 0 {
				t.Fatalf("output/status = %q/%v stderr=%q, want %q/0/empty", stdout.String(), err, stderr.String(), test.wantOutput)
			}
		})
	}
}

func directRuntimeResult(t *testing.T, program *Program, checker *Checker) ([]byte, *Diagnostic) {
	t.Helper()
	var output bytes.Buffer
	runtime, diagnostic := NewRuntime(program, checker, checker.Env.Lim, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	runtime.output = &output
	diagnostic = runtime.run()
	return output.Bytes(), diagnostic
}

func TestDirectELFWriteFailureExitsNonzero(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("ELF output failure test requires native linux-amd64")
	}
	for _, source := range []string{
		"println(\"static\")\n",
		"let mut value: Int = 1\nwhile value < 2 { println(value); value = value + 1 }\n",
	} {
		program, checker := buildDirectOutputProgram(t, source, 64)
		image, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
		if err != nil {
			t.Fatal(err)
		}
		path := writeDirectProgram(t, "", image)
		full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(path)
		cmd.Stdout = full
		err = cmd.Run()
		_ = full.Close()
		if status, ok := directExitCode(err); !ok || status != 1 {
			t.Fatalf("ELF write to /dev/full: err=%v, want exit status 1", err)
		}
	}
}

func TestDirectPEOutputLimitCountsNewlinesAndPriorPrints(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		limit      int64
		wantOutput string
		wantStatus int
	}{
		{name: "static exact println", source: "println(\"x\")\n", limit: 2, wantOutput: "x\n"},
		{name: "static newline over limit", source: "println(\"x\")\n", limit: 1, wantStatus: 1},
		{name: "cumulative println is atomic", source: "print(\"a\")\nprintln(\"b\")\n", limit: 2, wantOutput: "a", wantStatus: 1},
		{name: "dynamic string includes newline", source: "let mut text: String = \"xy\"\nif true { println(text) }\n", limit: 2, wantStatus: 1},
		{name: "dynamic string exact limit", source: "let mut text: String = \"xy\"\nif true { println(text) }\n", limit: 3, wantOutput: "xy\n"},
		{name: "dynamic integer includes newline", source: "let mut value: Int = 12\nif true { println(value) }\n", limit: 2, wantStatus: 1},
		{name: "dynamic integer exact limit", source: "let mut value: Int = 12\nif true { println(value) }\n", limit: 3, wantOutput: "12\n"},
		{name: "dynamic bool includes newline", source: "let mut value: Bool = true\nif true { println(value) }\n", limit: 4, wantStatus: 1},
		{name: "dynamic bool exact limit", source: "let mut value: Bool = true\nif true { println(value) }\n", limit: 5, wantOutput: "true\n"},
		{name: "dynamic repeated output", source: "let mut value: Int = 0\nwhile value < 2 { println(\"x\"); value = value + 1 }\n", limit: 3, wantOutput: "x\n", wantStatus: 1},
		{name: "counter shared with callee", source: "fn emit() -> Nil { println(\"b\"); return nil }\nfn main() -> Nil { print(\"a\"); emit(); return nil }\n", limit: 2, wantOutput: "a", wantStatus: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, checker := buildDirectOutputProgram(t, test.source, test.limit)
			image, err := BuildDirectPE(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := pe.NewFile(bytes.NewReader(image))
			if err != nil {
				t.Fatalf("PE parser rejected output image: %v", err)
			}
			defer parsed.Close()
			text, err := parsed.Sections[0].Data()
			if err != nil {
				t.Fatal(err)
			}
			var encodedLimit [8]byte
			binary.LittleEndian.PutUint64(encodedLimit[:], uint64(test.limit))
			if !bytes.Contains(text, append([]byte{0x48, 0xb9}, encodedLimit[:]...)) {
				t.Fatalf("PE code does not contain the configured cumulative output limit %d", test.limit)
			}
			imports, err := parsed.ImportedSymbols()
			if err != nil || !containsFold(imports, "WriteFile:KERNEL32.dll") {
				t.Fatalf("PE output image does not import WriteFile: err=%v imports=%v", err, imports)
			}
			if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
				return
			}
			path := writeDirectProgram(t, ".exe", image)
			out, runErr := exec.Command(path).CombinedOutput()
			status := 0
			if runErr != nil {
				var ok bool
				status, ok = directExitCode(runErr)
				if !ok {
					t.Fatalf("generated PE execution failed: %v output=%q", runErr, out)
				}
			}
			if status != test.wantStatus || string(out) != test.wantOutput {
				t.Fatalf("PE output/status = %q/%d, want %q/%d", out, status, test.wantOutput, test.wantStatus)
			}
		})
	}
}

func TestDirectPEWriteFailureExitsNonzero(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("PE output failure test requires native windows-amd64")
	}
	program, checker := buildDirectOutputProgram(t, "println(\"closed pipe\")\n", 128)
	image, err := BuildDirectPE(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	path := writeDirectProgram(t, ".exe", image)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(path)
	cmd.Stdout = writer
	err = cmd.Run()
	_ = writer.Close()
	if status, ok := directExitCode(err); !ok || status != 1 {
		t.Fatalf("WriteFile to closed stdout pipe: err=%v, want exit status 1", err)
	}
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}
