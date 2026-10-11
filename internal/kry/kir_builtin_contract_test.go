package kry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuiltinSignatureContractsCoverRegistryExactly(t *testing.T) {
	seen := make(map[string]bool, len(builtinList))
	for _, builtin := range builtinList {
		if builtin.Name == "" {
			t.Fatal("builtin registry contains an empty name")
		}
		if seen[builtin.Name] {
			t.Fatalf("builtin registry contains duplicate %q", builtin.Name)
		}
		seen[builtin.Name] = true

		signature, err := parseBuiltinSignature(builtin)
		if err != nil {
			t.Errorf("parse %q signature: %v", builtin.Name, err)
			continue
		}
		if signature.name != builtin.Name || len(signature.arguments) != builtin.Arity {
			t.Errorf("%q parsed signature = %q/%d arguments, registry says %q/%d", builtin.Name, signature.name, len(signature.arguments), builtin.Name, builtin.Arity)
			continue
		}

		bindings := make(map[string]string)
		argumentTypes := make([]string, len(signature.arguments))
		for index, argument := range signature.arguments {
			argumentTypes[index] = builtinFixtureType(argument.typePattern, bindings)
		}
		resultType := builtinFixtureType(signature.result, bindings)
		if err := validateBuiltinSignature(builtin, argumentTypes, resultType); err != nil {
			t.Errorf("validate representative signature for %q (%v -> %s): %v", builtin.Name, argumentTypes, resultType, err)
		}
	}

	if len(seen) != len(builtinList) {
		t.Fatalf("registry contains %d unique names for %d entries", len(seen), len(builtinList))
	}
	if len(builtinRegistry) != len(seen) {
		t.Fatalf("signature coverage has %d names, lookup registry has %d", len(seen), len(builtinRegistry))
	}
	for name := range builtinContextualRules {
		if !seen[name] {
			t.Errorf("contextual contract references unregistered builtin %q", name)
		}
	}
	for name := range builtinRegistry {
		if !seen[name] {
			t.Errorf("lookup registry name %q has no Builtin.Signature contract", name)
		}
	}
}

func builtinFixtureType(pattern *builtinTypePattern, bindings map[string]string) string {
	if len(pattern.alternatives) != 0 {
		return builtinFixtureType(pattern.alternatives[0], bindings)
	}
	if isBuiltinSignatureVariable(pattern.name) {
		if existing := bindings[pattern.name]; existing != "" {
			return existing
		}
		bindings[pattern.name] = "Int"
		return "Int"
	}
	if pattern.name == "Display" {
		return "String"
	}
	if len(pattern.parameters) == 0 {
		return pattern.name
	}
	parameters := make([]string, len(pattern.parameters))
	for index, parameter := range pattern.parameters {
		parameters[index] = builtinFixtureType(parameter, bindings)
	}
	return pattern.name + "[" + joinBuiltinFixtureTypes(parameters) + "]"
}

func joinBuiltinFixtureTypes(types []string) string {
	result := ""
	for index, typ := range types {
		if index != 0 {
			result += ", "
		}
		result += typ
	}
	return result
}

func TestBuiltinSignatureContractsMatchRegularAndGenericCalls(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		result    string
	}{
		{"array_push", []string{"Array[Int]", "Int"}, "Array[Int]"},
		{"array_push", []string{"Array[<unknown>]", "Int"}, "Array[Int]"},
		{"array_concat", []string{"Array[<unknown>]", "Array[Int]"}, "Array[Int]"},
		{"array_zip", []string{"Array[String]", "Array[String]"}, "Array[Array[String]]"},
		{"map_get", []string{"Map[String, Int]", "String"}, "Option[Int]"},
		{"map_insert", []string{"Map[String, Int]", "String", "Int"}, "Map[String, Int]"},
		{"some", []string{"Int"}, "Option[Int]"},
		{"none", nil, "Option[String]"},
		{"ok", []string{"Int"}, "Result[Int, String]"},
		{"err", []string{"String"}, "Result[Int, String]"},
		{"result_error", []string{"Result[Int, String]"}, "Option[String]"},
		{"unwrap_or", []string{"Option[Int]", "Int"}, "Int"},
		{"clamp", []string{"Float", "Float", "Float"}, "Float"},
		{"len", []string{"Map[String, Int]"}, "Int"},
		{"len", []string{"Set[Bool]"}, "Int"},
	}
	for _, test := range cases {
		t.Run(test.name+"/"+joinBuiltinFixtureTypes(test.arguments), func(t *testing.T) {
			builtin, ok := lookupBuiltin(test.name)
			if !ok {
				t.Fatalf("builtin %q is missing from the registry", test.name)
			}
			if err := validateBuiltinSignature(builtin, test.arguments, test.result); err != nil {
				t.Fatalf("valid signature rejected: %v", err)
			}
		})
	}
}

func TestBuiltinSignatureContractsModelUnsignedFamily(t *testing.T) {
	unsignedTypes := []string{"UInt8", "UInt16", "UInt32", "UInt64"}
	intBuiltin, ok := lookupBuiltin("int")
	if !ok {
		t.Fatal("int builtin is missing from the registry")
	}
	for _, unsigned := range unsignedTypes {
		t.Run("int/"+unsigned, func(t *testing.T) {
			if err := validateBuiltinSignature(intBuiltin, []string{unsigned}, "Int"); err != nil {
				t.Fatalf("int should accept %s: %v", unsigned, err)
			}
		})
	}

	for _, conversion := range []struct {
		name   string
		result string
	}{
		{"u8", "UInt8"}, {"u16", "UInt16"}, {"u32", "UInt32"}, {"u64", "UInt64"},
	} {
		builtin, found := lookupBuiltin(conversion.name)
		if !found {
			t.Fatalf("%s builtin is missing from the registry", conversion.name)
		}
		for _, unsigned := range unsignedTypes {
			t.Run(conversion.name+"/"+unsigned, func(t *testing.T) {
				if err := validateBuiltinSignature(builtin, []string{unsigned}, conversion.result); err != nil {
					t.Fatalf("%s should accept any unsigned width %s: %v", conversion.name, unsigned, err)
				}
			})
		}
	}
}

func TestBuiltinNumericSignatureContractsIncludeCheckerAcceptedUnsignedTypes(t *testing.T) {
	for _, unsigned := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
		for _, test := range []struct {
			name      string
			arguments []string
			result    string
		}{
			{"abs", []string{unsigned}, unsigned},
			{"sqrt", []string{unsigned}, "Float"},
			{"min", []string{unsigned, unsigned}, unsigned},
			{"max", []string{unsigned, unsigned}, unsigned},
			{"array_sort", []string{"Array[" + unsigned + "]"}, "Array[" + unsigned + "]"},
		} {
			t.Run(test.name+"/"+unsigned, func(t *testing.T) {
				builtin, ok := lookupBuiltin(test.name)
				if !ok {
					t.Fatalf("%s builtin is missing from the registry", test.name)
				}
				if err := validateBuiltinSignature(builtin, test.arguments, test.result); err != nil {
					t.Fatalf("%s should accept checked %s values: %v", test.name, unsigned, err)
				}
			})
		}
	}
}

func TestBuiltinNumericSignaturesAcceptOnlyCheckedNumericGenerics(t *testing.T) {
	for _, constraint := range []string{"Numeric", "Integer"} {
		constraints := map[string]string{"T": constraint}
		for _, test := range []struct {
			name      string
			arguments []string
			result    string
		}{
			{"abs", []string{"T"}, "T"},
			{"sqrt", []string{"T"}, "Float"},
			{"min", []string{"T", "T"}, "T"},
			{"max", []string{"T", "T"}, "T"},
		} {
			t.Run(test.name+"/T:"+constraint, func(t *testing.T) {
				builtin, ok := lookupBuiltin(test.name)
				if !ok {
					t.Fatalf("%s builtin is missing from the registry", test.name)
				}
				if err := validateBuiltinSignatureWithConstraints(builtin, test.arguments, test.result, constraints); err != nil {
					t.Fatalf("%s should accept T:%s per checkBuiltin: %v", test.name, constraint, err)
				}
			})
		}
	}
}

func TestBuiltinNumericSignaturesRejectUnconstrainedOrUnsupportedGenerics(t *testing.T) {
	cases := []struct {
		name        string
		arguments   []string
		result      string
		constraints map[string]string
	}{
		{"abs", []string{"T"}, "T", nil},
		{"abs", []string{"T"}, "T", map[string]string{"T": "Any"}},
		{"abs", []string{"T"}, "T", map[string]string{"T": "Comparable"}},
		{"int", []string{"T"}, "Int", map[string]string{"T": "Numeric"}},
		{"sign", []string{"T"}, "Int", map[string]string{"T": "Numeric"}},
		{"clamp", []string{"T", "T", "T"}, "T", map[string]string{"T": "Numeric"}},
		{"array_sort", []string{"Array[T]"}, "Array[T]", map[string]string{"T": "Numeric"}},
		{"abs", []string{"ArbitraryName"}, "ArbitraryName", nil},
	}
	for _, test := range cases {
		t.Run(test.name+"/"+joinBuiltinFixtureTypes(test.arguments), func(t *testing.T) {
			builtin, ok := lookupBuiltin(test.name)
			if !ok {
				t.Fatalf("%s builtin is missing from the registry", test.name)
			}
			if err := validateBuiltinSignatureWithConstraints(builtin, test.arguments, test.result, test.constraints); err == nil {
				t.Fatalf("accepted unsupported generic call %s(%v) -> %s with constraints %v", test.name, test.arguments, test.result, test.constraints)
			}
		})
	}
}

func TestBuiltinSignatureContractsRejectMismatchedArgumentsAndResults(t *testing.T) {
	cases := []struct {
		name      string
		label     string
		arguments []string
		result    string
	}{
		{"array_push", "element", []string{"Array[Int]", "String"}, "Array[Int]"},
		{"array_push", "result", []string{"Array[Int]", "Int"}, "Array[String]"},
		{"array_concat", "generic", []string{"Array[Int]", "Array[String]"}, "Array[Int]"},
		{"array_get", "index", []string{"Array[Int]", "String"}, "Option[Int]"},
		{"array_get", "result", []string{"Array[Int]", "Int"}, "Option[String]"},
		{"array_set", "replacement", []string{"Array[Int]", "Int", "String"}, "Result[Array[Int], String]"},
		{"map_get", "key", []string{"Map[String, Int]", "Int"}, "Option[Int]"},
		{"map_get", "result", []string{"Map[String, Int]", "String"}, "Option[String]"},
		{"map_insert", "value", []string{"Map[String, Int]", "String", "String"}, "Map[String, Int]"},
		{"some", "result", []string{"Int"}, "Option[String]"},
		{"ok", "result", []string{"Int"}, "Result[String, String]"},
		{"err", "result", []string{"String"}, "Result[Int, Int]"},
		{"unwrap_or", "fallback", []string{"Option[Int]", "String"}, "Int"},
		{"result_error", "result", []string{"Result[Int, String]"}, "Option[Int]"},
		{"abs", "result must track operand", []string{"Float"}, "Int"},
		{"min", "union operands must agree", []string{"Int", "Float"}, "Int"},
		{"int", "result is fixed", []string{"UInt32"}, "UInt32"},
		{"u8", "result width is fixed", []string{"UInt64"}, "UInt64"},
		{"u16", "rejects floating inputs", []string{"Float"}, "UInt16"},
		{"u32", "rejects booleans", []string{"Bool"}, "UInt32"},
		{"sign", "checker excludes unsigned inputs", []string{"UInt8"}, "Int"},
		{"clamp", "checker excludes unsigned inputs", []string{"UInt8", "UInt8", "UInt8"}, "UInt8"},
		{"array_sort", "checker excludes bool arrays", []string{"Array[Bool]"}, "Array[Bool]"},
		{"array_sort", "result tracks sorted element type", []string{"Array[Int]"}, "Array[UInt8]"},
		{"json_array_get", "index", []string{"Json", "String"}, "Result[Json, String]"},
	}
	for _, test := range cases {
		t.Run(test.name+"/"+test.label, func(t *testing.T) {
			builtin, ok := lookupBuiltin(test.name)
			if !ok {
				t.Fatalf("builtin %q is missing from the registry", test.name)
			}
			if err := validateBuiltinSignature(builtin, test.arguments, test.result); err == nil {
				t.Fatalf("accepted invalid signature %s(%v) -> %s", test.name, test.arguments, test.result)
			}
		})
	}
}

func TestBuiltinSignatureContractsDoNotConfuseUnknownArrayWithNominalTypes(t *testing.T) {
	some, ok := lookupBuiltin("some")
	if !ok {
		t.Fatal("some builtin is missing from the registry")
	}
	arrayPush, ok := lookupBuiltin("array_push")
	if !ok {
		t.Fatal("array_push builtin is missing from the registry")
	}
	if err := validateBuiltinSignature(arrayPush, []string{"Array[<unknown>]", "Int"}, "Array[Int]"); err != nil {
		t.Fatalf("untyped array marker should be refined by a concrete element: %v", err)
	}
	if err := validateBuiltinSignature(some, []string{"__KIR_UNKNOWN__"}, "Option[Int]"); err == nil {
		t.Fatal("nominal __KIR_UNKNOWN__ was treated as the source checker's untyped-array marker")
	}
}

func TestGoValidatorAppliesDeclaredBuiltinSignaturesOutsideCoreSubset(t *testing.T) {
	limits := DefaultLimits()
	encoded := emitValidatorCorpusKIR(t, limits, "builtin-signature.kry", "fn main() -> Float { return tan(0.5) }")
	var document KIRDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMIR(encoded, limits); err != nil {
		t.Fatalf("valid non-core builtin call rejected: %v", err)
	}
	call := findKIRFunction(&document, "main").Body[0].Return
	argument := call.Args[0]
	call.Args[0] = &KIRExpr{
		Kind: "string", Type: "String", String: "forged", Const: &KIRValue{Kind: "string", String: "forged"},
		Source: argument.Source, Line: argument.Line, Column: argument.Column, Span: argument.Span,
	}
	call.Const = nil
	forged, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMIR(forged, limits); err == nil {
		t.Fatal("non-core tan(String) call passed validated KIR admission")
	} else if !strings.Contains(err.Error(), `builtin "tan"`) || !strings.Contains(err.Error(), "checked argument or result types") {
		t.Fatalf("tan(String) was rejected for an unrelated invariant: %v", err)
	}
}

func TestBuiltinSignatureContractsExposeContextualRules(t *testing.T) {
	for _, name := range []string{"len", "none", "ok", "err", "thread_spawn", "thread_channel", "thread_channel_with_capacity", "thread_receive_timeout", "thread_send", "shared_new", "actor_send", "actor_channel", "actor_channel_with_capacity", "task_spawn", "poly_register", "poly_reorder", "array_contains", "array_index_of", "set_contains", "set_insert", "set_remove"} {
		if len(builtinSignatureContextualRules(name)) == 0 {
			t.Errorf("builtin %q has a contextual rule but no annotation", name)
		}
	}
	if rules := builtinSignatureContextualRules("array_push"); len(rules) != 0 {
		t.Errorf("array_push signature is fully type-relational; unexpected contextual rules: %v", rules)
	}
	if rules := builtinSignatureContextualRules("thread_spawn"); len(rules) == 0 {
		t.Fatal("thread_spawn literal worker resolution was not exposed")
	}
}
