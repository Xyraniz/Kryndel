package kry

import (
	"fmt"
	"sort"
)

func buildKIRDocument(p *Program, c *Checker, target NativeTarget) (*KIRDocument, error) {
	if p == nil || c == nil || c.Env == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	metadata, err := buildKIRMetadata(p, target)
	if err != nil {
		return nil, err
	}
	paths := newKIRPathNames(p)
	d := &KIRDocument{
		Format: metadata.Format, Version: metadata.Version, LanguageVersion: metadata.LanguageVersion, Module: metadata.Module,
		Source: metadata.Source, Target: metadata.Target,
		Imports: cloneKIRStrings(metadata.Imports), ImportRecords: cloneKIRImports(metadata.ImportRecords), Sources: cloneKIRStrings(metadata.Sources),
		Structs: cloneKIRStructs(metadata.Structs), Enums: cloneKIREnums(metadata.Enums), Traits: cloneKIRTraits(metadata.Traits), TraitImpls: cloneKIRTraitImpls(metadata.TraitImpls),
		Functions: make([]*KIRFunction, 0, len(p.Functions)), Statements: make([]*KIRStmt, 0, len(p.Statements)),
	}
	functionTargets := kirFunctionTargets(p, paths)
	for _, x := range p.Functions {
		f := &KIRFunction{Name: x.Name, Source: paths.tokenSource(x.Tok), Line: x.Tok.Line, Column: x.Tok.Column, Span: kirSourceSpan(x.Tok, x.EndToken), Public: x.Public, Worker: x.Worker, Unsafe: x.Unsafe, Trait: x.Trait, Module: paths.name(x.Module), Receiver: typeSpecString(x.Receiver), Return: typeSpecString(x.Return), TypeParams: make([]*KIRTypeParam, 0, len(x.TypeParams)), Params: make([]*KIRParam, 0, len(x.Params)), Captures: []*KIRCapture{}, Body: kirStmts(x.Body, c, functionTargets, paths)}
		for _, tp := range x.TypeParams {
			f.TypeParams = append(f.TypeParams, &KIRTypeParam{Name: tp.Name, Constraint: tp.Constraint, Source: paths.tokenSource(tp.Tok), Line: tp.Tok.Line, Column: tp.Tok.Column, Span: kirTokenSpan(tp.Tok)})
		}
		for _, param := range x.Params {
			f.Params = append(f.Params, &KIRParam{Name: param.Name, Type: typeSpecString(param.Type), Source: paths.tokenSource(param.Tok), Line: param.Tok.Line, Column: param.Tok.Column, Span: kirSourceSpan(param.Tok, param.EndToken), Default: kirExpr(param.Default, c, functionTargets, paths), Binding: kirBinding(param.Name, typeSpecString(param.Type), false, param.Tok, paths)})
		}
		d.Functions = append(d.Functions, f)
	}
	for _, x := range p.Statements {
		d.Statements = append(d.Statements, kirStmt(x, c, functionTargets, paths))
	}
	return d, nil
}

func kirStmts(in []*Stmt, c *Checker, functionTargets map[*Function]string, paths kirPathNames) []*KIRStmt {
	out := make([]*KIRStmt, 0, len(in))
	for _, x := range in {
		out = append(out, kirStmt(x, c, functionTargets, paths))
	}
	return out
}

func kirStmt(s *Stmt, c *Checker, functionTargets map[*Function]string, paths kirPathNames) *KIRStmt {
	if s == nil {
		return nil
	}
	k := &KIRStmt{Kind: stmtName(s.Kind), Source: paths.tokenSource(s.Tok), Line: s.Tok.Line, Column: s.Tok.Column, Span: kirSourceSpan(s.Tok, s.EndToken), Name: s.Name, Mutable: s.Mutable, Const: s.Const, Annotation: typeSpecString(s.Annotation), Init: kirExpr(s.Init, c, functionTargets, paths), Expr: kirExpr(s.Expr, c, functionTargets, paths), Target: kirExpr(s.Target, c, functionTargets, paths), Value: kirExpr(s.Value, c, functionTargets, paths), Cond: kirExpr(s.Cond, c, functionTargets, paths), Then: kirStmts(s.Then, c, functionTargets, paths), Else: kirStmts(s.Else, c, functionTargets, paths), Body: kirStmts(s.Body, c, functionTargets, paths), Iter: kirExpr(s.Iter, c, functionTargets, paths), Return: kirExpr(s.Return, c, functionTargets, paths), Scrutinee: kirExpr(s.Scrutinee, c, functionTargets, paths), Arms: make([]*KIRArm, 0, len(s.Arms))}
	if s.Kind == StLet || s.Kind == StConst || s.Kind == StFor {
		typ := typeString(s.Type, s.Annotation)
		k.Binding = kirBinding(s.Name, typ, s.Mutable, s.NameToken, paths)
	}
	for _, arm := range s.Arms {
		endToken := arm.Pattern.EndToken
		if len(arm.Body) > 0 {
			endToken = arm.Body[len(arm.Body)-1].EndToken
		}
		k.Arms = append(k.Arms, &KIRArm{Source: paths.tokenSource(arm.Pattern.Tok), Line: arm.Pattern.Tok.Line, Column: arm.Pattern.Tok.Column, Span: kirSourceSpan(arm.Pattern.Tok, endToken), Pattern: kirPattern(arm.Pattern, paths), Body: kirStmts(arm.Body, c, functionTargets, paths)})
	}
	return k
}

func kirPattern(p Pattern, paths kirPathNames) *KIRPattern {
	k := &KIRPattern{Kind: patternName(p.Kind), Source: paths.tokenSource(p.Tok), Line: p.Tok.Line, Column: p.Tok.Column, Span: kirSourceSpan(p.Tok, p.EndToken), Bool: p.Bool, Int: p.Int, String: p.Str, Type: p.TypeName, Variant: p.Variant, Binding: p.Binding, Present: p.Present, OK: p.OK}
	if p.Binding != "" {
		k.ResolvedBinding = kirBinding(p.Binding, typeString(p.BindingType, nil), false, p.BindingTok, paths)
	}
	return k
}

func kirExpr(e *Expr, c *Checker, functionTargets map[*Function]string, paths kirPathNames) *KIRExpr {
	if e == nil {
		return nil
	}
	k := kirExprScalars(e, c, functionTargets, paths)
	k.Args = make([]*KIRExpr, 0, len(e.Args))
	k.Items = make([]*KIRExpr, 0, len(e.Items))
	k.MapKeys = make([]*KIRExpr, 0, len(e.MapKeys))
	k.Values = make([]*KIRExpr, 0, len(e.Values))
	k.Left = kirExpr(e.Left, c, functionTargets, paths)
	k.Right = kirExpr(e.Right, c, functionTargets, paths)
	k.Operand = kirExpr(e.Operand, c, functionTargets, paths)
	k.Base = kirExpr(e.Base, c, functionTargets, paths)
	k.Receiver = kirExpr(e.Receiver, c, functionTargets, paths)
	for _, x := range e.Args {
		k.Args = append(k.Args, kirExpr(x, c, functionTargets, paths))
	}
	for _, x := range e.Items {
		k.Items = append(k.Items, kirExpr(x, c, functionTargets, paths))
	}
	for _, x := range e.MapKeys {
		k.MapKeys = append(k.MapKeys, kirExpr(x, c, functionTargets, paths))
	}
	for _, x := range e.Values {
		k.Values = append(k.Values, kirExpr(x, c, functionTargets, paths))
	}
	if e.ConstValue != nil {
		k.Const = kirValue(*e.ConstValue)
	}
	if e.Kind == ExVar && e.Function == nil && e.Scope != nil {
		if binding, _, ok := e.Scope.lookupOwner(e.Name); ok {
			k.Binding = kirBinding(e.Name, typeString(binding.Type, nil), binding.Mutable, binding.Token, paths)
		}
	}
	if e.Kind == ExCall && e.Function == nil && e.Receiver == nil && e.Callee != nil {
		k.Callee = kirExpr(e.Callee, c, functionTargets, paths)
	}
	return k
}

func kirLambda(function *Function, captures []Capture, c *Checker, functionTargets map[*Function]string, paths kirPathNames) *KIRFunction {
	if function == nil {
		return nil
	}
	k := &KIRFunction{Name: function.Name, Source: paths.tokenSource(function.Tok), Line: function.Tok.Line, Column: function.Tok.Column, Span: kirSourceSpan(function.Tok, function.EndToken), Worker: false, Unsafe: function.Unsafe, Module: paths.name(function.Module), Return: typeSpecString(function.Return), TypeParams: []*KIRTypeParam{}, Params: make([]*KIRParam, 0, len(function.Params)), Captures: make([]*KIRCapture, 0, len(captures)), Body: kirStmts(function.Body, c, functionTargets, paths)}
	for _, parameter := range function.Params {
		k.Params = append(k.Params, &KIRParam{Name: parameter.Name, Type: typeSpecString(parameter.Type), Source: paths.tokenSource(parameter.Tok), Line: parameter.Tok.Line, Column: parameter.Tok.Column, Span: kirSourceSpan(parameter.Tok, parameter.EndToken), Default: nil, Binding: kirBinding(parameter.Name, typeSpecString(parameter.Type), false, parameter.Tok, paths)})
	}
	ordered := append([]Capture(nil), captures...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i].Token, ordered[j].Token
		aSource, bSource := paths.tokenSource(a), paths.tokenSource(b)
		if aSource != bSource {
			return aSource < bSource
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return ordered[i].Name < ordered[j].Name
	})
	for _, capture := range ordered {
		k.Captures = append(k.Captures, &KIRCapture{Binding: kirBinding(capture.Name, capture.Type.String(), capture.Mutable, capture.Token, paths)})
	}
	return k
}

func kirValue(v Value) *KIRValue {
	items := arrayValues(v)
	k := &KIRValue{Kind: valueName(v.Kind), Int: v.I, UInt: v.U, UIntBits: v.UBits, Float: v.F, Bool: v.Bool, String: v.S, Bytes: append([]byte(nil), v.Bytes...), Present: v.Present, OK: v.OK, Array: make([]*KIRValue, 0, len(items))}
	for _, x := range items {
		k.Array = append(k.Array, kirValue(x))
	}
	if v.Inner != nil {
		k.Inner = kirValue(*v.Inner)
	}
	return k
}
