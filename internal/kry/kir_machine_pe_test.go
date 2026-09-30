package kry

import (
	"bytes"
	"debug/pe"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirectPELowersValidatedKIRWithoutAST(t *testing.T) {
	cases := []struct {
		name           string
		source         string
		wantOutput     string
		wantStatus     int
		wantCategory   Category
		wantDiagnostic string
	}{
		{
			name:       "static string and str builtin",
			source:     "fn main() -> Nil {\n    println(\"Kryndel \" + str(40 + 2))\n    return nil\n}\n",
			wantOutput: "Kryndel 42\n",
			wantStatus: 0,
		},
		{
			name:           "unsigned narrowing overflow",
			source:         "print(\"before:\")\nlet value: UInt8 = u8(256)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "value is outside unsigned range",
		},
		{
			name:           "shift count at UInt width",
			source:         "print(\"before:\")\nprintln(u8(1) << 8)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "shift count must be between 0 and UInt width minus one",
		},
		{
			name:           "negative shift count",
			source:         "print(\"before:\")\nprintln(u8(1) >> -1)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "shift count must be between 0 and UInt width minus one",
		},
		{
			name:           "integer negation overflow",
			source:         "let minimum: Int = -9223372036854775807 - 1\nprint(\"before:\")\nprintln(-minimum)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "negation overflow",
		},
		{
			name:           "integer addition overflow",
			source:         "let maximum: Int = 9223372036854775807\nprint(\"before:\")\nprintln(maximum + 1)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "checked integer arithmetic overflow",
		},
		{
			name:           "integer subtraction overflow",
			source:         "let minimum: Int = -9223372036854775807 - 1\nprint(\"before:\")\nprintln(minimum - 1)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "checked integer arithmetic overflow",
		},
		{
			name:           "integer multiplication overflow",
			source:         "let value: Int = 3037000500\nprint(\"before:\")\nprintln(value * value)\n",
			wantOutput:     "before:",
			wantStatus:     1,
			wantCategory:   CatRuntime,
			wantDiagnostic: "checked integer arithmetic overflow",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			program, diagnostic := Parse(&Source{Name: "kir-direct-pe.kry", Text: test.source}, limits)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			checker, diagnostic := Check(program, limits)
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			interpreter, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			var interpretedOutput bytes.Buffer
			interpreter.output = &interpretedOutput
			interpreterDiagnostic := interpreter.run()
			if test.wantDiagnostic == "" {
				if interpreterDiagnostic != nil {
					t.Fatalf("interpreter unexpectedly failed: %v", interpreterDiagnostic)
				}
			} else if interpreterDiagnostic == nil || interpreterDiagnostic.Category != test.wantCategory || interpreterDiagnostic.Message != test.wantDiagnostic {
				t.Fatalf("interpreter diagnostic = %#v, want %s %q", interpreterDiagnostic, test.wantCategory, test.wantDiagnostic)
			}

			encoded, err := EmitKIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			document, err := DecodeMIR(encoded, limits)
			if err != nil {
				t.Fatal(err)
			}
			// Ensure this backend boundary cannot accidentally fall back to the
			// source AST or checker after KIR has been emitted.
			program, checker = nil, nil
			imageBytes, err := lowerDirectPEKIR(document, limits)
			if err != nil {
				t.Fatalf("KIR-to-PE lowering failed: %v", err)
			}
			image, err := pe.NewFile(bytes.NewReader(imageBytes))
			if err != nil {
				t.Fatalf("Go PE parser rejected KIR image: %v", err)
			}
			if image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(image.Sections) != 4 {
				t.Fatalf("invalid KIR PE image: machine=%#x sections=%d", image.Machine, len(image.Sections))
			}
			if image.OptionalHeader.(*pe.OptionalHeader64).Subsystem != peSubsystemConsole {
				t.Fatalf("KIR PE subsystem = %d, want console", image.OptionalHeader.(*pe.OptionalHeader64).Subsystem)
			}
			if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
				return
			}
			path := filepath.Join(t.TempDir(), "kir-direct-pe.exe")
			if err := os.WriteFile(path, imageBytes, 0o755); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(path)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			status := 0
			if err := command.Run(); err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatalf("KIR PE execution failed: %v", err)
				}
				status = exitErr.ExitCode()
			}
			wantStderr := ""
			if test.wantDiagnostic != "" {
				wantStderr = "kryndel: " + test.wantDiagnostic + "\n"
			}
			if interpretedOutput.String() != test.wantOutput || stdout.String() != interpretedOutput.String() || stderr.String() != wantStderr || status != test.wantStatus {
				t.Fatalf("interpreter output/status=%q/%d; PE output/stderr/status=%q/%q/%d, want stderr %q", interpretedOutput.String(), test.wantStatus, stdout.String(), stderr.String(), status, wantStderr)
			}
		})
	}
}

func TestDirectPEKIRUnsupportedDiagnosticsDoNotNeedAST(t *testing.T) {
	program, checker := testProgram(t, "let mut value: String = \"x\"\nprintln(value == \"x\")\n")
	limits := checker.Env.Lim
	encoded, err := EmitKIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := DecodeMIR(encoded, limits)
	if err != nil {
		t.Fatal(err)
	}
	program, checker = nil, nil
	_, err = lowerDirectPEKIR(document, limits)
	if err == nil || !strings.Contains(err.Error(), "direct PE dynamic subset") || !strings.Contains(err.Error(), "String comparison is not supported by the direct PE runtime") {
		t.Fatalf("KIR unsupported diagnostic = %v", err)
	}
}
