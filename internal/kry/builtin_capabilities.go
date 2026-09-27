package kry

import "fmt"

// BuiltinCapability is one builtin/backend/target row. Interpreter and
// self-hosted refer to the checked runtime dispatch and the current
// source-compiler subset respectively. Native states describe registered
// lowering handlers, not proof that every runtime edge case is identical.
type BuiltinCapability struct {
	Builtin     string `json:"builtin"`
	Target      string `json:"target"`
	Interpreter string `json:"interpreter"`
	CAOT        string `json:"c_aot"`
	ELFDirect   string `json:"elf_direct"`
	PEDirect    string `json:"pe_direct"`
	SelfHosted  string `json:"self_hosted"`
}

// BuiltinCapabilityMatrix enumerates every registered builtin for every
// declared output target and reports the implementation inventory per backend.
func BuiltinCapabilityMatrix() []BuiltinCapability {
	rows := make([]BuiltinCapability, 0, len(builtinList)*len(nativeCapabilityTargets))
	for _, builtin := range builtinList {
		for _, target := range nativeCapabilityTargets {
			interpreter := "unsupported"
			if _, ok := generatedInterpreterBuiltinCases[builtin.Name]; ok {
				interpreter = "supported"
			}
			cFormat := "elf"
			if target.target.OS == "windows" {
				cFormat = "pe"
			}
			selfHosted := "unsupported"
			if target.name == "linux-x64" {
				if _, ok := generatedSelfHostedBuiltinNames[builtin.Name]; ok {
					selfHosted = "partial"
				}
			}
			rows = append(rows, BuiltinCapability{
				Builtin:     builtin.Name,
				Target:      target.name,
				Interpreter: interpreter,
				CAOT:        nativeBuiltinBackendStatus(builtin.Name, cFormat, target.target),
				ELFDirect:   nativeBuiltinBackendStatus(builtin.Name, "elf-direct", target.target),
				PEDirect:    nativeBuiltinBackendStatus(builtin.Name, "pe-direct", target.target),
				SelfHosted:  selfHosted,
			})
		}
	}
	return rows
}

func nativeBuiltinBackendStatus(name, format string, target NativeTarget) string {
	switch format {
	case "elf", "exe", "pe", "c":
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if _, ok := generatedCAOTBuiltinCases[name]; ok {
			return "supported"
		}
	case "elf-direct":
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if _, ok := generatedDirectELFBuiltinCases[name]; ok {
			if name == "print" || name == "println" {
				return "partial"
			}
			return "supported"
		}
	case "pe-direct":
		if nativeOutputTargetReason(format, target) != "" {
			return "unsupported"
		}
		if _, ok := generatedDirectPEBuiltinCases[name]; ok {
			if name == "print" || name == "println" {
				return "partial"
			}
			return "supported"
		}
	}
	return "unsupported"
}

func validateNativeFeatureSupport(p *Program, c *Checker, format string, target NativeTarget) error {
	if p == nil || c == nil || c.Env == nil {
		return fmt.Errorf("missing checked program or type environment")
	}
	// Prefer the actionable builtin diagnostic over a secondary unsupported
	// opaque-handle type error in a builtin's signature or arguments.
	if err := validateNativeBuiltinSupport(p, c, format, target); err != nil {
		return err
	}
	var walkExpr func(*Expr, bool) error
	walkExpr = func(e *Expr, allowDirectOutput bool) error {
		if e == nil {
			return nil
		}
		if kind := expressionKindName(e.Kind); kind == "" {
			return fmt.Errorf("expression kind %d is not listed as supported by the %s backend for %s-%s", e.Kind, format, target.OS, target.Arch)
		} else if err := validateLanguageItem("expression", kind, format, target); err != nil {
			return err
		}
		if e.Kind == ExUnary || e.Kind == ExBinary {
			category := "binary_operator"
			if e.Kind == ExUnary {
				category = "unary_operator"
			}
			if operator := operatorKindName(e.Op); operator == "" {
				return fmt.Errorf("operator %q is not listed as supported by the %s backend for %s-%s", opText(e.Op), format, target.OS, target.Arch)
			} else if err := validateLanguageItem(category, operator, format, target); err != nil {
				return err
			}
		}
		if e.Type != nil && e.Type.Kind != TyNil && e.Type.Kind != TyVoid && e.Type.Kind != TyError && e.Type.Kind != TyUnknown {
			if kind := typeKindName(e.Type.Kind); kind == "" {
				return fmt.Errorf("type kind %d is not listed as supported by the %s backend for %s-%s", e.Type.Kind, format, target.OS, target.Arch)
			} else if err := validateLanguageItem("type", kind, format, target); err != nil {
				return err
			}
		}
		if e.Kind == ExCall && e.Function == nil && c.Env != nil {
			if _, builtin := c.Env.Builtins[e.Name]; builtin {
				status := nativeBuiltinBackendStatus(e.Name, format, target)
				if format == "elf-direct" && (e.Name == "print" || e.Name == "println") && (!allowDirectOutput || len(e.Args) != 1) {
					status = "unsupported"
				}
				if status == "unsupported" {
					return fmt.Errorf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", e.Name, format, target.OS, target.Arch)
				}
			}
		}
		for _, child := range []*Expr{e.Left, e.Right, e.Operand, e.Base, e.Receiver} {
			if err := walkExpr(child, false); err != nil {
				return err
			}
		}
		for _, list := range [][]*Expr{e.Args, e.Items, e.MapKeys, e.Values} {
			for _, child := range list {
				if err := walkExpr(child, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var walkStmts func([]*Stmt) error
	walkStmts = func(stmts []*Stmt) error {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			if kind := statementKindName(s.Kind); kind == "" {
				return fmt.Errorf("statement kind %d is not listed as supported by the %s backend for %s-%s", s.Kind, format, target.OS, target.Arch)
			} else if err := validateLanguageItem("statement", kind, format, target); err != nil {
				return err
			}
			for _, e := range []*Expr{s.Init, s.Expr, s.Target, s.Value, s.Cond, s.Iter, s.Return, s.Scrutinee} {
				if s.Kind == StReturn && e == s.Return && e != nil && e.Kind == ExNil {
					// Nil is the source spelling of a no-value return. The direct
					// backend lowers that return as a void result without an ExNil
					// value representation.
					continue
				}
				allowOutput := format == "elf-direct" && s.Kind == StExpr && e == s.Expr
				if err := walkExpr(e, allowOutput); err != nil {
					return err
				}
			}
			for _, block := range [][]*Stmt{s.Then, s.Else, s.Body} {
				if err := walkStmts(block); err != nil {
					return err
				}
			}
			for _, arm := range s.Arms {
				if kind := patternKindName(arm.Pattern.Kind); kind == "" {
					return fmt.Errorf("pattern kind %d is not listed as supported by the %s backend for %s-%s", arm.Pattern.Kind, format, target.OS, target.Arch)
				} else if err := validateLanguageItem("pattern", kind, format, target); err != nil {
					return err
				}
				if err := walkStmts(arm.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walkStmts(p.Statements); err != nil {
		return err
	}
	for _, decl := range p.Structs {
		for _, field := range decl.Fields {
			if field.Type != nil && field.Type.Kind != TyNil && field.Type.Kind != TyVoid {
				if err := validateLanguageItem("type", typeKindName(field.Type.Kind), format, target); err != nil {
					return err
				}
			}
		}
	}
	for _, function := range p.Functions {
		env := *c.Env
		env.TypeParams = make(map[string]*Type, len(function.TypeParams))
		for name, typ := range c.Env.TypeParams {
			env.TypeParams[name] = typ
		}
		for _, param := range function.TypeParams {
			env.TypeParams[param.Name] = Generic(param.Name, param.Constraint)
		}
		for _, parameter := range function.Params {
			typ, diagnostic := resolveSpec(&env, parameter.Type, 0)
			if diagnostic == nil && typ != nil && typ.Kind != TyNil && typ.Kind != TyVoid {
				if err := validateLanguageItem("type", typeKindName(typ.Kind), format, target); err != nil {
					return err
				}
			}
		}
		returnType, diagnostic := resolveSpec(&env, function.Return, 0)
		if diagnostic == nil && returnType != nil && returnType.Kind != TyNil && returnType.Kind != TyVoid {
			if err := validateLanguageItem("type", typeKindName(returnType.Kind), format, target); err != nil {
				return err
			}
		}
		if err := walkStmts(function.Body); err != nil {
			return err
		}
		for _, param := range function.Params {
			if err := walkExpr(param.Default, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateNativeBuiltinSupport(p *Program, c *Checker, format string, target NativeTarget) error {
	if p == nil || c == nil || c.Env == nil {
		return fmt.Errorf("missing checked program or type environment")
	}
	var walkExpr func(*Expr) error
	walkExpr = func(e *Expr) error {
		if e == nil {
			return nil
		}
		if e.Kind == ExCall && e.Function == nil {
			if _, builtin := c.Env.Builtins[e.Name]; builtin && nativeBuiltinBackendStatus(e.Name, format, target) == "unsupported" {
				return fmt.Errorf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", e.Name, format, target.OS, target.Arch)
			}
		}
		for _, child := range []*Expr{e.Left, e.Right, e.Operand, e.Base, e.Receiver} {
			if err := walkExpr(child); err != nil {
				return err
			}
		}
		for _, list := range [][]*Expr{e.Args, e.Items, e.MapKeys, e.Values} {
			for _, child := range list {
				if err := walkExpr(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	var walkStmts func([]*Stmt) error
	walkStmts = func(stmts []*Stmt) error {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			for _, expr := range []*Expr{stmt.Init, stmt.Expr, stmt.Target, stmt.Value, stmt.Cond, stmt.Iter, stmt.Return, stmt.Scrutinee} {
				if err := walkExpr(expr); err != nil {
					return err
				}
			}
			for _, block := range [][]*Stmt{stmt.Then, stmt.Else, stmt.Body} {
				if err := walkStmts(block); err != nil {
					return err
				}
			}
			for _, arm := range stmt.Arms {
				if err := walkStmts(arm.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walkStmts(p.Statements); err != nil {
		return err
	}
	for _, function := range p.Functions {
		if function == nil {
			continue
		}
		if err := walkStmts(function.Body); err != nil {
			return err
		}
		for _, parameter := range function.Params {
			if err := walkExpr(parameter.Default); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateFunctionValueSupport(p *Program, format string) error {
	if p == nil {
		return fmt.Errorf("missing program")
	}
	if usesFunctionType(p) {
		return fmt.Errorf("%s backend does not support function values or closures; use the interpreter", format)
	}
	return nil
}

func usesFunctionType(program *Program) bool {
	var typeUsesFunction func(*TypeSpec) bool
	typeUsesFunction = func(spec *TypeSpec) bool {
		if spec == nil {
			return false
		}
		if spec.Function || typeUsesFunction(spec.Return) {
			return true
		}
		for _, child := range spec.Params {
			if typeUsesFunction(child) {
				return true
			}
		}
		return false
	}
	var usesExpr func(*Expr) bool
	var usesStmts func([]*Stmt) bool
	usesExpr = func(expression *Expr) bool {
		if expression == nil {
			return false
		}
		if expression.Kind == ExLambda || expression.Type != nil && expression.Type.Kind == TyFunction || expression.Kind == ExVar && expression.Function != nil || expression.Kind == ExCall && expression.Callee != nil && expression.Callee.Type != nil && expression.Callee.Type.Kind == TyFunction {
			return true
		}
		for _, child := range []*Expr{expression.Left, expression.Right, expression.Operand, expression.Base, expression.Receiver, expression.Callee} {
			if usesExpr(child) {
				return true
			}
		}
		for _, list := range [][]*Expr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				if usesExpr(child) {
					return true
				}
			}
		}
		return expression.Lambda != nil && usesStmts(expression.Lambda.Body)
	}
	usesStmts = func(statements []*Stmt) bool {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			for _, expression := range []*Expr{statement.Init, statement.Expr, statement.Target, statement.Value, statement.Cond, statement.Iter, statement.Return, statement.Scrutinee} {
				if usesExpr(expression) {
					return true
				}
			}
			for _, block := range [][]*Stmt{statement.Then, statement.Else, statement.Body} {
				if usesStmts(block) {
					return true
				}
			}
			for _, arm := range statement.Arms {
				if usesStmts(arm.Body) {
					return true
				}
			}
		}
		return false
	}
	for _, structure := range program.Structs {
		for _, field := range structure.Fields {
			if typeUsesFunction(field.Spec) {
				return true
			}
		}
	}
	for _, function := range program.Functions {
		if typeUsesFunction(function.Return) || typeUsesFunction(function.Receiver) {
			return true
		}
		for _, parameter := range function.Params {
			if typeUsesFunction(parameter.Type) || usesExpr(parameter.Default) {
				return true
			}
		}
		if usesStmts(function.Body) {
			return true
		}
	}
	return usesStmts(program.Statements)
}
