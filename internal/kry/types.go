package kry

import (
	"fmt"
	"strings"
)

type TypeKind int

const (
	TyError TypeKind = iota
	TyUnknown
	TyVoid
	TyNil
	TyInt
	TyUInt
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
	TyMap
	TySet
	TyJSON
	TyWebSocket
	TyGeneric
	TyActor
	TyShared
	TyTaskGroup
	TyRegex
	TyRandom
	TySQLite
	TyTCPSocket
	TyTCPListener
	TyUDPSocket
	TyFFILibrary
	TyFFISymbol
	TyFFIBuffer
	TyFunction
)

type Type struct {
	Kind   TypeKind
	Name   string
	Bits   uint8
	A, B   *Type
	Struct *StructDecl
	Enum   *EnumDecl
	Params []*Type
	Return *Type
}

var (
	TError   = &Type{Kind: TyError, Name: "<error>"}
	TUnknown = &Type{Kind: TyUnknown, Name: "<unknown>"}
	TVoid    = &Type{Kind: TyVoid, Name: "Void"}
	TNil     = &Type{Kind: TyNil, Name: "Nil"}
	TInt     = &Type{Kind: TyInt, Name: "Int"}
	TUInt8   = UInt(8)
	TUInt16  = UInt(16)
	TUInt32  = UInt(32)
	TUInt64  = UInt(64)
	TFloat   = &Type{Kind: TyFloat, Name: "Float"}
	TBool    = &Type{Kind: TyBool, Name: "Bool"}
	TString  = &Type{Kind: TyString, Name: "String"}
	TBytes   = &Type{Kind: TyBytes, Name: "Bytes"}
	TJSON    = &Type{Kind: TyJSON, Name: "Json"}
)

func UInt(bits uint8) *Type {
	name := fmt.Sprintf("UInt%d", bits)
	return &Type{Kind: TyUInt, Name: name, Bits: bits}
}

func isUInt(t *Type) bool { return t != nil && t.Kind == TyUInt }

func uintType(bits uint8) *Type {
	switch bits {
	case 8:
		return TUInt8
	case 16:
		return TUInt16
	case 32:
		return TUInt32
	case 64:
		return TUInt64
	default:
		return UInt(bits)
	}
}

func Arr(t *Type) *Type        { return &Type{Kind: TyArray, Name: "Array", A: t} }
func Opt(t *Type) *Type        { return &Type{Kind: TyOption, Name: "Option", A: t} }
func Res(a, b *Type) *Type     { return &Type{Kind: TyResult, Name: "Result", A: a, B: b} }
func Chan(t *Type) *Type       { return &Type{Kind: TyChannel, Name: "Channel", A: t} }
func TypeThread(t *Type) *Type { return &Type{Kind: TyThread, Name: "Thread", A: t} }
func MapOf(k, v *Type) *Type   { return &Type{Kind: TyMap, Name: "Map", A: k, B: v} }
func SetOf(t *Type) *Type      { return &Type{Kind: TySet, Name: "Set", A: t} }
func ActorOf(t *Type) *Type    { return &Type{Kind: TyActor, Name: "Actor", A: t} }
func SharedOf(t *Type) *Type   { return &Type{Kind: TyShared, Name: "Shared", A: t} }
func FunctionType(params []*Type, result *Type) *Type {
	return &Type{Kind: TyFunction, Params: append([]*Type(nil), params...), Return: result}
}

var TTaskGroup = &Type{Kind: TyTaskGroup, Name: "TaskGroup"}

func Generic(name, constraint string) *Type {
	return &Type{Kind: TyGeneric, Name: name, B: &Type{Name: constraint}}
}
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
	case TyMap:
		return "Map[" + t.A.String() + ", " + t.B.String() + "]"
	case TySet:
		return "Set[" + t.A.String() + "]"
	case TyActor:
		return "Actor[" + t.A.String() + "]"
	case TyShared:
		return "Shared[" + t.A.String() + "]"
	case TyTaskGroup:
		return "TaskGroup"
	case TyRegex:
		return "Regex"
	case TyRandom:
		return "Random"
	case TySQLite:
		return "SQLite"
	case TyTCPSocket:
		return "TcpSocket"
	case TyTCPListener:
		return "TcpListener"
	case TyUDPSocket:
		return "UdpSocket"
	case TyFFILibrary:
		return "FFILibrary"
	case TyFFISymbol:
		return "FFISymbol"
	case TyFFIBuffer:
		return "FFIBuffer"
	case TyFunction:
		params := make([]string, len(t.Params))
		for i, param := range t.Params {
			params[i] = param.String()
		}
		return "fn(" + strings.Join(params, ", ") + ") -> " + t.Return.String()
	case TyStruct:
		if len(t.Params) != 0 {
			params := make([]string, len(t.Params))
			for i, param := range t.Params {
				params[i] = param.String()
			}
			return t.Name + "[" + strings.Join(params, ", ") + "]"
		}
	case TyJSON:
		return "Json"
	case TyWebSocket:
		return "WebSocket"
	case TyGeneric:
		return t.Name

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
		if x.Kind == TyUInt {
			return x.Bits == y.Bits
		}
		if x.Kind == TyStruct {
			if x.Struct != y.Struct || len(x.Params) != len(y.Params) {
				return false
			}
			for i := range x.Params {
				if !eq(x.Params[i], y.Params[i], d+1) {
					return false
				}
			}
			return true
		}
		if x.Kind == TyEnum {
			return x.Enum == y.Enum
		}
		if x.Kind == TyGeneric {
			return x.Name == y.Name && genericConstraint(x) == genericConstraint(y)
		}
		k := [2]*Type{x, y}
		if seen[k] {
			return true
		}
		seen[k] = true
		switch x.Kind {
		case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
			return eq(x.A, y.A, d+1)
		case TyResult, TyMap:
			return eq(x.A, y.A, d+1) && eq(x.B, y.B, d+1)
		case TyFunction:
			if len(x.Params) != len(y.Params) || !eq(x.Return, y.Return, d+1) {
				return false
			}
			for i := range x.Params {
				if !eq(x.Params[i], y.Params[i], d+1) {
					return false
				}
			}
			return true

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
	case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
		return typeKnown(t.A)
	case TyResult, TyMap:
		return typeKnown(t.A) && typeKnown(t.B)
	case TyFunction:
		if !typeKnown(t.Return) {
			return false
		}
		for _, param := range t.Params {
			if !typeKnown(param) {
				return false
			}
		}
		return true
	case TyStruct:
		for _, argument := range t.Params {
			if !typeKnown(argument) {
				return false
			}
		}

	}
	return true
}

func containsFunctionType(root *Type) bool {
	seen := map[*Type]bool{}
	var visit func(*Type, int) bool
	visit = func(t *Type, depth int) bool {
		if t == nil || depth > 128 || seen[t] {
			return false
		}
		seen[t] = true
		if t.Kind == TyFunction {
			return true
		}
		for _, child := range typeArguments(t) {
			if visit(child, depth+1) {
				return true
			}
		}
		if t.Kind == TyStruct && t.Struct != nil {
			for _, field := range t.Struct.Fields {
				if visit(structFieldType(t, field), depth+1) {
					return true
				}
			}
		}
		return false
	}
	return visit(root, 0)
}

func numeric(t *Type) bool {
	if t == nil {
		return false
	}
	if t.Kind == TyGeneric {
		constraint := genericConstraint(t)
		return constraint == "Numeric" || constraint == "Integer"
	}
	return t.Kind == TyInt || t.Kind == TyUInt || t.Kind == TyFloat
}

func integer(t *Type) bool {
	if t == nil {
		return false
	}
	if t.Kind == TyGeneric {
		return genericConstraint(t) == "Integer"
	}
	return t.Kind == TyInt || t.Kind == TyUInt
}

func genericConstraint(t *Type) string {
	if t == nil || t.Kind != TyGeneric || t.B == nil {
		return ""
	}
	return t.B.Name
}

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
		case TyNil, TyInt, TyUInt, TyFloat, TyBool, TyString, TyBytes, TyEnum, TyJSON:
			ok = true
		case TyGeneric:
			constraint := genericConstraint(t)
			ok = constraint == "Copy" || constraint == "Integer" || constraint == "Numeric" || constraint == "Comparable"
		case TyArray, TyOption, TySet, TyActor, TyShared:
			if t.Kind == TyShared {
				ok = true
				break
			}
			ok = visit(t.A, d+1)
		case TyResult, TyMap:
			ok = visit(t.A, d+1) && visit(t.B, d+1)
		case TyFunction:
			ok = false

		case TyStruct:
			ok = true
			if t.Struct == nil {
				ok = false
			} else {
				for _, argument := range t.Params {
					if !visit(argument, d+1) {
						ok = false
						break
					}
				}
				for _, f := range t.Struct.Fields {
					if !visit(structFieldType(t, f), d+1) {
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

// TypeConstSafe excludes synchronization and external-resource handles from const values.
func TypeConstSafe(root *Type) bool {
	seen := map[*Type]bool{}
	var visit func(*Type, int) bool
	visit = func(t *Type, depth int) bool {
		if t == nil || depth > 128 {
			return false
		}
		if seen[t] {
			return true
		}
		seen[t] = true
		switch t.Kind {
		case TyNil, TyInt, TyUInt, TyFloat, TyBool, TyString, TyBytes, TyEnum:
			return true
		case TyArray, TyOption, TySet:
			return visit(t.A, depth+1)
		case TyResult, TyMap:
			return visit(t.A, depth+1) && visit(t.B, depth+1)
		case TyFunction:
			return false
		case TyStruct:
			if t.Struct == nil {
				return false
			}
			for _, argument := range t.Params {
				if !visit(argument, depth+1) {
					return false
				}
			}
			for _, f := range t.Struct.Fields {
				if !visit(structFieldType(t, f), depth+1) {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	return visit(root, 0)
}

func TypeSpecString(s *TypeSpec) string {
	if s == nil {
		return "Nil"
	}
	if s.Function {
		params := make([]string, len(s.Params))
		for i, p := range s.Params {
			params[i] = TypeSpecString(p)
		}
		return "fn(" + strings.Join(params, ", ") + ") -> " + TypeSpecString(s.Return)
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
	if s.Function {
		params := make([]*Type, len(s.Params))
		for i, param := range s.Params {
			resolved, d := resolveSpec(env, param, depth+1)
			if d != nil {
				return TError, d
			}
			params[i] = resolved
		}
		result, d := resolveSpec(env, s.Return, depth+1)
		if d != nil {
			return TError, d
		}
		return FunctionType(params, result), nil
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
		case "UInt8":
			return TUInt8, nil
		case "UInt16":
			return TUInt16, nil
		case "UInt32":
			return TUInt32, nil
		case "UInt64":
			return TUInt64, nil
		case "Float":
			return TFloat, nil
		case "Bool":
			return TBool, nil
		case "String":
			return TString, nil
		case "Bytes":
			return TBytes, nil
		case "Json":
			return TJSON, nil
		case "WebSocket":
			return &Type{Kind: TyWebSocket, Name: "WebSocket"}, nil
		case "Regex":
			return &Type{Kind: TyRegex, Name: "Regex"}, nil
		case "Random":
			return &Type{Kind: TyRandom, Name: "Random"}, nil
		case "SQLite":
			return &Type{Kind: TySQLite, Name: "SQLite"}, nil
		case "TcpSocket":
			return &Type{Kind: TyTCPSocket, Name: "TcpSocket"}, nil
		case "TcpListener":
			return &Type{Kind: TyTCPListener, Name: "TcpListener"}, nil
		case "UdpSocket":
			return &Type{Kind: TyUDPSocket, Name: "UdpSocket"}, nil
		case "FFILibrary":
			return &Type{Kind: TyFFILibrary, Name: "FFILibrary"}, nil
		case "FFISymbol":
			return &Type{Kind: TyFFISymbol, Name: "FFISymbol"}, nil
		case "FFIBuffer":
			return &Type{Kind: TyFFIBuffer, Name: "FFIBuffer"}, nil
		case "Array":
			return Arr(TUnknown), nil
		}
		if env.TypeParams != nil {
			if t := env.TypeParams[name]; t != nil {
				return t, nil
			}
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
	case "Map":
		if len(s.Params) == 2 {
			k, d := resolveSpec(env, s.Params[0], depth+1)
			if d != nil {
				return TError, d
			}
			v, d := resolveSpec(env, s.Params[1], depth+1)
			return MapOf(k, v), d
		}
	case "Set":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return SetOf(a), d
		}
	case "Actor":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return ActorOf(a), d
		}
	case "Shared":
		if len(s.Params) == 1 {
			a, d := resolveSpec(env, s.Params[0], depth+1)
			return SharedOf(a), d
		}
	case "TaskGroup":
		return TTaskGroup, nil

	}
	if template := env.Types[name]; template != nil && template.Kind == TyStruct {
		params := template.Struct.TypeParams
		if len(s.Params) != len(params) {
			if len(params) == 0 {
				return TError, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "type '%s' does not accept type arguments", name)
			}
			return TError, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "type '%s' expects %d type argument(s), got %d", name, len(params), len(s.Params))
		}
		arguments := make([]*Type, len(params))
		for i, parameter := range params {
			argument, d := resolveSpec(env, s.Params[i], depth+1)
			if d != nil {
				return TError, d
			}
			if !env.satisfiesConstraint(argument, parameter.Constraint) {
				return TError, Diag(CatType, s.Params[i].Tok.Source, s.Params[i].Tok.Line, s.Params[i].Tok.Column, "type argument %s does not satisfy %s constraint for %s", argument, parameter.Constraint, parameter.Name)
			}
			arguments[i] = argument
		}
		return &Type{Kind: TyStruct, Name: template.Name, Struct: template.Struct, Params: arguments}, nil
	}
	if len(s.Params) == 0 {
		if t := env.Types[name]; t != nil {
			return t, nil
		}
	}
	return TError, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "unknown or malformed type '%s'", name)
}

func methodKey(t *Type, name string) string {
	if t == nil {
		return "<unknown>::" + name
	}
	return t.String() + "::" + name
}

type TypeEnv struct {
	Types      map[string]*Type
	Functions  map[string]*Function
	Overloads  map[string][]*Function
	Traits     map[string]*TraitDecl
	TraitImpls map[string]map[string]*TraitImplDecl
	Builtins   map[string]Builtin
	TypeParams map[string]*Type
	Lim        Limits
	Module     string
}

func typeDecls(prog *Program, lim Limits) (*TypeEnv, *Diagnostic) {
	e := &TypeEnv{Types: map[string]*Type{}, Functions: map[string]*Function{}, Overloads: map[string][]*Function{}, Traits: map[string]*TraitDecl{}, TraitImpls: map[string]map[string]*TraitImplDecl{}, Lim: lim, Module: prog.Module}
	for _, s := range prog.Structs {
		if _, ok := e.Types[s.Name]; ok {
			return nil, Diag(CatType, s.Tok.Source, s.Tok.Line, s.Tok.Column, "declaration '%s' is already defined", s.Name)
		}
		s.Type = &Type{Kind: TyStruct, Name: s.Name, Struct: s}
		e.Types[s.Name] = s.Type
		seenParams := map[string]bool{}
		for _, parameter := range s.TypeParams {
			if parameter.Name == "" || seenParams[parameter.Name] {
				return nil, Diag(CatType, parameter.Tok.Source, parameter.Tok.Line, parameter.Tok.Column, "type parameter '%s' is duplicated", parameter.Name)
			}
			seenParams[parameter.Name] = true
		}
	}
	for _, d := range prog.Enums {
		if _, ok := e.Types[d.Name]; ok {
			return nil, Diag(CatType, d.Tok.Source, d.Tok.Line, d.Tok.Column, "declaration '%s' is already defined", d.Name)
		}
		d.Type = &Type{Kind: TyEnum, Name: d.Name, Enum: d}
		e.Types[d.Name] = d.Type
	}
	for _, trait := range prog.Traits {
		if _, exists := e.Types[trait.Name]; exists {
			return nil, Diag(CatType, trait.Tok.Source, trait.Tok.Line, trait.Tok.Column, "declaration '%s' is already defined", trait.Name)
		}
		if _, exists := e.Traits[trait.Name]; exists {
			return nil, Diag(CatType, trait.Tok.Source, trait.Tok.Line, trait.Tok.Column, "trait '%s' is already defined", trait.Name)
		}
		e.Traits[trait.Name] = trait
	}
	for _, s := range prog.Structs {
		for _, parameter := range s.TypeParams {
			if !knownTypeConstraint(e, parameter.Constraint) {
				return nil, Diag(CatType, parameter.Tok.Source, parameter.Tok.Line, parameter.Tok.Column, "unknown type constraint '%s'", parameter.Constraint)
			}
		}
	}
	for _, f := range prog.Functions {
		if f.Trait != "" {
			continue // Trait implementation methods have a separate, unambiguous registry.
		}
		key := f.Name
		if f.Receiver != nil {
			key = TypeSpecString(f.Receiver) + "::" + f.Name
		}
		for _, previous := range e.Overloads[key] {
			if sameFunctionSignature(previous, f) {
				return nil, Diag(CatType, f.Tok.Source, f.Tok.Line, f.Tok.Column, "function '%s' with the same signature is already defined", f.Name)
			}
		}
		maxOverloads := lim.MaxOverloadsPerName
		if maxOverloads <= 0 {
			maxOverloads = DefaultLimits().MaxOverloadsPerName
		}
		if len(e.Overloads[key]) >= maxOverloads {
			return nil, Diag(CatResource, f.Tok.Source, f.Tok.Line, f.Tok.Column, "function overload limit of %d exceeded for '%s'", maxOverloads, f.Name)
		}
		e.Overloads[key] = append(e.Overloads[key], f)
		if _, ok := e.Functions[key]; !ok {
			e.Functions[key] = f
		}
	}
	if d := registerTraitDeclarations(e, prog); d != nil {
		return nil, d
	}
	for _, s := range prog.Structs {
		previousParams := e.TypeParams
		e.TypeParams = make(map[string]*Type, len(s.TypeParams))
		for _, parameter := range s.TypeParams {
			e.TypeParams[parameter.Name] = Generic(parameter.Name, parameter.Constraint)
		}
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
			if name := inaccessibleTypeName(t, s.VisibilityScope, 0); name != "" {
				return nil, Diag(CatType, s.Fields[i].Tok.Source, s.Fields[i].Tok.Line, s.Fields[i].Tok.Column, "type '%s' is private", name)
			}
			s.Fields[i].Type = t
		}
		e.TypeParams = previousParams
	}
	if d := registerTraitImplementations(e, prog); d != nil {
		return nil, d
	}
	for _, s := range prog.Structs {
		if !s.Public {
			continue
		}
		for _, field := range s.Fields {
			if !ensurePublicType(field.Type, "", 0) {
				return nil, Diag(CatType, field.Tok.Source, field.Tok.Line, field.Tok.Column, "public struct '%s' exposes a private field type", s.Name)
			}
		}
	}
	return e, nil
}

func knownTypeConstraint(env *TypeEnv, constraint string) bool {
	if constraint == "" || constraint == "Any" || constraint == "Copy" || constraint == "Integer" || constraint == "Numeric" || constraint == "Comparable" {
		return true
	}
	_, ok := env.Traits[constraint]
	return ok
}

func (env *TypeEnv) satisfiesConstraint(typ *Type, constraint string) bool {
	if _, trait := env.Traits[constraint]; trait {
		if typ != nil && typ.Kind == TyGeneric {
			return genericConstraint(typ) == constraint
		}
		if typ == nil || typ.Kind != TyStruct || typ.Struct == nil {
			return false
		}
		return env.TraitImpls[constraint][typ.String()] != nil
	}
	return satisfiesConstraint(typ, constraint)
}

func registerTraitDeclarations(env *TypeEnv, program *Program) *Diagnostic {
	for _, trait := range program.Traits {
		if len(trait.Methods) == 0 {
			return Diag(CatType, trait.Tok.Source, trait.Tok.Line, trait.Tok.Column, "trait '%s' must declare at least one method", trait.Name)
		}
		seen := map[string]bool{}
		for _, method := range trait.Methods {
			if method == nil || method.Name == "" || method.Trait != trait.Name || method.Receiver != nil || len(method.TypeParams) != 0 {
				return Diag(CatType, trait.Tok.Source, trait.Tok.Line, trait.Tok.Column, "trait '%s' has an unsupported method declaration", trait.Name)
			}
			if seen[method.Name] {
				return Diag(CatType, method.NameToken.Source, method.NameToken.Line, method.NameToken.Column, "trait '%s' declares method '%s' more than once", trait.Name, method.Name)
			}
			seen[method.Name] = true
			for _, parameter := range method.Params {
				if parameter.Default != nil {
					return Diag(CatType, parameter.Tok.Source, parameter.Tok.Line, parameter.Tok.Column, "trait method parameters cannot have defaults")
				}
				if _, diagnostic := resolveSpec(env, parameter.Type, 0); diagnostic != nil {
					return diagnostic
				}
			}
			if _, diagnostic := resolveSpec(env, method.Return, 0); diagnostic != nil {
				return diagnostic
			}
		}
	}
	return nil
}

func registerTraitImplementations(env *TypeEnv, program *Program) *Diagnostic {
	for _, implementation := range program.TraitImpls {
		trait := env.Traits[implementation.Trait]
		if trait == nil {
			return Diag(CatType, implementation.TraitToken.Source, implementation.TraitToken.Line, implementation.TraitToken.Column, "unknown trait '%s'", implementation.Trait)
		}
		target, diagnostic := resolveSpec(env, implementation.Target, 0)
		if diagnostic != nil {
			return Diag(CatType, implementation.Target.Tok.Source, implementation.Target.Tok.Line, implementation.Target.Tok.Column, "trait implementation target must be a concrete known type; generic and blanket implementations are not supported")
		}
		if target.Kind != TyStruct || target.Struct == nil || containsGenericType(target, 0) {
			return Diag(CatType, implementation.Target.Tok.Source, implementation.Target.Tok.Line, implementation.Target.Tok.Column, "trait implementations currently require a concrete struct type; generic and blanket implementations are not supported")
		}
		implementations := env.TraitImpls[trait.Name]
		if implementations == nil {
			implementations = map[string]*TraitImplDecl{}
			env.TraitImpls[trait.Name] = implementations
		}
		targetKey := target.String()
		if implementations[targetKey] != nil {
			return Diag(CatType, implementation.Tok.Source, implementation.Tok.Line, implementation.Tok.Column, "trait '%s' is already implemented for %s", trait.Name, target)
		}
		methods := make(map[string]*Function, len(implementation.Methods))
		for _, method := range implementation.Methods {
			if method == nil || method.Name == "" || method.Trait != trait.Name || len(method.TypeParams) != 0 {
				return Diag(CatType, implementation.Tok.Source, implementation.Tok.Line, implementation.Tok.Column, "trait '%s' implementation contains an unsupported method", trait.Name)
			}
			if _, duplicate := methods[method.Name]; duplicate {
				return Diag(CatType, method.NameToken.Source, method.NameToken.Line, method.NameToken.Column, "trait '%s' implementation repeats method '%s'", trait.Name, method.Name)
			}
			methods[method.Name] = method
		}
		for _, signature := range trait.Methods {
			method := methods[signature.Name]
			if method == nil {
				return Diag(CatType, implementation.Tok.Source, implementation.Tok.Line, implementation.Tok.Column, "trait '%s' implementation for %s is missing method '%s'", trait.Name, target, signature.Name)
			}
			if !traitMethodSignaturesMatch(env, signature, method) {
				return Diag(CatType, method.NameToken.Source, method.NameToken.Line, method.NameToken.Column, "method '%s' does not match trait '%s' signature", method.Name, trait.Name)
			}
			delete(methods, signature.Name)
		}
		if len(methods) != 0 {
			for name, method := range methods {
				return Diag(CatType, method.NameToken.Source, method.NameToken.Line, method.NameToken.Column, "trait '%s' does not declare method '%s'", trait.Name, name)
			}
		}
		implementations[targetKey] = implementation
	}
	return nil
}

func traitMethodSignaturesMatch(env *TypeEnv, declaration, implementation *Function) bool {
	if declaration == nil || implementation == nil || len(declaration.Params) != len(implementation.Params) || len(implementation.TypeParams) != 0 {
		return false
	}
	for i := range declaration.Params {
		if implementation.Params[i].Default != nil {
			return false
		}
		declared, d1 := resolveSpec(env, declaration.Params[i].Type, 0)
		provided, d2 := resolveSpec(env, implementation.Params[i].Type, 0)
		if d1 != nil || d2 != nil || !typeEqual(declared, provided) {
			return false
		}
	}
	declared, d1 := resolveSpec(env, declaration.Return, 0)
	provided, d2 := resolveSpec(env, implementation.Return, 0)
	return d1 == nil && d2 == nil && typeEqual(declared, provided)
}

func containsGenericType(typ *Type, depth int) bool {
	if typ == nil || depth > 128 {
		return true
	}
	if typ.Kind == TyGeneric {
		return true
	}
	for _, argument := range typeArguments(typ) {
		if containsGenericType(argument, depth+1) {
			return true
		}
	}
	return false
}
func sameFunctionSignature(a, b *Function) bool {
	if TypeSpecString(a.Receiver) != TypeSpecString(b.Receiver) || len(a.Params) != len(b.Params) {
		return false
	}
	for i := range a.Params {
		if TypeSpecString(a.Params[i].Type) != TypeSpecString(b.Params[i].Type) {
			return false
		}
	}
	return true
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
		for _, argument := range t.Params {
			if !ensurePublicType(argument, local, depth+1) {
				return false
			}
		}
		for _, f := range t.Struct.Fields {
			if !ensurePublicType(structFieldType(t, f), local, depth+1) {
				return false
			}
		}
		return true
	case TyEnum:
		return t.Enum != nil && (t.Enum.Public || t.Enum.Module == local)
	case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
		return ensurePublicType(t.A, local, depth+1)
	case TyGeneric:
		return true
	case TyResult, TyMap:
		return ensurePublicType(t.A, local, depth+1) && ensurePublicType(t.B, local, depth+1)
	case TyFunction:
		if !ensurePublicType(t.Return, local, depth+1) {
			return false
		}
		for _, param := range t.Params {
			if !ensurePublicType(param, local, depth+1) {
				return false
			}
		}
		return true

	}
	return true
}

func inaccessibleTypeName(t *Type, visibilityScope string, depth int) string {
	if t == nil || depth > 128 {
		return ""
	}
	switch t.Kind {
	case TyStruct:
		if t.Struct == nil || !t.Struct.Public && t.Struct.VisibilityScope != visibilityScope {
			return t.Name
		}
		for _, argument := range t.Params {
			if name := inaccessibleTypeName(argument, visibilityScope, depth+1); name != "" {
				return name
			}
		}
	case TyEnum:
		if t.Enum == nil || !t.Enum.Public && t.Enum.VisibilityScope != visibilityScope {
			return t.Name
		}
	case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
		return inaccessibleTypeName(t.A, visibilityScope, depth+1)
	case TyResult, TyMap:
		if name := inaccessibleTypeName(t.A, visibilityScope, depth+1); name != "" {
			return name
		}
		return inaccessibleTypeName(t.B, visibilityScope, depth+1)
	case TyFunction:
		for _, param := range t.Params {
			if name := inaccessibleTypeName(param, visibilityScope, depth+1); name != "" {
				return name
			}
		}
		return inaccessibleTypeName(t.Return, visibilityScope, depth+1)
	}
	return ""
}

var _ = fmt.Sprintf
