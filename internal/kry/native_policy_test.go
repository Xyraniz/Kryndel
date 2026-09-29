package kry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
		{format: "macho", name: "C AOT", toolchain: "native Darwin C compiler", requiresTool: true},
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

func TestMachOTargetPolicySupportsDarwinArchitectures(t *testing.T) {
	for _, target := range []NativeTarget{
		{OS: "darwin", Arch: "amd64"},
		{OS: "darwin", Arch: "arm64"},
	} {
		if reason := nativeOutputTargetReason("macho", target); reason != "" {
			t.Errorf("Mach-O rejected supported target %s-%s: %s", target.OS, target.Arch, reason)
		}
	}
	for _, target := range []NativeTarget{
		{OS: "linux", Arch: "arm64"},
		{OS: "darwin", Arch: "386"},
	} {
		if reason := nativeOutputTargetReason("macho", target); reason == "" {
			t.Errorf("Mach-O accepted unsupported target %s-%s", target.OS, target.Arch)
		}
	}

	t.Setenv("KRY_CC", "")
	if runtime.GOOS != "darwin" {
		if _, err := compilerFor(NativeTarget{OS: "darwin", Arch: runtime.GOARCH}); err == nil {
			t.Fatal("Mach-O cross compiler unexpectedly selected on a non-Darwin host")
		}
	}
}

func TestInspectNativeMachO(t *testing.T) {
	for _, test := range []struct {
		name string
		cpu  byte
	}{
		{name: "x64", cpu: 7},
		{name: "arm64", cpu: 12},
	} {
		t.Run(test.name, func(t *testing.T) {
			image := make([]byte, 32)
			copy(image, []byte{0xcf, 0xfa, 0xed, 0xfe})
			image[4], image[7] = test.cpu, 1 // CPU_TYPE_X86_64 or CPU_TYPE_ARM64, little-endian
			image[12] = 2                    // MH_EXECUTE
			info, err := InspectNative(image)
			if err != nil || !strings.HasPrefix(info, "Mach-O\narchitecture: ") {
				t.Fatalf("InspectNative(%s) = %q, %v", test.name, info, err)
			}
		})
	}
}

func TestGeneratedBuiltinCapabilitiesMatchBackendDispatch(t *testing.T) {
	cases := []struct {
		file     string
		fn       string
		selector string
		prefix   string
		want     map[string]struct{}
	}{
		{file: "codegen.go", fn: "builtinCall", selector: "Name", want: generatedCAOTBuiltinCases},
		{file: "machine_dynamic.go", fn: "emitExpr", selector: "Name", want: generatedDirectELFBuiltinCases},
		{file: "kir_machine_pe.go", fn: "validateKIRDirectPEExpr", selector: "CallTarget", prefix: "builtin:", want: generatedDirectPEBuiltinCases},
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
				tagName := ""
				switch tag := switchStmt.Tag.(type) {
				case *ast.SelectorExpr:
					tagName = tag.Sel.Name
				case *ast.Ident:
					tagName = tag.Name
				}
				if tagName != tc.selector {
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
							if tc.prefix != "" {
								if !strings.HasPrefix(name, tc.prefix) {
									continue
								}
								name = strings.TrimPrefix(name, tc.prefix)
							}
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
		if row.PEDirect == "" {
			t.Fatalf("builtin capability row omits PE-direct status: %#v", row)
		}
		if _, duplicate := lookup[key]; duplicate {
			t.Fatalf("duplicate builtin capability row %s", key)
		}
		lookup[key] = row
	}
	jsonLinux := lookup["json_parse/linux-x64"]
	if jsonLinux.Interpreter != "supported" || jsonLinux.CAOT != "supported" || jsonLinux.ELFDirect != "unsupported" || jsonLinux.SelfHosted != "partial" {
		t.Fatalf("unexpected Linux x64 json_parse capability: %#v", jsonLinux)
	}
	jsonBuiltins := []string{
		"json_parse", "json_stringify", "json_kind", "json_object_get",
		"json_array_len", "json_array_get", "json_string", "json_int",
		"json_uint", "json_float", "json_bool", "json_is_null",
	}
	for _, name := range jsonBuiltins {
		row, ok := lookup[name+"/linux-x64"]
		if !ok {
			t.Fatalf("builtin capability matrix is missing %s on Linux x64", name)
		}
		if row.Interpreter != "supported" || row.CAOT != "supported" || row.ELFDirect != "unsupported" {
			t.Errorf("unexpected Linux x64 %s capability: %#v", name, row)
		}
		if _, listed := generatedCAOTBuiltinCases[name]; !listed {
			t.Errorf("%s is advertised for CAOT but absent from generated CAOT inventory", name)
		}
		if _, listed := generatedDirectELFBuiltinCases[name]; listed {
			t.Errorf("%s must remain absent from generated ELF-direct inventory", name)
		}
		wantSelfHosted := "unsupported"
		if _, listed := generatedSelfHostedBuiltinNames[name]; listed {
			wantSelfHosted = "partial"
		}
		if row.SelfHosted != wantSelfHosted {
			t.Errorf("%s self-hosted capability is %q, want %q from generated inventory", name, row.SelfHosted, wantSelfHosted)
		}
	}
	jsonWindows := lookup["json_parse/windows-x64"]
	if jsonWindows.CAOT != "supported" || jsonWindows.ELFDirect != "unsupported" || jsonWindows.SelfHosted != "unsupported" {
		t.Fatalf("unexpected Windows x64 json_parse capability: %#v", jsonWindows)
	}
	jsonDarwin := lookup["json_parse/darwin-arm64"]
	if jsonDarwin.CAOT != "supported" || jsonDarwin.ELFDirect != "unsupported" {
		t.Fatalf("unexpected Darwin ARM64 json_parse capability: %#v", jsonDarwin)
	}
	websocket := lookup["websocket_connect/linux-x64"]
	if websocket.Interpreter != "supported" || websocket.CAOT != "unsupported" || websocket.ELFDirect != "unsupported" || websocket.PEDirect != "unsupported" || websocket.SelfHosted != "unsupported" {
		t.Fatalf("unexpected websocket_connect capability: %#v", websocket)
	}
	for _, target := range []string{"linux-x64", "linux-arm64", "windows-x64"} {
		if row := lookup["http_request/"+target]; row.CAOT != "partial" {
			t.Errorf("http_request C AOT capability on %s = %q, want partial: %#v", target, row.CAOT, row)
		}
	}
	for _, name := range []string{"print", "println", "str"} {
		if row := lookup[name+"/windows-x64"]; row.PEDirect != "partial" {
			t.Errorf("%s PE-direct capability = %q, want partial: %#v", name, row.PEDirect, row)
		}
		if row := lookup[name+"/linux-x64"]; row.PEDirect != "unsupported" {
			t.Errorf("%s PE-direct capability = %q on Linux, want unsupported", name, row.PEDirect)
		}
	}
	for _, name := range []string{"u8", "u16", "u32", "u64"} {
		if row := lookup[name+"/windows-x64"]; row.PEDirect != "supported" {
			t.Errorf("%s PE-direct capability = %q, want supported: %#v", name, row.PEDirect, row)
		}
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
		if row.Interpreter == "" || row.CAOT == "" || row.ELFDirect == "" || row.PEDirect == "" || row.SelfHosted == "" {
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
		{key: "statement/StMatch/linux-x64", caot: "supported", direct: "unsupported", selfHosted: "partial"},
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
	if row := lookup["expression/ExFloat/darwin-arm64"]; row.CAOT != "supported" || row.ELFDirect != "unsupported" {
		t.Errorf("unexpected Darwin ARM64 float expression capability: %#v", row)
	}
	for key, want := range map[string]string{
		"expression/ExInt/windows-x64":      "partial",
		"expression/ExFloat/windows-x64":    "partial",
		"expression/ExNil/windows-x64":      "partial",
		"type/TyFloat/windows-x64":          "partial",
		"statement/StMatch/windows-x64":     "unsupported",
		"unary_operator/BITNOT/windows-x64": "partial",
		"binary_operator/SHL/windows-x64":   "partial",
		"type/TyUInt/windows-x64":           "partial",
		"type/TyInt/linux-x64":              "unsupported",
		"builtin/u64/windows-x64":           "supported",
	} {
		if got := lookup[key].PEDirect; got != want {
			t.Errorf("%s PE-direct capability = %q, want %q", key, got, want)
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

func TestDirectPERejectsUnsupportedFloatFeatureClearly(t *testing.T) {
	p, c := testProgram(t, "fn value() -> Float { return 1.5 }\nprintln(value())\n")
	_, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"})
	if err == nil || !strings.Contains(err.Error(), "direct PE dynamic subset") || !strings.Contains(err.Error(), "return type Float") {
		t.Fatalf("direct PE should clearly reject dynamic Float values, got %v", err)
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

func TestGUIIsRejectedByBackendsWithoutWindowsSubsystemSupport(t *testing.T) {
	p, c := testProgram(t, "println(1)\n")
	_, err := BuildNative(p, c, NativeTarget{OS: "windows", Arch: "amd64", GUI: true}, "exe")
	if err == nil || !strings.Contains(err.Error(), "C AOT backend does not support Windows GUI subsystem targets") {
		t.Fatalf("expected clear C AOT GUI rejection, got %v", err)
	}
}
