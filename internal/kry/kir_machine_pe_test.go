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

func TestDirectPEArenaViewsPreserveEdgesWithoutRecursiveChildren(t *testing.T) {
	program, checker := testProgram(t, `
fn add(value: Int) -> Int { return value + 1 }
fn main() -> Nil {
    let result: Int = add(41)
    println(result)
    return nil
}
`)
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	arena := mir.arena
	if err := arena.validateReferences(); err != nil {
		t.Fatal(err)
	}
	var sawBinary, sawCall, sawStatement, sawFunction bool
	for index, row := range arena.Expressions {
		ref := MIRRef{Index: MIRIndex(index), Present: true}
		view, err := kirPEExpressionAt(arena, ref)
		if err != nil {
			t.Fatal(err)
		}
		if view.KIRExpr.Left != nil || view.KIRExpr.Right != nil || view.KIRExpr.Operand != nil || view.KIRExpr.Callee != nil || len(view.KIRExpr.Args) != 0 {
			t.Fatalf("PE expression row %d contains recursive child fields", index)
		}
		if row.Value.Kind == "binary" {
			sawBinary = true
			if !view.Left.Present || !view.Right.Present || view.Left.Index != row.Left.Index || view.Right.Index != row.Right.Index {
				t.Fatalf("PE binary view lost arena child references: %#v", row)
			}
		}
		if row.Value.Kind == "call" && row.Args.Count != 0 {
			sawCall = true
			if uint32(len(view.Args)) != row.Args.Count {
				t.Fatalf("PE call view lost arguments: got %d, want %d", len(view.Args), row.Args.Count)
			}
			for _, argument := range view.Args {
				if !argument.Present || uint64(argument.Index) >= uint64(len(arena.Expressions)) {
					t.Fatalf("PE call view contains an invalid argument reference: %#v", argument)
				}
			}
		}
	}
	for index, row := range arena.Statements {
		view, err := kirPEStatementAt(arena, MIRIndex(index))
		if err != nil {
			t.Fatal(err)
		}
		if view.KIRStmt.Init != nil || view.KIRStmt.Expr != nil || view.KIRStmt.Target != nil || len(view.KIRStmt.Then) != 0 || len(view.KIRStmt.Body) != 0 {
			t.Fatalf("PE statement row %d contains recursive child fields", index)
		}
		if row.Init.Present {
			sawStatement = true
			if !view.Init.Present || view.Init.Index != row.Init.Index {
				t.Fatalf("PE statement view lost initializer reference: %#v", row)
			}
		}
	}
	for index, row := range arena.Functions {
		view, err := kirPEFunctionAt(arena, MIRIndex(index))
		if err != nil {
			t.Fatal(err)
		}
		if len(view.KIRFunction.Body) != 0 || len(view.KIRFunction.Params) != 0 || len(view.KIRFunction.Captures) != 0 {
			t.Fatalf("PE function row %d contains recursive child fields", index)
		}
		if row.Params.Count != 0 {
			sawFunction = true
			if uint32(len(view.Params)) != row.Params.Count {
				t.Fatalf("PE function view lost parameters: got %d, want %d", len(view.Params), row.Params.Count)
			}
		}
	}
	if !sawBinary || !sawCall || !sawStatement || !sawFunction {
		t.Fatalf("PE arena edge fixture incomplete: binary=%t call=%t statement=%t function=%t", sawBinary, sawCall, sawStatement, sawFunction)
	}
}

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

func TestDirectPEStaticOutputLoweringUsesValidatedArena(t *testing.T) {
	program, checker := testProgram(t, "fn main() -> Nil { println(\"Kryndel \" + str(40 + 2)); return nil }")
	encoded, err := EmitKIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	mir, err := DecodeMIR(encoded, checker.Env.Lim)
	if err != nil {
		t.Fatal(err)
	}
	program, checker = nil, nil
	chunks, err := directStaticOutputMIR(mir.arena, mir.limits.MaxOutputBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(bytes.Join(chunks, nil)); got != "Kryndel 42\n" {
		t.Fatalf("arena static output = %q, want %q", got, "Kryndel 42\n")
	}
	image, err := lowerDirectPEKIR(mir, mir.limits)
	if err != nil {
		t.Fatalf("lower validated arena to PE: %v", err)
	}
	if _, err := pe.NewFile(bytes.NewReader(image)); err != nil {
		t.Fatalf("Go PE parser rejected arena-lowered image: %v", err)
	}
}

func TestDirectPEStaticLoweringRequiresAnEntryPoint(t *testing.T) {
	program, checker := testProgram(t, "fn helper() -> Int { return 42 }")
	encoded, err := EmitKIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	mir, err := DecodeMIR(encoded, checker.Env.Lim)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lowerDirectPEKIR(mir, mir.limits)
	if err == nil || !strings.Contains(err.Error(), "program requires top-level statements or main() -> Nil") {
		t.Fatalf("PE lowering without an entry point returned %v", err)
	}
}
