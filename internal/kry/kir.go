package kry

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
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

// KIRSourceSpan is a byte range in the named source file. Its end offset is
// exclusive. A nil span means the older wire version did not carry offsets.
type KIRSourceSpan struct {
	Start int `json:"-"`
	End   int `json:"-"`
}

func cloneKIRSourceSpan(span *KIRSourceSpan) *KIRSourceSpan {
	if span == nil {
		return nil
	}
	value := *span
	return &value
}

func kirSourceSpan(start, end Token) *KIRSourceSpan {
	if start.Source == nil || end.Source == nil || start.Source != end.Source || start.Start < 0 || end.Start < start.Start || end.Length < 0 || start.Start > len(start.Source.Text) || end.Start > len(end.Source.Text) || end.Length > len(end.Source.Text)-end.Start {
		return nil
	}
	return &KIRSourceSpan{Start: start.Start, End: end.Start + end.Length}
}

func kirTokenSpan(token Token) *KIRSourceSpan { return kirSourceSpan(token, token) }

func kirBindingID(name string, token Token, paths kirPathNames) string {
	source := paths.tokenSource(token)
	return kirBindingIDFromParts(name, source, token.Start, token.Line, token.Column)
}

func kirBindingIDFromParts(name, source string, start, line, column int) string {
	identity := fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%s", source, start, line, column, name)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func kirLegacyBindingID(binding *KIRBinding) string {
	identity := fmt.Sprintf("legacy\x00%s\x00%d\x00%d\x00%s", binding.Source, binding.Line, binding.Column, binding.Name)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

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
	ImportRecords   []*KIRImport    `json:"-"`
	Sources         []string        `json:"sources"`
	Structs         []*KIRStruct    `json:"structs"`
	Enums           []*KIREnum      `json:"enums"`
	Traits          []*KIRTrait     `json:"traits"`
	TraitImpls      []*KIRTraitImpl `json:"trait_impls"`
	Functions       []*KIRFunction  `json:"functions"`
	Statements      []*KIRStmt      `json:"statements"`
}

type KIRImport struct {
	Path   string         `json:"-"`
	Source string         `json:"-"`
	Line   int            `json:"-"`
	Column int            `json:"-"`
	Span   *KIRSourceSpan `json:"-"`
}

type KIRStruct struct {
	Name       string          `json:"name"`
	Source     string          `json:"-"`
	Line       int             `json:"-"`
	Column     int             `json:"-"`
	Span       *KIRSourceSpan  `json:"-"`
	Public     bool            `json:"public"`
	Module     string          `json:"module"`
	TypeParams []*KIRTypeParam `json:"type_params"`
	Fields     []*KIRField     `json:"fields"`
}

type KIRField struct {
	Name   string         `json:"name"`
	Source string         `json:"-"`
	Line   int            `json:"-"`
	Column int            `json:"-"`
	Span   *KIRSourceSpan `json:"-"`
	Public bool           `json:"public"`
	Type   string         `json:"type"`
}

type KIREnum struct {
	Name         string           `json:"name"`
	Source       string           `json:"-"`
	Line         int              `json:"-"`
	Column       int              `json:"-"`
	Span         *KIRSourceSpan   `json:"-"`
	Public       bool             `json:"public"`
	Module       string           `json:"module"`
	Variants     []string         `json:"variants"`
	VariantSpans []*KIRSourceSpan `json:"-"`
}

type KIRTrait struct {
	Name    string            `json:"name"`
	Source  string            `json:"-"`
	Line    int               `json:"-"`
	Column  int               `json:"-"`
	Span    *KIRSourceSpan    `json:"-"`
	Public  bool              `json:"public"`
	Module  string            `json:"module"`
	Methods []*KIRTraitMethod `json:"methods"`
}

type KIRTraitMethod struct {
	Name   string         `json:"name"`
	Source string         `json:"-"`
	Line   int            `json:"-"`
	Column int            `json:"-"`
	Span   *KIRSourceSpan `json:"-"`
	Params []*KIRParam    `json:"params"`
	Return string         `json:"return"`
}

type KIRTraitImpl struct {
	Trait   string                `json:"trait"`
	Source  string                `json:"-"`
	Line    int                   `json:"-"`
	Column  int                   `json:"-"`
	Span    *KIRSourceSpan        `json:"-"`
	For     string                `json:"for"`
	Module  string                `json:"module"`
	Methods []*KIRTraitImplMethod `json:"methods"`
}

type KIRTraitImplMethod struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

type KIRTypeParam struct {
	Name       string         `json:"name"`
	Constraint string         `json:"constraint"`
	Source     string         `json:"-"`
	Line       int            `json:"-"`
	Column     int            `json:"-"`
	Span       *KIRSourceSpan `json:"-"`
}

type KIRParam struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Source  string         `json:"-"`
	Line    int            `json:"-"`
	Column  int            `json:"-"`
	Span    *KIRSourceSpan `json:"-"`
	Default *KIRExpr       `json:"default"`
	Binding *KIRBinding    `json:"binding"`
}

type KIRBinding struct {
	ID      string         `json:"-"`
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Mutable bool           `json:"mutable"`
	Source  string         `json:"source"`
	Line    int            `json:"line"`
	Column  int            `json:"column"`
	Span    *KIRSourceSpan `json:"-"`
}

type KIRFunction struct {
	Name       string          `json:"name"`
	Source     string          `json:"source"`
	Line       int             `json:"line"`
	Column     int             `json:"column"`
	Span       *KIRSourceSpan  `json:"-"`
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
	Kind             string         `json:"kind"`
	Source           string         `json:"source"`
	Line             int            `json:"line"`
	Column           int            `json:"column"`
	Span             *KIRSourceSpan `json:"-"`
	Type             string         `json:"type"`
	Const            *KIRValue      `json:"const"`
	Int              int64          `json:"int"`
	UInt             uint64         `json:"uint"`
	UIntBits         uint8          `json:"uint_bits"`
	Float            float64        `json:"float"`
	Bool             bool           `json:"bool"`
	String           string         `json:"string"`
	Name             string         `json:"name"`
	Operator         string         `json:"operator"`
	CallTarget       string         `json:"call_target"`
	TraitName        string         `json:"trait_name"`
	BuiltinID        string         `json:"builtin_id"`
	Left             *KIRExpr       `json:"left"`
	Right            *KIRExpr       `json:"right"`
	Operand          *KIRExpr       `json:"operand"`
	Args             []*KIRExpr     `json:"args"`
	Items            []*KIRExpr     `json:"items"`
	Base             *KIRExpr       `json:"base"`
	Field            string         `json:"field"`
	Receiver         *KIRExpr       `json:"receiver"`
	MapKeys          []*KIRExpr     `json:"map_keys"`
	StructName       string         `json:"struct_name"`
	StructType       string         `json:"struct_type"`
	GenericArguments []string       `json:"generic_arguments"`
	Fields           []string       `json:"fields"`
	Values           []*KIRExpr     `json:"values"`
	EnumType         string         `json:"enum_type"`
	EnumVariant      string         `json:"enum_variant"`
	Tail             bool           `json:"tail"`
	Callee           *KIRExpr       `json:"callee"`
	Lambda           *KIRFunction   `json:"lambda"`
	Binding          *KIRBinding    `json:"binding"`
}

type KIRStmt struct {
	Kind       string         `json:"kind"`
	Source     string         `json:"source"`
	Line       int            `json:"line"`
	Column     int            `json:"column"`
	Span       *KIRSourceSpan `json:"-"`
	Name       string         `json:"name"`
	Binding    *KIRBinding    `json:"binding"`
	Mutable    bool           `json:"mutable"`
	Const      bool           `json:"const"`
	Annotation string         `json:"annotation"`
	Init       *KIRExpr       `json:"init"`
	Expr       *KIRExpr       `json:"expr"`
	Target     *KIRExpr       `json:"target"`
	Value      *KIRExpr       `json:"value"`
	Cond       *KIRExpr       `json:"cond"`
	Then       []*KIRStmt     `json:"then"`
	Else       []*KIRStmt     `json:"else"`
	Body       []*KIRStmt     `json:"body"`
	Iter       *KIRExpr       `json:"iter"`
	Return     *KIRExpr       `json:"return"`
	Scrutinee  *KIRExpr       `json:"scrutinee"`
	Arms       []*KIRArm      `json:"arms"`
}

type KIRArm struct {
	Source  string         `json:"-"`
	Span    *KIRSourceSpan `json:"-"`
	Pattern *KIRPattern    `json:"pattern"`
	Body    []*KIRStmt     `json:"body"`
}

type KIRPattern struct {
	Kind            string         `json:"kind"`
	Source          string         `json:"source"`
	Line            int            `json:"line"`
	Column          int            `json:"column"`
	Span            *KIRSourceSpan `json:"-"`
	Bool            bool           `json:"bool"`
	Int             int64          `json:"int"`
	String          string         `json:"string"`
	Type            string         `json:"type"`
	Variant         string         `json:"variant"`
	Binding         string         `json:"binding"`
	Present         bool           `json:"present"`
	OK              bool           `json:"ok"`
	ResolvedBinding *KIRBinding    `json:"resolved_binding"`
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

// EmitKIR serializes the validated arena into deterministic KIR v5 JSON.
// Source compilation and in-process lowerers share CompileMIR and do not need
// a JSON encode/decode round trip.
func EmitKIR(p *Program, c *Checker, target NativeTarget) ([]byte, error) {
	mir, err := CompileMIR(p, c, target)
	if err != nil {
		return nil, err
	}
	return mir.MarshalKIR()
}

// MarshalKIR encodes the validated arena using the deterministic wire format
// used by `emit` and portable artifacts.
func (mir *ValidatedMIR) MarshalKIR() ([]byte, error) {
	document, err := mir.documentView()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
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
	return &KIRBinding{ID: kirBindingID(name, token, paths), Name: name, Type: typ, Mutable: mutable, Source: paths.tokenSource(token), Line: token.Line, Column: token.Column, Span: kirTokenSpan(token)}
}

// kirExprScalars builds only the checked scalar payload for an expression.
// Source compilation uses it to populate arena rows without first creating a
// recursive KIR tree; kirExpr adds the portable tree edges for wire fixtures.
func kirExprScalars(e *Expr, c *Checker, functionTargets map[*Function]string, paths kirPathNames) *KIRExpr {
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
	k := &KIRExpr{Kind: exprName(e.Kind), Source: paths.tokenSource(e.Tok), Line: e.Tok.Line, Column: e.Tok.Column, Span: kirSourceSpan(e.StartToken, e.EndToken), Type: typeString(e.Type, nil), Int: e.Int, Float: e.Float, Bool: e.Bool, String: e.Str, Name: e.Name, Operator: opText(e.Op), CallTarget: "", TraitName: e.TraitName, Field: e.Field, StructName: e.StructName, StructType: structType, GenericArguments: genericArguments, Fields: append([]string(nil), e.Fields...), EnumType: e.EnumType, EnumVariant: e.EnumVariant, Tail: e.Tail}
	if e.Kind == ExCall && e.Function == nil && e.Receiver == nil && e.Callee != nil {
		if _, builtin := c.Env.Builtins[e.Name]; !builtin {
			k.Name = ""
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
	} else if e.Kind == ExCall && e.Callee == nil {
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
	if d.Version < 6 {
		assignLegacyKIRBindingIDs(&d)
	}
	return &d, nil
}

func assignLegacyKIRBindingIDs(document *KIRDocument) {
	bind := func(binding *KIRBinding) {
		if binding != nil && binding.ID == "" {
			binding.ID = kirLegacyBindingID(binding)
		}
	}
	var expression func(*KIRExpr)
	var statement func(*KIRStmt)
	var function func(*KIRFunction)
	var pattern func(*KIRPattern)
	pattern = func(value *KIRPattern) {
		if value != nil {
			bind(value.ResolvedBinding)
		}
	}
	expression = func(value *KIRExpr) {
		if value == nil {
			return
		}
		bind(value.Binding)
		expression(value.Left)
		expression(value.Right)
		expression(value.Operand)
		expression(value.Base)
		expression(value.Receiver)
		expression(value.Callee)
		for _, child := range value.Args {
			expression(child)
		}
		for _, child := range value.Items {
			expression(child)
		}
		for _, child := range value.MapKeys {
			expression(child)
		}
		for _, child := range value.Values {
			expression(child)
		}
		function(value.Lambda)
	}
	statement = func(value *KIRStmt) {
		if value == nil {
			return
		}
		bind(value.Binding)
		expression(value.Init)
		expression(value.Expr)
		expression(value.Target)
		expression(value.Value)
		expression(value.Cond)
		expression(value.Iter)
		expression(value.Return)
		expression(value.Scrutinee)
		for _, child := range value.Then {
			statement(child)
		}
		for _, child := range value.Else {
			statement(child)
		}
		for _, child := range value.Body {
			statement(child)
		}
		for _, arm := range value.Arms {
			if arm == nil {
				continue
			}
			pattern(arm.Pattern)
			for _, child := range arm.Body {
				statement(child)
			}
		}
	}
	function = func(value *KIRFunction) {
		if value == nil {
			return
		}
		for _, parameter := range value.Params {
			if parameter == nil {
				continue
			}
			bind(parameter.Binding)
			expression(parameter.Default)
		}
		for _, capture := range value.Captures {
			if capture != nil {
				bind(capture.Binding)
			}
		}
		for _, child := range value.Body {
			statement(child)
		}
	}
	for _, value := range document.Functions {
		function(value)
	}
	for _, value := range document.Statements {
		statement(value)
	}
}

// DecodeMIR constructs a validated, context-free in-memory arena from the
// interchange document. KIR preserves source names and coordinates for
// diagnostics; source text and visibility sidecars are not part of its portable
// wire format. JSON is decoded once here; lowerers receive the resulting typed
// arena and never traverse the recursive wire document.
func DecodeMIR(data []byte, lim Limits) (*ValidatedMIR, error) {
	document, err := DecodeKIR(data, lim)
	if err != nil {
		return nil, err
	}
	arena, err := newKIRArena(document)
	if err != nil {
		return nil, fmt.Errorf("build validated MIR arena: %w", err)
	}
	return &ValidatedMIR{arena: arena, limits: lim}, nil
}
