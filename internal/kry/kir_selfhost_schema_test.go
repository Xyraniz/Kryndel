package kry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// This drift check ensures that every KIR v5 wire field is accounted for by
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
	documentValidator := selfhostFunctionSection(validatorText, "validate_kir_document_mode")
	traitReject := `if result_unwrap(json_array_len(result_unwrap(traits))) != 0 || result_unwrap(json_array_len(result_unwrap(trait_impls))) != 0 { return err("selfhost backends do not support KIR trait declarations") }`
	if rejectAt := strings.Index(documentValidator, traitReject); rejectAt < 0 || rejectAt > strings.Index(documentValidator, "validate_structs(") {
		t.Fatal("selfhost KIR validator must reject non-empty trait arrays before declaration validation")
	}
	functionValidator := selfhostFunctionSection(validatorText, "validate_functions")
	captureReject := `if result_unwrap(json_array_len(result_unwrap(captures))) != 0 { return err("selfhost backends reject captured functions") }`
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
		"KIRTarget":    "|os|arch|gui|",
		"KIRDocument":  "|format|version|language_version|module|source|target|imports|sources|structs|enums|traits|trait_impls|functions|statements|",
		"KIRStruct":    "|name|public|module|type_params|fields|",
		"KIRField":     "|name|public|type|",
		"KIREnum":      "|name|public|module|variants|",
		"KIRTypeParam": "|name|constraint|",
		"KIRParam":     "|name|type|default|binding|",
		"KIRBinding":   "|name|type|mutable|source|line|column|",
		"KIRFunction":  "|name|source|line|column|public|worker|unsafe|trait|module|receiver|return|type_params|params|captures|body|",
		"KIRExpr":      "|kind|source|line|column|type|const|int|uint|uint_bits|float|bool|string|name|operator|call_target|trait_name|builtin_id|left|right|operand|args|items|base|field|receiver|map_keys|struct_name|struct_type|generic_arguments|fields|values|enum_type|enum_variant|tail|callee|lambda|binding|",
		"KIRStmt":      "|kind|source|line|column|name|binding|mutable|const|annotation|init|expr|target|value|cond|then|else|body|iter|return|scrutinee|arms|",
		"KIRArm":       "|pattern|body|",
		"KIRPattern":   "|kind|source|line|column|bool|int|string|type|variant|binding|present|ok|resolved_binding|",
		"KIRValue":     "|kind|int|uint|uint_bits|float|bool|string|bytes|array|inner|present|ok|",
	}
}

func selfhostKIRValidatorOwners() map[string]string {
	return map[string]string{
		"KIRTarget": "validate_kir_document_mode", "KIRDocument": "validate_kir_document_mode",
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
