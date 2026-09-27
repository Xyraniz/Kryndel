package kry

import (
	"fmt"
	"math"
	"strings"
)

// validateKIRDocument enforces invariants that a backend may otherwise rely
// on accidentally. DecodeKIR calls it for both the current schema and the
// explicitly supported legacy version.
func validateKIRDocument(document *KIRDocument, limits Limits) error {
	if document == nil {
		return fmt.Errorf("missing document")
	}
	if !validKIRTarget(document.Target) {
		return fmt.Errorf("unsupported target %s-%s", document.Target.OS, document.Target.Arch)
	}
	if err := checkKIRCount("imports", len(document.Imports), limits.MaxImports); err != nil {
		return err
	}
	if err := checkKIRCount("sources", len(document.Sources), limits.MaxImports+1); err != nil {
		return err
	}
	if err := checkKIRCount("structs", len(document.Structs), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("enums", len(document.Enums), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("functions", len(document.Functions), limits.MaxASTNodes); err != nil {
		return err
	}
	if err := checkKIRCount("top-level statements", len(document.Statements), limits.MaxASTNodes); err != nil {
		return err
	}
	structs := make(map[string]*KIRStruct, len(document.Structs))
	enums := make(map[string]*KIREnum, len(document.Enums))
	functionCounts := make(map[string]int, len(document.Functions))
	functionTargets := make(map[string]string, len(document.Functions))
	for _, decl := range document.Structs {
		if decl == nil || decl.Name == "" {
			return fmt.Errorf("struct declaration has no name")
		}
		if _, duplicate := structs[decl.Name]; duplicate {
			return fmt.Errorf("duplicate struct declaration %q", decl.Name)
		}
		if err := checkKIRCount("struct fields", len(decl.Fields), limits.MaxArrayElements); err != nil {
			return err
		}
		fields := map[string]bool{}
		for _, field := range decl.Fields {
			if field == nil || field.Name == "" || field.Type == "" {
				return fmt.Errorf("struct %q has an incomplete field", decl.Name)
			}
			if fields[field.Name] {
				return fmt.Errorf("struct %q has duplicate field %q", decl.Name, field.Name)
			}
			fields[field.Name] = true
		}
		structs[decl.Name] = decl
	}
	for _, decl := range document.Enums {
		if decl == nil || decl.Name == "" {
			return fmt.Errorf("enum declaration has no name")
		}
		if _, duplicate := enums[decl.Name]; duplicate {
			return fmt.Errorf("duplicate enum declaration %q", decl.Name)
		}
		if len(decl.Variants) == 0 {
			return fmt.Errorf("enum %q has no variants", decl.Name)
		}
		if err := checkKIRCount("enum variants", len(decl.Variants), limits.MaxArrayElements); err != nil {
			return err
		}
		variants := map[string]bool{}
		for _, variant := range decl.Variants {
			if variant == "" || variants[variant] {
				return fmt.Errorf("enum %q has an empty or duplicate variant", decl.Name)
			}
			variants[variant] = true
		}
		enums[decl.Name] = decl
	}
	for _, function := range document.Functions {
		if function == nil || function.Name == "" || function.Return == "" {
			return fmt.Errorf("function declaration is incomplete")
		}
		if len(function.Captures) != 0 {
			return fmt.Errorf("top-level function declarations cannot have closure captures")
		}
		if document.Version < 3 && len(function.Captures) != 0 {
			return fmt.Errorf("function captures require KIR version 3")
		}
		functionCounts[function.Name]++
		if err := checkKIRCount("function parameters", len(function.Params), limits.MaxArrayElements); err != nil {
			return err
		}
		if err := checkKIRCount("function type parameters", len(function.TypeParams), limits.MaxArrayElements); err != nil {
			return err
		}
		seenTypeParams := map[string]bool{}
		for _, parameter := range function.TypeParams {
			if parameter == nil || parameter.Name == "" || seenTypeParams[parameter.Name] {
				return fmt.Errorf("function %q has an empty or duplicate type parameter", function.Name)
			}
			seenTypeParams[parameter.Name] = true
		}
		for _, parameter := range function.Params {
			if parameter == nil || parameter.Name == "" || parameter.Type == "" {
				return fmt.Errorf("function %q has an incomplete parameter", function.Name)
			}
			if document.Version < 3 && parameter.Binding != nil {
				return fmt.Errorf("resolved parameter bindings require KIR version 3")
			}
			if document.Version >= 3 && (!validKIRBinding(parameter.Binding) || parameter.Binding.Name != parameter.Name || parameter.Binding.Type != parameter.Type || parameter.Binding.Mutable) {
				return fmt.Errorf("function %q has an invalid resolved parameter binding", function.Name)
			}
		}
		if err := checkKIRCount("function body statements", len(function.Body), limits.MaxArrayElements); err != nil {
			return err
		}
	}
	for _, function := range document.Functions {
		target := function.Name
		if functionCounts[function.Name] > 1 {
			target = kirFunctionTargetFromDocument(function)
		}
		if _, duplicate := functionTargets[target]; duplicate {
			return fmt.Errorf("duplicate function target %q", target)
		}
		functionTargets[target] = function.Name
	}
	// Declarations count toward the same document-wide node budget as the
	// recursive expression, statement, pattern, and constant trees below.
	nodeCount := len(document.Structs) + len(document.Enums) + len(document.Functions) + len(document.Statements)
	if limits.MaxASTNodes > 0 && nodeCount > limits.MaxASTNodes {
		return fmt.Errorf("node count exceeds configured limit")
	}
	if limits.MaxInstructions > 0 && uint64(nodeCount) > limits.MaxInstructions {
		return fmt.Errorf("node count exceeds configured instruction limit")
	}
	addNode := func(kind string, depth int) error {
		nodeCount++
		if limits.MaxASTNodes > 0 && nodeCount > limits.MaxASTNodes {
			return fmt.Errorf("node count exceeds configured limit")
		}
		if limits.MaxInstructions > 0 && uint64(nodeCount) > limits.MaxInstructions {
			return fmt.Errorf("node count exceeds configured instruction limit")
		}
		if limits.MaxNesting > 0 && depth > limits.MaxNesting {
			return fmt.Errorf("tree depth exceeds configured nesting limit")
		}
		return nil
	}
	var validateExpr func(*KIRExpr, int) error
	var validateStmt func(*KIRStmt, int) error
	var validatePattern func(*KIRPattern, int) error
	var validateValue func(*KIRValue, int) error
	validateValue = func(value *KIRValue, depth int) error {
		if value == nil {
			return nil
		}
		if err := addNode("constant", depth); err != nil {
			return err
		}
		switch value.Kind {
		case "nil", "int", "uint", "float", "bool", "string", "bytes", "array", "struct", "enum", "option", "result", "channel", "thread", "map", "set", "json", "websocket", "actor", "shared", "task_group", "regex", "random", "sqlite", "tcp", "tcp_listener", "udp", "ffi_library", "ffi_symbol", "ffi_buffer", "tail_call":
		default:
			return fmt.Errorf("unknown constant value kind %q", value.Kind)
		}
		if value.Kind == "float" && (math.IsNaN(value.Float) || math.IsInf(value.Float, 0)) {
			return fmt.Errorf("non-finite Float constant")
		}
		if value.Kind == "uint" && value.UIntBits != 8 && value.UIntBits != 16 && value.UIntBits != 32 && value.UIntBits != 64 {
			return fmt.Errorf("UInt constant has invalid bit width %d", value.UIntBits)
		}
		if err := checkKIRCount("constant array elements", len(value.Array), limits.MaxArrayElements); err != nil {
			return err
		}
		for _, item := range value.Array {
			if item == nil {
				return fmt.Errorf("constant array contains a missing value")
			}
			if err := validateValue(item, depth+1); err != nil {
				return err
			}
		}
		return validateValue(value.Inner, depth+1)
	}
	validateExpr = func(expression *KIRExpr, depth int) error {
		if expression == nil {
			return nil
		}
		if err := addNode("expression", depth); err != nil {
			return err
		}
		if expression.Type == "" {
			return fmt.Errorf("%s expression %q at %s:%d has no checked type", expression.Kind, expression.Name, expression.Source, expression.Line)
		}
		if len(expression.Type) > limits.MaxSourceBytes && limits.MaxSourceBytes > 0 {
			return fmt.Errorf("expression type exceeds configured string limit")
		}
		if document.Version < 3 && (expression.Binding != nil || expression.Callee != nil || expression.Lambda != nil) {
			return fmt.Errorf("function values and resolved expression bindings require KIR version 3")
		}
		if expression.Kind != "var" && expression.Binding != nil {
			return fmt.Errorf("non-variable expression has a resolved binding")
		}
		if strings.HasPrefix(expression.Type, "fn(") {
			if _, _, ok := parseKIRFunctionType(expression.Type); !ok {
				return fmt.Errorf("expression has malformed function type %q", expression.Type)
			}
		}
		require := func(child *KIRExpr, field string) error {
			if child == nil {
				return fmt.Errorf("%s expression is missing %s", expression.Kind, field)
			}
			return nil
		}
		switch expression.Kind {
		case "int", "float", "bool", "nil", "string":
		case "var":
			if expression.Name == "" {
				return fmt.Errorf("variable expression has no name")
			}
			if expression.CallTarget != "" {
				if expression.Binding != nil {
					return fmt.Errorf("function reference cannot also name a local binding")
				}
				if document.Version < 3 || !validKIRCallTarget(expression.CallTarget) || !strings.HasPrefix(expression.Type, "fn(") {
					return fmt.Errorf("function value has an invalid target or function type")
				}
				prefix, target, _ := strings.Cut(expression.CallTarget, ":")
				resolvedName, ok := functionTargets[target]
				if prefix != "function" || !ok || resolvedName != expression.Name {
					return fmt.Errorf("function value references undeclared function or unresolved overload %q", target)
				}
			} else if document.Version >= 3 {
				if !validKIRBinding(expression.Binding) || expression.Binding.Name != expression.Name || expression.Binding.Type != expression.Type {
					return fmt.Errorf("variable %q has an invalid resolved binding", expression.Name)
				}
			}
		case "unary":
			if !isKIRUnaryOperator(expression.Operator) {
				return fmt.Errorf("unary expression has invalid operator %q", expression.Operator)
			}
			if err := require(expression.Operand, "operand"); err != nil {
				return err
			}
		case "binary":
			if !isKIRBinaryOperator(expression.Operator) {
				return fmt.Errorf("binary expression has invalid operator %q", expression.Operator)
			}
			if err := require(expression.Left, "left operand"); err != nil {
				return err
			}
			if err := require(expression.Right, "right operand"); err != nil {
				return err
			}
		case "call":
			if expression.Callee != nil {
				if document.Version < 3 || expression.Name != "" || expression.CallTarget != "" || expression.BuiltinID != "" {
					return fmt.Errorf("indirect call has an invalid name or target")
				}
				params, result, ok := parseKIRFunctionType(expression.Callee.Type)
				if !ok {
					return fmt.Errorf("indirect call callee does not have a function type")
				}
				if len(params) != len(expression.Args) {
					return fmt.Errorf("indirect call has %d arguments for a %d-parameter function", len(expression.Args), len(params))
				}
				if expression.Type != result {
					return fmt.Errorf("indirect call result type does not match its function type")
				}
				for i, arg := range expression.Args {
					if arg == nil || arg.Type != params[i] {
						return fmt.Errorf("indirect call argument %d has a mismatched type", i+1)
					}
				}
			} else {
				if expression.Name == "" || !validKIRCallTarget(expression.CallTarget) {
					return fmt.Errorf("call expression has an invalid name or target")
				}
				prefix, name, _ := strings.Cut(expression.CallTarget, ":")
				if prefix == "builtin" {
					if name != expression.Name {
						return fmt.Errorf("call expression name does not match its target")
					}
					builtin, ok := Builtins()[name]
					if !ok {
						return fmt.Errorf("call references unknown builtin %q", name)
					}
					if expression.BuiltinID != "" && expression.BuiltinID != builtin.ID {
						return fmt.Errorf("call to builtin %q has a mismatched builtin id", name)
					}
				} else {
					resolvedName, ok := functionTargets[name]
					if !ok {
						return fmt.Errorf("call references undeclared function or unresolved overload %q", name)
					}
					if resolvedName != expression.Name {
						return fmt.Errorf("call expression name does not match its target")
					}
				}
			}
		case "lambda":
			if document.Version < 3 || expression.Lambda == nil || expression.Callee != nil {
				return fmt.Errorf("lambda expression is missing its versioned function body")
			}
			lambda := expression.Lambda
			if lambda.Name == "" || lambda.Return == "" || len(lambda.TypeParams) != 0 || lambda.Receiver != "" || lambda.Public || lambda.Worker {
				return fmt.Errorf("lambda function metadata is invalid")
			}
			if err := checkKIRCount("lambda parameters", len(lambda.Params), limits.MaxArrayElements); err != nil {
				return err
			}
			parts := make([]string, len(lambda.Params))
			parameterNames := map[string]bool{}
			for i, parameter := range lambda.Params {
				if parameter == nil || parameter.Name == "" || parameter.Type == "" || parameter.Default != nil || parameterNames[parameter.Name] {
					return fmt.Errorf("lambda has an invalid or duplicate parameter")
				}
				if !validKIRBinding(parameter.Binding) || parameter.Binding.Name != parameter.Name || parameter.Binding.Type != parameter.Type || parameter.Binding.Mutable {
					return fmt.Errorf("lambda has an invalid resolved parameter binding")
				}
				parameterNames[parameter.Name] = true
				parts[i] = parameter.Type
			}
			if expression.Type != "fn("+strings.Join(parts, ", ")+") -> "+lambda.Return {
				return fmt.Errorf("lambda expression type does not match its signature")
			}
			if _, _, ok := parseKIRFunctionType(expression.Type); !ok {
				return fmt.Errorf("lambda has a malformed function type")
			}
			if err := checkKIRCount("lambda captures", len(lambda.Captures), limits.MaxArrayElements); err != nil {
				return err
			}
			captures := map[string]bool{}
			for _, capture := range lambda.Captures {
				if err := addNode("capture", depth+1); err != nil {
					return err
				}
				if capture == nil || !validKIRBinding(capture.Binding) {
					return fmt.Errorf("lambda has an incomplete capture")
				}
				identity := kirBindingIdentity(capture.Binding)
				if captures[identity] {
					return fmt.Errorf("lambda has a duplicate capture %q", capture.Binding.Name)
				}
				captures[identity] = true
			}
			if err := checkKIRCount("lambda body statements", len(lambda.Body), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, statement := range lambda.Body {
				if err := validateStmt(statement, depth+1); err != nil {
					return fmt.Errorf("lambda body: %w", err)
				}
			}
			if err := validateKIRLambdaCaptures(lambda); err != nil {
				return err
			}
		case "array", "set":
		case "index":
			if err := require(expression.Base, "base"); err != nil {
				return err
			}
			if err := require(expression.Left, "index"); err != nil {
				return err
			}
		case "field":
			if err := require(expression.Base, "base"); err != nil {
				return err
			}
			if expression.Field == "" {
				return fmt.Errorf("field expression has no field name")
			}
		case "struct":
			decl := structs[expression.StructName]
			if expression.StructName == "" || expression.Type != expression.StructName || decl == nil || len(expression.Fields) != len(expression.Values) || len(expression.Fields) != len(decl.Fields) {
				return fmt.Errorf("struct expression has an unknown type or mismatched fields and values")
			}
			declaredFields := make(map[string]bool, len(decl.Fields))
			for _, field := range decl.Fields {
				declaredFields[field.Name] = true
			}
			seenFields := make(map[string]bool, len(expression.Fields))
			for _, field := range expression.Fields {
				if !declaredFields[field] || seenFields[field] {
					return fmt.Errorf("struct expression references an unknown or duplicate field %q", field)
				}
				seenFields[field] = true
			}
		case "enum":
			decl := enums[expression.EnumType]
			if expression.Type != expression.EnumType || decl == nil || expression.EnumVariant == "" || !containsKIRString(decl.Variants, expression.EnumVariant) {
				return fmt.Errorf("enum expression references an unknown type or variant")
			}
		case "map":
			if len(expression.MapKeys) != len(expression.Values) {
				return fmt.Errorf("map expression has mismatched keys and values")
			}
		case "propagate":
			if err := require(expression.Operand, "operand"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown expression kind %q", expression.Kind)
		}
		if err := checkKIRCount("expression list", len(expression.Args), limits.MaxArrayElements); err != nil {
			return err
		}
		if err := checkKIRCount("struct expression fields", len(expression.Fields), limits.MaxArrayElements); err != nil {
			return err
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if err := validateExpr(child, depth+1); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			if err := checkKIRCount("expression list", len(list), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, child := range list {
				if child == nil {
					return fmt.Errorf("expression list contains a missing node")
				}
				if err := validateExpr(child, depth+1); err != nil {
					return err
				}
			}
		}
		if err := validateValue(expression.Const, depth+1); err != nil {
			return err
		}
		return nil
	}
	validatePattern = func(pattern *KIRPattern, depth int) error {
		if pattern == nil {
			return fmt.Errorf("match arm has no pattern")
		}
		if err := addNode("pattern", depth); err != nil {
			return err
		}
		if document.Version < 3 && pattern.ResolvedBinding != nil {
			return fmt.Errorf("resolved pattern bindings require KIR version 3")
		}
		if pattern.Binding == "" {
			if pattern.ResolvedBinding != nil {
				return fmt.Errorf("pattern without a binding has resolved binding metadata")
			}
		} else if document.Version >= 3 && (!validKIRBinding(pattern.ResolvedBinding) || pattern.ResolvedBinding.Name != pattern.Binding || pattern.ResolvedBinding.Mutable) {
			return fmt.Errorf("pattern has an invalid resolved binding")
		}
		switch pattern.Kind {
		case "wildcard", "nil", "bool", "int", "string", "option", "result":
		case "enum":
			decl := enums[pattern.Type]
			if decl == nil || !containsKIRString(decl.Variants, pattern.Variant) {
				return fmt.Errorf("enum pattern references an unknown type or variant")
			}
		default:
			return fmt.Errorf("unknown pattern kind %q", pattern.Kind)
		}
		return nil
	}
	validateStmt = func(statement *KIRStmt, depth int) error {
		if statement == nil {
			return fmt.Errorf("statement list contains a missing node")
		}
		if err := addNode("statement", depth); err != nil {
			return err
		}
		if document.Version < 3 && statement.Binding != nil {
			return fmt.Errorf("resolved statement bindings require KIR version 3")
		}
		if statement.Binding != nil && statement.Kind != "let" && statement.Kind != "const" && statement.Kind != "for" {
			return fmt.Errorf("%s statement cannot have a resolved declaration binding", statement.Kind)
		}
		requireExpr := func(expression *KIRExpr, field string) error {
			if expression == nil {
				return fmt.Errorf("%s statement is missing %s", statement.Kind, field)
			}
			return nil
		}
		switch statement.Kind {
		case "let", "const":
			if statement.Name == "" {
				return fmt.Errorf("%s statement has no binding name", statement.Kind)
			}
			if err := requireExpr(statement.Init, "initializer"); err != nil {
				return err
			}
			if document.Version >= 3 && (!validKIRBinding(statement.Binding) || statement.Binding.Name != statement.Name || statement.Binding.Mutable != statement.Mutable) {
				return fmt.Errorf("%s statement has an invalid resolved binding", statement.Kind)
			}
		case "expr":
			if err := requireExpr(statement.Expr, "expression"); err != nil {
				return err
			}
		case "assign":
			if err := requireExpr(statement.Target, "target"); err != nil {
				return err
			}
			if statement.Target.Kind != "var" || statement.Target.Name == "" {
				return fmt.Errorf("assignment target is not a binding")
			}
			if err := requireExpr(statement.Value, "value"); err != nil {
				return err
			}
		case "if", "while":
			if err := requireExpr(statement.Cond, "condition"); err != nil {
				return err
			}
		case "for":
			if statement.Name == "" {
				return fmt.Errorf("for statement has no binding name")
			}
			if err := requireExpr(statement.Iter, "iterator"); err != nil {
				return err
			}
			if document.Version >= 3 && (!validKIRBinding(statement.Binding) || statement.Binding.Name != statement.Name || statement.Binding.Type == "") {
				return fmt.Errorf("for statement has an invalid resolved binding")
			}
		case "return", "break", "continue", "defer", "unsafe":
		case "match":
			if err := requireExpr(statement.Scrutinee, "scrutinee"); err != nil {
				return err
			}
			if len(statement.Arms) == 0 {
				return fmt.Errorf("match statement has no arms")
			}
		default:
			return fmt.Errorf("unknown statement kind %q", statement.Kind)
		}
		for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			if err := validateExpr(expression, depth+1); err != nil {
				return err
			}
		}
		if document.Version >= 3 && (statement.Kind == "let" || statement.Kind == "const") && statement.Binding.Type != statement.Init.Type {
			return fmt.Errorf("%s statement binding type does not match its initializer", statement.Kind)
		}
		for _, list := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
			if err := checkKIRCount("statement block", len(list), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, child := range list {
				if err := validateStmt(child, depth+1); err != nil {
					return err
				}
			}
		}
		if err := checkKIRCount("match arms", len(statement.Arms), limits.MaxArrayElements); err != nil {
			return err
		}
		for _, arm := range statement.Arms {
			if arm == nil {
				return fmt.Errorf("match statement contains a missing arm")
			}
			if err := validatePattern(arm.Pattern, depth+1); err != nil {
				return err
			}
			if err := checkKIRCount("match arm statements", len(arm.Body), limits.MaxArrayElements); err != nil {
				return err
			}
			for _, child := range arm.Body {
				if err := validateStmt(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, function := range document.Functions {
		for _, parameter := range function.Params {
			if err := validateExpr(parameter.Default, 1); err != nil {
				return fmt.Errorf("function %q default: %w", function.Name, err)
			}
		}
		for _, statement := range function.Body {
			if err := validateStmt(statement, 1); err != nil {
				return fmt.Errorf("function %q: %w", function.Name, err)
			}
		}
	}
	if err := checkKIRCount("top-level statements", len(document.Statements), limits.MaxArrayElements); err != nil {
		return err
	}
	for _, statement := range document.Statements {
		if err := validateStmt(statement, 1); err != nil {
			return err
		}
	}
	return nil
}

func checkKIRCount(label string, count, maximum int) error {
	if maximum > 0 && count > maximum {
		return fmt.Errorf("%s count %d exceeds configured limit %d", label, count, maximum)
	}
	return nil
}

func validKIRTarget(target KIRTarget) bool {
	switch target.OS {
	case "linux", "windows", "darwin":
	default:
		return false
	}
	return target.Arch == "amd64" || target.Arch == "arm64"
}

func validKIRCallTarget(target string) bool {
	prefix, name, ok := strings.Cut(target, ":")
	return ok && name != "" && (prefix == "builtin" || prefix == "function")
}

func isKIRUnaryOperator(operator string) bool {
	switch operator {
	case "+", "-", "!", "~":
		return true
	default:
		return false
	}
}

func isKIRBinaryOperator(operator string) bool {
	switch operator {
	case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "&", "|", "^", "<<", ">>":
		return true
	default:
		return false
	}
}

func containsKIRString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func parseKIRFunctionType(raw string) ([]string, string, bool) {
	if !strings.HasPrefix(raw, "fn(") {
		return nil, "", false
	}
	var params []string
	start, parentheses, brackets := 3, 1, 0
	end := -1
	for i := 3; i < len(raw); i++ {
		switch raw[i] {
		case '[':
			brackets++
		case ']':
			brackets--
			if brackets < 0 {
				return nil, "", false
			}
		case '(':
			parentheses++
		case ')':
			parentheses--
			if parentheses < 0 {
				return nil, "", false
			}
			if parentheses == 0 {
				end = i
				rawParam := strings.TrimSpace(raw[start:i])
				if rawParam != "" {
					params = append(params, rawParam)
				}
				break
			}
		case ',':
			if parentheses == 1 && brackets == 0 {
				rawParam := strings.TrimSpace(raw[start:i])
				if rawParam == "" {
					return nil, "", false
				}
				params = append(params, rawParam)
				start = i + 1
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 || brackets != 0 || parentheses != 0 {
		return nil, "", false
	}
	remainder := strings.TrimSpace(raw[end+1:])
	if !strings.HasPrefix(remainder, "->") {
		return nil, "", false
	}
	result := strings.TrimSpace(strings.TrimPrefix(remainder, "->"))
	if result == "" {
		return nil, "", false
	}
	return params, result, true
}

func validKIRBinding(binding *KIRBinding) bool {
	return binding != nil && binding.Name != "" && binding.Type != "" && binding.Source != "" && binding.Line >= 1 && binding.Column >= 1
}

func kirBindingIdentity(binding *KIRBinding) string {
	if binding == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d:%s", binding.Source, binding.Line, binding.Column, binding.Name)
}

func validateKIRLambdaCaptures(function *KIRFunction) error {
	locals := map[string]*KIRBinding{}
	uses := map[string]*KIRBinding{}
	captures := map[string]*KIRBinding{}
	for _, parameter := range function.Params {
		locals[kirBindingIdentity(parameter.Binding)] = parameter.Binding
	}
	for _, capture := range function.Captures {
		captures[kirBindingIdentity(capture.Binding)] = capture.Binding
	}
	addUse := func(binding *KIRBinding) error {
		if !validKIRBinding(binding) {
			return fmt.Errorf("lambda contains an unresolved variable binding")
		}
		identity := kirBindingIdentity(binding)
		if previous := uses[identity]; previous != nil && (previous.Type != binding.Type || previous.Mutable != binding.Mutable) {
			return fmt.Errorf("lambda references a binding with inconsistent type or mutability")
		}
		uses[identity] = binding
		return nil
	}
	var walkExpr func(*KIRExpr) error
	var walkStmt func(*KIRStmt) error
	walkExpr = func(expression *KIRExpr) error {
		if expression == nil {
			return nil
		}
		if expression.Kind == "lambda" {
			if expression.Lambda == nil {
				return fmt.Errorf("nested lambda has no function body")
			}
			for _, capture := range expression.Lambda.Captures {
				if capture == nil {
					return fmt.Errorf("nested lambda has an incomplete capture")
				}
				if err := addUse(capture.Binding); err != nil {
					return err
				}
			}
			return nil
		}
		if expression.Kind == "var" && expression.Binding != nil {
			if err := addUse(expression.Binding); err != nil {
				return err
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
		return nil
	}
	walkStmt = func(statement *KIRStmt) error {
		if statement == nil {
			return nil
		}
		if statement.Binding != nil {
			locals[kirBindingIdentity(statement.Binding)] = statement.Binding
		}
		for _, expression := range []*KIRExpr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
			if err := walkExpr(expression); err != nil {
				return err
			}
		}
		for _, list := range [][]*KIRStmt{statement.Then, statement.Else, statement.Body} {
			for _, child := range list {
				if err := walkStmt(child); err != nil {
					return err
				}
			}
		}
		for _, arm := range statement.Arms {
			if arm == nil {
				continue
			}
			if arm.Pattern != nil && arm.Pattern.ResolvedBinding != nil {
				locals[kirBindingIdentity(arm.Pattern.ResolvedBinding)] = arm.Pattern.ResolvedBinding
			}
			for _, child := range arm.Body {
				if err := walkStmt(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, statement := range function.Body {
		if err := walkStmt(statement); err != nil {
			return err
		}
	}
	for identity, binding := range uses {
		if local := locals[identity]; local != nil {
			if local.Type != binding.Type || local.Mutable != binding.Mutable {
				return fmt.Errorf("lambda local binding %q has inconsistent type or mutability", binding.Name)
			}
			continue
		}
		capture := captures[identity]
		if capture == nil {
			return fmt.Errorf("lambda is missing capture %q", binding.Name)
		}
		if capture.Type != binding.Type || capture.Mutable != binding.Mutable {
			return fmt.Errorf("lambda capture %q does not match its resolved binding", binding.Name)
		}
	}
	for identity, capture := range captures {
		if locals[identity] != nil {
			return fmt.Errorf("lambda capture %q is local to its own body", capture.Name)
		}
		if uses[identity] == nil {
			return fmt.Errorf("lambda has unused capture %q", capture.Name)
		}
	}
	return nil
}
