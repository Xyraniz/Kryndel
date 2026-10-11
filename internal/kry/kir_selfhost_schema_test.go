package kry

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestKIRV6JSONSchemaMatchesGoWireFields(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "kir-v6.schema.json"))
	if err != nil {
		t.Fatalf("read KIR v6 JSON schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode KIR v6 JSON schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("KIR v6 JSON schema has no root properties")
	}
	version, ok := properties["version"].(map[string]any)
	if !ok || version["const"] != float64(KIRVersion) {
		t.Fatalf("KIR schema version = %#v, want %d", version, KIRVersion)
	}

	definitions, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("KIR v6 JSON schema has no $defs")
	}
	wireTypes := map[string]reflect.Type{
		"target": reflect.TypeOf(KIRTarget{}), "span": reflect.TypeOf(KIRSourceSpan{}),
		"import": reflect.TypeOf(KIRImport{}), "struct": reflect.TypeOf(KIRStruct{}),
		"field": reflect.TypeOf(KIRField{}), "enum": reflect.TypeOf(KIREnum{}),
		"trait": reflect.TypeOf(KIRTrait{}), "traitMethod": reflect.TypeOf(KIRTraitMethod{}),
		"traitImpl": reflect.TypeOf(KIRTraitImpl{}), "traitImplMethod": reflect.TypeOf(KIRTraitImplMethod{}),
		"typeParam": reflect.TypeOf(KIRTypeParam{}), "param": reflect.TypeOf(KIRParam{}),
		"binding": reflect.TypeOf(KIRBinding{}), "value": reflect.TypeOf(KIRValue{}),
		"pattern": reflect.TypeOf(KIRPattern{}), "arm": reflect.TypeOf(KIRArm{}),
		"expression": reflect.TypeOf(KIRExpr{}), "statement": reflect.TypeOf(KIRStmt{}),
		"function": reflect.TypeOf(KIRFunction{}),
	}
	for name, wireType := range wireTypes {
		definition, ok := definitions[name].(map[string]any)
		if !ok {
			t.Errorf("KIR schema is missing definition %q", name)
			continue
		}
		got := schemaPropertyNames(definition)
		want := make(map[string]bool, wireType.NumField())
		for index := 0; index < wireType.NumField(); index++ {
			field := wireType.Field(index)
			wireName := strings.Split(field.Tag.Get("json"), ",")[0]
			if wireName == "" || wireName == "-" {
				continue
			}
			want[wireName] = true
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("KIR schema definition %q properties = %v, want Go wire properties %v", name, sortedSchemaNames(got), sortedSchemaNames(want))
		}
	}
	rootProperties := schemaPropertyNames(schema)
	wantRootProperties := make(map[string]bool, reflect.TypeOf(KIRDocument{}).NumField())
	for index := 0; index < reflect.TypeOf(KIRDocument{}).NumField(); index++ {
		field := reflect.TypeOf(KIRDocument{}).Field(index)
		wireName := strings.Split(field.Tag.Get("json"), ",")[0]
		if wireName != "" && wireName != "-" {
			wantRootProperties[wireName] = true
		}
	}
	if !reflect.DeepEqual(rootProperties, wantRootProperties) {
		t.Errorf("KIR schema root properties = %v, want Go wire properties %v", sortedSchemaNames(rootProperties), sortedSchemaNames(wantRootProperties))
	}
}

func schemaPropertyNames(schema map[string]any) map[string]bool {
	names := map[string]bool{}
	if properties, ok := schema["properties"].(map[string]any); ok {
		for name := range properties {
			names[name] = true
		}
	}
	if allOf, ok := schema["allOf"].([]any); ok {
		for _, part := range allOf {
			if definition, ok := part.(map[string]any); ok {
				for name := range schemaPropertyNames(definition) {
					names[name] = true
				}
			}
		}
	}
	return names
}

func sortedSchemaNames(names map[string]bool) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// This drift check ensures that every KIR v6 wire field is accounted for by
// the selfhost validator; declaration-specific semantic validation stays in
// each compiler and backend.
func TestSelfhostValidatorContractsMatchSupportedKIRWireFields(t *testing.T) {
	kirFile, err := parser.ParseFile(token.NewFileSet(), "kir.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse Go KIR schema: %v", err)
	}
	validatorPath := filepath.Join("..", "..", "selfhost", "validated_kir.kry")
	validator, err := os.ReadFile(validatorPath)
	if err != nil {
		t.Fatalf("read selfhost KIR validator: %v", err)
	}
	validatorText := string(validator)
	documentValidator := selfhostFunctionSection(validatorText, "validate_kir_typed_arena")
	traitReject := `if result_unwrap(kir_typed_array_len(result_unwrap(traits))) != 0 || result_unwrap(kir_typed_array_len(result_unwrap(trait_impls))) != 0 { return err("selfhost backends do not support KIR trait declarations") }`
	if rejectAt := strings.Index(documentValidator, traitReject); rejectAt < 0 || rejectAt > strings.Index(documentValidator, "validate_structs(") {
		t.Fatal("selfhost KIR validator must reject non-empty trait arrays before declaration validation")
	}
	functionValidator := selfhostFunctionSection(validatorText, "validate_functions")
	captureReject := `if result_unwrap(kir_typed_array_len(result_unwrap(captures))) != 0 { return err("selfhost backends reject captured functions") }`
	if rejectAt := strings.Index(functionValidator, captureReject); rejectAt < 0 || rejectAt > strings.Index(functionValidator, "validate_binding(") {
		t.Fatal("selfhost KIR validator must reject captured functions before validating capture entries")
	}

	// Selfhost rejects traits and captured functions before validating their
	// nested declaration shapes; those nested object types have no field contract.
	allowedFields := selfhostKIRAllowedFields()
	validatorOwners := selfhostKIRValidatorOwners()
	unsupportedTypes := map[string]bool{"KIRTrait": true, "KIRTraitMethod": true, "KIRTraitImpl": true, "KIRTraitImplMethod": true, "KIRCapture": true}
	for _, declaration := range kirFile.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || !strings.HasPrefix(typeSpec.Name.Name, "KIR") {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			if unsupportedTypes[typeSpec.Name.Name] {
				continue
			}
			contract, supported := allowedFields[typeSpec.Name.Name]
			if !supported {
				t.Fatalf("KIR wire type %s has no selfhost field contract", typeSpec.Name.Name)
			}
			owner := validatorOwners[typeSpec.Name.Name]
			section := selfhostFunctionSection(validatorText, owner)
			if section == "" || !strings.Contains(section, contract) {
				t.Errorf("selfhost validator is missing the owner-scoped %s field contract %s", typeSpec.Name.Name, contract)
			}
			allowed := make(map[string]bool)
			for _, wireName := range strings.Split(strings.Trim(contract, "|"), "|") {
				allowed[wireName] = true
			}
			for _, field := range structure.Fields.List {
				if field.Tag == nil {
					continue
				}
				rawTag, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					t.Fatalf("parse %s field tag: %v", typeSpec.Name.Name, err)
				}
				wireName := reflect.StructTag(rawTag).Get("json")
				if wireName == "" || wireName == "-" {
					continue
				}
				wireName = strings.Split(wireName, ",")[0]
				if !allowed[wireName] {
					t.Errorf("selfhost %s field contract does not account for Go wire field %q", typeSpec.Name.Name, wireName)
				}
			}
		}
	}
}

// These contracts are scoped to the specific object validators in
// validated_kir.kry, avoiding false passes from an identically named field on
// an unrelated KIR object.
func selfhostKIRAllowedFields() map[string]string {
	return map[string]string{
		"KIRTarget":     "|os|arch|gui|",
		"KIRSourceSpan": "|start|end|",
		"KIRImport":     "|path|source|line|column|span|",
		"KIRDocument":   "|format|version|language_version|module|source|target|imports|import_records|sources|structs|enums|traits|trait_impls|functions|statements|",
		"KIRStruct":     "|name|source|line|column|span|public|module|type_params|fields|",
		"KIRField":      "|name|source|line|column|span|public|type|",
		"KIREnum":       "|name|source|line|column|span|public|module|variants|variant_spans|",
		"KIRTypeParam":  "|name|constraint|source|line|column|span|",
		"KIRParam":      "|name|type|source|line|column|span|default|binding|",
		"KIRBinding":    "|id|name|type|mutable|source|line|column|span|",
		"KIRFunction":   "|name|source|line|column|span|public|worker|unsafe|trait|module|receiver|return|type_params|params|captures|body|",
		"KIRExpr":       "|kind|source|line|column|span|type|const|int|uint|uint_bits|float|bool|string|name|operator|call_target|trait_name|builtin_id|left|right|operand|args|items|base|field|receiver|map_keys|struct_name|struct_type|generic_arguments|fields|values|enum_type|enum_variant|tail|callee|lambda|binding|",
		"KIRStmt":       "|kind|source|line|column|span|name|binding|mutable|const|annotation|init|expr|target|value|cond|then|else|body|iter|return|scrutinee|arms|",
		"KIRArm":        "|source|line|column|span|pattern|body|",
		"KIRPattern":    "|kind|source|line|column|span|bool|int|string|type|variant|binding|present|ok|resolved_binding|",
		"KIRValue":      "|kind|int|uint|uint_bits|float|bool|string|bytes|array|inner|present|ok|",
	}
}

func selfhostKIRValidatorOwners() map[string]string {
	return map[string]string{
		"KIRTarget": "validate_kir_typed_arena", "KIRDocument": "validate_kir_typed_arena",
		"KIRSourceSpan": "validate_v6_span", "KIRImport": "validate_v6_metadata",
		"KIRStruct": "validate_structs", "KIRField": "validate_structs", "KIRTypeParam": "validate_structs",
		"KIREnum": "validate_enums", "KIRParam": "validate_functions", "KIRBinding": "validate_binding",
		"KIRFunction": "validate_functions", "KIRExpr": "validate_expression", "KIRStmt": "validate_statement",
		"KIRArm": "validate_statement", "KIRPattern": "validate_statement", "KIRValue": "validate_kir_value",
	}
}

func selfhostFunctionSection(source, function string) string {
	start := strings.Index(source, "fn "+function+"(")
	if start < 0 {
		return ""
	}
	end := strings.Index(source[start+1:], "\nfn ")
	if end < 0 {
		return source[start:]
	}
	return source[start : start+1+end]
}
