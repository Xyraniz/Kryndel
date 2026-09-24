package kry

import (
	"fmt"
	"strings"
)

type Binding struct {
	Type    *Type
	Mutable bool
	Const   bool
	Global  bool
}
type Scope struct {
	Parent          *Scope
	Values          map[string]Binding
	Worker          bool
	Unsafe          bool
	Module          string
	VisibilityScope string
}

func NewScope(parent *Scope, worker bool) *Scope {
	m := ""
	visibilityScope := ""
	unsafe := false
	if parent != nil {
		m = parent.Module
		visibilityScope = parent.VisibilityScope
		unsafe = parent.Unsafe
	}
	return &Scope{Parent: parent, Values: map[string]Binding{}, Worker: worker, Unsafe: unsafe, Module: m, VisibilityScope: visibilityScope}
}
func (s *Scope) lookup(n string) (Binding, bool) {
	for q := s; q != nil; q = q.Parent {
		if b, ok := q.Values[n]; ok {
			return b, true
		}
	}
	return Binding{}, false
}
func (s *Scope) local(n string) bool { _, ok := s.Values[n]; return ok }

type Flow struct{ MustReturn, MayFallthrough, MayBreak, MayContinue, Reachable, HasError bool }

func normalFlow() Flow { return Flow{MayFallthrough: true, Reachable: true} }
func returnFlow() Flow { return Flow{MustReturn: true, Reachable: true} }

type Checker struct {
	Prog            *Program
	Env             *TypeEnv
	Globals         *Scope
	Lim             Limits
	Err             *Diagnostic
	funcs           map[string]bool
	currentReturn   *Type
	currentFunction *Function
	typeParams      map[string]*Type
}

func Check(prog *Program, lim Limits) (*Checker, *Diagnostic) {
	env, d := typeDecls(prog, lim)
	if d != nil {
		return nil, d
	}
	env.Builtins = Builtins()
	c := &Checker{Prog: prog, Env: env, Lim: lim, funcs: map[string]bool{}}
	c.Globals = NewScope(nil, false)
	c.Globals.Module = prog.Module
	c.Globals.VisibilityScope = prog.VisibilityScope
	c.markWorkers()
	for _, s := range prog.Statements {
		if (s.Kind == StLet || s.Kind == StConst) && s.Annotation != nil {
			a, dd := resolveSpec(env, s.Annotation, 0)
			if dd != nil {
				return nil, dd
			}
			if a.Kind == TyChannel || a.Kind == TyShared || s.Kind == StConst {
				if c.Globals.local(s.Name) {
					return nil, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "binding '%s' is already defined in this scope", s.Name)
				}
				c.Globals.Values[s.Name] = Binding{Type: a, Mutable: s.Mutable, Const: s.Const, Global: true}
			}
		}
	}
	for _, f := range prog.Functions {
		if d := c.checkFunction(f); d != nil {
			return nil, d
		}
	}
	top := NewScope(c.Globals, false)
	if d := c.checkStatements(top, prog.Statements, TNil, 0, false); d != nil {
		return nil, d
	}
	return c, nil
}
func compatible(a, b *Type) bool {
	if typeEqual(a, b) {
		return true
	}
	if a != nil && b != nil && a.Kind == TyArray && a.A.Kind == TyUnknown && b.Kind == TyArray {
		return true
	}
	if a != nil && b != nil && b.Kind == TyArray && b.A.Kind == TyUnknown && a.Kind == TyArray {
		return true
	}
	return false
}

func constantExpr(e *Expr) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case ExInt, ExFloat, ExBool, ExNil, ExString, ExEnum:
		return true
	case ExUnary:
		return constantExpr(e.Operand)
	case ExBinary:
		return constantExpr(e.Left) && constantExpr(e.Right)
	case ExArray, ExSet:
		for _, item := range e.Items {
			if !constantExpr(item) {
				return false
			}
		}
		return true
	case ExMap:
		for i := range e.MapKeys {
			if !constantExpr(e.MapKeys[i]) || !constantExpr(e.Values[i]) {
				return false
			}
		}
		return true
	case ExStruct:
		for _, value := range e.Values {
			if !constantExpr(value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (c *Checker) markWorkers() {
	for _, s := range c.Prog.Statements {
		c.walkStmt(s)
	}
	for _, f := range c.Prog.Functions {
		c.walkFunction(f)
	}
	changed := true
	for changed {
		changed = false
		for _, f := range c.Prog.Functions {
			if c.funcs[f.Name] {
				for _, n := range calledFunctions(f.Body) {
					if !c.funcs[n] {
						c.funcs[n] = true
						changed = true
					}
				}
			}
		}
	}
	for _, f := range c.Prog.Functions {
		f.Worker = c.funcs[f.Name]
	}
}
func (c *Checker) walkFunction(f *Function) {
	for _, s := range f.Body {
		c.walkStmt(s)
	}
}
func (c *Checker) walkStmt(s *Stmt) {
	if s == nil {
		return
	}
	if s.Init != nil {
		c.walkExpr(s.Init)
	}
	if s.Expr != nil {
		c.walkExpr(s.Expr)
	}
	if s.Target != nil {
		c.walkExpr(s.Target)
		c.walkExpr(s.Value)
	}
	if s.Cond != nil {
		c.walkExpr(s.Cond)
	}
	if s.Return != nil {
		c.walkExpr(s.Return)
	}
	if s.Scrutinee != nil {
		c.walkExpr(s.Scrutinee)
	}
	if s.Iter != nil {
		c.walkExpr(s.Iter)
	}
	for _, x := range s.Then {
		c.walkStmt(x)
	}
	for _, x := range s.Else {
		c.walkStmt(x)
	}
	for _, x := range s.Body {
		c.walkStmt(x)
	}
	for _, a := range s.Arms {
		for _, x := range a.Body {
			c.walkStmt(x)
		}
	}
}
func (c *Checker) walkExpr(e *Expr) {
	if e == nil {
		return
	}
	if e.Kind == ExCall && e.Name == "thread_spawn" && len(e.Args) == 1 && e.Args[0].Kind == ExString {
		c.funcs[e.Args[0].Str] = true
	}
	c.walkExpr(e.Left)
	c.walkExpr(e.Right)
	c.walkExpr(e.Operand)
	c.walkExpr(e.Base)
	for _, x := range e.Args {
		c.walkExpr(x)
	}
	for _, x := range e.Items {
		c.walkExpr(x)
	}
	for _, x := range e.Values {
		c.walkExpr(x)
	}
	for _, x := range e.MapKeys {
		c.walkExpr(x)
	}
	c.walkExpr(e.Receiver)
}
func calledFunctions(body []*Stmt) []string {
	var out []string
	var ex func(*Expr)
	var st func(*Stmt)
	ex = func(e *Expr) {
		if e == nil {
			return
		}
		if e.Kind == ExCall {
			out = append(out, e.Name)
		}
		ex(e.Left)
		ex(e.Right)
		ex(e.Operand)
		ex(e.Base)
		for _, x := range e.Args {
			ex(x)
		}
		for _, x := range e.Items {
			ex(x)
		}
		for _, x := range e.Values {
			ex(x)
		}
		for _, x := range e.MapKeys {
			ex(x)
		}
		ex(e.Receiver)
	}
	st = func(s *Stmt) {
		if s == nil {
			return
		}
		ex(s.Init)
		ex(s.Expr)
		ex(s.Target)
		ex(s.Value)
		ex(s.Cond)
		ex(s.Return)
		ex(s.Scrutinee)
		for _, x := range s.Then {
			st(x)
		}
		for _, x := range s.Else {
			st(x)
		}
		for _, x := range s.Body {
			st(x)
		}
		if s.Iter != nil {
			ex(s.Iter)
		}

		for _, a := range s.Arms {
			for _, x := range a.Body {
				st(x)
			}
		}
	}
	for _, s := range body {
		st(s)
	}
	return out
}

func (c *Checker) checkFunction(f *Function) *Diagnostic {
	sc := NewScope(c.Globals, f.Worker)
	sc.Module = f.Module
	sc.VisibilityScope = f.VisibilityScope
	defaultScope := NewScope(c.Globals, f.Worker)
	defaultScope.Module = f.Module
	defaultScope.VisibilityScope = f.VisibilityScope
	c.currentFunction = f
	previousParams := c.Env.TypeParams
	c.Env.TypeParams = map[string]*Type{}
	for _, param := range f.TypeParams {
		if _, exists := c.Env.TypeParams[param.Name]; exists {
			return Diag(CatType, param.Tok.Source, param.Tok.Line, param.Tok.Column, "type parameter '%s' is duplicated", param.Name)
		}
		c.Env.TypeParams[param.Name] = Generic(param.Name, param.Constraint)
	}
	defer func() { c.currentFunction = nil; c.currentReturn = nil; c.Env.TypeParams = previousParams }()
	for _, param := range f.TypeParams {
		if param.Constraint != "" && param.Constraint != "Copy" && param.Constraint != "Numeric" && param.Constraint != "Comparable" {
			return Diag(CatType, param.Tok.Source, param.Tok.Line, param.Tok.Column, "unknown type constraint '%s'", param.Constraint)
		}
	}
	if f.Receiver != nil {
		rt, d := resolveSpec(c.Env, f.Receiver, 0)
		if d != nil {
			return d
		}
		if name := inaccessibleTypeName(rt, f.VisibilityScope, 0); name != "" {
			return Diag(CatType, f.Receiver.Tok.Source, f.Receiver.Tok.Line, f.Receiver.Tok.Column, "type '%s' is private", name)
		}
		sc.Values["self"] = Binding{Type: rt, Mutable: false}
		defaultScope.Values["self"] = Binding{Type: rt, Mutable: false}
	}
	for _, p := range f.Params {
		t, d := resolveSpec(c.Env, p.Type, 0)
		if d != nil {
			return d
		}
		if name := inaccessibleTypeName(t, f.VisibilityScope, 0); name != "" {
			return Diag(CatType, p.Tok.Source, p.Tok.Line, p.Tok.Column, "type '%s' is private", name)
		}
		if sc.local(p.Name) {
			return Diag(CatType, p.Tok.Source, p.Tok.Line, p.Tok.Column, "parameter '%s' is duplicated", p.Name)
		}
		sc.Values[p.Name] = Binding{Type: t, Mutable: false}
	}
	for _, p := range f.Params {
		t, _ := resolveSpec(c.Env, p.Type, 0)
		if p.Default != nil {
			dt, d := c.checkExpr(defaultScope, p.Default, t)
			if d != nil {
				return d
			}
			if !compatible(t, dt) {
				return Diag(CatType, p.Default.Tok.Source, p.Default.Tok.Line, p.Default.Tok.Column, "default value for parameter '%s' expected %s, found %s", p.Name, t, dt)
			}
		}
		defaultScope.Values[p.Name] = Binding{Type: t, Mutable: false}
	}
	rt, d := resolveSpec(c.Env, f.Return, 0)
	if d != nil {
		return d
	}
	if name := inaccessibleTypeName(rt, f.VisibilityScope, 0); name != "" {
		return Diag(CatType, f.Return.Tok.Source, f.Return.Tok.Line, f.Return.Tok.Column, "type '%s' is private", name)
	}
	for _, p := range f.Params {
		t, _ := resolveSpec(c.Env, p.Type, 0)
		if f.Public && !ensurePublicType(t, "", 0) {
			return Diag(CatType, p.Tok.Source, p.Tok.Line, p.Tok.Column, "public function '%s' exposes a private type", f.Name)
		}
	}
	if f.Public && !ensurePublicType(rt, "", 0) {
		return Diag(CatType, f.Tok.Source, f.Tok.Line, f.Tok.Column, "public function '%s' exposes a private return type", f.Name)
	}
	c.currentReturn = rt
	fl := c.checkBlock(sc, f.Body, rt, 0, true)
	if fl.HasError {
		return c.Err
	}
	if rt.Kind != TyNil && !fl.MustReturn {
		return Diag(CatType, f.Tok.Source, f.Tok.Line, f.Tok.Column, "function must return %s; '%s' may finish without returning", rt, f.Name)
	}
	return nil
}
func (c *Checker) checkStatements(sc *Scope, body []*Stmt, rt *Type, loop int, inFn bool) *Diagnostic {
	fl := c.checkBlock(sc, body, rt, loop, inFn)
	if fl.HasError {
		return c.Err
	}
	return nil
}
func (c *Checker) checkBlock(sc *Scope, body []*Stmt, rt *Type, loop int, inFn bool) Flow {
	flow := normalFlow()
	for _, s := range body {
		if !flow.MayFallthrough {
			continue
		}
		f := c.checkStmt(sc, s, rt, loop, inFn)
		if c.Err != nil {
			flow.HasError = true
			return flow
		}
		flow.MustReturn = f.MustReturn
		flow.MayFallthrough = f.MayFallthrough
		flow.MayBreak = flow.MayBreak || f.MayBreak
		flow.MayContinue = flow.MayContinue || f.MayContinue
		flow.Reachable = f.Reachable
	}
	return flow
}
func (c *Checker) checkStmt(sc *Scope, s *Stmt, rt *Type, loop int, inFn bool) Flow {
	switch s.Kind {
	case StLet, StConst:
		var expected *Type
		if s.Annotation != nil {
			var dd *Diagnostic
			expected, dd = resolveSpec(c.Env, s.Annotation, 0)
			if dd != nil {
				c.Err = dd
				return Flow{HasError: true}
			}
			if name := inaccessibleTypeName(expected, sc.VisibilityScope, 0); name != "" {
				c.Err = Diag(CatType, s.Annotation.Tok.Source, s.Annotation.Tok.Line, s.Annotation.Tok.Column, "type '%s' is private", name)
				return Flow{HasError: true}
			}
		}
		t, d := c.checkExpr(sc, s.Init, expected)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		if s.Annotation != nil {
			a := expected
			if !compatible(a, t) {
				c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "binding '%s' expected %s, found %s", s.Name, a, t)
				return Flow{HasError: true}
			}
			t = a
		}
		if sc.local(s.Name) {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "binding '%s' is already defined in this scope", s.Name)
			return Flow{HasError: true}
		}
		if s.Const && !constantExpr(s.Init) {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "const initializer must be a compile-time expression")
			return Flow{HasError: true}
		}
		if s.Const && !TypeConstSafe(t) {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "const type %s is not deeply immutable", t)
			return Flow{HasError: true}
		}
		sc.Values[s.Name] = Binding{Type: t, Mutable: s.Mutable, Const: s.Const, Global: false}
		return normalFlow()
	case StExpr:
		_, d := c.checkExpr(sc, s.Expr, nil)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		return normalFlow()
	case StAssign:
		b, ok := sc.lookup(s.Target.Name)
		if s.Target.Kind != ExVar || !ok {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "assignment target must be a binding")
			return Flow{HasError: true}
		}
		if !b.Mutable {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "immutable binding '%s' cannot be assigned", s.Target.Name)
			return Flow{HasError: true}
		}
		// Assignment places bypass checkExpr, so retain the resolved binding
		// type on the target node for typed IR serialization.
		s.Target.Type = b.Type
		t, d := c.checkExpr(sc, s.Value, b.Type)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		if !compatible(b.Type, t) {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "assignment to '%s' expected %s, found %s", s.Target.Name, b.Type, t)
			return Flow{HasError: true}
		}
		return normalFlow()
	case StIf:
		t, d := c.checkExpr(sc, s.Cond, TBool)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		if !typeEqual(t, TBool) {
			c.Err = Diag(CatType, s.Cond.Tok.Source, s.Cond.Tok.Line, s.Cond.Tok.Column, "condition must be Bool, found %s", t)
			return Flow{HasError: true}
		}
		a := c.checkBlock(NewScope(sc, sc.Worker), s.Then, rt, loop, inFn)
		if c.Err != nil {
			return Flow{HasError: true}
		}
		b := normalFlow()
		if len(s.Else) > 0 {
			b = c.checkBlock(NewScope(sc, sc.Worker), s.Else, rt, loop, inFn)
			if c.Err != nil {
				return Flow{HasError: true}
			}
		}
		return Flow{MustReturn: a.MustReturn && b.MustReturn, MayFallthrough: a.MayFallthrough || b.MayFallthrough, Reachable: true}
	case StWhile:
		t, d := c.checkExpr(sc, s.Cond, TBool)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		if !typeEqual(t, TBool) {
			c.Err = Diag(CatType, s.Cond.Tok.Source, s.Cond.Tok.Line, s.Cond.Tok.Column, "condition must be Bool, found %s", t)
			return Flow{HasError: true}
		}
		c.checkBlock(NewScope(sc, sc.Worker), s.Body, rt, loop+1, inFn)
		if c.Err != nil {
			return Flow{HasError: true}
		}
		return normalFlow()
	case StFor:
		it, d := c.checkExpr(sc, s.Iter, nil)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		var elem *Type
		switch it.Kind {
		case TyArray, TySet:
			elem = it.A
		case TyString:
			elem = TString
		case TyBytes:
			elem = TInt
		default:
			c.Err = Diag(CatType, s.Iter.Tok.Source, s.Iter.Tok.Line, s.Iter.Tok.Column, "for expects Array[T], Set[T], String, or Bytes")
			return Flow{HasError: true}
		}
		loopScope := NewScope(sc, sc.Worker)
		loopScope.Values[s.Name] = Binding{Type: elem, Mutable: false}
		c.checkBlock(loopScope, s.Body, rt, loop+1, inFn)
		if c.Err != nil {
			return Flow{HasError: true}
		}
		return normalFlow()
	case StDefer:
		deferScope := NewScope(sc, sc.Worker)
		c.checkBlock(deferScope, s.Body, rt, loop, inFn)
		if c.Err != nil {
			return Flow{HasError: true}
		}
		return normalFlow()
	case StUnsafe:
		unsafeScope := NewScope(sc, sc.Worker)
		unsafeScope.Unsafe = true
		if c.currentFunction != nil {
			c.currentFunction.Unsafe = true
		}
		c.checkBlock(unsafeScope, s.Body, rt, loop, inFn)
		if c.Err != nil {
			return Flow{HasError: true}
		}
		return normalFlow()
	case StReturn:
		if !inFn {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "return is only valid inside a function")
			return Flow{HasError: true}
		}
		t := TNil
		var d *Diagnostic
		if s.Return != nil {
			t, d = c.checkExpr(sc, s.Return, rt)
		}
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		if !compatible(rt, t) {
			if s.Return == nil || s.Return.Kind != ExPropagate || (rt.Kind != TyOption && rt.Kind != TyResult) || !compatible(rt.A, t) {
				c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "function must return %s; found %s", rt, t)
				return Flow{HasError: true}
			}
		}
		if s.Return != nil && s.Return.Kind == ExCall && c.currentFunction != nil && s.Return.Function == c.currentFunction {
			s.Return.Tail = true
		}
		return returnFlow()
	case StBreak:
		if loop == 0 {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "break is only valid inside a loop")
			return Flow{HasError: true}
		}
		return Flow{MayBreak: true, Reachable: true}
	case StContinue:
		if loop == 0 {
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "continue is only valid inside a loop")
			return Flow{HasError: true}
		}
		return Flow{MayContinue: true, Reachable: true}
	case StMatch:
		t, d := c.checkExpr(sc, s.Scrutinee, nil)
		if d != nil {
			c.Err = d
			return Flow{HasError: true}
		}
		for _, arm := range s.Arms {
			if t.Kind == TyResult && arm.Pattern.Kind == PatNil {
				c.Err = Diag(CatType, arm.Pattern.Tok.Source, arm.Pattern.Tok.Line, arm.Pattern.Tok.Column, "nil pattern does not match Result")
				return Flow{HasError: true}
			}
		}
		ex := c.exhaustive(t, s)
		if !ex {
			label := t.String()
			if t.Kind == TyResult {
				label = "Result"
			}
			c.Err = Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "non-exhaustive match for %s", label)
			return Flow{HasError: true}
		}
		all := true
		fall := false
		for _, a := range s.Arms {
			as := NewScope(sc, sc.Worker)
			if a.Pattern.Binding != "" {
				bt := patternBinding(t, a.Pattern)
				if bt == nil {
					c.Err = Diag(CatType, a.Pattern.Tok.Source, a.Pattern.Tok.Line, a.Pattern.Tok.Column, "invalid pattern binding")
					return Flow{HasError: true}
				}
				as.Values[a.Pattern.Binding] = Binding{Type: bt}
			}
			af := c.checkBlock(as, a.Body, rt, loop, inFn)
			if c.Err != nil {
				return Flow{HasError: true}
			}
			all = all && af.MustReturn
			fall = fall || af.MayFallthrough
		}
		return Flow{MustReturn: all, MayFallthrough: fall, Reachable: true}
	}
	return normalFlow()
}
func patternBinding(t *Type, p Pattern) *Type {
	switch p.Kind {
	case PatOption:
		if t != nil && t.Kind == TyOption {
			return t.A
		}
	case PatResult:
		if t != nil && t.Kind == TyResult {
			if p.OK {
				return t.A
			}
			return t.B
		}
	}
	return nil
}
func (c *Checker) exhaustive(t *Type, s *Stmt) bool {
	for _, a := range s.Arms {
		if a.Pattern.Kind == PatWildcard {
			return true
		}
	}
	seen := map[string]bool{}
	for _, a := range s.Arms {
		p := a.Pattern
		switch p.Kind {
		case PatBool:
			seen[fmt.Sprint(p.Bool)] = true
		case PatNil:
			seen["nil"] = true
		case PatOption:
			if t.Kind != TyOption {
				return false
			}
			seen[fmt.Sprint(p.Present)] = true
		case PatResult:
			if t.Kind != TyResult {
				return false
			}
			seen["ok"] = seen["ok"] || p.OK
			seen["err"] = seen["err"] || !p.OK
		case PatEnum:
			if t.Kind != TyEnum {
				return false
			}
			seen[p.Variant] = true
		case PatInt, PatString:
			seen[fmt.Sprintf("%d:%s", p.Int, p.Str)] = true
		}
	}
	switch t.Kind {
	case TyBool:
		return seen["true"] && seen["false"]
	case TyNil:
		return seen["nil"]
	case TyOption:
		return seen["true"] && seen["false"]
	case TyResult:
		return seen["ok"] && seen["err"]
	case TyEnum:
		if t.Enum == nil {
			return false
		}
		for _, v := range t.Enum.Variants {
			if !seen[v] {
				return false
			}
		}
		return true
	}
	return false
}
func (c *Checker) checkExpr(sc *Scope, e *Expr, expected *Type) (*Type, *Diagnostic) {
	if e == nil {
		return TNil, nil
	}
	if e.Type != nil {
		return e.Type, nil
	}
	var t *Type
	var d *Diagnostic
	switch e.Kind {
	case ExInt:
		t = TInt
	case ExFloat:
		t = TFloat
	case ExBool:
		t = TBool
	case ExNil:
		t = TNil
	case ExString:
		t = TString
	case ExVar:
		b, ok := sc.lookup(e.Name)
		if !ok {
			if _, ok := c.Env.Functions[e.Name]; ok {
				t = TUnknown
			} else {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown variable '%s'", e.Name)
			}
		} else {
			if sc.Worker && b.Global && b.Type.Kind != TyChannel && b.Type.Kind != TyShared {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "global binding '%s' is not available in a worker-safe function", e.Name)
			}
			t = b.Type
		}
	case ExEnum:
		et := c.Env.Types[e.EnumType]
		if et == nil || et.Kind != TyEnum {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown enum '%s'", e.EnumType)
		} else if name := inaccessibleTypeName(et, sc.VisibilityScope, 0); name != "" {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "enum '%s' is private", name)
		} else {
			found := false
			for _, v := range et.Enum.Variants {
				if v == e.EnumVariant {
					found = true
				}
			}
			if !found {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown variant '%s'", e.EnumVariant)
			}
			t = et
		}
	case ExUnary:
		ot, dd := c.checkExpr(sc, e.Operand, nil)
		if dd != nil {
			d = dd
		} else if e.Op == BANG {
			if !typeEqual(ot, TBool) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "'!' expects Bool")
			} else {
				t = TBool
			}
		} else {
			if e.Op == BITNOT {
				if !isUInt(ot) {
					d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "'~' expects UInt8, UInt16, UInt32, or UInt64")
				} else {
					t = ot
				}
			} else if !numeric(ot) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unary sign expects Int, UInt, or Float")
			} else if isUInt(ot) && e.Op == MINUS {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unary '-' is not defined for UInt; use wrapping subtraction")
			} else {
				t = ot
			}
		}
	case ExBinary:
		lt, dd := c.checkExpr(sc, e.Left, nil)
		if dd != nil {
			d = dd
			break
		}
		if e.Op == AND || e.Op == OR {
			if !typeEqual(lt, TBool) {
				d = Diag(CatType, e.Left.Tok.Source, e.Left.Tok.Line, e.Left.Tok.Column, "logical operators require Bool")
			}
			rt, dd := c.checkExpr(sc, e.Right, TBool)
			if dd != nil {
				d = dd
			} else if !typeEqual(rt, TBool) {
				d = Diag(CatType, e.Right.Tok.Source, e.Right.Tok.Line, e.Right.Tok.Column, "logical operators require Bool")
			}
			t = TBool
			break
		}
		rightExpected := lt
		if e.Op == SHL || e.Op == SHR {
			rightExpected = TInt
		}
		rt, dd := c.checkExpr(sc, e.Right, rightExpected)
		if dd != nil {
			d = dd
			break
		}
		if e.Op == PLUS && (typeEqual(lt, TString) || typeEqual(lt, TBytes) || lt.Kind == TyArray) {
			if !compatible(lt, rt) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "'+' operands must have matching types")
			} else {
				t = lt
			}
			break
		}
		if e.Op == EQEQ || e.Op == NEQ {
			if !compatible(lt, rt) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "equality operands must have the same type")
			}
			t = TBool
			break
		}
		if e.Op == BITAND || e.Op == BITXOR || e.Op == PIPE {
			if !isUInt(lt) || !typeEqual(lt, rt) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "bitwise operands must have matching UInt types")
			} else {
				t = lt
			}
			break
		}
		if e.Op == SHL || e.Op == SHR {
			if !isUInt(lt) || !typeEqual(rt, TInt) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "shift expects a UInt left operand and an Int count")
			} else {
				t = lt
			}
			break
		}
		if e.Op == LESS || e.Op == LEQ || e.Op == GREATER || e.Op == GEQ {
			if !numeric(lt) || !typeEqual(lt, rt) {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "ordered operands must have matching numeric types")
			} else {
				t = TBool
			}
			break
		}
		if !numeric(lt) || !typeEqual(lt, rt) {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "arithmetic operands must have matching numeric types")
		} else {
			t = lt
		}
	case ExMap:
		if len(e.MapKeys) == 0 {
			if expected != nil && expected.Kind == TyMap {
				t = expected
			} else {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "empty map requires Map[K, V] context")
			}
			break
		}
		var kt, vt *Type
		if expected != nil && expected.Kind == TyMap {
			kt, vt = expected.A, expected.B
		}
		for i, key := range e.MapKeys {
			got, dd := c.checkExpr(sc, key, kt)
			if dd != nil {
				d = dd
				break
			}
			if kt == nil {
				kt = got
			} else if !compatible(kt, got) {
				d = Diag(CatType, key.Tok.Source, key.Tok.Line, key.Tok.Column, "map keys must have one homogeneous type")
				break
			}
			if kt.Kind != TyInt && kt.Kind != TyUInt && kt.Kind != TyBool && kt.Kind != TyString {
				d = Diag(CatType, key.Tok.Source, key.Tok.Line, key.Tok.Column, "map keys must be Int, UInt, Bool, or String")
				break
			}
			got, dd = c.checkExpr(sc, e.Values[i], vt)
			if dd != nil {
				d = dd
				break
			}
			if vt == nil {
				vt = got
			} else if !compatible(vt, got) {
				d = Diag(CatType, e.Values[i].Tok.Source, e.Values[i].Tok.Line, e.Values[i].Tok.Column, "map values must have one homogeneous type")
				break
			}
		}
		if d == nil {
			t = MapOf(kt, vt)
		}
	case ExSet:
		if len(e.Items) == 0 {
			if expected != nil && expected.Kind == TySet {
				t = expected
			} else {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "empty set requires Set[T] context")
			}
			break
		}
		var et *Type
		if expected != nil && expected.Kind == TySet {
			et = expected.A
		}
		for _, x := range e.Items {
			xt, dd := c.checkExpr(sc, x, et)
			if dd != nil {
				d = dd
				break
			}
			if et == nil {
				et = xt
			} else if !compatible(et, xt) {
				d = Diag(CatType, x.Tok.Source, x.Tok.Line, x.Tok.Column, "set elements must have one homogeneous type")
				break
			}
		}
		if d == nil {
			t = SetOf(et)
		}
	case ExArray:
		if len(e.Items) == 0 {
			if expected != nil && expected.Kind == TyArray {
				t = expected
			} else {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "empty array requires Array[T] context")
			}
			break
		}
		et := expected
		if et != nil && et.Kind == TyArray {
			et = et.A
			if et.Kind == TyUnknown {
				et = nil
			}
		} else {
			et = nil
		}
		for _, x := range e.Items {
			xt, dd := c.checkExpr(sc, x, et)
			if dd != nil {
				d = dd
				break
			}
			if et == nil {
				et = xt
			} else if !compatible(et, xt) {
				d = Diag(CatType, x.Tok.Source, x.Tok.Line, x.Tok.Column, "array elements must have one homogeneous type")
			}
		}
		if d == nil {
			t = Arr(et)
		}
	case ExIndex:
		bt, dd := c.checkExpr(sc, e.Base, nil)
		if dd != nil {
			d = dd
			break
		}
		wantIndex := TInt
		if bt.Kind == TyMap {
			wantIndex = bt.A
		}
		it, dd := c.checkExpr(sc, e.Left, wantIndex)
		if dd != nil {
			d = dd
			break
		}
		if bt.Kind != TyMap && !typeEqual(it, TInt) {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "index must be Int")
		} else if bt.Kind == TyArray {

			t = bt.A
		} else if typeEqual(bt, TString) {
			t = TString
		} else if typeEqual(bt, TBytes) {
			t = TInt
		} else if bt.Kind == TyMap {
			if !compatible(bt.A, it) {
				d = Diag(CatType, e.Left.Tok.Source, e.Left.Tok.Line, e.Left.Tok.Column, "map index has type %s; expected %s", it, bt.A)
			} else {
				t = bt.B
			}
		} else {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "indexing expects String, Bytes, Array[T], or Map[K,V]")
		}
	case ExField:
		bt, dd := c.checkExpr(sc, e.Base, nil)
		if dd != nil {
			d = dd
			break
		}
		if bt.Kind != TyStruct || bt.Struct == nil {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "field access expects a struct")
			break
		}
		found := false
		for _, f := range bt.Struct.Fields {
			if f.Name == e.Field {
				found = true
				if !f.Public && bt.Struct.VisibilityScope != sc.VisibilityScope {
					d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "field '%s' is private", e.Field)
					break
				}
				t = f.Type
			}
		}
		if !found && d == nil {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown field '%s'", e.Field)
		}
	case ExStruct:
		st := c.Env.Types[e.StructName]
		if st == nil || st.Kind != TyStruct {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown struct '%s'", e.StructName)
			break
		} else if name := inaccessibleTypeName(st, sc.VisibilityScope, 0); name != "" {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "struct '%s' is private", name)
			break
		}
		if len(e.Fields) != len(st.Struct.Fields) {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "struct '%s' has wrong field count", e.StructName)
			break
		}
		seen := map[string]bool{}
		for i, n := range e.Fields {
			if seen[n] {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "duplicate struct field '%s'", n)
				break
			}
			seen[n] = true
			var ft *Type
			for _, f := range st.Struct.Fields {
				if f.Name == n {
					ft = f.Type
				}
			}
			if ft == nil {
				d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown field '%s'", n)
				break
			}
			for _, f := range st.Struct.Fields {
				if f.Name == n && !f.Public && st.Struct.VisibilityScope != sc.VisibilityScope {
					d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "field '%s' is private", n)
				}
			}
			vt, dd := c.checkExpr(sc, e.Values[i], ft)
			if dd != nil {
				d = dd
				break
			}
			if !compatible(ft, vt) {
				d = Diag(CatType, e.Values[i].Tok.Source, e.Values[i].Tok.Line, e.Values[i].Tok.Column, "field '%s' expected %s, found %s", n, ft, vt)
			}
		}
		t = st
	case ExPropagate:
		inner, dd := c.checkExpr(sc, e.Operand, c.currentReturn)
		if dd != nil {
			d = dd
			break
		}
		if c.currentReturn == nil {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "'?' is only valid inside a function returning Option or Result")
			break
		}
		if inner.Kind == TyOption && c.currentReturn.Kind == TyOption {
			t = inner.A
		} else if inner.Kind == TyResult && c.currentReturn.Kind == TyResult && compatible(inner.B, c.currentReturn.B) {
			t = inner.A
		} else {
			d = Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "'?' requires an Option or Result matching the enclosing function return type")
		}
	case ExCall:
		t, d = c.checkCall(sc, e, expected)

	}
	if d == nil && t == nil {
		t = TError
	}
	if d == nil {
		e.Type = t
		foldConstExpr(e)
	}
	return t, d
}
func (c *Checker) checkCall(sc *Scope, e *Expr, expected *Type) (*Type, *Diagnostic) {
	if e.Receiver != nil {
		rt, d := c.checkExpr(sc, e.Receiver, nil)
		if d != nil {
			return TError, d
		}
		key := methodKey(rt, e.Name)
		candidates := c.Env.Overloads[key]
		if len(candidates) == 0 && c.Env.Functions[key] != nil {
			candidates = []*Function{c.Env.Functions[key]}
		}
		if len(candidates) == 0 {
			return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown method '%s' for %s", e.Name, rt)
		}
		visible := false
		var matched *Function
		var matchedType *Type
		for _, candidate := range candidates {
			if !candidate.Public && candidate.VisibilityScope != sc.VisibilityScope {
				continue
			}
			visible = true
			if returnType, ok := c.matchFunctionCall(sc, e, candidate, expected); ok {
				if matched != nil {
					return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "ambiguous call to method '%s': multiple overloads match", e.Name)
				}
				matched, matchedType = candidate, returnType
			}
		}
		if !visible {
			return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown method '%s' for %s", e.Name, rt)
		}
		if matched == nil {
			return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "no overload of method '%s' matches the argument types", e.Name)
		}
		e.Function = matched
		return matchedType, nil
	}
	b, ok := c.Env.Builtins[e.Name]
	if ok {
		if sc.Worker && (strings.HasPrefix(b.Name, "fs_") || b.Name == "env_get" || b.Name == "thread_spawn") {
			return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "builtin '%s' is not available in a worker-safe function", e.Name)
		}
		if len(e.Args) != b.Arity {
			return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "builtin '%s' expects %d argument(s), got %d", e.Name, b.Arity, len(e.Args))
		}
		return c.checkBuiltin(sc, e, b, expected)
	}
	candidates := c.Env.Overloads[e.Name]
	if len(candidates) == 0 && c.Env.Functions[e.Name] != nil {
		candidates = []*Function{c.Env.Functions[e.Name]}
	}
	if len(candidates) == 0 {
		return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown function '%s'", e.Name)
	}
	visible := false
	var matched *Function
	var matchedType *Type
	for _, f := range candidates {
		if f.Public || f.VisibilityScope == sc.VisibilityScope {
			visible = true
			if rt, ok := c.matchFunctionCall(sc, e, f, expected); ok {
				if matched != nil {
					return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "ambiguous call to '%s': multiple overloads match", e.Name)
				}
				matched, matchedType = f, rt
			}
		}
	}
	if !visible {
		return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "unknown function '%s'", e.Name)
	}
	if matched != nil {
		e.Function = matched
		return matchedType, nil
	}
	return TError, Diag(CatType, e.Tok.Source, e.Tok.Line, e.Tok.Column, "no overload of '%s' matches the argument types", e.Name)
}

func (c *Checker) matchFunctionCall(sc *Scope, e *Expr, f *Function, expectedReturn *Type) (*Type, bool) {
	if len(e.Args) < minArgs(f) || len(e.Args) > len(f.Params) {
		return nil, false
	}
	previous := c.Env.TypeParams
	c.Env.TypeParams = map[string]*Type{}
	constraints := make(map[string]string, len(f.TypeParams))
	inferred := make(map[string]bool, len(f.TypeParams))
	for _, param := range f.TypeParams {
		c.Env.TypeParams[param.Name] = Generic(param.Name, param.Constraint)
		constraints[param.Name] = param.Constraint
	}
	defer func() { c.Env.TypeParams = previous }()
	for i, arg := range e.Args {
		paramSpec := f.Params[i].Type
		argumentExpected, err := resolveSpec(c.Env, paramSpec, 0)
		if err != nil {
			return nil, false
		}
		actual, d := c.checkExpr(sc, arg, argumentExpected)
		if d != nil || !unifyGenericSpec(c.Env, paramSpec, actual, 0, constraints, inferred) {
			return nil, false
		}
	}
	if expectedReturn != nil && functionReturnContainsGeneric(f.Return, f.TypeParams) {
		if !unifyGenericSpec(c.Env, f.Return, expectedReturn, 0, constraints, inferred) {
			return nil, false
		}
	}
	for _, param := range f.TypeParams {
		if !inferred[param.Name] {
			return nil, false
		}
	}
	rt, d := resolveSpec(c.Env, f.Return, 0)
	if d != nil || expectedReturn != nil && functionReturnContainsGeneric(f.Return, f.TypeParams) && !compatible(expectedReturn, rt) {
		return nil, false
	}
	return rt, true
}

func functionReturnContainsGeneric(spec *TypeSpec, params []TypeParam) bool {
	if spec == nil {
		return false
	}
	for _, param := range params {
		if len(spec.Params) == 0 && spec.Name == param.Name {
			return true
		}
	}
	for _, child := range spec.Params {
		if functionReturnContainsGeneric(child, params) {
			return true
		}
	}
	return false
}

func unifyGenericSpec(env *TypeEnv, spec *TypeSpec, actual *Type, depth int, constraints map[string]string, inferred map[string]bool) bool {
	if spec == nil || actual == nil || depth > env.Lim.MaxTypeDepth {
		return false
	}
	if len(spec.Params) == 0 {
		if generic := env.TypeParams[spec.Name]; generic != nil {
			if actual.Kind == TyGeneric && actual.Name == spec.Name {
				return true
			}
			if inferred[spec.Name] {
				return typeEqual(generic, actual)
			}
			if !satisfiesConstraint(actual, constraints[spec.Name]) {
				return false
			}
			env.TypeParams[spec.Name] = actual
			inferred[spec.Name] = true
			return true
		}
		expected, err := resolveSpec(env, spec, depth)
		return err == nil && compatible(expected, actual)
	}
	shape, err := resolveSpec(env, spec, depth)
	if err != nil || shape.Kind != actual.Kind {
		return false
	}
	actualArgs := typeArguments(actual)
	if len(spec.Params) != len(actualArgs) {
		return false
	}
	for i, child := range spec.Params {
		if !unifyGenericSpec(env, child, actualArgs[i], depth+1, constraints, inferred) {
			return false
		}
	}
	return true
}

func typeArguments(t *Type) []*Type {
	if t == nil {
		return nil
	}
	switch t.Kind {
	case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
		return []*Type{t.A}
	case TyResult, TyMap:
		return []*Type{t.A, t.B}
	default:
		return nil
	}
}

// minArgs returns the number of required (non-defaulted) parameters.
func minArgs(f *Function) int {
	n := 0
	for _, p := range f.Params {
		if p.Default == nil {
			n++
		}
	}
	return n
}

func satisfiesConstraint(t *Type, constraint string) bool {
	if t != nil && t.Kind == TyGeneric {
		inherited := ""
		if t.B != nil {
			inherited = t.B.Name
		}
		switch constraint {
		case "", "Any":
			return true
		case "Copy":
			return inherited == "Copy" || inherited == "Numeric"
		case "Numeric":
			return inherited == "Numeric"
		case "Comparable":
			return inherited == "Comparable" || inherited == "Numeric"
		default:
			return false
		}
	}
	switch constraint {
	case "", "Any":
		return t != nil && t.Kind != TyError && t.Kind != TyUnknown
	case "Copy":
		return TypeCopyable(t)
	case "Numeric":
		return numeric(t)
	case "Comparable":
		return t != nil && (t.Kind == TyNil || t.Kind == TyInt || t.Kind == TyUInt || t.Kind == TyFloat || t.Kind == TyBool || t.Kind == TyString || t.Kind == TyBytes || t.Kind == TyEnum)
	default:
		return false
	}
}
