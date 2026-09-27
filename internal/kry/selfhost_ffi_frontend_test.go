package kry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type selfhostFFIFrontend struct {
	program *Program
	checker *Checker
	limits  Limits
}

func loadSelfhostFFIFrontend(t *testing.T) selfhostFFIFrontend {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	selfhostDir := filepath.Join(root, "..", "..", "selfhost")
	source, err := os.ReadFile(filepath.Join(selfhostDir, "source_kir_compiler.kry"))
	if err != nil {
		t.Fatal(err)
	}
	// In this test-only copy, return the parsed KIR document instead of invoking
	// the dynamic backend. FFI parsing/serialization can then be tested before
	// backend lowering, which does not implement these builtins yet.
	const backendCall = "    let lowered: Result[Bytes, String] = compile_document(result_unwrap(document))\n    return lowered"
	const rawKIRReturn = "    return ok(string_to_bytes(json_stringify(result_unwrap(document))))"
	frontendSource := strings.ReplaceAll(string(source), "\r\n", "\n")
	if count := strings.Count(frontendSource, backendCall); count != 1 {
		t.Fatalf("source compiler backend call appears %d times, want exactly one instrumentation point", count)
	}
	frontendSource = strings.Replace(frontendSource, backendCall, rawKIRReturn, 1)

	modulesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(modulesDir, "source_kir_compiler.kry"), []byte(frontendSource), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, module := range []string{"dynamic_backend.kry", "elf_backend.kry", "pe_backend.kry"} {
		contents, err := os.ReadFile(filepath.Join(selfhostDir, module))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(modulesDir, module), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	program, diagnostic := LoadProgram(filepath.Join(modulesDir, "source_kir_compiler.kry"), DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load instrumented selfhost source frontend: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("check instrumented selfhost source frontend: %s", diagnostic.Message)
	}
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 180_000
	return selfhostFFIFrontend{program: program, checker: checker, limits: limits}
}

func (frontend selfhostFFIFrontend) compile(t *testing.T, source string) ([]byte, *Diagnostic) {
	t.Helper()
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "input.kry")
	outputPath := filepath.Join(dir, "output.kir.json")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, diagnostic := NewRuntimeWithArgs(frontend.program, frontend.checker, frontend.limits, Sandbox{}, []string{sourcePath, outputPath})
	if diagnostic != nil {
		return nil, diagnostic
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		return nil, diagnostic
	}
	serializedKIR, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return serializedKIR, nil
}

func TestSelfhostSourceFrontendSerializesFFISignatures(t *testing.T) {
	frontend := loadSelfhostFFIFrontend(t)
	const source = `
fn accept_ffi_handles(library: FFILibrary, symbol: FFISymbol, buffer: FFIBuffer) -> Nil {
    ffi_library_close(library)
    let resolved: Result[FFISymbol, String] = ffi_symbol(library, "native_symbol")
    let called: Result[Int, String] = ffi_call(symbol, "i(i)", [7])
    let owned: FFIBuffer = ffi_buffer_new(string_to_bytes("input"))
    let allocated: Result[FFIBuffer, String] = ffi_buffer_new_sized(32)
    let address: Result[Int, String] = ffi_buffer_address(buffer)
    let copied: Result[Bytes, String] = ffi_buffer_read(buffer)
    ffi_buffer_close(buffer)
    ffi_thread_pin()
    ffi_thread_unpin()
    return nil
}

fn main() -> Nil {
    let opened: Result[FFILibrary, String] = ffi_library_open("native-library")
    return nil
}
`
	serializedKIR, diagnostic := frontend.compile(t, source)
	if diagnostic != nil {
		t.Fatalf("compile valid FFI source to KIR: %s", diagnostic.Message)
	}
	var document map[string]any
	if err := json.Unmarshal(serializedKIR, &document); err != nil {
		t.Fatalf("decode serialized KIR: %v\n%s", err, serializedKIR)
	}

	wantCalls := map[string]struct {
		returnType    string
		argumentTypes []string
	}{
		"ffi_library_open":     {"Result[FFILibrary,String]", []string{"String"}},
		"ffi_library_close":    {"Nil", []string{"FFILibrary"}},
		"ffi_symbol":           {"Result[FFISymbol,String]", []string{"FFILibrary", "String"}},
		"ffi_call":             {"Result[Int,String]", []string{"FFISymbol", "String", "Array[Int]"}},
		"ffi_buffer_new":       {"FFIBuffer", []string{"Bytes"}},
		"ffi_buffer_new_sized": {"Result[FFIBuffer,String]", []string{"Int"}},
		"ffi_buffer_address":   {"Result[Int,String]", []string{"FFIBuffer"}},
		"ffi_buffer_read":      {"Result[Bytes,String]", []string{"FFIBuffer"}},
		"ffi_buffer_close":     {"Nil", []string{"FFIBuffer"}},
		"ffi_thread_pin":       {"Nil", []string{}},
		"ffi_thread_unpin":     {"Nil", []string{}},
	}
	seenCalls := make(map[string]int, len(wantCalls))
	walkSelfhostKIR(document, func(node map[string]any) {
		if node["kind"] != "call" {
			return
		}
		name, _ := node["name"].(string)
		want, ok := wantCalls[name]
		if !ok {
			return
		}
		seenCalls[name]++
		if got, _ := node["call_target"].(string); got != "builtin:"+name {
			t.Errorf("%s call target = %q, want builtin:%s", name, got, name)
		}
		if got, _ := node["type"].(string); got != want.returnType {
			t.Errorf("%s return type = %q, want %q", name, got, want.returnType)
		}
		args, ok := node["args"].([]any)
		if !ok || len(args) != len(want.argumentTypes) {
			t.Errorf("%s arguments = %#v, want %d arguments", name, node["args"], len(want.argumentTypes))
			return
		}
		for index, argument := range args {
			argumentNode, ok := argument.(map[string]any)
			if !ok {
				t.Errorf("%s argument %d is not a KIR expression: %#v", name, index, argument)
				continue
			}
			if got, _ := argumentNode["type"].(string); got != want.argumentTypes[index] {
				t.Errorf("%s argument %d type = %q, want %q", name, index, got, want.argumentTypes[index])
			}
		}
	})
	for name := range wantCalls {
		if seenCalls[name] != 1 {
			t.Errorf("serialized KIR contains %d calls to %s, want exactly one", seenCalls[name], name)
		}
	}

	functions, ok := document["functions"].([]any)
	if !ok {
		t.Fatalf("KIR functions has type %T, want []any", document["functions"])
	}
	handleParameters := map[string]string{}
	for _, rawFunction := range functions {
		function, ok := rawFunction.(map[string]any)
		if !ok {
			continue
		}
		parameters, _ := function["params"].([]any)
		for _, rawParameter := range parameters {
			parameter, ok := rawParameter.(map[string]any)
			if !ok {
				continue
			}
			name, _ := parameter["name"].(string)
			typeName, _ := parameter["type"].(string)
			handleParameters[name] = typeName
		}
	}
	for name, want := range map[string]string{"library": "FFILibrary", "symbol": "FFISymbol", "buffer": "FFIBuffer"} {
		if got := handleParameters[name]; got != want {
			t.Errorf("serialized parameter %q has type %q, want %q", name, got, want)
		}
	}
}

func TestSelfhostSourceFrontendRejectsInvalidFFISignatures(t *testing.T) {
	frontend := loadSelfhostFFIFrontend(t)
	for _, test := range []struct {
		name        string
		expression  string
		wantMessage string
	}{
		{
			name:        "ffi_call argument types",
			expression:  `ffi_call(1, "i(i)", [])`,
			wantMessage: "ffi_call expects FFISymbol, String, and Array[Int]",
		},
		{
			name:        "ffi_thread_pin arity",
			expression:  "ffi_thread_pin(1)",
			wantMessage: "ffi_thread_pin expects no arguments",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "fn main() -> Nil {\n    " + test.expression + "\n    return nil\n}\n"
			_, diagnostic := frontend.compile(t, source)
			if diagnostic == nil || !strings.Contains(diagnostic.Message, test.wantMessage) {
				t.Fatalf("invalid FFI signature diagnostic = %v, want message containing %q", diagnostic, test.wantMessage)
			}
		})
	}
}

func walkSelfhostKIR(value any, visit func(map[string]any)) {
	switch value := value.(type) {
	case map[string]any:
		visit(value)
		for _, child := range value {
			walkSelfhostKIR(child, visit)
		}
	case []any:
		for _, child := range value {
			walkSelfhostKIR(child, visit)
		}
	}
}
