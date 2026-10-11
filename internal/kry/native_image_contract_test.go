package kry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfhostLoweringProducesTypedImageBeforeELFOrPESerialization(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(root, "..", "..", "selfhost", "native_image.kry")
	imageSource, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	imageText := string(imageSource)
	for _, required := range []string{
		"pub struct LoweredNativeImage",
		"pub struct NativeImageImport",
		"pub struct NativeImageDataRelocation",
		"pub struct NativeImageImportRelocation",
		"pub struct NativeImageFunctionRange",
		"pub fn emit_native_image(image: LoweredNativeImage)",
	} {
		if !strings.Contains(imageText, required) {
			t.Errorf("native image contract is missing %q", required)
		}
	}
	if strings.Contains(imageText, "ValidatedKIR") || strings.Contains(imageText, "Json") {
		t.Fatal("native image serializer depends on KIR or a recursive JSON representation")
	}

	dynamicPath := filepath.Join(root, "..", "..", "selfhost", "dynamic_backend.kry")
	dynamicSource, err := os.ReadFile(dynamicPath)
	if err != nil {
		t.Fatal(err)
	}
	dynamicText := string(dynamicSource)
	if !strings.Contains(dynamicText, "fn lower_dynamic(kir: ValidatedKIR) -> Result[LoweredNativeImage, String]") {
		t.Fatal("dynamic lowerer does not produce the typed native image contract")
	}
	if !strings.Contains(dynamicText, "return emit_native_image(result_unwrap(lowered))") {
		t.Fatal("dynamic backend does not route its typed image through the platform serializer")
	}
	if strings.Contains(dynamicText, "return emit_pe32plus_with_imports_and_data_patches") || strings.Contains(dynamicText, "return emit_pe32plus_gui_with_imports_and_data_patches") {
		t.Fatal("dynamic lowering still calls the PE serializer directly")
	}
}

func TestSelfhostLowerersConsumeValidatedTypedKIR(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	selfhost := filepath.Join(root, "..", "..", "selfhost")
	read := func(name string) string {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(selfhost, name))
		if err != nil {
			t.Fatalf("read selfhost/%s: %v", name, err)
		}
		return string(contents)
	}
	validated := read("validated_kir.kry")
	backend := read("elf_backend.kry")
	dynamic := read("dynamic_backend.kry")
	for _, required := range []string{
		"private typed_arena: KIRTypedArena",
		"validated_kir_typed_root(kir: ValidatedKIR) -> KIRTypedNodeView",
		"kir_typed_arena_from_validated_json(document)",
	} {
		if !strings.Contains(validated, required) {
			t.Errorf("validated KIR arena wrapper is missing %q", required)
		}
	}
	for _, forbidden := range []string{"private root: KIRNodeRef", "private document: Json", "ELFKIRSlice", "ELFKIRExpression", "validated_kir_document", "kir_arena_from_json(document)"} {
		if strings.Contains(validated, forbidden) {
			t.Errorf("validated KIR wrapper retains a recursive or incomplete representation %q", forbidden)
		}
	}
	for _, required := range []string{
		"compile_kir(kir: ValidatedKIR)",
		"validated_kir_typed_root(kir)",
		"KIRTypedNodeView",
		"kir_typed_object_get",
		"import \"kir_typed_arena\"",
		"requires a non-GUI linux-amd64 target",
		"function declarations",
		"does not support statement kind",
	} {
		if !strings.Contains(backend, required) {
			t.Errorf("ELF consumer does not guard %q", required)
		}
	}
	if strings.Contains(backend, "ELFKIRSlice") || strings.Contains(backend, "ELFKIRExpression") {
		t.Fatal("ELF lowerer still consumes an incomplete projection instead of ValidatedKIR")
	}
	if strings.Contains(backend, "KIRNodeRef") || strings.Contains(backend, "kir_arena") {
		t.Fatal("ELF lowerer still traverses generic KIR JSON storage")
	}
	for _, required := range []string{
		"fn lower_dynamic(kir: ValidatedKIR) -> Result[LoweredNativeImage, String]",
		"validated_kir_typed_root(kir)",
		"KIRTypedNodeView",
		"kir_typed_object_get",
		"import \"kir_typed_arena\"",
		"pub fn compile_document(document: Json)",
	} {
		if !strings.Contains(dynamic, required) {
			t.Errorf("dynamic lowerer does not use the validated arena contract %q", required)
		}
	}
	for _, forbidden := range []string{
		"validated_kir_document",
		"json_object_get(",
		"json_array_get(",
		"json_array_len(",
		"json_string(",
		"json_int(",
		"json_bool(",
	} {
		if strings.Contains(dynamic, forbidden) {
			t.Errorf("dynamic lowerer still traverses recursive JSON through %q", forbidden)
		}
	}
	if strings.Contains(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(dynamic, "pub fn compile_document(document: Json)", ""), "pub fn compile_generated_document(document: Json)", ""), "fn compile_validated_document(document: Json, strict_fields: Bool)", ""), "document: Json") {
		t.Fatal("a non-decoder dynamic lowering entrypoint accepts Json")
	}
	if strings.Contains(dynamic, "KIRNodeRef") || strings.Contains(dynamic, "kir_arena") {
		t.Fatal("dynamic lowerer still traverses generic KIR JSON storage")
	}
}

func TestSelfhostDecoderBuildsArenaOnlyAfterValidation(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	validatedPath := filepath.Join(root, "..", "..", "selfhost", "validated_kir.kry")
	validatedSource, err := os.ReadFile(validatedPath)
	if err != nil {
		t.Fatal(err)
	}
	validated := string(validatedSource)
	for _, required := range []string{
		"fn validate_portable_kir(document: Json, strict_fields: Bool)",
		"pub fn validate_kir_typed_arena(arena: KIRTypedArena)",
		"return validate_kir_typed_arena(result_unwrap(decoded))",
		"return ok(ValidatedKIR{ typed_arena: arena })",
	} {
		if !strings.Contains(validated, required) {
			t.Errorf("selfhost validator is missing arena-validation boundary %q", required)
		}
	}
	if strings.Count(validated, "document: Json") != 3 {
		t.Fatalf("recursive Json appears outside decoder/metadata validation helpers; got %d document parameters", strings.Count(validated, "document: Json"))
	}
	start := strings.Index(validated, "fn validate_portable_kir(")
	if start < 0 {
		t.Fatal("cannot locate selfhost portable KIR decode boundary")
	}
	end := strings.Index(validated[start:], "\npub fn validate_kir_document(")
	if end < 0 {
		t.Fatal("cannot locate selfhost portable KIR validation entrypoint")
	}
	decodeAndValidate := validated[start : start+end]
	validateAt := strings.Index(decodeAndValidate, "wire_kir_shape(document,")
	typedAt := strings.Index(decodeAndValidate, "kir_typed_arena_from_validated_json(document)")
	if typedAt < 0 || validateAt < 0 || typedAt < validateAt {
		t.Fatal("the typed KIR arena must be constructed only after portable KIR shape validation succeeds")
	}
	program := parseSelfhostBoundaryModule(t, "validated_kir.kry")
	constructors := 0
	for _, function := range program.Functions {
		walkSelfhostBoundaryStatements(function.Body, func(expression *Expr) {
			if expression.Kind == ExStruct && expression.StructName == "ValidatedKIR" {
				constructors++
				if function.Name != "validate_kir_typed_arena" {
					t.Errorf("%s mints ValidatedKIR outside the shared typed validator", function.Name)
				}
			}
		})
	}
	if constructors != 1 {
		t.Fatalf("ValidatedKIR has %d construction sites, want the single typed validator", constructors)
	}
	typedValidator := selfhostFunctionSection(validated, "validate_kir_typed_arena")
	for _, required := range []string{"kir_typed_arena_structure_valid(arena)", "validate_functions(", "validate_statement_array(", "validate_v6_metadata(document)"} {
		checkedAt := strings.Index(typedValidator, required)
		if checkedAt < 0 || checkedAt > strings.Index(typedValidator, "ValidatedKIR{ typed_arena: arena }") {
			t.Errorf("typed KIR constructor does not perform %s before minting ValidatedKIR", required)
		}
	}
}
