package kry

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
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
	checkCalls("runtime.go", "NewRuntime", "NewRuntimeWithArgs")
	checkCalls("runtime.go", "RunForREPL", "runValidatedMIR")
	checkCalls("codegen.go", "GenerateC", "generateC")
	checkCalls("codegen.go", "GenerateCObfuscated", "generateC")
	checkCalls("codegen.go", "generateC", "CompileMIR", "generateCFromValidatedKIR")
	checkCalls("engine.go", "RunPathWithArgs", "CompileMIR", "newRuntimeFromMIR")
	checkCalls("engine.go", "RunPath", "RunPathWithArgs")
	checkCalls("engine.go", "DebugPathWithArgs", "CompileMIR", "newRuntimeFromMIR")
	checkCalls("machine.go", "BuildDirectELF", "CompileMIR", "buildDirectELFFromMIR")
	checkCalls("machine_pe.go", "BuildDirectPE", "CompileMIR", "lowerDirectPEKIR")
	checkCalls("native.go", "BuildNative", "BuildNativeOpts")
	checkCalls("native.go", "BuildNativeOpts", "BuildNativeWithPolicyOpts")
	checkCalls("native.go", "BuildNativeWithPolicyOpts", "CompileMIR", "generateCFromValidatedKIR")
	checkCalls("native.go", "EmitC", "GenerateC")
}

func TestNativeMachineLowerersDoNotAcceptSourceAST(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	loweringFiles := map[string]bool{
		"kir_codegen.go": true, "kir_exec.go": true, "machine_code.go": true, "machine_kir.go": true,
	}
	machineFiles, err := filepath.Glob("kir_machine*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, fileName := range machineFiles {
		if strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		loweringFiles[fileName] = true
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
			if (!isDirectMachineMethod(function) && !loweringFiles[fileName]) || function.Type.Params == nil {
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

func TestNativeCapabilityPreflightTraversesValidatedArena(t *testing.T) {
	checks := map[string][]string{
		"kir_capabilities.go":       {"validateMIRNativeBuiltinSupport", "validateMIRFunctionValueSupport", "kirArenaUsesFunctionValue"},
		"kir_capabilities_arena.go": {"validateMIRNativeFeatureSupport"},
	}
	for fileName, functionNames := range checks {
		file, err := parser.ParseFile(token.NewFileSet(), fileName, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", fileName, err)
		}
		for _, functionName := range functionNames {
			var function *ast.FuncDecl
			for _, declaration := range file.Decls {
				candidate, ok := declaration.(*ast.FuncDecl)
				if ok && candidate.Name.Name == functionName {
					function = candidate
					break
				}
			}
			if function == nil || function.Body == nil {
				t.Fatalf("%s does not declare %s", fileName, functionName)
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch callee := call.Fun.(type) {
				case *ast.Ident:
					name = callee.Name
				case *ast.SelectorExpr:
					name = callee.Sel.Name
				}
				switch name {
				case "documentView", "toKIRDocument", "DecodeKIR":
					t.Errorf("%s.%s uses recursive wire view %s during capability preflight", fileName, functionName, name)
				}
				return true
			})
		}
	}
}

func TestDirectPEStaticLowererConsumesOnlyTheValidatedArena(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "kir_machine_pe_arena.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"directStaticOutputMIR":        false,
		"directPEStaticMIRValue":       false,
		"directStaticMIRArenaConstant": false,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !wantedHas(wanted, function.Name.Name) {
			continue
		}
		wanted[function.Name.Name] = true
		ast.Inspect(function.Type, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "KIRDocument" || identifier.Name == "KIRExpr" || identifier.Name == "KIRStmt") {
				t.Errorf("%s accepts recursive wire type %s", function.Name.Name, identifier.Name)
			}
			return true
		})
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				name = callee.Name
			case *ast.SelectorExpr:
				name = callee.Sel.Name
			}
			switch name {
			case "documentView", "toKIRDocument", "DecodeKIR":
				t.Errorf("%s reconstructs a recursive KIR document through %s", function.Name.Name, name)
			}
			return true
		})
	}
	for name, found := range wanted {
		if !found {
			t.Errorf("arena static lowerer does not declare %s", name)
		}
	}

	peFile, err := parser.ParseFile(token.NewFileSet(), "kir_machine_pe.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	var lowerer *ast.FuncDecl
	for _, declaration := range peFile.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "lowerDirectPEKIR" {
			lowerer = candidate
			break
		}
	}
	if lowerer == nil {
		t.Fatal("lowerDirectPEKIR is missing")
	}
	var staticPos, recursiveViewPos token.Pos
	ast.Inspect(lowerer.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "documentView" {
			recursiveViewPos = call.Pos()
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if ok && identifier.Name == "directStaticOutputMIR" {
			staticPos = call.Pos()
		}
		return true
	})
	if staticPos == token.NoPos || recursiveViewPos == token.NoPos || staticPos >= recursiveViewPos {
		t.Fatal("PE static output must lower from arena indexes before any dynamic compatibility view is created")
	}
}

func wantedHas(wanted map[string]bool, name string) bool {
	_, exists := wanted[name]
	return exists
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
