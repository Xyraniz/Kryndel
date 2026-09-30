package kry

import (
	"fmt"
	"strings"
)

// validateMIRNativeBuiltinSupport applies the native builtin inventory to
// builtin call targets in validated KIR without rebuilding a checked AST.
func validateMIRNativeBuiltinSupport(mir *ValidatedMIR, format string, target NativeTarget) error {
	if mir == nil || mir.document == nil {
		return fmt.Errorf("missing validated MIR")
	}

	var walkExpr func(*KIRExpr) error
	var walkStmts func([]*KIRStmt) error
	var walkFunction func(*KIRFunction) error
	walkExpr = func(expression *KIRExpr) error {
		if expression == nil {
			return nil
		}
		if expression.Kind == "call" && strings.HasPrefix(expression.CallTarget, "builtin:") {
			name := strings.TrimPrefix(expression.CallTarget, "builtin:")
			if nativeBuiltinBackendStatus(name, format, target) == "unsupported" {
				return fmt.Errorf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", name, format, target.OS, target.Arch)
			}
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := walkExpr(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if err := walkExpr(child); err != nil {
					return err
				}
			}
		}
		return walkFunction(expression.Lambda)
	}
	walkStmts = func(statements []*KIRStmt) error {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
				if err := walkExpr(expression); err != nil {
					return err
				}
			}
			for _, block := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
				if err := walkStmts(block); err != nil {
					return err
				}
			}
			for _, arm := range statement.Arms {
				if arm != nil {
					if err := walkStmts(arm.Body); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	walkFunction = func(function *KIRFunction) error {
		if function == nil {
			return nil
		}
		for _, parameter := range function.Params {
			if parameter != nil {
				if err := walkExpr(parameter.Default); err != nil {
					return err
				}
			}
		}
		return walkStmts(function.Body)
	}

	for _, statement := range mir.document.Statements {
		if err := walkStmts([]*KIRStmt{statement}); err != nil {
			return err
		}
	}
	for _, function := range mir.document.Functions {
		if err := walkFunction(function); err != nil {
			return err
		}
	}
	return nil
}

// validateMIRNativeFeatureSupport checks the language constructs represented
// by KIR v5 against the existing backend capability inventories.
func validateMIRNativeFeatureSupport(mir *ValidatedMIR, format string, target NativeTarget) error {
	if mir == nil || mir.document == nil {
		return fmt.Errorf("missing validated MIR")
	}
	// Report unsupported builtins before their opaque handle types, matching
	// the source-AST preflight's diagnostic priority.
	if err := validateMIRNativeBuiltinSupport(mir, format, target); err != nil {
		return err
	}

	document := mir.document
	var walkExpr func(*KIRExpr, map[string]struct{}, bool) error
	var walkStmts func([]*KIRStmt, map[string]struct{}) error
	var walkFunction func(*KIRFunction) error
	walkExpr = func(expression *KIRExpr, generics map[string]struct{}, allowDirectOutput bool) error {
		if expression == nil {
			return nil
		}
		if name := kirExpressionCapabilityName(expression.Kind); name == "" {
			return fmt.Errorf("expression kind %q is not listed as supported by the %s backend for %s-%s", expression.Kind, format, target.OS, target.Arch)
		} else if err := validateLanguageItem("expression", name, format, target); err != nil {
			return err
		}
		if expression.Kind == "unary" || expression.Kind == "binary" {
			category := "binary_operator"
			if expression.Kind == "unary" {
				category = "unary_operator"
			}
			if operator := kirOperatorCapabilityName(expression.Operator); operator == "" {
				return fmt.Errorf("operator %q is not listed as supported by the %s backend for %s-%s", expression.Operator, format, target.OS, target.Arch)
			} else if err := validateLanguageItem(category, operator, format, target); err != nil {
				return err
			}
		}
		if feature, skip := kirTypeCapabilityName(expression.Type, generics, document); !skip {
			if feature == "" {
				return fmt.Errorf("type kind %q is not listed as supported by the %s backend for %s-%s", expression.Type, format, target.OS, target.Arch)
			}
			if err := validateLanguageItem("type", feature, format, target); err != nil {
				return err
			}
		}
		if expression.Kind == "call" && strings.HasPrefix(expression.CallTarget, "builtin:") {
			name := strings.TrimPrefix(expression.CallTarget, "builtin:")
			status := nativeBuiltinBackendStatus(name, format, target)
			if format == "elf-direct" && (name == "print" || name == "println") && (!allowDirectOutput || len(expression.Args) != 1) {
				status = "unsupported"
			}
			if status == "unsupported" {
				return fmt.Errorf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", name, format, target.OS, target.Arch)
			}
		}

		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := walkExpr(child, generics, false); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if err := walkExpr(child, generics, false); err != nil {
					return err
				}
			}
		}
		return walkFunction(expression.Lambda)
	}
	walkStmts = func(statements []*KIRStmt, generics map[string]struct{}) error {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			if name := kirStatementCapabilityName(statement.Kind); name == "" {
				return fmt.Errorf("statement kind %q is not listed as supported by the %s backend for %s-%s", statement.Kind, format, target.OS, target.Arch)
			} else if err := validateLanguageItem("statement", name, format, target); err != nil {
				return err
			}
			for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
				if statement.Kind == "return" && expression == statement.Return && expression != nil && expression.Kind == "nil" {
					// A source nil return is lowered as a no-value return.
					continue
				}
				allowOutput := format == "elf-direct" && statement.Kind == "expr" && expression == statement.Expr
				if err := walkExpr(expression, generics, allowOutput); err != nil {
					return err
				}
			}
			for _, block := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
				if err := walkStmts(block, generics); err != nil {
					return err
				}
			}
			for _, arm := range statement.Arms {
				if arm == nil {
					continue
				}
				if arm.Pattern == nil {
					return fmt.Errorf("pattern kind %q is not listed as supported by the %s backend for %s-%s", "<nil>", format, target.OS, target.Arch)
				}
				if name := kirPatternCapabilityName(arm.Pattern.Kind); name == "" {
					return fmt.Errorf("pattern kind %q is not listed as supported by the %s backend for %s-%s", arm.Pattern.Kind, format, target.OS, target.Arch)
				} else if err := validateLanguageItem("pattern", name, format, target); err != nil {
					return err
				}
				if err := walkStmts(arm.Body, generics); err != nil {
					return err
				}
			}
		}
		return nil
	}
	walkFunction = func(function *KIRFunction) error {
		if function == nil {
			return nil
		}
		generics := kirFunctionTypeParameters(function)
		for _, parameter := range function.Params {
			if parameter == nil {
				continue
			}
			if feature, skip := kirTypeCapabilityName(parameter.Type, generics, document); !skip {
				if feature == "" {
					return fmt.Errorf("type kind %q is not listed as supported by the %s backend for %s-%s", parameter.Type, format, target.OS, target.Arch)
				}
				if err := validateLanguageItem("type", feature, format, target); err != nil {
					return err
				}
			}
			if err := walkExpr(parameter.Default, generics, false); err != nil {
				return err
			}
		}
		if feature, skip := kirTypeCapabilityName(function.Return, generics, document); !skip {
			if feature == "" {
				return fmt.Errorf("type kind %q is not listed as supported by the %s backend for %s-%s", function.Return, format, target.OS, target.Arch)
			}
			if err := validateLanguageItem("type", feature, format, target); err != nil {
				return err
			}
		}
		return walkStmts(function.Body, generics)
	}

	for _, statement := range document.Statements {
		if err := walkStmts([]*KIRStmt{statement}, map[string]struct{}{}); err != nil {
			return err
		}
	}
	for _, structure := range document.Structs {
		if structure == nil {
			continue
		}
		generics := kirTypeParameterNames(structure.TypeParams)
		for _, field := range structure.Fields {
			if field == nil {
				continue
			}
			if feature, skip := kirTypeCapabilityName(field.Type, generics, document); !skip {
				if feature == "" {
					return fmt.Errorf("type kind %q is not listed as supported by the %s backend for %s-%s", field.Type, format, target.OS, target.Arch)
				}
				if err := validateLanguageItem("type", feature, format, target); err != nil {
					return err
				}
			}
		}
	}
	for _, function := range document.Functions {
		if err := walkFunction(function); err != nil {
			return err
		}
	}
	return nil
}

// validateMIRFunctionValueSupport rejects function values and closures using
// KIR's encoded types, call targets, lambda nodes, and capture metadata.
func validateMIRFunctionValueSupport(mir *ValidatedMIR, format string) error {
	if mir == nil || mir.document == nil {
		return fmt.Errorf("missing MIR")
	}
	if kirDocumentUsesFunctionValue(mir.document) {
		return fmt.Errorf("%s backend does not support function values or closures; use the interpreter", format)
	}
	return nil
}

func kirDocumentUsesFunctionValue(document *KIRDocument) bool {
	if document == nil {
		return false
	}
	for _, structure := range document.Structs {
		if structure == nil {
			continue
		}
		for _, field := range structure.Fields {
			if field != nil && kirTypeContainsFunction(field.Type) {
				return true
			}
		}
	}
	for _, trait := range document.Traits {
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
	for _, statement := range document.Statements {
		if kirStmtUsesFunctionValue(statement) {
			return true
		}
	}
	for _, function := range document.Functions {
		if kirFunctionUsesFunctionValue(function) {
			return true
		}
	}
	return false
}

func kirFunctionUsesFunctionValue(function *KIRFunction) bool {
	if function == nil {
		return false
	}
	if len(function.Captures) != 0 || kirTypeContainsFunction(function.Return) || kirTypeContainsFunction(function.Receiver) {
		return true
	}
	for _, parameter := range function.Params {
		if parameter == nil {
			continue
		}
		if kirTypeContainsFunction(parameter.Type) || kirExprUsesFunctionValue(parameter.Default) {
			return true
		}
	}
	for _, statement := range function.Body {
		if kirStmtUsesFunctionValue(statement) {
			return true
		}
	}
	return false
}

func kirStmtUsesFunctionValue(statement *KIRStmt) bool {
	if statement == nil {
		return false
	}
	if kirTypeContainsFunction(statement.Annotation) || kirTypeContainsFunction(bindingType(statement.Binding)) {
		return true
	}
	for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
		if kirExprUsesFunctionValue(expression) {
			return true
		}
	}
	for _, block := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
		for _, child := range block {
			if kirStmtUsesFunctionValue(child) {
				return true
			}
		}
	}
	for _, arm := range statement.Arms {
		if arm != nil {
			if arm.Pattern != nil && kirTypeContainsFunction(arm.Pattern.Type) {
				return true
			}
			for _, child := range arm.Body {
				if kirStmtUsesFunctionValue(child) {
					return true
				}
			}
		}
	}
	return false
}

func kirExprUsesFunctionValue(expression *KIRExpr) bool {
	if expression == nil {
		return false
	}
	if expression.Kind == "lambda" || expression.Kind == "var" && strings.HasPrefix(expression.CallTarget, "function:") || expression.Kind == "call" && expression.Callee != nil || kirTypeContainsFunction(expression.Type) || kirTypeContainsFunction(expression.StructType) || kirTypeContainsFunction(bindingType(expression.Binding)) {
		return true
	}
	for _, typeName := range expression.GenericArguments {
		if kirTypeContainsFunction(typeName) {
			return true
		}
	}
	for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
		if kirExprUsesFunctionValue(child) {
			return true
		}
	}
	for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
		for _, child := range list {
			if kirExprUsesFunctionValue(child) {
				return true
			}
		}
	}
	return kirFunctionUsesFunctionValue(expression.Lambda)
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

func kirTypeCapabilityName(encoded string, generics map[string]struct{}, document *KIRDocument) (feature string, skip bool) {
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
	if document != nil {
		for _, structure := range document.Structs {
			if structure != nil && structure.Name == spec.Name {
				return "TyStruct", false
			}
		}
		for _, enum := range document.Enums {
			if enum != nil && enum.Name == spec.Name {
				return "TyEnum", false
			}
		}
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
