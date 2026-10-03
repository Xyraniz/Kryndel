package kry

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestProductionLoweringEntrypointsUseValidatedMIR(t *testing.T) {
	checkCalls := func(fileName, functionName string, required ...string) {
		t.Helper()
		file, err := parser.ParseFile(token.NewFileSet(), fileName, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", fileName, err)
		}
		var function *ast.FuncDecl
		for _, declaration := range file.Decls {
			candidate, ok := declaration.(*ast.FuncDecl)
			if ok && candidate.Name.Name == functionName {
				function = candidate
				break
			}
		}
		if function == nil || function.Body == nil {
			t.Fatalf("%s does not declare function %s", fileName, functionName)
		}
		calls := map[string]bool{}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				calls[callee.Name] = true
			case *ast.SelectorExpr:
				calls[callee.Sel.Name] = true
			}
			return true
		})
		for _, name := range required {
			if !calls[name] {
				t.Errorf("%s.%s no longer calls %s", fileName, functionName, name)
			}
		}
	}

	checkCalls("runtime.go", "NewRuntimeWithArgs", "CompileMIR", "newRuntimeFromMIR")
	checkCalls("runtime.go", "RunForREPL", "runValidatedMIR")
	checkCalls("codegen.go", "GenerateC", "generateC")
	checkCalls("codegen.go", "GenerateCObfuscated", "generateC")
	checkCalls("codegen.go", "generateC", "CompileMIR", "generateCFromValidatedKIR")
	checkCalls("engine.go", "RunPathWithArgs", "CompileMIR", "newRuntimeFromMIR")
	checkCalls("engine.go", "DebugPathWithArgs", "CompileMIR", "newRuntimeFromMIR")
	checkCalls("machine.go", "BuildDirectELF", "CompileMIR", "buildDirectELFFromMIR")
	checkCalls("machine_pe.go", "BuildDirectPE", "CompileMIR", "lowerDirectPEKIR")
	checkCalls("native.go", "BuildNativeWithPolicyOpts", "CompileMIR", "generateCFromValidatedKIR")
}

func TestNativeMachineLowerersDoNotAcceptSourceAST(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	legacyEntryPoints := map[string]bool{
		"buildDirectDynamicELF":   true,
		"directDynamicStatements": true,
		"validateDirectPEProgram": true,
		"directStaticOutput":      true,
		"directStaticValue":       true,
	}
	for _, fileName := range files {
		file, err := parser.ParseFile(token.NewFileSet(), fileName, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", fileName, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if legacyEntryPoints[function.Name.Name] {
				t.Errorf("legacy AST lowering entrypoint %s remains in %s", function.Name.Name, fileName)
			}
			if !isDirectMachineMethod(function) || function.Type.Params == nil {
				continue
			}
			ast.Inspect(function.Type.Params, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				switch identifier.Name {
				case "Expr", "Stmt", "Program", "TypeSpec":
					t.Errorf("directMachine.%s accepts source AST type %s in %s", function.Name.Name, identifier.Name, fileName)
				}
				return true
			})
		}
	}
}

func TestRunForREPLRequiresValidatedMIR(t *testing.T) {
	program, checker := testProgram(t, "let visible = 42")
	runtime, diagnostic := newASTOracleRuntime(program, checker, DefaultLimits(), Sandbox{}, nil)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	diagnostic = runtime.RunForREPL()
	if diagnostic == nil {
		t.Fatal("RunForREPL without validated MIR should fail closed")
	}
	if diagnostic.Category != CatArtifact || diagnostic.Message != "REPL execution requires validated MIR" {
		t.Fatalf("RunForREPL without validated MIR returned %#v", diagnostic)
	}
	if _, exists := runtime.Global.get("visible"); exists {
		t.Fatal("RunForREPL executed the AST fallback despite missing MIR")
	}
}

func TestRuntimeExecutionRequiresValidatedMIR(t *testing.T) {
	runtime := &Runtime{}
	diagnostic := runtime.run()
	if diagnostic == nil {
		t.Fatal("runtime without validated MIR executed")
	}
	if diagnostic.Category != CatArtifact || diagnostic.Message != "interpreter execution requires validated MIR" {
		t.Fatalf("runtime without validated MIR returned %#v", diagnostic)
	}
}

func TestRunForREPLExecutesValidatedMIR(t *testing.T) {
	program, checker := testProgram(t, "40 + 2")
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.RunForREPL(); diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if output.String() != "42\n" {
		t.Fatalf("RunForREPL output = %q, want 42 followed by a newline", output.String())
	}
}

func isDirectMachineMethod(function *ast.FuncDecl) bool {
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver, ok := function.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	name, ok := receiver.X.(*ast.Ident)
	return ok && name.Name == "directMachine"
}
