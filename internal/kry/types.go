package kry

import "fmt"

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
)

type Type struct {
	Kind   TypeKind
	Name   string
	Bits   uint8
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
		case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
			return eq(x.A, y.A, d+1)
		case TyResult, TyMap:
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
	case TyArray, TyOption, TyChannel, TyThread, TySet, TyActor, TyShared:
		return typeKnown(t.A)
	case TyResult, TyMap:
		return typeKnown(t.A) && typeKnown(t.B)

	}
	return true
}
func numeric(t *Type) bool {
	return t != nil && (t.Kind == TyInt || t.Kind == TyUInt || t.Kind == TyFloat)
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
			ok = t.B != nil && t.B.Name == "Copy"
		case TyArray, TyOption, TySet, TyActor, TyShared:
			if t.Kind == TyShared {
				ok = true
				break
			}
			ok = visit(t.A, d+1)
		case TyResult, TyMap:
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
		case TyStruct:
			if t.Struct == nil {
				return false
			}
			for _, f := range t.Struct.Fields {
				if !visit(f.Type, depth+1) {
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
	Builtins   map[string]Builtin
	TypeParams map[string]*Type
	Lim        Limits
	Module     string
}

func typeDecls(prog *Program, lim Limits) (*TypeEnv, *Diagnostic) {
	e := &TypeEnv{Types: map[string]*Type{}, Functions: map[string]*Function{}, Overloads: map[string][]*Function{}, Lim: lim, Module: prog.Module}
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
		key := f.Name
		if f.Receiver != nil {
			key = TypeSpecString(f.Receiver) + "::" + f.Name
		}
		for _, previous := range e.Overloads[key] {
			if sameFunctionSignature(previous, f) {
				return nil, Diag(CatType, f.Tok.Source, f.Tok.Line, f.Tok.Column, "function '%s' with the same signature is already defined", f.Name)
			}
		}
		e.Overloads[key] = append(e.Overloads[key], f)
		if _, ok := e.Functions[key]; !ok {
			e.Functions[key] = f
		}
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
		for _, f := range t.Struct.Fields {
			if !ensurePublicType(f.Type, local, depth+1) {
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

	}
	return true
}

var _ = fmt.Sprintf
