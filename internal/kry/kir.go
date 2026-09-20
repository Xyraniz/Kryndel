package kry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// KIR is the stable, host-independent representation exchanged between the
// checked Kryndel frontend and future Kryndel-written backends. It is a typed
// tree for v1 rather than a source dump: every expression carries its checked
// type, every call identifies its builtin or user-function target, and source
// locations are retained for diagnostics. The wire format is canonical JSON
// so a Kryndel program can consume it using the standard Json value API.
const (
	KIRFormat  = "kry-ir"
	KIRVersion = 1
)

type KIRTarget struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	GUI  bool   `json:"gui"`
}

type KIRDocument struct {
	Format     string         `json:"format"`
	Version    int            `json:"version"`
	Module     string         `json:"module"`
	Source     string         `json:"source"`
	Target     KIRTarget      `json:"target"`
	Imports    []string       `json:"imports"`
	Sources    []string       `json:"sources"`
	Structs    []*KIRStruct   `json:"structs"`
	Enums      []*KIREnum     `json:"enums"`
	Functions  []*KIRFunction `json:"functions"`
	Statements []*KIRStmt     `json:"statements"`
}

type KIRStruct struct {
	Name   string      `json:"name"`
	Public bool        `json:"public"`
	Module string      `json:"module"`
	Fields []*KIRField `json:"fields"`
}

type KIRField struct {
	Name   string `json:"name"`
	Public bool   `json:"public"`
	Type   string `json:"type"`
}

type KIREnum struct {
	Name     string   `json:"name"`
	Public   bool     `json:"public"`
	Module   string   `json:"module"`
	Variants []string `json:"variants"`
}

type KIRTypeParam struct {
	Name       string `json:"name"`
	Constraint string `json:"constraint"`
}

type KIRParam struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Default *KIRExpr `json:"default"`
}

type KIRFunction struct {
	Name       string          `json:"name"`
	Public     bool            `json:"public"`
	Worker     bool            `json:"worker"`
	Unsafe     bool            `json:"unsafe"`
	Module     string          `json:"module"`
	Receiver   string          `json:"receiver"`
	Return     string          `json:"return"`
	TypeParams []*KIRTypeParam `json:"type_params"`
	Params     []*KIRParam     `json:"params"`
	Body       []*KIRStmt      `json:"body"`
}

type KIRExpr struct {
	Kind        string     `json:"kind"`
	Source      string     `json:"source"`
	Line        int        `json:"line"`
	Column      int        `json:"column"`
	Type        string     `json:"type"`
	Const       *KIRValue  `json:"const"`
	Int         int64      `json:"int"`
	UInt        uint64     `json:"uint"`
	UIntBits    uint8      `json:"uint_bits"`
	Float       float64    `json:"float"`
	Bool        bool       `json:"bool"`
	String      string     `json:"string"`
	Name        string     `json:"name"`
	Operator    string     `json:"operator"`
	CallTarget  string     `json:"call_target"`
	BuiltinID   string     `json:"builtin_id"`
	Left        *KIRExpr   `json:"left"`
	Right       *KIRExpr   `json:"right"`
	Operand     *KIRExpr   `json:"operand"`
	Args        []*KIRExpr `json:"args"`
	Items       []*KIRExpr `json:"items"`
	Base        *KIRExpr   `json:"base"`
	Field       string     `json:"field"`
	Receiver    *KIRExpr   `json:"receiver"`
	MapKeys     []*KIRExpr `json:"map_keys"`
	StructName  string     `json:"struct_name"`
	Fields      []string   `json:"fields"`
	Values      []*KIRExpr `json:"values"`
	EnumType    string     `json:"enum_type"`
	EnumVariant string     `json:"enum_variant"`
	Tail        bool       `json:"tail"`
}

type KIRStmt struct {
	Kind       string     `json:"kind"`
	Source     string     `json:"source"`
	Line       int        `json:"line"`
	Column     int        `json:"column"`
	Name       string     `json:"name"`
	Mutable    bool       `json:"mutable"`
	Const      bool       `json:"const"`
	Annotation string     `json:"annotation"`
	Init       *KIRExpr   `json:"init"`
	Expr       *KIRExpr   `json:"expr"`
	Target     *KIRExpr   `json:"target"`
	Value      *KIRExpr   `json:"value"`
	Cond       *KIRExpr   `json:"cond"`
	Then       []*KIRStmt `json:"then"`
	Else       []*KIRStmt `json:"else"`
	Body       []*KIRStmt `json:"body"`
	Iter       *KIRExpr   `json:"iter"`
	Return     *KIRExpr   `json:"return"`
	Scrutinee  *KIRExpr   `json:"scrutinee"`
	Arms       []*KIRArm  `json:"arms"`
}

type KIRArm struct {
	Pattern *KIRPattern `json:"pattern"`
	Body    []*KIRStmt  `json:"body"`
}

type KIRPattern struct {
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Bool    bool   `json:"bool"`
	Int     int64  `json:"int"`
	String  string `json:"string"`
	Type    string `json:"type"`
	Variant string `json:"variant"`
	Binding string `json:"binding"`
	Present bool   `json:"present"`
	OK      bool   `json:"ok"`
}

type KIRValue struct {
	Kind     string      `json:"kind"`
	Int      int64       `json:"int"`
	UInt     uint64      `json:"uint"`
	UIntBits uint8       `json:"uint_bits"`
	Float    float64     `json:"float"`
	Bool     bool        `json:"bool"`
	String   string      `json:"string"`
	Bytes    []byte      `json:"bytes"`
	Array    []*KIRValue `json:"array"`
	Inner    *KIRValue   `json:"inner"`
	Present  bool        `json:"present"`
	OK       bool        `json:"ok"`
}

// EmitKIR serializes a checked program into deterministic KIR v1 JSON. The
// checker is required so the format cannot accidentally become an untyped
// source transport when a caller forgets to validate first.
func EmitKIR(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if p == nil || c == nil || c.Env == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	d := &KIRDocument{
		Format: KIRFormat, Version: KIRVersion, Module: p.Module,
		Source: sourceName(p.Source), Target: KIRTarget{OS: target.OS, Arch: target.Arch, GUI: target.GUI},
		Imports: make([]string, 0, len(p.Imports)), Sources: make([]string, 0, len(p.Sources)),
		Structs: make([]*KIRStruct, 0, len(p.Structs)), Enums: make([]*KIREnum, 0, len(p.Enums)),
		Functions: make([]*KIRFunction, 0, len(p.Functions)), Statements: make([]*KIRStmt, 0, len(p.Statements)),
	}
	for _, x := range p.Imports {
		d.Imports = append(d.Imports, x.Path)
	}
	for _, x := range p.Sources {
		d.Sources = append(d.Sources, sourceName(x))
	}
	for _, x := range p.Structs {
		s := &KIRStruct{Name: x.Name, Public: x.Public, Module: x.Module, Fields: make([]*KIRField, 0, len(x.Fields))}
		for _, f := range x.Fields {
			s.Fields = append(s.Fields, &KIRField{Name: f.Name, Public: f.Public, Type: typeString(f.Type, f.Spec)})
		}
		d.Structs = append(d.Structs, s)
	}
	for _, x := range p.Enums {
		d.Enums = append(d.Enums, &KIREnum{Name: x.Name, Public: x.Public, Module: x.Module, Variants: append([]string(nil), x.Variants...)})
	}
	for _, x := range p.Functions {
		f := &KIRFunction{Name: x.Name, Public: x.Public, Worker: x.Worker, Unsafe: x.Unsafe, Module: x.Module, Receiver: typeSpecString(x.Receiver), Return: typeSpecString(x.Return), TypeParams: make([]*KIRTypeParam, 0, len(x.TypeParams)), Params: make([]*KIRParam, 0, len(x.Params)), Body: kirStmts(x.Body, c)}
		for _, tp := range x.TypeParams {
			f.TypeParams = append(f.TypeParams, &KIRTypeParam{Name: tp.Name, Constraint: tp.Constraint})
		}
		for _, param := range x.Params {
			f.Params = append(f.Params, &KIRParam{Name: param.Name, Type: typeSpecString(param.Type), Default: kirExpr(param.Default, c)})
		}
		d.Functions = append(d.Functions, f)
	}
	for _, x := range p.Statements {
		d.Statements = append(d.Statements, kirStmt(x, c))
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode KIR: %w", err)
	}
	return append(data, '\n'), nil
}

func typeString(t *Type, spec *TypeSpec) string {
	if t != nil && t.Kind != TyUnknown && t.Kind != TyError {
		return t.String()
	}
	return typeSpecString(spec)
}

func typeSpecString(s *TypeSpec) string {
	if s == nil {
		return ""
	}
	return TypeSpecString(s)
}

func sourceName(s *Source) string {
	if s == nil {
		return ""
	}
	return s.Name
}

func tokenSource(t Token) string { return sourceName(t.Source) }

func kirStmts(in []*Stmt, c *Checker) []*KIRStmt {
	out := make([]*KIRStmt, 0, len(in))
	for _, x := range in {
		out = append(out, kirStmt(x, c))
	}
	return out
}

func kirStmt(s *Stmt, c *Checker) *KIRStmt {
	if s == nil {
		return nil
	}
	k := &KIRStmt{Kind: stmtName(s.Kind), Source: tokenSource(s.Tok), Line: s.Tok.Line, Column: s.Tok.Column, Name: s.Name, Mutable: s.Mutable, Const: s.Const, Annotation: typeSpecString(s.Annotation), Init: kirExpr(s.Init, c), Expr: kirExpr(s.Expr, c), Target: kirExpr(s.Target, c), Value: kirExpr(s.Value, c), Cond: kirExpr(s.Cond, c), Then: kirStmts(s.Then, c), Else: kirStmts(s.Else, c), Body: kirStmts(s.Body, c), Iter: kirExpr(s.Iter, c), Return: kirExpr(s.Return, c), Scrutinee: kirExpr(s.Scrutinee, c), Arms: make([]*KIRArm, 0, len(s.Arms))}
	for _, arm := range s.Arms {
		k.Arms = append(k.Arms, &KIRArm{Pattern: kirPattern(arm.Pattern), Body: kirStmts(arm.Body, c)})
	}
	return k
}

func kirPattern(p Pattern) *KIRPattern {
	return &KIRPattern{Kind: patternName(p.Kind), Source: tokenSource(p.Tok), Line: p.Tok.Line, Column: p.Tok.Column, Bool: p.Bool, Int: p.Int, String: p.Str, Type: p.TypeName, Variant: p.Variant, Binding: p.Binding, Present: p.Present, OK: p.OK}
}

func kirExpr(e *Expr, c *Checker) *KIRExpr {
	if e == nil {
		return nil
	}
	k := &KIRExpr{Kind: exprName(e.Kind), Source: tokenSource(e.Tok), Line: e.Tok.Line, Column: e.Tok.Column, Type: typeString(e.Type, nil), Int: e.Int, Float: e.Float, Bool: e.Bool, String: e.Str, Name: e.Name, Operator: opText(e.Op), Left: kirExpr(e.Left, c), Right: kirExpr(e.Right, c), Operand: kirExpr(e.Operand, c), Args: make([]*KIRExpr, 0, len(e.Args)), Items: make([]*KIRExpr, 0, len(e.Items)), Base: kirExpr(e.Base, c), Field: e.Field, Receiver: kirExpr(e.Receiver, c), MapKeys: make([]*KIRExpr, 0, len(e.MapKeys)), StructName: e.StructName, Fields: append([]string(nil), e.Fields...), Values: make([]*KIRExpr, 0, len(e.Values)), EnumType: e.EnumType, EnumVariant: e.EnumVariant, Tail: e.Tail}
	for _, x := range e.Args {
		k.Args = append(k.Args, kirExpr(x, c))
	}
	for _, x := range e.Items {
		k.Items = append(k.Items, kirExpr(x, c))
	}
	for _, x := range e.MapKeys {
		k.MapKeys = append(k.MapKeys, kirExpr(x, c))
	}
	for _, x := range e.Values {
		k.Values = append(k.Values, kirExpr(x, c))
	}
	if e.ConstValue != nil {
		k.Const = kirValue(*e.ConstValue)
	}
	if b, ok := c.Env.Builtins[e.Name]; ok && e.Kind == ExCall {
		k.BuiltinID = b.ID
		k.CallTarget = "builtin:" + b.Name
	} else if e.Function != nil {
		k.CallTarget = "function:" + e.Function.Name
	} else if e.Kind == ExCall {
		k.CallTarget = "function:" + e.Name
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

func exprName(k ExprKind) string {
	return []string{"int", "float", "bool", "nil", "string", "var", "unary", "binary", "call", "array", "index", "field", "struct", "enum", "map", "set", "propagate"}[int(k)]
}

func stmtName(k StmtKind) string {
	return []string{"let", "expr", "assign", "if", "while", "return", "break", "continue", "match", "for", "defer", "unsafe", "const"}[int(k)]
}

func patternName(k PatternKind) string {
	return []string{"wildcard", "nil", "bool", "int", "string", "enum", "option", "result"}[int(k)]
}

func valueName(k ValueKind) string {
	return []string{"nil", "int", "uint", "float", "bool", "string", "bytes", "array", "struct", "enum", "option", "result", "channel", "thread", "map", "set", "json", "websocket", "actor", "shared", "task_group", "regex", "random", "sqlite", "tcp", "tcp_listener", "udp", "ffi_library", "ffi_symbol", "ffi_buffer", "tail_call"}[int(k)]
}

// DecodeKIR validates the versioned wire format before a backend consumes it.
// Unknown JSON fields are rejected deliberately: a backend must opt into a
// later KIR version instead of silently dropping new semantics.
func DecodeKIR(data []byte, lim Limits) (*KIRDocument, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty KIR document")
	}
	if lim.MaxArtifactBytes > 0 && len(data) > lim.MaxArtifactBytes {
		return nil, fmt.Errorf("KIR document exceeds configured input limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var d KIRDocument
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("decode KIR: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("KIR document has trailing JSON")
		}
		return nil, fmt.Errorf("KIR document has trailing data: %w", err)
	}
	if d.Format != KIRFormat {
		return nil, fmt.Errorf("unsupported KIR format %q", d.Format)
	}
	if d.Version != KIRVersion {
		return nil, fmt.Errorf("unsupported KIR version %d", d.Version)
	}
	if d.Target.OS == "" || d.Target.Arch == "" {
		return nil, fmt.Errorf("KIR target is incomplete")
	}
	return &d, nil
}
