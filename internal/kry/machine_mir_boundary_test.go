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
	checkCalls("runtime.go", "newRuntimeFromMIR", "validateReferences")
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
	checkCalls("kir_machine_pe.go", "lowerDirectPEKIR", "validateMIRNativeFeatureSupport")
	checkCalls("native.go", "BuildNative", "BuildNativeOpts")
	checkCalls("native.go", "BuildNativeOpts", "BuildNativeWithPolicyOpts")
	checkCalls("native.go", "BuildNativeWithPolicyOpts", "CompileMIR", "generateCFromValidatedKIR")
	checkCalls("native.go", "EmitC", "GenerateC")
}

func TestCompileMIRBuildsFlatArenaWithoutRecursiveKIRDocument(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "ir.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	var compile *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "CompileMIR" {
			compile = function
			break
		}
	}
	if compile == nil || compile.Body == nil {
		t.Fatal("ir.go does not declare CompileMIR")
	}
	compileCalls := map[string]bool{}
	ast.Inspect(compile.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if identifier, ok := call.Fun.(*ast.Ident); ok {
			compileCalls[identifier.Name] = true
		}
		return true
	})
	if !compileCalls["buildKIRArenaFromCheckedSource"] {
		t.Fatal("CompileMIR does not build the arena directly from checked source")
	}
	for _, forbidden := range []string{"buildKIRDocument", "validateKIRDocument", "newKIRArena", "documentView", "toKIRDocument"} {
		if compileCalls[forbidden] {
			t.Errorf("CompileMIR constructs or validates through recursive KIR helper %s", forbidden)
		}
	}

	file, err = parser.ParseFile(token.NewFileSet(), "kir_arena_builder.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"buildKIRDocument", "validateKIRDocument", "newKIRArena", "documentView", "toKIRDocument", "kirExpr", "kirStmt", "kirStmts", "kirLambda"} {
		ast.Inspect(file, func(node ast.Node) bool {
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
			if name == forbidden {
				t.Errorf("direct source arena builder calls recursive KIR helper %s", forbidden)
			}
			return true
		})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == "KIRDocument" {
			t.Error("direct checked-source arena builder depends on the recursive wire document type")
		}
		return true
	})

	file, err = parser.ParseFile(token.NewFileSet(), "kir.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch function.Name.Name {
		case "buildKIRDocument", "kirExpr", "kirStmt", "kirStmts", "kirLambda", "kirValue":
			t.Errorf("recursive source-to-wire builder %s remains in production", function.Name.Name)
		}
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "kirExprScalars" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if identifier, ok := call.Fun.(*ast.Ident); ok && (identifier.Name == "kirExpr" || identifier.Name == "kirLambda" || identifier.Name == "kirValue") {
				t.Errorf("scalar expression lowering calls recursive tree builder %s", identifier.Name)
			}
			return true
		})
		return
	}
	t.Fatal("kir.go does not declare kirExprScalars")
}

func TestRuntimePreparationDoesNotMaterializeRecursiveKIR(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "runtime.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "newRuntimeFromMIR" {
			continue
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
			case "documentView", "toKIRDocument", "validateKIRDocument":
				t.Errorf("runtime preparation calls %s after MIR validation", name)
			}
			return true
		})
		return
	}
	t.Fatal("runtime.go does not declare newRuntimeFromMIR")
}

func TestCAOTLoweringUsesFlatValidatedMIRRows(t *testing.T) {
	for _, fileName := range []string{"codegen.go", "codegen_arena.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), fileName, nil, parser.AllErrors)
		if err != nil {
			t.Fatalf("parse %s: %v", fileName, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && (identifier.Name == "documentView" || identifier.Name == "toKIRDocument") {
				t.Errorf("C AOT lowering in %s reconstructs a recursive KIR document", fileName)
			}
			return true
		})
		if fileName != "codegen.go" {
			continue
		}
		for _, declaration := range file.Decls {
			typeDeclaration, ok := declaration.(*ast.GenDecl)
			if !ok || typeDeclaration.Tok != token.TYPE {
				continue
			}
			for _, spec := range typeDeclaration.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != "cgen" {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					t.Fatal("cgen is not a struct")
				}
				ast.Inspect(structure.Fields, func(node ast.Node) bool {
					identifier, ok := node.(*ast.Ident)
					if ok && identifier.Name == "KIRDocument" {
						t.Error("cgen retains a recursive KIR document instead of the validated arena")
					}
					return true
				})
			}
		}
	}

	program, checker := testProgram(t, "fn main() -> Nil { println(1 + 2); return nil }\n")
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	arena := mir.arena
	generator := &cgen{
		arena:           arena,
		expressionNodes: make([]*KIRExpr, len(arena.Expressions)),
		expressionIndex: make(map[*KIRExpr]MIRIndex, len(arena.Expressions)),
	}
	for index, row := range arena.Expressions {
		if row.Value.Kind != "binary" {
			continue
		}
		expression := generator.expression(MIRRef{Index: MIRIndex(index), Present: true})
		if expression.Left != nil || expression.Right != nil || len(expression.Args) != 0 {
			t.Fatal("C AOT expression view contains recursive child nodes")
		}
		if !row.Left.Present || !row.Right.Present || generator.expression(row.Left) == nil || generator.expression(row.Right) == nil {
			t.Fatal("C AOT cannot follow checked expression references through the validated arena")
		}
		return
	}
	t.Fatal("test KIR has no binary expression row")
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

func TestDirectPECapabilitiesAreCheckedBeforeAnyEmissionPath(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "kir_machine_pe.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	var lowerer *ast.FuncDecl
	for _, declaration := range file.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "lowerDirectPEKIR" {
			lowerer = candidate
			break
		}
	}
	if lowerer == nil {
		t.Fatal("lowerDirectPEKIR is missing")
	}
	var preflight, staticLowering, recursiveView token.Pos
	ast.Inspect(lowerer.Body, func(node ast.Node) bool {
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
		case "validateMIRNativeFeatureSupport":
			preflight = call.Pos()
		case "directStaticOutputMIR":
			staticLowering = call.Pos()
		case "documentView":
			recursiveView = call.Pos()
		}
		return true
	})
	if preflight == token.NoPos || staticLowering == token.NoPos || preflight >= staticLowering {
		t.Fatal("PE must validate supported language features from typed MIR before attempting output generation")
	}
	if recursiveView != token.NoPos {
		t.Fatal("PE lowering must not materialize a recursive KIR view")
	}
}

func TestDirectPEStaticLowererConsumesOnlyTheValidatedArena(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "kir_machine_static_arena.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"directStaticOutputMIR":        false,
		"directStaticMIRValue":         false,
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
	if staticPos == token.NoPos || recursiveViewPos != token.NoPos {
		t.Fatal("PE static and dynamic output paths must lower without a recursive compatibility view")
	}
}

func TestDirectPEDynamicLoweringFollowsValidatedArenaReferences(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "kir_machine_pe.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{
		"validateKIRDirectPE":           false,
		"validateKIRDirectPEExpr":       false,
		"validateKIRDirectPEStatements": false,
		"collectKIRPEBindings":          false,
		"emitStatements":                false,
		"emitExpr":                      false,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, needed := wanted[function.Name.Name]; needed {
			wanted[function.Name.Name] = true
			ast.Inspect(function.Type, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if ok && identifier.Name == "KIRDocument" {
					t.Errorf("%s accepts recursive KIRDocument", function.Name.Name)
				}
				return true
			})
		}
		if function.Body != nil {
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
				if name == "documentView" || name == "toKIRDocument" || name == "validateKIRDocument" {
					t.Errorf("PE dynamic lowerer %s reconstructs or revalidates recursive KIR via %s", function.Name.Name, name)
				}
				return true
			})
		}
	}
	for name, found := range wanted {
		if !found {
			t.Errorf("PE dynamic lowerer is missing arena consumer %s", name)
		}
	}
	for _, function := range file.Decls {
		declaration, ok := function.(*ast.FuncDecl)
		if !ok || declaration.Name.Name != "validateKIRDirectPE" {
			continue
		}
		acceptsArena := false
		ast.Inspect(declaration.Type.Params, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == "KIRArena" {
				acceptsArena = true
			}
			return true
		})
		if !acceptsArena {
			t.Fatal("validateKIRDirectPE must accept the validated arena")
		}
		return
	}
	t.Fatal("validateKIRDirectPE is missing")
}

func TestDirectELFStaticLowererConsumesOnlyTheValidatedArena(t *testing.T) {
	staticFile, err := parser.ParseFile(token.NewFileSet(), "machine_kir.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	var staticLowerer *ast.FuncDecl
	for _, declaration := range staticFile.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "directStaticKIROutput" {
			staticLowerer = candidate
			break
		}
	}
	if staticLowerer == nil {
		t.Fatal("directStaticKIROutput is missing")
	}
	ast.Inspect(staticLowerer.Type, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && (identifier.Name == "KIRDocument" || identifier.Name == "KIRExpr" || identifier.Name == "KIRStmt") {
			t.Errorf("directStaticKIROutput accepts recursive wire type %s", identifier.Name)
		}
		return true
	})
	ast.Inspect(staticLowerer.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "documentView" {
			t.Error("directStaticKIROutput reconstructs the recursive KIR document")
		}
		return true
	})

	machineFile, err := parser.ParseFile(token.NewFileSet(), "machine.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatal(err)
	}
	var entrypoint *ast.FuncDecl
	for _, declaration := range machineFile.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if ok && candidate.Name.Name == "buildDirectELFFromMIR" {
			entrypoint = candidate
			break
		}
	}
	if entrypoint == nil {
		t.Fatal("buildDirectELFFromMIR is missing")
	}
	var staticPos, dynamicValidationPos token.Pos
	ast.Inspect(entrypoint.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		switch identifier.Name {
		case "directStaticKIROutput":
			staticPos = call.Pos()
		case "validateKIRDirectELFValueSubset":
			dynamicValidationPos = call.Pos()
		}
		return true
	})
	if staticPos == token.NoPos || dynamicValidationPos == token.NoPos || staticPos >= dynamicValidationPos {
		t.Fatal("ELF static output must consume the validated arena before dynamic lowering")
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
