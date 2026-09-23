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
	}
	return "unsupported"
}

func validateNativeFeatureSupport(p *Program, c *Checker, format string, target NativeTarget) error {
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
