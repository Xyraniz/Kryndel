package kry

import "fmt"

type TypeKind int

const (
	TyError TypeKind = iota
	TyUnknown
	TyVoid
	TyNil
	TyInt
	TyFloat
	TyBool
	TyString
	TyBytes
	TyArray
	TyOption
	TyResult
	TyChannel
	TyThread
	TyStruct
	TyEnum
)

type Type struct {
	Kind   TypeKind
	Name   string
	A, B   *Type
	Struct *StructDecl
	Enum   *EnumDecl
}

var (
	TError   = &Type{Kind: TyError, Name: "<error>"}
	TUnknown = &Type{Kind: TyUnknown, Name: "<unknown>"}
	TVoid    = &Type{Kind: TyVoid, Name: "Void"}
	TNil     = &Type{Kind: TyNil, Name: "Nil"}
	TInt     = &Type{Kind: TyInt, Name: "Int"}
	TFloat   = &Type{Kind: TyFloat, Name: "Float"}
	TBool    = &Type{Kind: TyBool, Name: "Bool"}
	TString  = &Type{Kind: TyString, Name: "String"}
	TBytes   = &Type{Kind: TyBytes, Name: "Bytes"}
)

func Arr(t *Type) *Type        { return &Type{Kind: TyArray, Name: "Array", A: t} }
func Opt(t *Type) *Type        { return &Type{Kind: TyOption, Name: "Option", A: t} }
func Res(a, b *Type) *Type     { return &Type{Kind: TyResult, Name: "Result", A: a, B: b} }
func Chan(t *Type) *Type       { return &Type{Kind: TyChannel, Name: "Channel", A: t} }
func TypeThread(t *Type) *Type { return &Type{Kind: TyThread, Name: "Thread", A: t} }
func (t *Type) String() string {
	if t == nil {
		return "<unknown>"
	}
	switch t.Kind {
	case TyArray:
		return "Array[" + t.A.String() + "]"
	case TyOption:
		return "Option[" + t.A.String() + "]"
	case TyResult:
		return "Result[" + t.A.String() + ", " + t.B.String() + "]"
	case TyChannel:
		return "Channel[" + t.A.String() + "]"
	case TyThread:
		return "Thread[" + t.A.String() + "]"
	}
	if t.Name != "" {
		return t.Name
	}
	return "<unknown>"
}
func typeEqual(a, b *Type) bool {
	seen := map[[2]*Type]bool{}
	var eq func(*Type, *Type, int) bool
	eq = func(x, y *Type, d int) bool {
		if d > 128 || x == nil || y == nil {
			return false
		}
		if x.Kind == TyUnknown || y.Kind == TyUnknown {
			return false
		}
		if x.Kind != y.Kind {
			return false
		}
		if x.Kind == TyStruct {
			return x.Struct == y.Struct
		}
		if x.Kind == TyEnum {
			return x.Enum == y.Enum
		}
		k := [2]*Type{x, y}
		if seen[k] {
			return true
		}
		seen[k] = true
		switch x.Kind {
		case TyArray, TyOption, TyChannel, TyThread:
			return eq(x.A, y.A, d+1)
		case TyResult:
			return eq(x.A, y.A, d+1) && eq(x.B, y.B, d+1)
		default:
			return true
		}
	}
	return eq(a, b, 0)
}
func typeKnown(t *Type) bool {
	if t == nil || t.Kind == TyUnknown || t.Kind == TyError {
		return false
	}
	switch t.Kind {
	case TyArray, TyOption, TyChannel, TyThread:
		return typeKnown(t.A)
	case TyResult:
		return typeKnown(t.A) && typeKnown(t.B)
	}
	return true
}
func numeric(t *Type) bool { return t != nil && (t.Kind == TyInt || t.Kind == TyFloat) }

type copyState uint8

const (
	copyUnknown copyState = iota
	copyVisiting
	copyable
	nonCopyable
)

func TypeCopyable(root *Type) bool {
	states := map[*Type]copyState{}
	var visit func(*Type, int) bool
	visit = func(t *Type, d int) bool {
		if t == nil || d > 128 {
			return false
		}
		s := states[t]
		if s == copyable {
			return true
		}
		if s == nonCopyable || s == copyVisiting {
			return false
		}
		states[t] = copyVisiting
		ok := false
		switch t.Kind {
		case TyNil, TyInt, TyFloat, TyBool, TyString, TyBytes, TyEnum:
			ok = true
		case TyArray, TyOption:
			ok = visit(t.A, d+1)
		case TyResult:
			ok = visit(t.A, d+1) && visit(t.B, d+1)
		case TyStruct:
			ok = true
			if t.Struct == nil {
				ok = false
			} else {
				for _, f := range t.Struct.Fields {
					if !visit(f.Type, d+1) {
						ok = false
						break
					}
				}
			}
		default:
			ok = false
		}
		if ok {
			states[t] = copyable
		} else {
			states[t] = nonCopyable
		}
		return ok
	}
	return visit(root, 0)
}
func TypeSpecString(s *TypeSpec) string {
	if s == nil {
		return "Nil"
	}
	if len(s.Params) == 0 {
		return s.Name
	}
	out := s.Name + "["
	for i, p := range s.Params {
		if i > 0 {
			out += ", "
		}
		out += TypeSpecString(p)
	}
	return out + "]"
}
func resolveSpec(env *TypeEnv, s *TypeSpec, depth int) (*Type, *Diagnostic) {
	if depth > env.Lim.MaxTypeDepth {
		return TError, Diag(CatResource, s.Tok.Source, s.Tok.Line, s.Tok.Column, "type depth limit exceeded")
	}
	if s == nil {
		return TNil, nil
	}
	name := s.Name
	if len(s.Params) == 0 {
		switch name {
		case "Void":
			return TVoid, nil
		case "Nil":
			return TNil, nil
		case "Int":
			return TInt, nil
		case "Float":
			return TFloat, nil
		case "Bool":
			return TBool, nil
		case "String":
			return TString, nil
		case "Bytes":
			return TBytes, nil
		case "Array":
			return Arr(TUnknown), nil
		}
	}
	switch name {
	case "Array":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return Arr(a), d
		}
	case "Option":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return Opt(a), d
		}
	case "Result":
		if len(s.Params) == 2 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			if d != nil {
				return TError, d
			}
			b, d := resolveSpec(env, s.Params[1], depth+1)
			return Res(a, b), d
		}
	case "Channel":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return Chan(a), d
		}
	case "Thread":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return TypeThread(a), d
		}
	}
	if len(s.Params) == 0 {
		if t := env.Types[name]; t != nil {
			return t, nil
		}
	}
	return TError, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "unknown or malformed type '%s'", name)
}

type TypeEnv struct {
	Types     map[string]*Type
	Functions map[string]*Function
	Builtins  map[string]Builtin
	Lim       Limits
	Module    string
}

func typeDecls(prog *Program, lim Limits) (*TypeEnv, *Diagnostic) {
	e := &TypeEnv{Types: map[string]*Type{}, Functions: map[string]*Function{}, Lim: lim, Module: prog.Module}
	for _, s := range prog.Structs {
		if _, ok := e.Types[s.Name]; ok {
			return nil, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "declaration '%s' is already defined", s.Name)
		}
		s.Type = &Type{Kind: TyStruct, Name: s.Name, Struct: s}
		e.Types[s.Name] = s.Type
	}
	for _, d := range prog.Enums {
		if _, ok := e.Types[d.Name]; ok {
			return nil, Diag(CatType, d.Tok.Source, d.Tok.Line, d.Tok.Column, "declaration '%s' is already defined", d.Name)
		}
		d.Type = &Type{Kind: TyEnum, Name: d.Name, Enum: d}
		e.Types[d.Name] = d.Type
	}
	for _, f := range prog.Functions {
		if _, ok := e.Functions[f.Name]; ok {
			return nil, Diag(CatType, f.Tok.Source, f.Tok.Line, f.Tok.Column, "function '%s' is already defined", f.Name)
		}
		e.Functions[f.Name] = f
	}
	for _, s := range prog.Structs {
		seenFields := map[string]bool{}
		for i := range s.Fields {
			if seenFields[s.Fields[i].Name] {
				return nil, Diag(CatType, s.Fields[i].Tok.Source, s.Fields[i].Tok.Line, s.Fields[i].Tok.Column, "duplicate field '%s'", s.Fields[i].Name)
			}
			seenFields[s.Fields[i].Name] = true
			t, d := resolveSpec(e, s.Fields[i].Spec, 0)
			if d != nil {
				return nil, d
			}
			s.Fields[i].Type = t
			if s.Public && !ensurePublicType(t, "", 0) {
				return nil, Diag(CatType, s.Fields[i].Tok.Source, s.Fields[i].Tok.Line, s.Fields[i].Tok.Column, "public struct '%s' exposes a private field type", s.Name)
			}
		}
	}
	return e, nil
}
func ensurePublicType(t *Type, local string, depth int) bool {
	if t == nil || depth > 128 {
		return false
	}
	switch t.Kind {
	case TyStruct:
		if t.Struct == nil || (!t.Struct.Public && t.Struct.Module != local) {
			return false
		}
		for _, f := range t.Struct.Fields {
			if !ensurePublicType(f.Type, local, depth+1) {
				return false
			}
		}
		return true
	case TyEnum:
		return t.Enum != nil && (t.Enum.Public || t.Enum.Module == local)
	case TyArray, TyOption, TyChannel, TyThread:
		return ensurePublicType(t.A, local, depth+1)
	case TyResult:
		return ensurePublicType(t.A, local, depth+1) && ensurePublicType(t.B, local, depth+1)
	}
	return true
}

var _ = fmt.Sprintf
