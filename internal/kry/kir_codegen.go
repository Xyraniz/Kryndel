package kry

import "strings"

func kirExprKind(kind string) ExprKind {
	for i, name := range []string{"int", "float", "bool", "nil", "string", "var", "unary", "binary", "call", "array", "index", "field", "struct", "enum", "map", "set", "propagate", "lambda"} {
		if kind == name {
			return ExprKind(i)
		}
	}
	return ExprKind(-1)
}

func kirStmtKind(kind string) StmtKind {
	for i, name := range []string{"let", "expr", "assign", "if", "while", "return", "break", "continue", "match", "for", "defer", "unsafe", "const"} {
		if kind == name {
			return StmtKind(i)
		}
	}
	return StmtKind(-1)
}

func kirPatternKind(kind string) PatternKind {
	for i, name := range []string{"wildcard", "nil", "bool", "int", "string", "enum", "option", "result"} {
		if kind == name {
			return PatternKind(i)
		}
	}
	return PatternKind(-1)
}

func substituteKIRType(encoded string, substitutions map[string]string) string {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok || len(substitutions) == 0 {
		return encoded
	}
	var visit func(*TypeSpec, int)
	visit = func(current *TypeSpec, depth int) {
		if current == nil || depth > 128 {
			return
		}
		if !current.Function && len(current.Params) == 0 {
			if replacement := substitutions[current.Name]; replacement != "" && replacement != current.Name {
				if resolved, valid := parseKIRTypeExpression(replacement); valid {
					*current = *resolved
				}
			}
		}
		for _, child := range current.Params {
			visit(child, depth+1)
		}
		visit(current.Return, depth+1)
	}
	visit(spec, 0)
	return TypeSpecString(spec)
}

func typeContainsKIRVariable(encoded string, parameters []string) bool {
	if len(parameters) == 0 {
		return false
	}
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok {
		return true
	}
	known := make(map[string]bool, len(parameters))
	for _, parameter := range parameters {
		known[parameter] = true
	}
	var visit func(*TypeSpec, int) bool
	visit = func(current *TypeSpec, depth int) bool {
		if current == nil || depth > 128 {
			return depth > 128
		}
		if !current.Function && len(current.Params) == 0 && known[current.Name] {
			return true
		}
		for _, child := range current.Params {
			if visit(child, depth+1) {
				return true
			}
		}
		return visit(current.Return, depth+1)
	}
	return visit(spec, 0)
}

func kirTypeDepth(encoded string) int {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok {
		return 129
	}
	var depth func(*TypeSpec, int) int
	depth = func(current *TypeSpec, level int) int {
		if current == nil {
			return level
		}
		maximum := level
		for _, child := range current.Params {
			if value := depth(child, level+1); value > maximum {
				maximum = value
			}
		}
		if value := depth(current.Return, level+1); value > maximum {
			maximum = value
		}
		return maximum
	}
	return depth(spec, 1)
}

func kirOpenTypeParameters(substitutions map[string]string) []string {
	open := make([]string, 0, len(substitutions))
	for name, replacement := range substitutions {
		if replacement == name {
			open = append(open, name)
		}
	}
	return open
}

func (g *cgen) uniqueFunction(name string) *KIRFunction {
	functions := g.functionsByName[name]
	if len(functions) != 1 {
		return nil
	}
	return functions[0]
}

func kirDocumentUsesFunctionValues(document *KIRDocument) bool {
	isFunctionType := kirTypeContainsFunction
	var expression func(*KIRExpr) bool
	var statements func([]*KIRStmt) bool
	expression = func(value *KIRExpr) bool {
		if value == nil {
			return false
		}
		if value.Kind == "lambda" || isFunctionType(value.Type) || value.CallTarget != "" && strings.HasPrefix(value.CallTarget, "function:") && value.Kind == "var" {
			return true
		}
		for _, child := range []*KIRExpr{value.Left, value.Right, value.Operand, value.Callee, value.Base, value.Receiver} {
			if expression(child) {
				return true
			}
		}
		for _, list := range [][]*KIRExpr{value.Args, value.Items, value.MapKeys, value.Values} {
			for _, child := range list {
				if expression(child) {
					return true
				}
			}
		}
		return value.Lambda != nil
	}
	statements = func(values []*KIRStmt) bool {
		for _, value := range values {
			if value == nil {
				continue
			}
			for _, item := range []*KIRExpr{value.Init, value.Expr, value.Target, value.Value, value.Cond, value.Iter, value.Return, value.Scrutinee} {
				if expression(item) {
					return true
				}
			}
			for _, block := range [][]*KIRStmt{value.Then, value.Else, value.Body} {
				if statements(block) {
					return true
				}
			}
			for _, arm := range value.Arms {
				if arm != nil && statements(arm.Body) {
					return true
				}
			}
		}
		return false
	}
	for _, declaration := range document.Structs {
		for _, field := range declaration.Fields {
			if isFunctionType(field.Type) {
				return true
			}
		}
	}
	for _, trait := range document.Traits {
		for _, method := range trait.Methods {
			if isFunctionType(method.Return) {
				return true
			}
			for _, parameter := range method.Params {
				if parameter != nil && isFunctionType(parameter.Type) {
					return true
				}
			}
		}
	}
	for _, function := range document.Functions {
		if function == nil {
			continue
		}
		if isFunctionType(function.Return) {
			return true
		}
		for _, parameter := range function.Params {
			if parameter != nil && (isFunctionType(parameter.Type) || expression(parameter.Default)) {
				return true
			}
		}
		if statements(function.Body) {
			return true
		}
	}
	return statements(document.Statements)
}

func kirTypeContainsFunction(encoded string) bool {
	spec, ok := parseKIRTypeExpression(encoded)
	if !ok {
		return false
	}
	var visit func(*TypeSpec, int) bool
	visit = func(current *TypeSpec, depth int) bool {
		if current == nil || depth > 128 {
			return false
		}
		if current.Function {
			return true
		}
		for _, child := range current.Params {
			if visit(child, depth+1) {
				return true
			}
		}
		return visit(current.Return, depth+1)
	}
	return visit(spec, 0)
}
