package kry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestNativeBackendDescriptions(t *testing.T) {
	cases := []struct {
		format       string
		name         string
		toolchain    string
		requiresTool bool
	}{
		{format: "elf-direct", name: "direct ELF", toolchain: "none"},
		{format: "pe-direct", name: "direct PE32+", toolchain: "none"},
		{format: "c", name: "C source", toolchain: "none (source only)"},
		{format: "elf", name: "C AOT", toolchain: "external C compiler", requiresTool: true},
		{format: "exe", name: "C AOT", toolchain: "external C compiler", requiresTool: true},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			got, err := DescribeNativeBackend(tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tc.name || got.ExternalToolchain != tc.toolchain || got.RequiresExternalToolchain != tc.requiresTool {
				t.Fatalf("unexpected backend description: %#v", got)
			}
		})
	}
}

func TestNativeCapabilityMatrixMatchesTargetPolicy(t *testing.T) {
	rows := NativeCapabilityMatrix()
	if len(rows) != len(nativeCapabilityTargets)*6+1 {
		t.Fatalf("capability matrix has %d rows, want %d", len(rows), len(nativeCapabilityTargets)*6+1)
	}
	targets := make(map[string]NativeTarget, len(nativeCapabilityTargets))
	for _, target := range nativeCapabilityTargets {
		targets[target.name] = target.target
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := row.Format + "/" + row.Target
		if seen[key] {
			t.Fatalf("duplicate capability row %s", key)
		}
		seen[key] = true
		if row.Format == "c" {
			if row.Target != "any" || row.Status != "source-only" {
				t.Fatalf("unexpected C source capability: %#v", row)
			}
			continue
		}
		target, ok := targets[row.Target]
		if !ok {
			t.Fatalf("capability row has unknown target %q", row.Target)
		}
		if row.Format == "macho" {
			if row.Status != "unsupported" {
				t.Fatalf("Mach-O capability should be unsupported: %#v", row)
			}
			continue
		}
		reason := nativeOutputTargetReason(row.Format, target)
		want := "supported"
		if (row.Format == "elf-direct" || row.Format == "pe-direct") && reason == "" {
			want = "partial"
		} else if reason != "" {
			want = "unsupported"
		}
		if row.Status != want {
			t.Fatalf("%s status is %q, want %q (policy reason %q)", key, row.Status, want, reason)
		}
	}
	if len(seen) != len(rows) {
		t.Fatalf("matrix has %d rows but only %d unique rows", len(rows), len(seen))
	}
}

func TestGeneratedBuiltinCapabilitiesMatchBackendDispatch(t *testing.T) {
	cases := []struct {
		file string
		fn   string
		want map[string]struct{}
	}{
		{file: "codegen.go", fn: "builtinCall", want: generatedCAOTBuiltinCases},
		{file: "machine_dynamic.go", fn: "emitExpr", want: generatedDirectELFBuiltinCases},
	}
	for _, tc := range cases {
		t.Run(tc.fn, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tc.file, nil, parser.AllErrors)
			if err != nil {
				t.Fatal(err)
			}
			var function *ast.FuncDecl
			for _, declaration := range file.Decls {
				if candidate, ok := declaration.(*ast.FuncDecl); ok && candidate.Name.Name == tc.fn {
					function = candidate
					break
				}
			}
			if function == nil {
				t.Fatalf("function %s not found in %s", tc.fn, tc.file)
			}
			actual := map[string]bool{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switchStmt, ok := node.(*ast.SwitchStmt)
				if !ok {
					return true
				}
				tag, ok := switchStmt.Tag.(*ast.SelectorExpr)
				if !ok || tag.Sel.Name != "Name" {
					return true
				}
				for _, rawClause := range switchStmt.Body.List {
					clause := rawClause.(*ast.CaseClause)
					for _, expression := range clause.List {
						literal, ok := expression.(*ast.BasicLit)
						if !ok || literal.Kind != token.STRING {
							continue
						}
						name, err := strconv.Unquote(literal.Value)
						if err == nil {
							actual[name] = true
						}
					}
				}
				return true
			})
			if tc.fn == "emitExpr" {
				// These two builtins are statement-lowered by emitStatements.
				actual["print"] = true
				actual["println"] = true
			}
			if len(actual) != len(tc.want) {
				t.Fatalf("generated capability inventory has %d names, backend dispatch has %d", len(tc.want), len(actual))
			}
			for name := range actual {
				if _, ok := tc.want[name]; !ok {
					t.Errorf("backend dispatch supports %q, missing from generated inventory", name)
				}
			}
		})
	}
}

func TestGeneratedInterpreterInventoryMatchesBuiltinRegistry(t *testing.T) {
	registered := Builtins()
	for name := range registered {
		if _, ok := generatedInterpreterBuiltinCases[name]; !ok {
			t.Errorf("registered builtin %q has no interpreter dispatch case", name)
		}
	}
	for name := range generatedSelfHostedBuiltinNames {
		if _, ok := registered[name]; !ok {
			t.Errorf("self-host source inventory contains unknown builtin %q", name)
		}
	}
	source, err := os.ReadFile("../../selfhost/source_kir_compiler.kry")
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`(?:name|token\.text)\s*==\s*"([a-z][a-z0-9_]*)"`)
	actualSelfHosted := map[string]bool{}
	for _, match := range pattern.FindAllSubmatch(source, -1) {
		name := string(match[1])
		if _, ok := registered[name]; ok {
			actualSelfHosted[name] = true
		}
	}
	if len(actualSelfHosted) != len(generatedSelfHostedBuiltinNames) {
		t.Fatalf("generated self-host inventory has %d names, source lists %d", len(generatedSelfHostedBuiltinNames), len(actualSelfHosted))
	}
	for name := range actualSelfHosted {
		if _, ok := generatedSelfHostedBuiltinNames[name]; !ok {
			t.Errorf("self-host source recognizes %q, missing from generated inventory", name)
		}
	}
}

func TestBuiltinCapabilityMatrixListsEveryBackendAndTarget(t *testing.T) {
	rows := BuiltinCapabilityMatrix()
	if len(rows) != len(Builtins())*len(nativeCapabilityTargets) {
		t.Fatalf("builtin matrix has %d rows, want %d", len(rows), len(Builtins())*len(nativeCapabilityTargets))
	}
	lookup := map[string]BuiltinCapability{}
	for _, row := range rows {
		key := row.Builtin + "/" + row.Target
		if _, duplicate := lookup[key]; duplicate {
			t.Fatalf("duplicate builtin capability row %s", key)
		}
		lookup[key] = row
	}
	jsonLinux := lookup["json_parse/linux-x64"]
	if jsonLinux.Interpreter != "supported" || jsonLinux.CAOT != "supported" || jsonLinux.ELFDirect != "supported" || jsonLinux.SelfHosted != "partial" {
		t.Fatalf("unexpected Linux x64 json_parse capability: %#v", jsonLinux)
	}
	jsonWindows := lookup["json_parse/windows-x64"]
	if jsonWindows.CAOT != "supported" || jsonWindows.ELFDirect != "unsupported" || jsonWindows.SelfHosted != "unsupported" {
		t.Fatalf("unexpected Windows x64 json_parse capability: %#v", jsonWindows)
	}
	websocket := lookup["websocket_connect/linux-x64"]
	if websocket.Interpreter != "supported" || websocket.CAOT != "unsupported" || websocket.ELFDirect != "unsupported" || websocket.SelfHosted != "unsupported" {
		t.Fatalf("unexpected websocket_connect capability: %#v", websocket)
	}
}

func TestLanguageCapabilityMatrixCoversGeneratedItemsAndTargets(t *testing.T) {
	rows := LanguageCapabilityMatrix()
	expectedItems := len(Builtins()) + len(generatedLanguageExprKinds) + len(generatedLanguageStmtKinds) + len(generatedLanguagePatternKinds) + len(generatedLanguageUnaryOperators) + len(generatedLanguageBinaryOperators) + len(generatedLanguageTypeKinds)
	if len(rows) != expectedItems*len(nativeCapabilityTargets) {
		t.Fatalf("language matrix has %d rows, want %d", len(rows), expectedItems*len(nativeCapabilityTargets))
	}
	lookup := map[string]LanguageCapability{}
	for _, row := range rows {
		key := row.Category + "/" + row.Feature + "/" + row.Target
		if _, duplicate := lookup[key]; duplicate {
			t.Fatalf("duplicate language capability row %s", key)
		}
		if row.Interpreter == "" || row.CAOT == "" || row.ELFDirect == "" || row.SelfHosted == "" {
			t.Fatalf("capability row has an empty backend state: %#v", row)
		}
		lookup[key] = row
	}
	if len(lookup) != len(rows) {
		t.Fatalf("language matrix has %d rows but %d unique rows", len(rows), len(lookup))
	}
	checks := []struct {
		key          string
		caot, direct string
		selfHosted   string
	}{
		{key: "expression/ExFloat/linux-x64", caot: "supported", direct: "unsupported", selfHosted: "unsupported"},
		{key: "statement/StMatch/linux-x64", caot: "supported", direct: "unsupported", selfHosted: "unsupported"},
		{key: "pattern/PatResult/linux-x64", caot: "supported", direct: "unsupported", selfHosted: "unsupported"},
		{key: "unary_operator/MINUS/linux-x64", caot: "supported", direct: "partial", selfHosted: "partial"},
		{key: "binary_operator/SHL/linux-x64", caot: "unsupported", direct: "partial", selfHosted: "partial"},
		{key: "type/TyChannel/linux-x64", caot: "supported", direct: "unsupported", selfHosted: "unsupported"},
	}
	for _, check := range checks {
		row, ok := lookup[check.key]
		if !ok {
			t.Fatalf("language matrix is missing %s", check.key)
		}
		if row.CAOT != check.caot || row.ELFDirect != check.direct || row.SelfHosted != check.selfHosted {
			t.Errorf("%s has unexpected backend states: %#v", check.key, row)
		}
	}
}

func TestLanguageCapabilityNamesCoverDeclaredKinds(t *testing.T) {
	for kind := ExprKind(0); kind < ExprKind(len(generatedLanguageExprKinds)); kind++ {
		name := expressionKindName(kind)
		if name == "" {
			t.Errorf("expression kind %d has no language capability name", kind)
			continue
		}
		if _, ok := generatedLanguageExprKinds[name]; !ok {
			t.Errorf("expression kind %q is missing from generated language inventory", name)
		}
	}
	for kind := StmtKind(0); kind < StmtKind(len(generatedLanguageStmtKinds)); kind++ {
		name := statementKindName(kind)
		if name == "" {
			t.Errorf("statement kind %d has no language capability name", kind)
			continue
		}
		if _, ok := generatedLanguageStmtKinds[name]; !ok {
			t.Errorf("statement kind %q is missing from generated language inventory", name)
		}
	}
	for kind := PatternKind(0); kind < PatternKind(len(generatedLanguagePatternKinds)); kind++ {
		name := patternKindName(kind)
		if name == "" {
			t.Errorf("pattern kind %d has no language capability name", kind)
			continue
		}
		if _, ok := generatedLanguagePatternKinds[name]; !ok {
			t.Errorf("pattern kind %q is missing from generated language inventory", name)
		}
	}
	for kind := TyVoid; kind <= TyFFIBuffer; kind++ {
		name := typeKindName(kind)
		if name == "" {
			t.Errorf("language type kind %d has no capability name", kind)
			continue
		}
		if _, ok := generatedLanguageTypeKinds[name]; !ok {
			t.Errorf("language type %q is missing from generated inventory", name)
		}
	}
}

func TestNativeBuildPreflightsGeneratedSyntaxCapabilities(t *testing.T) {
	p, c := testProgram(t, "unsafe { }\n")
	_, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"})
	if err == nil || !strings.Contains(err.Error(), `statement "StUnsafe" is not listed as supported by the elf-direct backend`) {
		t.Fatalf("direct ELF should reject unsupported statements during capability preflight, got %v", err)
	}
}

func TestNoExternalToolchainNeverLaunchesCCompiler(t *testing.T) {
	old := nativeExecCommand
	t.Cleanup(func() { nativeExecCommand = old })
	called := false
	nativeExecCommand = func(name string, args ...string) *exec.Cmd {
		called = true
		return exec.Command(name, args...)
	}

	p, c := testProgram(t, "println(\"must not invoke C\")\n")
	_, err := BuildNativeWithPolicyOpts(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf", false, true)
	if err == nil || !strings.Contains(err.Error(), "--no-external-toolchain") || !strings.Contains(err.Error(), "external C compiler") {
		t.Fatalf("expected explicit external-toolchain rejection, got %v", err)
	}
	if called {
		t.Fatal("no-external-toolchain build attempted to execute an external command")
	}
}

func TestNativeBuildPreflightsGeneratedBuiltinCapabilities(t *testing.T) {
	p, c := testProgram(t, `let library = ffi_library_open("missing.so")`)
	if _, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "c"); err == nil || !strings.Contains(err.Error(), `builtin "ffi_library_open" is not listed as supported by the c backend`) {
		t.Fatalf("C source output should reject unsupported builtins during capability preflight, got %v", err)
	}
	p, c = testProgram(t, `let socket = tcp_connect("127.0.0.1", 1)`)
	if _, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf-direct"); err == nil || !strings.Contains(err.Error(), `builtin "tcp_connect" is not listed as supported by the elf-direct backend`) {
		t.Fatalf("direct ELF should reject unsupported builtins during capability preflight, got %v", err)
	}
	if _, err := BuildDirectELF(p, c, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), `builtin "tcp_connect" is not listed as supported by the elf-direct backend`) {
		t.Fatalf("direct ELF API should reject unsupported builtins during capability preflight, got %v", err)
	}
}

func TestNativeTargetIsRejectedBeforeFeatureLowering(t *testing.T) {
	p, c := testProgram(t, `let result: Result[FFILibrary, String] = ffi_library_open("missing.dll")`)
	_, err := BuildNative(p, c, NativeTarget{OS: "windows", Arch: "amd64"}, "elf")
	if err == nil || !strings.Contains(err.Error(), "ELF output requires a Linux target") {
		t.Fatalf("expected target validation before C feature lowering, got %v", err)
	}
}
