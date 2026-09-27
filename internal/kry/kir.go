package kry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// KIR is the host-independent representation exchanged between the
// checked Kryndel frontend and future Kryndel-written backends. It is a typed
// tree rather than a source dump: every expression carries its checked
// type, every call identifies its builtin or user-function target, and source
// locations are retained for diagnostics. The wire format is canonical JSON
// so a Kryndel program can consume it using the standard Json value API.
const (
	KIRFormat  = "kry-ir"
	KIRVersion = 5
)

type KIRTarget struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	GUI  bool   `json:"gui"`
}

type KIRDocument struct {
	Format          string          `json:"format"`
	Version         int             `json:"version"`
	LanguageVersion string          `json:"language_version"`
	Module          string          `json:"module"`
	Source          string          `json:"source"`
	Target          KIRTarget       `json:"target"`
	Imports         []string        `json:"imports"`
	Sources         []string        `json:"sources"`
	Structs         []*KIRStruct    `json:"structs"`
	Enums           []*KIREnum      `json:"enums"`
	Traits          []*KIRTrait     `json:"traits"`
	TraitImpls      []*KIRTraitImpl `json:"trait_impls"`
	Functions       []*KIRFunction  `json:"functions"`
	Statements      []*KIRStmt      `json:"statements"`
}

type KIRStruct struct {
	Name       string          `json:"name"`
	Public     bool            `json:"public"`
	Module     string          `json:"module"`
	TypeParams []*KIRTypeParam `json:"type_params"`
	Fields     []*KIRField     `json:"fields"`
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

type KIRTrait struct {
	Name    string            `json:"name"`
	Public  bool              `json:"public"`
	Module  string            `json:"module"`
	Methods []*KIRTraitMethod `json:"methods"`
}

type KIRTraitMethod struct {
	Name   string      `json:"name"`
	Params []*KIRParam `json:"params"`
	Return string      `json:"return"`
}

type KIRTraitImpl struct {
	Trait   string                `json:"trait"`
	For     string                `json:"for"`
	Module  string                `json:"module"`
	Methods []*KIRTraitImplMethod `json:"methods"`
}

type KIRTraitImplMethod struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type KIRTypeParam struct {
	Name       string `json:"name"`
	Constraint string `json:"constraint"`
}

type KIRParam struct {
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Default *KIRExpr    `json:"default"`
	Binding *KIRBinding `json:"binding"`
}

type KIRBinding struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Mutable bool   `json:"mutable"`
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

type KIRFunction struct {
	Name       string          `json:"name"`
	Source     string          `json:"source"`
	Line       int             `json:"line"`
	Column     int             `json:"column"`
	Public     bool            `json:"public"`
	Worker     bool            `json:"worker"`
	Unsafe     bool            `json:"unsafe"`
	Trait      string          `json:"trait"`
	Module     string          `json:"module"`
	Receiver   string          `json:"receiver"`
	Return     string          `json:"return"`
	TypeParams []*KIRTypeParam `json:"type_params"`
	Params     []*KIRParam     `json:"params"`
	Captures   []*KIRCapture   `json:"captures"`
	Body       []*KIRStmt      `json:"body"`
}

type KIRCapture struct {
	Binding *KIRBinding `json:"binding"`
}

type KIRExpr struct {
	Kind             string       `json:"kind"`
	Source           string       `json:"source"`
	Line             int          `json:"line"`
	Column           int          `json:"column"`
	Type             string       `json:"type"`
	Const            *KIRValue    `json:"const"`
	Int              int64        `json:"int"`
	UInt             uint64       `json:"uint"`
	UIntBits         uint8        `json:"uint_bits"`
	Float            float64      `json:"float"`
	Bool             bool         `json:"bool"`
	String           string       `json:"string"`
	Name             string       `json:"name"`
	Operator         string       `json:"operator"`
	CallTarget       string       `json:"call_target"`
	TraitName        string       `json:"trait_name"`
	BuiltinID        string       `json:"builtin_id"`
	Left             *KIRExpr     `json:"left"`
	Right            *KIRExpr     `json:"right"`
	Operand          *KIRExpr     `json:"operand"`
	Args             []*KIRExpr   `json:"args"`
	Items            []*KIRExpr   `json:"items"`
	Base             *KIRExpr     `json:"base"`
	Field            string       `json:"field"`
	Receiver         *KIRExpr     `json:"receiver"`
	MapKeys          []*KIRExpr   `json:"map_keys"`
	StructName       string       `json:"struct_name"`
	StructType       string       `json:"struct_type"`
	GenericArguments []string     `json:"generic_arguments"`
	Fields           []string     `json:"fields"`
	Values           []*KIRExpr   `json:"values"`
	EnumType         string       `json:"enum_type"`
	EnumVariant      string       `json:"enum_variant"`
	Tail             bool         `json:"tail"`
	Callee           *KIRExpr     `json:"callee"`
	Lambda           *KIRFunction `json:"lambda"`
	Binding          *KIRBinding  `json:"binding"`
}

type KIRStmt struct {
	Kind       string      `json:"kind"`
	Source     string      `json:"source"`
	Line       int         `json:"line"`
	Column     int         `json:"column"`
	Name       string      `json:"name"`
	Binding    *KIRBinding `json:"binding"`
	Mutable    bool        `json:"mutable"`
	Const      bool        `json:"const"`
	Annotation string      `json:"annotation"`
	Init       *KIRExpr    `json:"init"`
	Expr       *KIRExpr    `json:"expr"`
	Target     *KIRExpr    `json:"target"`
	Value      *KIRExpr    `json:"value"`
	Cond       *KIRExpr    `json:"cond"`
	Then       []*KIRStmt  `json:"then"`
	Else       []*KIRStmt  `json:"else"`
	Body       []*KIRStmt  `json:"body"`
	Iter       *KIRExpr    `json:"iter"`
	Return     *KIRExpr    `json:"return"`
	Scrutinee  *KIRExpr    `json:"scrutinee"`
	Arms       []*KIRArm   `json:"arms"`
}

type KIRArm struct {
	Pattern *KIRPattern `json:"pattern"`
	Body    []*KIRStmt  `json:"body"`
}

type KIRPattern struct {
	Kind            string      `json:"kind"`
	Source          string      `json:"source"`
	Line            int         `json:"line"`
	Column          int         `json:"column"`
	Bool            bool        `json:"bool"`
	Int             int64       `json:"int"`
	String          string      `json:"string"`
	Type            string      `json:"type"`
	Variant         string      `json:"variant"`
	Binding         string      `json:"binding"`
	Present         bool        `json:"present"`
	OK              bool        `json:"ok"`
	ResolvedBinding *KIRBinding `json:"resolved_binding"`
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

// EmitKIR serializes a checked program into deterministic KIR v5 JSON. The
// checker is required so the format cannot accidentally become an untyped
// source transport when a caller forgets to validate first.
func EmitKIR(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	if p == nil || c == nil || c.Env == nil {
		return nil, fmt.Errorf("missing checked program")
	}
	paths := newKIRPathNames(p)
	d := &KIRDocument{
		Format: KIRFormat, Version: KIRVersion, LanguageVersion: LanguageVersion, Module: paths.name(p.Module),
		Source: paths.source(p.Source), Target: KIRTarget{OS: target.OS, Arch: target.Arch, GUI: target.GUI},
		Imports: make([]string, 0, len(p.Imports)), Sources: make([]string, 0, len(p.Sources)),
		Structs: make([]*KIRStruct, 0, len(p.Structs)), Enums: make([]*KIREnum, 0, len(p.Enums)), Traits: make([]*KIRTrait, 0, len(p.Traits)), TraitImpls: make([]*KIRTraitImpl, 0, len(p.TraitImpls)),
		Functions: make([]*KIRFunction, 0, len(p.Functions)), Statements: make([]*KIRStmt, 0, len(p.Statements)),
	}
	functionTargets := kirFunctionTargets(p, paths)
	for _, x := range p.Imports {
		d.Imports = append(d.Imports, x.Path)
	}
	for _, x := range p.Sources {
		d.Sources = append(d.Sources, paths.source(x))
	}
	for _, x := range p.Structs {
		s := &KIRStruct{Name: x.Name, Public: x.Public, Module: paths.name(x.Module), TypeParams: make([]*KIRTypeParam, 0, len(x.TypeParams)), Fields: make([]*KIRField, 0, len(x.Fields))}
		for _, parameter := range x.TypeParams {
			s.TypeParams = append(s.TypeParams, &KIRTypeParam{Name: parameter.Name, Constraint: parameter.Constraint})
		}
		for _, f := range x.Fields {
			s.Fields = append(s.Fields, &KIRField{Name: f.Name, Public: f.Public, Type: typeString(f.Type, f.Spec)})
		}
		d.Structs = append(d.Structs, s)
	}
	for _, x := range p.Enums {
		d.Enums = append(d.Enums, &KIREnum{Name: x.Name, Public: x.Public, Module: paths.name(x.Module), Variants: append([]string(nil), x.Variants...)})
	}
	for _, x := range p.Traits {
		trait := &KIRTrait{Name: x.Name, Public: x.Public, Module: paths.name(x.Module), Methods: make([]*KIRTraitMethod, 0, len(x.Methods))}
		for _, method := range x.Methods {
			entry := &KIRTraitMethod{Name: method.Name, Return: typeSpecString(method.Return), Params: make([]*KIRParam, 0, len(method.Params))}
			for _, parameter := range method.Params {
				entry.Params = append(entry.Params, &KIRParam{Name: parameter.Name, Type: typeSpecString(parameter.Type)})
			}
			trait.Methods = append(trait.Methods, entry)
		}
		d.Traits = append(d.Traits, trait)
	}
	for _, x := range p.Functions {
		f := &KIRFunction{Name: x.Name, Source: paths.tokenSource(x.Tok), Line: x.Tok.Line, Column: x.Tok.Column, Public: x.Public, Worker: x.Worker, Unsafe: x.Unsafe, Trait: x.Trait, Module: paths.name(x.Module), Receiver: typeSpecString(x.Receiver), Return: typeSpecString(x.Return), TypeParams: make([]*KIRTypeParam, 0, len(x.TypeParams)), Params: make([]*KIRParam, 0, len(x.Params)), Captures: []*KIRCapture{}, Body: kirStmts(x.Body, c, functionTargets, paths)}
		for _, tp := range x.TypeParams {
			f.TypeParams = append(f.TypeParams, &KIRTypeParam{Name: tp.Name, Constraint: tp.Constraint})
		}
		for _, param := range x.Params {
			f.Params = append(f.Params, &KIRParam{Name: param.Name, Type: typeSpecString(param.Type), Default: kirExpr(param.Default, c, functionTargets, paths), Binding: kirBinding(param.Name, typeSpecString(param.Type), false, param.Tok, paths)})
		}
		d.Functions = append(d.Functions, f)
	}
	for _, implementation := range p.TraitImpls {
		entry := &KIRTraitImpl{Trait: implementation.Trait, For: typeSpecString(implementation.Target), Module: paths.name(implementation.Module), Methods: make([]*KIRTraitImplMethod, 0, len(implementation.Methods))}
		for _, method := range implementation.Methods {
			entry.Methods = append(entry.Methods, &KIRTraitImplMethod{Name: method.Name, Target: "function:" + functionTargets[method]})
		}
		d.TraitImpls = append(d.TraitImpls, entry)
	}
	for _, x := range p.Statements {
		d.Statements = append(d.Statements, kirStmt(x, c, functionTargets, paths))
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

type kirPathNames struct {
	root string
}

func newKIRPathNames(program *Program) kirPathNames {
	name := program.Module
	if name == "" && program.Source != nil {
		name = program.Source.Name
	}
	if filepath.IsAbs(name) {
		return kirPathNames{root: findModuleRoot(filepath.Dir(name))}
	}
	return kirPathNames{}
}

func (paths kirPathNames) name(name string) string {
	if name == "" {
		return ""
	}
	if paths.root != "" && filepath.IsAbs(name) {
		relative, err := filepath.Rel(paths.root, name)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(name)
}

func (paths kirPathNames) source(source *Source) string {
	if source == nil {
		return ""
	}
	return paths.name(source.Name)
}

func (paths kirPathNames) tokenSource(token Token) string { return paths.source(token.Source) }

func kirBinding(name, typ string, mutable bool, token Token, paths kirPathNames) *KIRBinding {
	return &KIRBinding{Name: name, Type: typ, Mutable: mutable, Source: paths.tokenSource(token), Line: token.Line, Column: token.Column}
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
	k := &KIRStmt{Kind: stmtName(s.Kind), Source: paths.tokenSource(s.Tok), Line: s.Tok.Line, Column: s.Tok.Column, Name: s.Name, Mutable: s.Mutable, Const: s.Const, Annotation: typeSpecString(s.Annotation), Init: kirExpr(s.Init, c, functionTargets, paths), Expr: kirExpr(s.Expr, c, functionTargets, paths), Target: kirExpr(s.Target, c, functionTargets, paths), Value: kirExpr(s.Value, c, functionTargets, paths), Cond: kirExpr(s.Cond, c, functionTargets, paths), Then: kirStmts(s.Then, c, functionTargets, paths), Else: kirStmts(s.Else, c, functionTargets, paths), Body: kirStmts(s.Body, c, functionTargets, paths), Iter: kirExpr(s.Iter, c, functionTargets, paths), Return: kirExpr(s.Return, c, functionTargets, paths), Scrutinee: kirExpr(s.Scrutinee, c, functionTargets, paths), Arms: make([]*KIRArm, 0, len(s.Arms))}
	if s.Kind == StLet || s.Kind == StConst || s.Kind == StFor {
		typ := typeString(s.Type, s.Annotation)
		k.Binding = kirBinding(s.Name, typ, s.Mutable, s.NameToken, paths)
	}
	for _, arm := range s.Arms {
		k.Arms = append(k.Arms, &KIRArm{Pattern: kirPattern(arm.Pattern, paths), Body: kirStmts(arm.Body, c, functionTargets, paths)})
	}
	return k
}

func kirPattern(p Pattern, paths kirPathNames) *KIRPattern {
	k := &KIRPattern{Kind: patternName(p.Kind), Source: paths.tokenSource(p.Tok), Line: p.Tok.Line, Column: p.Tok.Column, Bool: p.Bool, Int: p.Int, String: p.Str, Type: p.TypeName, Variant: p.Variant, Binding: p.Binding, Present: p.Present, OK: p.OK}
	if p.Binding != "" {
		k.ResolvedBinding = kirBinding(p.Binding, typeString(p.BindingType, nil), false, p.BindingTok, paths)
	}
	return k
}

func kirExpr(e *Expr, c *Checker, functionTargets map[*Function]string, paths kirPathNames) *KIRExpr {
	if e == nil {
		return nil
	}
	genericArguments := make([]string, 0, len(e.GenericArguments))
	for _, argument := range e.GenericArguments {
		genericArguments = append(genericArguments, argument.String())
	}
	structType := ""
	if e.StructType != nil {
		structType = TypeSpecString(e.StructType)
	}
	k := &KIRExpr{Kind: exprName(e.Kind), Source: paths.tokenSource(e.Tok), Line: e.Tok.Line, Column: e.Tok.Column, Type: typeString(e.Type, nil), Int: e.Int, Float: e.Float, Bool: e.Bool, String: e.Str, Name: e.Name, Operator: opText(e.Op), CallTarget: "", TraitName: e.TraitName, Left: kirExpr(e.Left, c, functionTargets, paths), Right: kirExpr(e.Right, c, functionTargets, paths), Operand: kirExpr(e.Operand, c, functionTargets, paths), Args: make([]*KIRExpr, 0, len(e.Args)), Items: make([]*KIRExpr, 0, len(e.Items)), Base: kirExpr(e.Base, c, functionTargets, paths), Field: e.Field, Receiver: kirExpr(e.Receiver, c, functionTargets, paths), MapKeys: make([]*KIRExpr, 0, len(e.MapKeys)), StructName: e.StructName, StructType: structType, GenericArguments: genericArguments, Fields: append([]string(nil), e.Fields...), Values: make([]*KIRExpr, 0, len(e.Values)), EnumType: e.EnumType, EnumVariant: e.EnumVariant, Tail: e.Tail}
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
	if e.Kind == ExLambda {
		k.Lambda = kirLambda(e.Lambda, e.Captures, c, functionTargets, paths)
	} else if e.Kind == ExCall && e.Function == nil && e.Receiver == nil {
		if _, builtin := c.Env.Builtins[e.Name]; !builtin {
			k.Name = ""
			k.Callee = kirExpr(e.Callee, c, functionTargets, paths)
		}
	}
	if b, ok := c.Env.Builtins[e.Name]; ok && e.Kind == ExCall && e.Receiver == nil && e.Function == nil && (e.Callee == nil || e.Callee.Type == nil || e.Callee.Type.Kind != TyFunction) {
		k.BuiltinID = b.ID
		k.CallTarget = "builtin:" + b.Name
	} else if e.Function != nil && e.TraitName != "" && e.Function.Receiver == nil {
		k.CallTarget = "trait:" + e.TraitName + "::" + e.Function.Name
	} else if e.Function != nil {
		target := functionTargets[e.Function]
		if target == "" {
			target = e.Function.Name
		}
		k.CallTarget = "function:" + target
	} else if e.Kind == ExCall && k.Callee == nil {
		k.CallTarget = "function:" + e.Name
	} else if e.Kind == ExVar && e.Function != nil {
		target := functionTargets[e.Function]
		if target == "" {
			target = e.Function.Name
		}
		k.CallTarget = "function:" + target
	}
	return k
}

func kirLambda(function *Function, captures []Capture, c *Checker, functionTargets map[*Function]string, paths kirPathNames) *KIRFunction {
	if function == nil {
		return nil
	}
	k := &KIRFunction{Name: function.Name, Source: paths.tokenSource(function.Tok), Line: function.Tok.Line, Column: function.Tok.Column, Worker: false, Unsafe: function.Unsafe, Module: paths.name(function.Module), Return: typeSpecString(function.Return), TypeParams: []*KIRTypeParam{}, Params: make([]*KIRParam, 0, len(function.Params)), Captures: make([]*KIRCapture, 0, len(captures)), Body: kirStmts(function.Body, c, functionTargets, paths)}
	for _, parameter := range function.Params {
		k.Params = append(k.Params, &KIRParam{Name: parameter.Name, Type: typeSpecString(parameter.Type), Default: nil, Binding: kirBinding(parameter.Name, typeSpecString(parameter.Type), false, parameter.Tok, paths)})
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

func exprName(k ExprKind) string {
	return []string{"int", "float", "bool", "nil", "string", "var", "unary", "binary", "call", "array", "index", "field", "struct", "enum", "map", "set", "propagate", "lambda"}[int(k)]
}

func stmtName(k StmtKind) string {
	return []string{"let", "expr", "assign", "if", "while", "return", "break", "continue", "match", "for", "defer", "unsafe", "const"}[int(k)]
}

func patternName(k PatternKind) string {
	return []string{"wildcard", "nil", "bool", "int", "string", "enum", "option", "result"}[int(k)]
}

func valueName(k ValueKind) string {
	return []string{"nil", "int", "uint", "float", "bool", "string", "bytes", "array", "struct", "enum", "option", "result", "channel", "thread", "map", "set", "json", "websocket", "actor", "shared", "task_group", "regex", "random", "sqlite", "tcp", "tcp_listener", "udp", "ffi_library", "ffi_symbol", "ffi_buffer", "tail_call", "function"}[int(k)]
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
	if lim.MaxJSONBytes > 0 && len(data) > lim.MaxJSONBytes {
		return nil, fmt.Errorf("KIR document exceeds configured JSON limit")
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
	if d.Version < 1 || d.Version > KIRVersion {
		return nil, fmt.Errorf("unsupported KIR version %d", d.Version)
	}
	if d.LanguageVersion == "" && d.Version == 1 {
		// KIR v1 predates explicit language-version metadata and is defined to
		// describe the single legacy dialect, now identified as language 1.0.0.
		d.LanguageVersion = LanguageVersion
	}
	if err := checkLanguageVersion(d.LanguageVersion); err != nil {
		return nil, err
	}
	if d.Target.OS == "" || d.Target.Arch == "" {
		return nil, fmt.Errorf("KIR target is incomplete")
	}
	if err := validateKIRDocument(&d, lim); err != nil {
		return nil, fmt.Errorf("invalid KIR: %w", err)
	}
	return &d, nil
}
