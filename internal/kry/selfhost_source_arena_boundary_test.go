package kry

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Differential oracles remain available to tests, but production code must
// never reach them through a new wrapper or a function-value alias.
func TestRecursiveInterpreterOraclesHaveNoProductionCallers(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		positions := token.NewFileSet()
		file, err := parser.ParseFile(positions, name, nil, parser.AllErrors)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			caller := "<package initializer>"
			region := ast.Node(declaration)
			if function, ok := declaration.(*ast.FuncDecl); ok {
				caller = function.Name.Name
				if function.Body == nil {
					continue
				}
				region = function.Body
			}
			ast.Inspect(region, func(node ast.Node) bool {
				if identifier, ok := node.(*ast.Ident); ok {
					allowed := true
					switch identifier.Name {
					case "executeKIRWithOptions", "executeKIRSubset":
						allowed = caller == "executeKIRSubset"
					case "newASTOracleRuntime":
						allowed = caller == "newASTOracleRuntime"
					}
					if !allowed {
						t.Errorf("%s: production %s references test oracle %s", positions.Position(identifier.Pos()), caller, identifier.Name)
					}
				}
				if caller == "newASTOracleRuntime" {
					return true
				}
				switch value := node.(type) {
				case *ast.KeyValueExpr:
					if key, ok := value.Key.(*ast.Ident); ok && key.Name == "astOracle" {
						t.Errorf("%s: production %s sets the AST oracle flag", positions.Position(key.Pos()), caller)
					}
				case *ast.AssignStmt:
					for _, target := range value.Lhs {
						if field, ok := target.(*ast.SelectorExpr); ok && field.Sel.Name == "astOracle" {
							t.Errorf("%s: production %s assigns the AST oracle flag", positions.Position(field.Pos()), caller)
						}
					}
				}
				return true
			})
		}
	}
}

// Parse the Kryndel AST instead of matching source text: builtin names in the
// frontend's signature tables and backend runtime emitters are legitimate.
func parseSelfhostBoundaryModule(t *testing.T, name string) *Program {
	t.Helper()
	path := filepath.Join("..", "..", "selfhost", name)
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := Parse(&Source{Name: path, Text: string(text)}, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("parse %s: %s", name, diagnostic.Message)
	}
	return program
}

func walkSelfhostBoundaryStatements(statements []*Stmt, visit func(*Expr)) {
	var expression func(*Expr)
	expression = func(node *Expr) {
		if node == nil {
			return
		}
		visit(node)
		for _, child := range []*Expr{node.Left, node.Right, node.Operand, node.Callee, node.Base, node.Receiver} {
			expression(child)
		}
		for _, children := range [][]*Expr{node.Args, node.Items, node.MapKeys, node.Values} {
			for _, child := range children {
				expression(child)
			}
		}
		if node.Lambda != nil {
			walkSelfhostBoundaryStatements(node.Lambda.Body, visit)
		}
	}
	for _, statement := range statements {
		for _, node := range []*Expr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			expression(node)
		}
		for _, children := range [][]*Stmt{statement.Then, statement.Else, statement.Body} {
			walkSelfhostBoundaryStatements(children, visit)
		}
		for _, arm := range statement.Arms {
			walkSelfhostBoundaryStatements(arm.Body, visit)
		}
	}
}

func TestSelfhostSourceCompilationDoesNotRoundTripPortableKIR(t *testing.T) {
	for _, entrypoint := range []struct{ module, function string }{{"source_kir_compiler.kry", "compile_path"}, {"source_compiler.kry", "main"}} {
		t.Run(entrypoint.module, func(t *testing.T) {
			assertSelfhostSourceCompilationUsesTypedArena(t, entrypoint.module, entrypoint.function)
		})
	}
}

func assertSelfhostSourceCompilationUsesTypedArena(t *testing.T, module, entrypointName string) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "selfhost", module))
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(path, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load productive selfhost source graph: %s", diagnostic.Message)
	}
	if _, diagnostic := Check(program, DefaultLimits()); diagnostic != nil {
		t.Fatalf("check productive selfhost source graph: %s", diagnostic.Message)
	}
	var entrypoint *Function
	for _, function := range program.Functions {
		if function.Name == entrypointName && function.Tok.Source != nil && filepath.Base(function.Tok.Source.Name) == "source_kir_compiler.kry" {
			entrypoint = function
		}
	}
	if entrypoint == nil {
		t.Fatalf("source frontend %s does not reach shared entrypoint %s", module, entrypointName)
	}
	var validatedBackend *Function
	for _, function := range program.Functions {
		if function.Name == "compile_validated_kir" && function.Tok.Source != nil && filepath.Base(function.Tok.Source.Name) == "dynamic_backend.kry" {
			validatedBackend = function
			break
		}
	}
	if validatedBackend == nil {
		t.Fatal("productive source graph does not resolve the shared ValidatedKIR backend")
	}
	if module == "source_compiler.kry" {
		wrapper := parseSelfhostBoundaryModule(t, module)
		if len(wrapper.Functions) != 0 || len(wrapper.Statements) != 0 || len(wrapper.Structs) != 0 {
			t.Fatal("historical source compiler regained an executable frontend instead of sharing the typed CLI")
		}
	}
	forbidden := map[string]bool{
		"json_parse": true, "json_stringify": true, "build_document_text": true,
		"validate_generated_kir_document": true, "validate_kir_document": true,
		"compile_document": true, "compile_generated_document": true,
		"kir_typed_arena_from_validated_json": true, "kir_typed_arena_serialize": true,
	}
	type callState struct {
		function *Function
		lowered  bool
	}
	visited := map[callState]bool{}
	visitedNames := map[string]bool{}
	var inspect func(*Function, bool)
	inspect = func(function *Function, lowered bool) {
		if function == nil || visited[callState{function, lowered}] {
			return
		}
		visited[callState{function, lowered}] = true
		visitedNames[function.Name] = true
		walkSelfhostBoundaryStatements(function.Body, func(expression *Expr) {
			if forbidden[expression.Name] || strings.HasPrefix(expression.Name, "typed_json_") {
				t.Errorf("productive source path %s references portable/recursive KIR helper %s at %d:%d", function.Name, expression.Name, expression.Tok.Line, expression.Tok.Column)
			}
			if (expression.Name == "emit_elf" || expression.Name == "emit_native_image" || expression.Name == "emit_linux_amd64_image" || strings.HasPrefix(expression.Name, "emit_pe32plus")) && !lowered {
				t.Errorf("productive source path %s emits native payload before the ValidatedKIR boundary", function.Name)
			}
			if entrypointName == "main" && expression.Name == "emit_source_kir" {
				// Portable output is an explicit CLI mode, not native lowering.
				return
			}
			nextLowered := lowered || expression.Function == validatedBackend
			if expression.Kind == ExCall || expression.Kind == ExVar {
				inspect(expression.Function, nextLowered)
			}
		})
	}
	inspect(entrypoint, false)
	if entrypointName == "compile_path" {
		calls := []string{}
		walkSelfhostBoundaryStatements(entrypoint.Body, func(expression *Expr) {
			if expression.Kind != ExCall {
				return
			}
			name := expression.Name
			if expression.Function != nil {
				name = expression.Function.Name
			}
			calls = append(calls, name)
		})
		buildAt, validateAt, lowerAt := -1, -1, -1
		for index, name := range calls {
			if name == "build_source_arena" && buildAt < 0 {
				buildAt = index
			}
			if name == "validate_kir_typed_arena" && validateAt < 0 {
				validateAt = index
			}
			if name == "compile_validated_kir" && lowerAt < 0 {
				lowerAt = index
			}
		}
		if buildAt < 0 || validateAt <= buildAt || lowerAt <= validateAt {
			t.Errorf("compile_path must build the typed arena, validate it, and then call the ValidatedKIR backend; calls=%v", calls)
		}
		var sourceBuilder *Function
		for _, function := range program.Functions {
			if function.Name == "build_source_arena" && function.Tok.Source != nil && filepath.Base(function.Tok.Source.Name) == "source_kir_compiler.kry" {
				sourceBuilder = function
				break
			}
		}
		if sourceBuilder == nil {
			t.Fatal("productive source path is missing build_source_arena")
		}
		builderCalls := map[string]bool{}
		walkSelfhostBoundaryStatements(sourceBuilder.Body, func(expression *Expr) {
			if expression.Kind == ExCall {
				name := expression.Name
				if expression.Function != nil {
					name = expression.Function.Name
				}
				builderCalls[name] = true
			}
		})
		if !builderCalls["kir_typed_empty_source_arena"] || !builderCalls["kir_typed_finish_source_build"] || builderCalls["kir_typed_empty_arena"] {
			t.Errorf("productive source build must use chunked typed rows and flatten them once before validation; calls=%v", builderCalls)
		}
	}
	if !visitedNames["compile_validated_kir"] || !visitedNames["validate_kir_typed_arena"] {
		t.Error("compile_path no longer validates typed KIR before handing it to the shared backend entrypoint")
	}
	for _, structure := range program.Structs {
		if structure.Name != "SourceExpr" && structure.Name != "ParsedStatements" && structure.Name != "ParsedProgram" && structure.Name != "ParsedFunction" && structure.Name != "ParsedStruct" && structure.Name != "ParsedEnum" {
			continue
		}
		for _, field := range structure.Fields {
			if field.Name == "json" || ((field.Name == "statements" || field.Name == "functions" || field.Name == "structs" || field.Name == "enums") && field.Spec.Name == "String") {
				t.Errorf("parser result %s.%s retains recursive KIR text instead of typed row references", structure.Name, field.Name)
			}
		}
	}
}

func TestSelfhostLoweringFunctionsRejectFrontendAndRecursiveWireInputs(t *testing.T) {
	for _, module := range []string{"elf_backend.kry", "dynamic_backend.kry", "pe_backend.kry", "native_image.kry"} {
		t.Run(module, func(t *testing.T) {
			program := parseSelfhostBoundaryModule(t, module)
			for _, function := range program.Functions {
				decoder := module == "dynamic_backend.kry" && function.Name == "compile_document"
				for _, parameter := range function.Params {
					var inspectType func(*TypeSpec)
					inspectType = func(spec *TypeSpec) {
						if spec == nil {
							return
						}
						if (!decoder && spec.Name == "Json") || spec.Name == "KIRTypedArena" || spec.Name == "KIRNodeRef" || spec.Name == "KIRArenaCell" || strings.HasPrefix(spec.Name, "Source") || strings.HasPrefix(spec.Name, "Parsed") {
							t.Errorf("lowerer %s.%s accepts forbidden input %s in parameter %s", module, function.Name, spec.Name, parameter.Name)
						}
						for _, child := range spec.Params {
							inspectType(child)
						}
						inspectType(spec.Return)
					}
					inspectType(parameter.Type)
				}
				decoderCalls := []string{}
				decoderDocumentUses := 0
				decoderDocumentArguments := 0
				validateCalls := 0
				lowerCalls := 0
				walkSelfhostBoundaryStatements(function.Body, func(expression *Expr) {
					if expression.Kind == ExVar && expression.Name == "document" {
						decoderDocumentUses++
					}
					if expression.Kind != ExCall {
						if !decoder && expression.Kind == ExVar {
							switch expression.Name {
							case "json_parse", "json_object_get", "json_array_get", "json_array_len", "json_string", "json_int", "json_uint", "json_bool", "kir_arena_from_json", "validated_kir_document", "kir_typed_arena_from_validated_json":
								t.Errorf("lowerer %s.%s references portable KIR helper %s", module, function.Name, expression.Name)
							}
						}
						return
					}
					decoderCalls = append(decoderCalls, expression.Name)
					if decoder {
						switch expression.Name {
						case "validate_kir_document":
							validateCalls++
							if len(expression.Args) == 1 && expression.Args[0] != nil && expression.Args[0].Kind == ExVar && expression.Args[0].Name == "document" {
								decoderDocumentArguments++
							} else {
								t.Errorf("portable decoder %s must pass its Json document directly to validate_kir_document", function.Name)
							}
						case "compile_validated_kir":
							lowerCalls++
							if len(expression.Args) != 1 || expression.Args[0] == nil || expression.Args[0].Kind != ExCall || expression.Args[0].Name != "result_unwrap" {
								t.Errorf("portable decoder %s must lower only the ValidatedKIR result", function.Name)
							}
						case "is_err", "err", "unwrap_or", "result_error", "result_unwrap":
						default:
							t.Errorf("portable decoder %s performs unexpected operation %s", function.Name, expression.Name)
						}
						return
					}
					switch expression.Name {
					case "json_parse", "json_object_get", "json_array_get", "json_array_len", "json_string", "json_int", "json_uint", "json_bool", "kir_arena_from_json", "validated_kir_document", "kir_typed_arena_from_validated_json":
						t.Errorf("lowerer %s.%s traverses or reconstructs portable KIR through %s", module, function.Name, expression.Name)
					}
				})
				if decoder {
					validateAt, lowerAt := -1, -1
					for index, name := range decoderCalls {
						if name == "validate_kir_document" && validateAt < 0 {
							validateAt = index
						}
						if name == "compile_validated_kir" && lowerAt < 0 {
							lowerAt = index
						}
					}
					if validateCalls != 1 || lowerCalls != 1 || decoderDocumentUses != 1 || decoderDocumentArguments != 1 || validateAt < 0 || lowerAt <= validateAt {
						t.Errorf("portable decoder %s must validate KIR before calling compile_validated_kir; calls=%v", function.Name, decoderCalls)
					}
				}
			}
		})
	}
}

func TestSelfhostNativeLoweringEntrypointsRequireValidatedKIR(t *testing.T) {
	expected := map[string][]string{
		"dynamic_backend.kry": {"lower_dynamic", "compile_dynamic", "compile_validated_kir"},
		"elf_backend.kry":     {"compile_kir"},
	}
	for module, entrypoints := range expected {
		program := parseSelfhostBoundaryModule(t, module)
		functions := map[string]*Function{}
		for _, function := range program.Functions {
			functions[function.Name] = function
		}
		for _, name := range entrypoints {
			function := functions[name]
			if function == nil {
				t.Errorf("%s is missing lowering entrypoint %s", module, name)
				continue
			}
			acceptsValidatedKIR := false
			for _, parameter := range function.Params {
				if parameter.Type != nil && parameter.Type.Name == "ValidatedKIR" {
					acceptsValidatedKIR = true
				}
			}
			if !acceptsValidatedKIR {
				t.Errorf("lowering entrypoint %s.%s must accept ValidatedKIR", module, name)
			}
		}
	}
}
func TestSelfhostNativeCodegenKeepsPagedBuffersAndBalancedMaterialization(t *testing.T) {
	dynamic := parseSelfhostBoundaryModule(t, "dynamic_backend.kry")
	structs := map[string]*StructDecl{}
	functions := map[string]*Function{}
	for _, structure := range dynamic.Structs {
		structs[structure.Name] = structure
	}
	for _, function := range dynamic.Functions {
		functions[function.Name] = function
	}
	fieldType := func(structure, field string) *TypeSpec {
		t.Helper()
		value := structs[structure]
		if value == nil {
			t.Fatalf("dynamic backend is missing %s", structure)
		}
		for _, candidate := range value.Fields {
			if candidate.Name == field {
				return candidate.Spec
			}
		}
		t.Fatalf("dynamic backend is missing %s.%s", structure, field)
		return nil
	}
	if spec := fieldType("AssemblerCode", "code_chunks"); spec == nil || spec.Name != "ChunkedCodeChunks" {
		t.Fatalf("assembler code chunks must use the paged representation, got %#v", spec)
	}
	if spec := fieldType("Assembler", "labels"); spec == nil || spec.Name != "ChunkedLabelPositions" {
		t.Fatalf("assembler labels must use the indexed paged representation, got %#v", spec)
	}
	if spec := fieldType("ChunkedCodeChunks", "current"); spec == nil || spec.Name != "Array" || len(spec.Params) != 1 || spec.Params[0].Name != "UInt8" {
		t.Fatalf("code builder must keep only a bounded byte tail outside completed chunks, got %#v", spec)
	}
	arrayDepth := func(spec *TypeSpec) int {
		depth := 0
		for spec != nil && spec.Name == "Array" && len(spec.Params) == 1 {
			depth++
			spec = spec.Params[0]
		}
		return depth
	}
	if depth := arrayDepth(fieldType("ChunkedCodeChunks", "pages")); depth < 4 {
		t.Fatalf("code chunk directory lost a bounded hierarchy: Array depth=%d", depth)
	}
	calls := func(name string) map[string]int {
		t.Helper()
		function := functions[name]
		if function == nil {
			t.Fatalf("dynamic backend is missing %s", name)
		}
		found := map[string]int{}
		walkSelfhostBoundaryStatements(function.Body, func(expression *Expr) {
			if expression.Kind == ExCall {
				found[expression.Name]++
			}
		})
		return found
	}
	appendCalls := calls("append_code")
	if appendCalls["chunked_code_chunks_push"] == 0 || appendCalls["array_slice"] == 0 || appendCalls["array_set"] != 0 {
		t.Fatalf("append_code must flush bounded tails into pages without replacing elements in a growing directory: calls=%v", appendCalls)
	}
	if materializeCalls := calls("materialize_code"); materializeCalls["join_code_chunk_range"] == 0 {
		t.Fatalf("materialize_code must concatenate completed chunks with a balanced join: calls=%v", materializeCalls)
	}
	if flattenCalls := calls("flatten_code_pages"); flattenCalls["join_code_page_range"] == 0 {
		t.Fatalf("flatten_code_pages must concatenate patched pages with a balanced join: calls=%v", flattenCalls)
	}

	arena := parseSelfhostBoundaryModule(t, "kir_typed_arena.kry")
	for _, structure := range arena.Structs {
		if structure.Name != "KIRTypedColors" {
			continue
		}
		for _, field := range structure.Fields {
			if field.Name == "pages" && arrayDepth(field.Spec) >= 4 {
				return
			}
		}
		t.Fatal("typed arena validator must keep color rows in a bounded page hierarchy")
	}
	t.Fatal("typed arena validator is missing KIRTypedColors")
}

func TestSelfhostPortableKIRBackendDropsWireInputBeforeLowering(t *testing.T) {
	program := parseSelfhostBoundaryModule(t, "kir_backend.kry")
	functions := map[string]*Function{}
	for _, function := range program.Functions {
		functions[function.Name] = function
	}
	loader := functions["read_validated_kir"]
	if loader == nil {
		t.Fatal("portable KIR backend must isolate read and validation in read_validated_kir")
	}
	if loader.Return == nil || loader.Return.Name != "Result" || len(loader.Return.Params) < 1 || loader.Return.Params[0].Name != "ValidatedKIR" {
		t.Fatalf("read_validated_kir must return Result[ValidatedKIR, String], got %#v", loader.Return)
	}
	loaderCalls := []string{}
	walkSelfhostBoundaryStatements(loader.Body, func(expression *Expr) {
		if expression.Kind == ExCall {
			loaderCalls = append(loaderCalls, expression.Name)
		}
	})
	if !containsString(loaderCalls, "json_parse") || !containsString(loaderCalls, "validate_kir_document") || containsString(loaderCalls, "compile_validated_kir") {
		t.Fatalf("portable input helper must parse and validate, then return before lowering; calls=%v", loaderCalls)
	}
	main := functions["main"]
	if main == nil {
		t.Fatal("portable KIR backend is missing main")
	}
	readValidatedKIRAssignedTo := ""
	loweredValidatedKIRFrom := ""
	for _, statement := range main.Body {
		if statement.Kind != StLet || statement.Init == nil || statement.Init.Kind != ExCall || statement.Init.Name != "result_unwrap" || len(statement.Init.Args) != 1 {
			continue
		}
		value := statement.Init.Args[0]
		if statement.Name == "kir" && statement.Annotation != nil && statement.Annotation.Name == "ValidatedKIR" && value.Kind == ExCall && value.Name == "read_validated_kir" {
			readValidatedKIRAssignedTo = statement.Name
		}
		if statement.Name == "executable" && value.Kind == ExCall && value.Name == "compile_validated_kir" && len(value.Args) == 1 && value.Args[0].Kind == ExVar {
			loweredValidatedKIRFrom = value.Args[0].Name
		}
	}
	if readValidatedKIRAssignedTo == "" || loweredValidatedKIRFrom != readValidatedKIRAssignedTo {
		t.Fatalf("portable backend must lower the exact ValidatedKIR returned by read_validated_kir; assigned=%q lowered=%q", readValidatedKIRAssignedTo, loweredValidatedKIRFrom)
	}
	mainCalls := []string{}
	walkSelfhostBoundaryStatements(main.Body, func(expression *Expr) {
		if expression.Kind == ExCall {
			mainCalls = append(mainCalls, expression.Name)
		}
	})
	readAt, lowerAt := -1, -1
	for index, name := range mainCalls {
		if name == "read_validated_kir" && readAt < 0 {
			readAt = index
		}
		if name == "compile_validated_kir" && lowerAt < 0 {
			lowerAt = index
		}
		if name == "compile_document" || name == "json_parse" || name == "validate_kir_document" {
			t.Errorf("portable backend main must leave parsing and validation to its helper, got %s", name)
		}
	}
	if readAt < 0 || lowerAt <= readAt {
		t.Fatalf("portable backend main must lower only the ValidatedKIR returned by its helper; calls=%v", mainCalls)
	}
}
func TestSelfhostSourceArenaPreservesImportsBindingsCallsAndExecution(t *testing.T) {
	const helpers = "pub struct Counter { value: Int }\n" +
		"pub enum Status { Start, End }\n" +
		"pub fn step(value: Int) -> Int { let result: Int = value + 1; return result }\n" +
		"pub fn make_counter(value: Int) -> Counter { return Counter { value: value } }\n"
	const main = "import \"helpers\"\n" +
		"fn main() -> Nil { // π🙂 force UTF-8 byte spans\n" +
		"  let mut count: Int = step(1)\n" +
		"  if true { let count: Int = 7; println(count) }\n" +
		"  count = step(count)\n" +
		"  let item: Counter = make_counter(count)\n" +
		"  println(item.value)\n" +
		"  let status: Status = Status::End\n" +
		"  match status { Status::Start => { println(\"start\") } Status::End => { println(\"end\") } }\n" +
		"}\n"
	limits := DefaultLimits()
	compilerPath, err := filepath.Abs(filepath.Join("..", "..", "selfhost", "source_kir_compiler.kry"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, diagnostic := LoadProgram(compilerPath, limits, "")
	if diagnostic != nil {
		t.Fatalf("load selfhost source frontend: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(compiler, limits)
	if diagnostic != nil {
		t.Fatalf("check selfhost source frontend: %s", diagnostic.Message)
	}
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "main.kry")
	for name, text := range map[string]string{"main.kry": main, "helpers.kry": helpers} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	kirPath := filepath.Join(directory, "main.kir")
	runCompiler := func(arguments []string) {
		t.Helper()
		runtime, diagnostic := NewRuntimeWithArgs(compiler, checker, limits, Sandbox{}, arguments)
		if diagnostic != nil {
			t.Fatalf("prepare selfhost compiler: %s", diagnostic.Message)
		}
		var output bytes.Buffer
		runtime.output = &output
		if diagnostic := runtime.run(); diagnostic != nil {
			t.Fatalf("run selfhost compiler: %s; output=%q", diagnostic.Message, output.String())
		}
	}
	runCompiler([]string{"--emit-kir", sourcePath, kirPath})
	encoded, err := os.ReadFile(kirPath)
	if err != nil {
		t.Fatal(err)
	}
	selfhostMIR, err := DecodeMIR(encoded, limits)
	if err != nil {
		t.Fatalf("decode emitted selfhost arena: %v", err)
	}
	goProgram, diagnostic := LoadProgram(sourcePath, limits, "")
	if diagnostic != nil {
		t.Fatalf("load Go parity program: %s", diagnostic.Message)
	}
	goChecker, diagnostic := Check(goProgram, limits)
	if diagnostic != nil {
		t.Fatalf("check Go parity program: %s", diagnostic.Message)
	}
	goMIR, err := CompileMIR(goProgram, goChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	for name, mir := range map[string]*ValidatedMIR{"Go source": goMIR, "selfhost source": selfhostMIR} {
		if err := mir.arena.validateReferences(); err != nil {
			t.Fatalf("%s has invalid references: %v", name, err)
		}
		var output bytes.Buffer
		if _, err := executeKIRArenaWithOptions(mir.arena, limits, nil, kirExecOptions{output: &output}); err != nil {
			t.Fatalf("execute %s typed arena: %v", name, err)
		}
		if output.String() != "7\n3\nend\n" {
			t.Fatalf("%s output=%q, want shared binding/call/enum semantics", name, output.String())
		}
	}
	// Compare stable binding identities and diagnostics directly in flat rows.
	// Function/type mangling differs across compilers, so those names are checked
	// against each arena's declarations rather than normalized heuristically.
	bindingProjection := func(arena *KIRArena) map[string]KIRBinding {
		bindings := map[string]KIRBinding{}
		for _, binding := range arena.Bindings {
			if binding.Type == "Int" {
				bindings[binding.ID] = binding
			}
		}
		return bindings
	}
	goBindings, selfhostBindings := bindingProjection(goMIR.arena), bindingProjection(selfhostMIR.arena)
	if !reflect.DeepEqual(goBindings, selfhostBindings) {
		t.Fatalf("Int binding metadata differs: Go=%#v selfhost=%#v", goBindings, selfhostBindings)
	}
	if !reflect.DeepEqual(goMIR.arena.ImportRecords, selfhostMIR.arena.ImportRecords) {
		for _, arena := range []*KIRArena{goMIR.arena, selfhostMIR.arena} {
			for _, record := range arena.ImportRecords {
				t.Logf("import metadata=%+v span=%+v", *record, record.Span)
			}
		}
		t.Fatal("import source metadata differs between Go and selfhost")
	}
	type expressionMetadata struct {
		typeName string
		span     KIRSourceSpan
	}
	expressionProjection := func(arena *KIRArena) map[string]expressionMetadata {
		metadata := map[string]expressionMetadata{}
		for _, row := range arena.Expressions {
			if row.Value.Type != "Int" && row.Value.Type != "Bool" && row.Value.Type != "String" && row.Value.Type != "Nil" {
				continue
			}
			key := fmt.Sprintf("%s:%d:%d:%s", row.Value.Source, row.Value.Line, row.Value.Column, row.Value.Kind)
			if row.Value.Span == nil {
				t.Fatalf("expression %s has no byte span", key)
			}
			metadata[key] = expressionMetadata{typeName: row.Value.Type, span: *row.Value.Span}
		}
		return metadata
	}
	goExpressions, selfhostExpressions := expressionProjection(goMIR.arena), expressionProjection(selfhostMIR.arena)
	if !reflect.DeepEqual(goExpressions, selfhostExpressions) {
		t.Fatalf("checked scalar types and UTF-8 byte spans differ: Go=%v selfhost=%v", goExpressions, selfhostExpressions)
	}
	type bindingMetadata struct {
		id       string
		typeName string
		mutable  bool
		span     KIRSourceSpan
	}
	nominalBindings := func(arena *KIRArena) map[string]bindingMetadata {
		metadata := map[string]bindingMetadata{}
		for _, binding := range arena.Bindings {
			if binding.Name != "item" && binding.Name != "status" {
				continue
			}
			expectedType := "Counter"
			if binding.Name == "status" {
				expectedType = "Status"
			}
			if binding.Type != expectedType && !strings.HasSuffix(binding.Type, "_"+expectedType) {
				t.Fatalf("nominal binding %s has unexpected type %q, want %q", binding.Name, binding.Type, expectedType)
			}
			if binding.Span == nil {
				t.Fatalf("nominal binding %s at %s:%d:%d has no source span", binding.Name, binding.Source, binding.Line, binding.Column)
			}
			key := fmt.Sprintf("%s:%d:%d:%s", binding.Source, binding.Line, binding.Column, binding.Name)
			metadata[key] = bindingMetadata{id: binding.ID, typeName: expectedType, mutable: binding.Mutable, span: *binding.Span}
		}
		return metadata
	}
	goNominalBindings, selfhostNominalBindings := nominalBindings(goMIR.arena), nominalBindings(selfhostMIR.arena)
	if !reflect.DeepEqual(goNominalBindings, selfhostNominalBindings) {
		t.Fatalf("nominal binding IDs, types, mutability, or spans differ: Go=%v selfhost=%v", goNominalBindings, selfhostNominalBindings)
	}
	declarationSpans := func(arena *KIRArena) map[string]KIRSourceSpan {
		metadata := map[string]KIRSourceSpan{}
		for _, declaration := range arena.Structs {
			if declaration.Name != "Counter" && !strings.HasSuffix(declaration.Name, "_Counter") {
				continue
			}
			if declaration.Span == nil {
				t.Fatalf("struct %s has no source span", declaration.Name)
			}
			key := fmt.Sprintf("struct:%s:%d:%d:Counter", declaration.Source, declaration.Line, declaration.Column)
			metadata[key] = *declaration.Span
			for _, field := range declaration.Fields {
				if field.Span == nil {
					t.Fatalf("field %s.%s has no source span", declaration.Name, field.Name)
				}
				fieldKey := fmt.Sprintf("field:%s:%d:%d:%s", field.Source, field.Line, field.Column, field.Name)
				metadata[fieldKey] = *field.Span
			}
		}
		for _, declaration := range arena.Enums {
			if declaration.Name != "Status" && !strings.HasSuffix(declaration.Name, "_Status") {
				continue
			}
			if declaration.Span == nil {
				t.Fatalf("enum %s has no source span", declaration.Name)
			}
			key := fmt.Sprintf("enum:%s:%d:%d:Status", declaration.Source, declaration.Line, declaration.Column)
			metadata[key] = *declaration.Span
			for index, variant := range declaration.Variants {
				if index >= len(declaration.VariantSpans) || declaration.VariantSpans[index] == nil {
					t.Fatalf("enum variant %s::%s has no source span", declaration.Name, variant)
				}
				variantSpan := declaration.VariantSpans[index]
				variantKey := fmt.Sprintf("variant:%s:%d:%d:%s", declaration.Source, variantSpan.Start, variantSpan.End, variant)
				metadata[variantKey] = *variantSpan
			}
		}
		return metadata
	}
	goDeclarationSpans, selfhostDeclarationSpans := declarationSpans(goMIR.arena), declarationSpans(selfhostMIR.arena)
	if !reflect.DeepEqual(goDeclarationSpans, selfhostDeclarationSpans) {
		t.Fatalf("imported struct/enum/field/variant spans differ: Go=%v selfhost=%v", goDeclarationSpans, selfhostDeclarationSpans)
	}
	functionSpans := func(arena *KIRArena) map[string]KIRSourceSpan {
		metadata := map[string]KIRSourceSpan{}
		for _, row := range arena.Functions {
			function := row.Value
			if function.Span == nil {
				t.Fatalf("function at %s:%d:%d has no source span", function.Source, function.Line, function.Column)
			}
			key := fmt.Sprintf("%s:%d:%d", function.Source, function.Line, function.Column)
			metadata[key] = *function.Span
		}
		return metadata
	}
	goFunctionSpans, selfhostFunctionSpans := functionSpans(goMIR.arena), functionSpans(selfhostMIR.arena)
	if !reflect.DeepEqual(goFunctionSpans, selfhostFunctionSpans) {
		t.Fatalf("imported function source spans differ: Go=%v selfhost=%v", goFunctionSpans, selfhostFunctionSpans)
	}
	for name, arena := range map[string]*KIRArena{"Go": goMIR.arena, "selfhost": selfhostMIR.arena} {
		if !reflect.DeepEqual(arena.Imports, []string{"helpers"}) || len(arena.ImportRecords) != 1 || len(arena.Sources) != 2 || len(arena.Structs) != 1 || len(arena.Enums) != 1 {
			t.Fatalf("%s lost import/declaration tables: imports=%v records=%v sources=%v structs=%d enums=%d", name, arena.Imports, arena.ImportRecords, arena.Sources, len(arena.Structs), len(arena.Enums))
		}
		functions := map[string]bool{}
		for _, row := range arena.Functions {
			functions["function:"+row.Value.Name] = true
			functions["function:"+kirFunctionTargetFromDocument(&row.Value)] = true
		}
		calls := 0
		for _, row := range arena.Expressions {
			if row.Value.Span == nil || row.Value.Span.End <= row.Value.Span.Start || row.Value.Type == "" {
				t.Fatalf("%s expression lost checked type or byte span: %#v", name, row.Value)
			}
			if strings.HasPrefix(row.Value.CallTarget, "function:") {
				if !functions[row.Value.CallTarget] {
					t.Fatalf("%s has unresolved call target %q", name, row.Value.CallTarget)
				}
				calls++
			}
		}
		if calls != 3 {
			t.Fatalf("%s has %d function calls, want 3", name, calls)
		}
		counts := map[string]int{}
		for _, binding := range arena.Bindings {
			if binding.Source == "main.kry" && binding.Name == "count" {
				key := fmt.Sprintf("%s:%t", binding.ID, binding.Mutable)
				counts[key]++
			}
		}
		if len(counts) != 2 {
			t.Fatalf("%s shadowed count bindings were conflated: %v", name, counts)
		}
	}
	t.Run("transitive imports preserve flat symbols and nominal types", func(t *testing.T) {
		transitiveDir := t.TempDir()
		transitiveSources := map[string]string{
			"main.kry":  "import \"api\"\nfn main() -> Nil {\n  let point = create_point(41)\n  println(score(point))\n  return nil\n}\n",
			"api.kry":   "import \"model\"\npub fn create_point(value: Int) -> Point { return model_make_point(value) }\npub fn score(point: Point) -> Int { return model_point_value(point) + 1 }\n",
			"model.kry": "pub struct Point { value: Int }\npub fn model_make_point(value: Int) -> Point { return Point { value: value } }\npub fn model_point_value(point: Point) -> Int { return point.value }\n",
		}
		for name, source := range transitiveSources {
			if err := os.WriteFile(filepath.Join(transitiveDir, name), []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		transitiveMain := filepath.Join(transitiveDir, "main.kry")
		transitiveKIR := filepath.Join(transitiveDir, "main.kir")
		runCompiler([]string{"--emit-kir", transitiveMain, transitiveKIR})
		encoded, err := os.ReadFile(transitiveKIR)
		if err != nil {
			t.Fatal(err)
		}
		selfhostMIR, err := DecodeMIR(encoded, limits)
		if err != nil {
			t.Fatalf("decode selfhost transitive-import arena: %v", err)
		}
		goProgram, diagnostic := LoadProgram(transitiveMain, limits, "")
		if diagnostic != nil {
			t.Fatalf("load Go transitive-import source: %s", diagnostic.Message)
		}
		goChecker, diagnostic := Check(goProgram, limits)
		if diagnostic != nil {
			t.Fatalf("check Go transitive-import source: %s", diagnostic.Message)
		}
		goMIR, err := CompileMIR(goProgram, goChecker, NativeTarget{OS: "linux", Arch: "amd64"})
		if err != nil {
			t.Fatal(err)
		}

		canonicalType := func(typeName string) string {
			if strings.HasSuffix(typeName, "_Point") {
				return "Point"
			}
			return typeName
		}
		callProjection := func(arena *KIRArena) map[string]string {
			targets := map[string]KIRFunction{}
			for _, row := range arena.Functions {
				function := row.Value
				targets["function:"+function.Name] = function
				targets["function:"+kirFunctionTargetFromDocument(&function)] = function
			}
			projection := map[string]string{}
			for _, row := range arena.Expressions {
				expression := row.Value
				if expression.Kind != "call" || !strings.HasPrefix(expression.CallTarget, "function:") {
					continue
				}
				function, ok := targets[expression.CallTarget]
				if !ok {
					t.Errorf("unresolved transitive function target %q", expression.CallTarget)
					continue
				}
				callSite := fmt.Sprintf("%s:%d:%d", expression.Source, expression.Line, expression.Column)
				projection[callSite] = fmt.Sprintf("%s:%d:%s", function.Source, function.Line, canonicalType(expression.Type))
			}
			return projection
		}
		goCalls := callProjection(goMIR.arena)
		selfhostCalls := callProjection(selfhostMIR.arena)
		if !reflect.DeepEqual(goCalls, selfhostCalls) {
			t.Fatalf("transitive function targets/types differ: Go=%v selfhost=%v", goCalls, selfhostCalls)
		}
		if len(goCalls) != 4 {
			t.Fatalf("transitive call projection has %d calls, want main->api and api->model: %v", len(goCalls), goCalls)
		}

		type pointBindingMetadata struct {
			typeName string
			mutable  bool
			start    int
			end      int
		}
		pointBindingProjection := func(arena *KIRArena) map[string]pointBindingMetadata {
			projection := map[string]pointBindingMetadata{}
			for _, binding := range arena.Bindings {
				if binding.Source != "main.kry" || binding.Name != "point" {
					continue
				}
				if binding.Span == nil || canonicalType(binding.Type) != "Point" {
					t.Errorf("lost the inferred transitive nominal binding type/span: %+v", binding)
					continue
				}
				metadata := pointBindingMetadata{typeName: canonicalType(binding.Type), mutable: binding.Mutable, start: binding.Span.Start, end: binding.Span.End}
				if previous, ok := projection[binding.ID]; ok && previous != metadata {
					t.Errorf("binding ID %q has inconsistent transitive metadata: %+v and %+v", binding.ID, previous, metadata)
				}
				projection[binding.ID] = metadata
			}
			return projection
		}
		goPointBindings := pointBindingProjection(goMIR.arena)
		selfhostPointBindings := pointBindingProjection(selfhostMIR.arena)
		if len(goPointBindings) != 1 || !reflect.DeepEqual(goPointBindings, selfhostPointBindings) {
			t.Fatalf("inferred root Point binding differs: Go=%v selfhost=%v", goPointBindings, selfhostPointBindings)
		}

		for label, mir := range map[string]*ValidatedMIR{"Go": goMIR, "selfhost": selfhostMIR} {
			if !reflect.DeepEqual(mir.arena.Imports, []string{"api"}) || len(mir.arena.Sources) != 3 {
				t.Fatalf("%s lost root import or transitive source rows: imports=%v sources=%v", label, mir.arena.Imports, mir.arena.Sources)
			}
			pointTypes := 0
			for _, declaration := range mir.arena.Structs {
				if canonicalType(declaration.Name) == "Point" {
					pointTypes++
				}
			}
			if pointTypes != 1 {
				t.Fatalf("%s arena has %d Point declarations, want one model type", label, pointTypes)
			}
			var output bytes.Buffer
			if _, err := executeKIRArenaWithOptions(mir.arena, limits, nil, kirExecOptions{output: &output}); err != nil {
				t.Fatalf("execute %s transitive-import arena: %v", label, err)
			}
			if output.String() != "42\n" {
				t.Fatalf("%s transitive-import output=%q, want 42", label, output.String())
			}
		}
	})
	elfPath := filepath.Join(directory, "main.elf")
	runCompiler([]string{sourcePath, elfPath})
	image, err := os.ReadFile(elfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) < 64 || !bytes.Equal(image[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		t.Fatal("productive direct source path did not produce an ELF image")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		if err := os.Chmod(elfPath, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(elfPath).CombinedOutput()
		if err != nil || string(output) != "7\n3\nend\n" {
			t.Fatalf("selfhost source ELF execution: output=%q error=%v", output, err)
		}
	} else {
		t.Log("native ELF execution requires Linux amd64; this host verifies the image and both typed-arena interpreters")
	}
}

func TestSelfhostSourceArenaTypesCodepointsBuiltin(t *testing.T) {
	limits := DefaultLimits()
	compilerPath, err := filepath.Abs(filepath.Join("..", "..", "selfhost", "source_kir_compiler.kry"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, diagnostic := LoadProgram(compilerPath, limits, "")
	if diagnostic != nil {
		t.Fatalf("load selfhost source frontend: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(compiler, limits)
	if diagnostic != nil {
		t.Fatalf("check selfhost source frontend: %s", diagnostic.Message)
	}
	directory := t.TempDir()
	sourcePath, kirPath := filepath.Join(directory, "main.kry"), filepath.Join(directory, "main.kir")
	if err := os.WriteFile(sourcePath, []byte("fn main() -> Nil { let points: Array[Int] = codepoints(\"A\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, diagnostic := NewRuntimeWithArgs(compiler, checker, limits, Sandbox{}, []string{"--emit-kir", sourcePath, kirPath})
	if diagnostic != nil {
		t.Fatalf("prepare selfhost source compiler: %s", diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("emit source KIR: %s", diagnostic.Message)
	}
	encoded, err := os.ReadFile(kirPath)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := DecodeMIR(encoded, limits)
	if err != nil {
		t.Fatalf("decode emitted typed codepoints call: %v", err)
	}
	for _, expression := range validated.arena.Expressions {
		if expression.Value.Kind == "call" && expression.Value.CallTarget == "builtin:codepoints" {
			if expression.Value.Type != "Array[Int]" {
				t.Fatalf("codepoints checked type=%q, want Array[Int]", expression.Value.Type)
			}
			return
		}
	}
	t.Fatal("selfhost source arena omitted the typed codepoints call")
}

func TestSelfhostSourceBuiltinTypeParity(t *testing.T) {
	limits := DefaultLimits()
	compilerPath, err := filepath.Abs(filepath.Join("..", "..", "selfhost", "source_kir_compiler.kry"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, diagnostic := LoadProgram(compilerPath, limits, "")
	if diagnostic != nil {
		t.Fatalf("load selfhost source frontend: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(compiler, limits)
	if diagnostic != nil {
		t.Fatalf("check selfhost source frontend: %s", diagnostic.Message)
	}

	positive := "fn int_from_float(value: Float) -> Int { return int(value) }\n" +
		"fn map_length(value: Map[String, Array[Int]]) -> Int { return len(value) }\n" +
		"fn set_length(value: Set[String]) -> Int { return len(value) }\n" +
		"fn compare(values: Array[Int], mapping: Map[String, Array[Int]], labels: Set[String]) -> Nil {\n" +
		"  assert_eq(values, values)\n" +
		"  assert_eq(mapping, mapping)\n" +
		"  assert_eq(labels, labels)\n" +
		"}\n" +
		"fn main() -> Nil {}\n"
	cases := []struct {
		name                     string
		source                   string
		wantAccept               bool
		compareBuiltinSignatures bool
		wantBuiltinCallCount     int
		goError                  string
		selfhostError            string
	}{
		{name: "positive_float_map_set_and_composites", source: positive, wantAccept: true, compareBuiltinSignatures: true, wantBuiltinCallCount: 6},
		{
			name:                 "positive_assert_eq_plain_struct",
			wantAccept:           true,
			wantBuiltinCallCount: 1,
			source: "struct PlainHolder { value: Int }\n" +
				"fn compare(value: PlainHolder) -> Nil { assert_eq(value, value) }\n" +
				"fn main() -> Nil {}\n",
		},
		{
			name:          "negative_int_custom_struct",
			goError:       "int accepts",
			selfhostError: "int expects",
			source: "struct Marker { value: Int }\n" +
				"fn invalid(value: Marker) -> Int { return int(value) }\n",
		},
		{
			name:          "negative_assert_eq_mismatched_composites",
			goError:       "expected Array[Int], found Array[String]",
			selfhostError: "assert_eq expects matching values",
			source:        "fn invalid(left: Array[Int], right: Array[String]) -> Nil { assert_eq(left, right) }\n",
		},
		{
			name:          "negative_assert_eq_struct_callback",
			goError:       "assert_eq does not support values containing functions",
			selfhostError: "assert_eq does not support values containing functions",
			source: "struct Holder { callback: fn(Int) -> Int }\n" +
				"fn invalid(left: Holder, right: Holder) -> Nil { assert_eq(left, right) }\n",
		},
		{
			name:          "negative_public_callback_exposes_private_type",
			goError:       "public function 'expose' exposes a private type",
			selfhostError: "public function 'expose' exposes a private type",
			source: "struct Hidden { value: Int }\n" +
				"pub fn expose(callback: fn(Hidden) -> Int) -> Nil {}\n" +
				"fn main() -> Nil {}\n",
		},
		{
			name:    "negative_assert_eq_function_values",
			goError: "assert_eq does not support values containing functions",
			source: "fn identity(value: Int) -> Int { return value }\n" +
				"fn invalid() -> Nil { assert_eq(identity, identity) }\n",
		},
		{
			name:          "negative_len_scalar",
			goError:       "len expects",
			selfhostError: "len expects one String",
			source:        "fn invalid(value: Int) -> Int { return len(value) }\n",
		},
	}

	builtinCalls := func(arena *KIRArena) map[string]int {
		t.Helper()
		calls := map[string]int{}
		for _, expression := range arena.Expressions {
			value := expression.Value
			if value.Kind != "call" || !strings.HasPrefix(value.CallTarget, "builtin:") {
				continue
			}
			start := int(expression.Args.Start)
			end := start + int(expression.Args.Count)
			if start < 0 || end > len(arena.ExpressionRefs) {
				t.Fatalf("%s call %q has an invalid argument range %+v", value.Source, value.CallTarget, expression.Args)
			}
			argumentTypes := make([]string, 0, expression.Args.Count)
			for _, reference := range arena.ExpressionRefs[start:end] {
				index := int(reference)
				if index < 0 || index >= len(arena.Expressions) {
					t.Fatalf("%s call %q has invalid argument expression reference %d", value.Source, value.CallTarget, index)
				}
				argumentTypes = append(argumentTypes, strings.ReplaceAll(arena.Expressions[index].Value.Type, " ", ""))
			}
			key := fmt.Sprintf("%s(%s)->%s", value.CallTarget, strings.Join(argumentTypes, ","), strings.ReplaceAll(value.Type, " ", ""))
			calls[key]++
		}
		return calls
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			program, goDiagnostic := Parse(&Source{Name: "main.kry", Text: test.source}, limits)
			var goMIR *ValidatedMIR
			if goDiagnostic == nil {
				goChecker, checkDiagnostic := Check(program, limits)
				goDiagnostic = checkDiagnostic
				if goDiagnostic == nil && test.wantAccept {
					goMIR, err = CompileMIR(program, goChecker, NativeTarget{OS: "linux", Arch: "amd64"})
					if err != nil {
						t.Fatalf("compile Go parity source to typed MIR: %v", err)
					}
				}
			}

			directory := t.TempDir()
			sourcePath, kirPath := filepath.Join(directory, "main.kry"), filepath.Join(directory, "main.kir")
			if err := os.WriteFile(sourcePath, []byte(test.source), 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, runtimeDiagnostic := NewRuntimeWithArgs(compiler, checker, limits, Sandbox{}, []string{"--emit-kir", sourcePath, kirPath})
			if runtimeDiagnostic != nil {
				t.Fatalf("prepare selfhost source compiler: %s", runtimeDiagnostic.Message)
			}
			var output bytes.Buffer
			runtime.output = &output
			selfhostDiagnostic := runtime.run()
			if test.wantAccept {
				if goDiagnostic != nil {
					t.Fatalf("Go frontend rejected valid source: %s", goDiagnostic.Message)
				}
				if selfhostDiagnostic != nil {
					t.Fatalf("selfhost frontend rejected valid source: %s; output=%q", selfhostDiagnostic.Message, output.String())
				}
				encoded, err := os.ReadFile(kirPath)
				if err != nil {
					t.Fatalf("read selfhost emitted KIR: %v", err)
				}
				selfhostMIR, err := DecodeMIR(encoded, limits)
				if err != nil {
					t.Fatalf("ValidateKIR rejected selfhost emitted KIR: %v", err)
				}
				goCalls, selfhostCalls := builtinCalls(goMIR.arena), builtinCalls(selfhostMIR.arena)
				if test.compareBuiltinSignatures && !reflect.DeepEqual(goCalls, selfhostCalls) {
					t.Fatalf("Go and selfhost builtin signatures differ: Go=%v selfhost=%v", goCalls, selfhostCalls)
				}
				if test.wantBuiltinCallCount > 0 {
					countCalls := func(calls map[string]int) int {
						count := 0
						for _, countForSignature := range calls {
							count += countForSignature
						}
						return count
					}
					if countCalls(goCalls) != test.wantBuiltinCallCount || countCalls(selfhostCalls) != test.wantBuiltinCallCount {
						t.Fatalf("Go/selfhost builtin call counts=%d/%d, want %d; Go=%v selfhost=%v", countCalls(goCalls), countCalls(selfhostCalls), test.wantBuiltinCallCount, goCalls, selfhostCalls)
					}
				}
				if test.compareBuiltinSignatures {
					wantCalls := map[string]int{
						"builtin:int(Float)->Int":                                               1,
						"builtin:len(Map[String,Array[Int]])->Int":                              1,
						"builtin:len(Set[String])->Int":                                         1,
						"builtin:assert_eq(Array[Int],Array[Int])->Nil":                         1,
						"builtin:assert_eq(Map[String,Array[Int]],Map[String,Array[Int]])->Nil": 1,
						"builtin:assert_eq(Set[String],Set[String])->Nil":                       1,
					}
					if !reflect.DeepEqual(selfhostCalls, wantCalls) {
						t.Fatalf("selfhost emitted wrong checked builtin calls: got=%v want=%v", selfhostCalls, wantCalls)
					}
				}
			} else {
				if goDiagnostic == nil {
					t.Fatal("Go frontend accepted invalid source")
				}
				if test.goError != "" && !strings.Contains(goDiagnostic.Message, test.goError) {
					t.Fatalf("Go frontend rejected source for an unexpected reason: %s; want %q", goDiagnostic.Message, test.goError)
				}
				if selfhostDiagnostic == nil {
					t.Fatal("selfhost frontend accepted invalid source")
				}
				if test.selfhostError != "" && !strings.Contains(output.String(), test.selfhostError) {
					t.Fatalf("selfhost frontend rejected source for an unexpected reason: %q; want %q", output.String(), test.selfhostError)
				}
			}
		})
	}
}

func TestSelfhostTypedArenaValidationRejectsForgedRows(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "selfhost", "kir_typed_validation_test.kry"))
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	program, diagnostic := LoadProgram(path, limits, "")
	if diagnostic != nil {
		t.Fatalf("load typed arena validation fixture: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatalf("check typed arena validation fixture: %s", diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatalf("prepare typed arena validation fixture: %s", diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("typed arena validation fixture: %s; output=%q", diagnostic.Message, output.String())
	}
	if output.Len() != 0 {
		t.Fatalf("typed arena validation fixture unexpectedly wrote %q", output.String())
	}
}
