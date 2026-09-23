package kry

import "fmt"

// validateNativeFeatureSupport checks builtin lowerings against the generated
// backend inventories before a native backend creates output bytes.
func validateNativeFeatureSupport(p *Program, c *Checker, format string, target NativeTarget) error {
	var builtinCases map[string]struct{}
	switch format {
	case "elf", "exe", "pe", "c":
		builtinCases = generatedCAOTBuiltinCases
	case "elf-direct":
		builtinCases = generatedDirectELFBuiltinCases
	default:
		return nil
	}

	var walkExpr func(*Expr, bool) error
	walkExpr = func(e *Expr, allowDirectOutput bool) error {
		if e == nil {
			return nil
		}
		if format == "elf-direct" && e.ConstValue != nil {
			switch e.ConstValue.Kind {
			case VInt, VUInt, VBool, VString:
				return nil
			}
		}
		if e.Kind == ExCall && e.Function == nil && c.Env != nil {
			if _, builtin := c.Env.Builtins[e.Name]; builtin {
				_, supported := builtinCases[e.Name]
				if format == "elf-direct" && allowDirectOutput && (e.Name == "print" || e.Name == "println") && len(e.Args) == 1 {
					supported = true
				}
				if !supported {
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
			for _, e := range []*Expr{s.Init, s.Expr, s.Target, s.Value, s.Cond, s.Iter, s.Return, s.Scrutinee} {
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
