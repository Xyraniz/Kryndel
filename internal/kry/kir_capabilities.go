package kry

import (
	"fmt"
	"strings"
)

// validateMIRNativeBuiltinSupport applies the native builtin inventory to
// builtin call targets in validated KIR without rebuilding a checked AST.
func validateMIRNativeBuiltinSupport(mir *ValidatedMIR, format string, target NativeTarget) error {
	if mir == nil || mir.arena == nil {
		return fmt.Errorf("missing validated MIR")
	}
	if err := mir.arena.validateReferences(); err != nil {
		return fmt.Errorf("invalid validated MIR arena: %w", err)
	}
	for _, expression := range mir.arena.Expressions {
		if expression.Value.Kind == "call" && strings.HasPrefix(expression.Value.CallTarget, "builtin:") {
			name := strings.TrimPrefix(expression.Value.CallTarget, "builtin:")
			if nativeBuiltinBackendStatus(name, format, target) == "unsupported" {
				return fmt.Errorf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", name, format, target.OS, target.Arch)
			}
		}
	}
	return nil
}

// validateMIRFunctionValueSupport rejects function values and closures using
// KIR's encoded types, call targets, lambda nodes, and capture metadata.
func validateMIRFunctionValueSupport(mir *ValidatedMIR, format string) error {
	if mir == nil || mir.arena == nil {
		return fmt.Errorf("missing MIR")
	}
	if err := mir.arena.validateReferences(); err != nil {
		return fmt.Errorf("invalid validated MIR arena: %w", err)
	}
	if kirArenaUsesFunctionValue(mir.arena) {
		return fmt.Errorf("%s backend does not support function values or closures; use the interpreter", format)
	}
	return nil
}

func kirArenaUsesFunctionValue(arena *KIRArena) bool {
	if arena == nil {
		return false
	}
	for _, structure := range arena.Structs {
		if structure == nil {
			continue
		}
		for _, field := range structure.Fields {
			if field != nil && kirTypeContainsFunction(field.Type) {
				return true
			}
		}
	}
	for _, trait := range arena.Traits {
		if trait == nil {
			continue
		}
		for _, method := range trait.Methods {
			if method == nil {
				continue
			}
			if kirTypeContainsFunction(method.Return) {
				return true
			}
			for _, parameter := range method.Params {
				if parameter != nil && kirTypeContainsFunction(parameter.Type) {
					return true
				}
			}
		}
	}
	for _, expression := range arena.Expressions {
		value := expression.Value
		if value.Kind == "lambda" || value.Kind == "var" && strings.HasPrefix(value.CallTarget, "function:") || value.Kind == "call" && expression.Callee.Present || kirTypeContainsFunction(value.Type) || kirTypeContainsFunction(value.StructType) || kirTypeContainsFunction(bindingType(value.Binding)) {
			return true
		}
		for _, typeName := range value.GenericArguments {
			if kirTypeContainsFunction(typeName) {
				return true
			}
		}
	}
	for _, statement := range arena.Statements {
		if kirTypeContainsFunction(statement.Value.Annotation) || kirTypeContainsFunction(bindingType(statement.Value.Binding)) {
			return true
		}
	}
	for _, pattern := range arena.Patterns {
		if kirTypeContainsFunction(pattern.Value.Type) {
			return true
		}
	}
	for _, function := range arena.Functions {
		if kirTypeContainsFunction(function.Value.Return) || kirTypeContainsFunction(function.Value.Receiver) {
			return true
		}
		if function.Captures.Count != 0 {
			return true
		}
		parameters, _ := arena.indexList(arena.ParameterRefs, function.Params)
		for _, index := range parameters {
			parameter := arena.Parameters[index]
			if kirTypeContainsFunction(parameter.Value.Type) {
				return true
			}
		}
	}
	return false
}

func bindingType(binding *KIRBinding) string {
	if binding == nil {
		return ""
	}
	return binding.Type
}

func kirExpressionCapabilityName(kind string) string {
	return map[string]string{
		"int": "ExInt", "float": "ExFloat", "bool": "ExBool", "nil": "ExNil", "string": "ExString", "var": "ExVar", "unary": "ExUnary", "binary": "ExBinary", "call": "ExCall", "array": "ExArray", "index": "ExIndex", "field": "ExField", "struct": "ExStruct", "enum": "ExEnum", "map": "ExMap", "set": "ExSet", "propagate": "ExPropagate", "lambda": "ExLambda",
	}[kind]
}

func kirStatementCapabilityName(kind string) string {
	return map[string]string{
		"let": "StLet", "expr": "StExpr", "assign": "StAssign", "if": "StIf", "while": "StWhile", "return": "StReturn", "break": "StBreak", "continue": "StContinue", "match": "StMatch", "for": "StFor", "defer": "StDefer", "unsafe": "StUnsafe", "const": "StConst",
	}[kind]
}

func kirPatternCapabilityName(kind string) string {
	return map[string]string{
		"wildcard": "PatWildcard", "nil": "PatNil", "bool": "PatBool", "int": "PatInt", "string": "PatString", "enum": "PatEnum", "option": "PatOption", "result": "PatResult",
	}[kind]
}

func kirOperatorCapabilityName(operator string) string {
	return map[string]string{
		"+": "PLUS", "-": "MINUS", "*": "STAR", "/": "SLASH", "%": "PERCENT", "!": "BANG", "~": "BITNOT", "==": "EQEQ", "!=": "NEQ", "<": "LESS", "<=": "LEQ", ">": "GREATER", ">=": "GEQ", "&&": "AND", "||": "OR", "&": "BITAND", "|": "PIPE", "^": "BITXOR", "<<": "SHL", ">>": "SHR",
	}[operator]
}

func kirTypeCapabilityName(encoded string, generics map[string]struct{}) (feature string, skip bool) {
	if encoded == "" {
		return "", true
	}
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok || spec == nil {
		return "", false
	}
	if spec.Function {
		return "TyFunction", false
	}
	if _, generic := generics[spec.Name]; generic {
		return "TyGeneric", false
	}
	switch spec.Name {
	case "Void":
		return "", true
	case "Nil":
		return "", true
	case "Int":
		return "TyInt", false
	case "UInt8", "UInt16", "UInt32", "UInt64":
		return "TyUInt", false
	case "Float":
		return "TyFloat", false
	case "Bool":
		return "TyBool", false
	case "String":
		return "TyString", false
	case "Bytes":
		return "TyBytes", false
	case "Array":
		return "TyArray", false
	case "Option":
		return "TyOption", false
	case "Result":
		return "TyResult", false
	case "Channel":
		return "TyChannel", false
	case "Thread":
		return "TyThread", false
	case "Map":
		return "TyMap", false
	case "Set":
		return "TySet", false
	case "Json":
		return "TyJSON", false
	case "WebSocket":
		return "TyWebSocket", false
	case "Actor":
		return "TyActor", false
	case "Shared":
		return "TyShared", false
	case "TaskGroup":
		return "TyTaskGroup", false
	case "Regex":
		return "TyRegex", false
	case "Random":
		return "TyRandom", false
	case "SQLite":
		return "TySQLite", false
	case "TcpSocket":
		return "TyTCPSocket", false
	case "TcpListener":
		return "TyTCPListener", false
	case "UdpSocket":
		return "TyUDPSocket", false
	case "FFILibrary":
		return "TyFFILibrary", false
	case "FFISymbol":
		return "TyFFISymbol", false
	case "FFIBuffer":
		return "TyFFIBuffer", false
	}
	return "", false
}

func kirTypeParameterNames(parameters []*KIRTypeParam) map[string]struct{} {
	names := make(map[string]struct{}, len(parameters))
	for _, parameter := range parameters {
		if parameter != nil && parameter.Name != "" {
			names[parameter.Name] = struct{}{}
		}
	}
	return names
}

func kirFunctionTypeParameters(function *KIRFunction) map[string]struct{} {
	if function == nil {
		return map[string]struct{}{}
	}
	return kirTypeParameterNames(function.TypeParams)
}
