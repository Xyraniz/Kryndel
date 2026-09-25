package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Xyraniz/Kryndel/internal/kry"
	"go.lsp.dev/jsonrpc2"
)

type Position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type document struct {
	URI     string
	Path    string
	Text    string
	Version int32
}

type Server struct {
	mu       sync.RWMutex
	docs     map[string]document
	conn     jsonrpc2.Conn
	exit     chan struct{}
	shutdown bool
}

func NewServer() *Server {
	return &Server{docs: make(map[string]document), exit: make(chan struct{})}
}

// Run serves the LSP over the standard input and output streams. It returns
// status 1 when the client exits without first sending shutdown.
func Run(ctx context.Context, in io.Reader, out io.Writer) (int, error) {
	s := NewServer()
	stream := jsonrpc2.NewStream(&stdio{Reader: in, Writer: out})
	return s.serve(ctx, stream)
}

func (s *Server) serve(ctx context.Context, stream jsonrpc2.Stream) (int, error) {
	conn := jsonrpc2.NewConn(stream)
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	conn.Go(ctx, s.handle)

	select {
	case <-s.exit:
		_ = conn.Close()
		<-conn.Done()
		s.mu.RLock()
		shutdown := s.shutdown
		s.mu.RUnlock()
		if !shutdown {
			return 1, nil
		}
		return 0, nil
	case <-conn.Done():
		if err := conn.Err(); err != nil && !errors.Is(err, io.EOF) {
			return 1, err
		}
		return 0, nil
	case <-ctx.Done():
		_ = conn.Close()
		<-conn.Done()
		return 1, ctx.Err()
	}
}

type stdio struct {
	Reader io.Reader
	Writer io.Writer
}

func (s *stdio) Read(p []byte) (int, error)  { return s.Reader.Read(p) }
func (s *stdio) Write(p []byte) (int, error) { return s.Writer.Write(p) }
func (s *stdio) Close() error {
	if closer, ok := s.Reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *Server) handle(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	result, err := s.dispatch(ctx, req)
	if _, ok := req.(*jsonrpc2.Call); !ok {
		return err
	}
	return reply(ctx, result, err)
}

func (s *Server) dispatch(ctx context.Context, req jsonrpc2.Request) (any, error) {
	switch req.Method() {
	case "initialize":
		return map[string]any{
			"capabilities": map[string]any{
				"positionEncoding":           "utf-16",
				"textDocumentSync":           map[string]any{"openClose": true, "change": 1},
				"definitionProvider":         true,
				"hoverProvider":              true,
				"completionProvider":         map[string]any{"resolveProvider": false},
				"documentFormattingProvider": true,
			},
			"serverInfo": map[string]any{"name": "kryndel-language-server"},
		}, nil
	case "initialized", "$/setTrace":
		return nil, nil
	case "shutdown":
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		return nil, nil
	case "exit":
		select {
		case <-s.exit:
		default:
			close(s.exit)
		}
		return nil, nil
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI        string `json:"uri"`
				LanguageID string `json:"languageId"`
				Version    int32  `json:"version"`
				Text       string `json:"text"`
			} `json:"textDocument"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return nil, s.open(ctx, p.TextDocument.URI, p.TextDocument.Text, p.TextDocument.Version)
	case "textDocument/didChange":
		var p struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version int32  `json:"version"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		if len(p.ContentChanges) == 0 {
			return nil, nil
		}
		return nil, s.change(ctx, p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text, p.TextDocument.Version)
	case "textDocument/didSave":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Text *string `json:"text"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		if p.Text != nil {
			return nil, s.change(ctx, p.TextDocument.URI, *p.Text, 0)
		}
		return nil, s.validateWorkspace(ctx)
	case "textDocument/didClose":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		s.mu.Lock()
		delete(s.docs, p.TextDocument.URI)
		s.mu.Unlock()
		if err := s.publish(ctx, p.TextDocument.URI, nil, []any{}); err != nil {
			return nil, err
		}
		return nil, s.validateWorkspace(ctx)
	case "textDocument/definition":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.definition(p.TextDocument.URI, p.Position)
	case "textDocument/hover":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.hover(p.TextDocument.URI, p.Position)
	case "textDocument/completion":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.completion(p.TextDocument.URI, p.Position)
	case "textDocument/formatting":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Options struct {
				TabSize      uint32 `json:"tabSize"`
				InsertSpaces bool   `json:"insertSpaces"`
			} `json:"options"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.format(p.TextDocument.URI)
	default:
		if _, ok := req.(*jsonrpc2.Call); ok {
			return nil, jsonrpc2.Errorf(-32601, "method not found: %s", req.Method())
		}
		return nil, nil
	}
}

func decodeParams(req jsonrpc2.Request, dst any) error {
	if len(req.Params()) == 0 {
		return fmt.Errorf("missing request parameters")
	}
	if err := json.Unmarshal(req.Params(), dst); err != nil {
		return fmt.Errorf("invalid request parameters: %w", err)
	}
	return nil
}

func invalidParams(err error) error { return jsonrpc2.Errorf(-32602, "%v", err) }

func (s *Server) open(ctx context.Context, uri, text string, version int32) error {
	path, err := pathFromURI(uri)
	if err != nil || filepath.Ext(path) != ".kry" {
		return s.publish(ctx, uri, nil, []any{})
	}
	s.mu.Lock()
	s.docs[uri] = document{URI: uri, Path: path, Text: text, Version: version}
	s.mu.Unlock()
	return s.validateWorkspace(ctx)
}

func (s *Server) change(ctx context.Context, uri, text string, version int32) error {
	s.mu.Lock()
	doc, ok := s.docs[uri]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if version != 0 && version < doc.Version {
		s.mu.Unlock()
		return nil
	}
	doc.Text = text
	if version != 0 {
		doc.Version = version
	}
	s.docs[uri] = doc
	s.mu.Unlock()
	return s.validateWorkspace(ctx)
}

func (s *Server) snapshot(uri string) (document, map[string]string, bool) {
	s.mu.RLock()
	doc, ok := s.docs[uri]
	if !ok {
		s.mu.RUnlock()
		return document{}, nil, false
	}
	sources := make(map[string]string, len(s.docs))
	for _, open := range s.docs {
		sources[open.Path] = open.Text
	}
	s.mu.RUnlock()
	return doc, sources, true
}

func (s *Server) analysis(uri string) (document, *kry.Program, *kry.Checker, *kry.Diagnostic) {
	doc, sources, ok := s.snapshot(uri)
	if !ok {
		return document{}, nil, nil, nil
	}
	prog, d := kry.LoadProgramWithSources(doc.Path, sources, kry.DefaultLimits())
	if d != nil {
		return doc, nil, nil, d
	}
	checker, d := kry.Check(prog, kry.DefaultLimits())
	if d != nil {
		return doc, prog, nil, d
	}
	if _, d = kry.ValidateIR(prog, kry.DefaultLimits()); d != nil {
		return doc, prog, checker, d
	}
	return doc, prog, checker, nil
}

func (s *Server) validate(ctx context.Context, uri string) error {
	doc, _, _, d := s.analysis(uri)
	if doc.URI == "" {
		return nil
	}
	if d == nil {
		return s.publish(ctx, uri, doc.Version, []any{})
	}
	return s.publishDiagnostic(ctx, uri, doc, d)
}

func (s *Server) validateWorkspace(ctx context.Context) error {
	s.mu.RLock()
	uris := make([]string, 0, len(s.docs))
	for uri := range s.docs {
		uris = append(uris, uri)
	}
	s.mu.RUnlock()
	sort.Strings(uris)
	for _, uri := range uris {
		if err := s.validate(ctx, uri); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) publishDiagnostic(ctx context.Context, uri string, doc document, d *kry.Diagnostic) error {
	fileURI := uri
	if d.Source != "" && d.Source != "<input>" {
		fileURI = uriFromPath(d.Source)
	}
	text := doc.Text
	if d.Source != doc.Path {
		s.mu.RLock()
		for _, open := range s.docs {
			if open.Path == d.Source {
				text = open.Text
				break
			}
		}
		s.mu.RUnlock()
		if text == doc.Text {
			if source, err := os.ReadFile(d.Source); err == nil {
				text = string(source)
			}
		}
	}
	start := offsetFromLineColumn(text, d.Line, d.Column)
	end := start
	if start < len(text) {
		_, size := utf8.DecodeRuneInString(text[start:])
		if size > 0 {
			end += size
		}
	}
	severity := 1
	params := map[string]any{
		"uri": fileURI,
		"diagnostics": []any{map[string]any{
			"range":    Range{Start: positionAt(text, start), End: positionAt(text, end)},
			"severity": severity,
			"code":     d.Code,
			"source":   "kryndel",
			"message":  d.Message,
		}},
	}
	if fileURI == uri {
		params["version"] = doc.Version
	}
	return s.notify(ctx, "textDocument/publishDiagnostics", params)
}

func (s *Server) publish(ctx context.Context, uri string, version any, diagnostics []any) error {
	params := map[string]any{"uri": uri, "diagnostics": diagnostics}
	if version != nil {
		params["version"] = version
	}
	return s.notify(ctx, "textDocument/publishDiagnostics", params)
}

func (s *Server) notify(ctx context.Context, method string, params any) error {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil {
		return nil
	}
	return conn.Notify(ctx, method, params)
}

func (s *Server) definition(uri string, pos Position) (any, error) {
	doc, prog, _, d := s.analysis(uri)
	if doc.URI == "" || d != nil || prog == nil {
		return nil, nil
	}
	selected, ok := tokenAt(doc.Text, pos)
	if !ok {
		return nil, nil
	}
	selected.Source.Name = doc.Path
	var found *kry.Expr
	walkProgram(prog, func(e *kry.Expr) {
		if exprMatchesToken(e, selected) {
			found = e
		}
	})
	if found == nil {
		return nil, nil
	}
	tok := found.Definition
	if found.VariantToken.Source != nil && found.VariantToken.Source.Name == doc.Path && found.VariantToken.Start == selected.Start {
		tok = found.VariantDefinition
	}
	if tok.Source == nil {
		return nil, nil
	}
	return map[string]any{
		"uri":   uriFromPath(tok.Source.Name),
		"range": tokenRange(tok),
	}, nil
}

func (s *Server) hover(uri string, pos Position) (any, error) {
	doc, prog, checker, d := s.analysis(uri)
	if doc.URI == "" || d != nil || prog == nil || checker == nil {
		return nil, nil
	}
	selected, ok := tokenAt(doc.Text, pos)
	if !ok {
		return nil, nil
	}
	selected.Source.Name = doc.Path
	var found *kry.Expr
	walkProgram(prog, func(e *kry.Expr) {
		if exprMatchesToken(e, selected) {
			found = e
		}
	})
	if found == nil {
		return nil, nil
	}
	detail := hoverText(found, checker, selected)
	if detail == "" {
		return nil, nil
	}
	return map[string]any{
		"contents": map[string]any{"kind": "markdown", "value": "```kryndel\n" + detail + "\n```"},
		"range":    tokenRange(selected),
	}, nil
}

func hoverText(e *kry.Expr, checker *kry.Checker, selected kry.Token) string {
	if e.VariantToken.Source != nil && e.VariantToken.Source.Name == selected.Source.Name && e.VariantToken.Start == selected.Start {
		if e.Type != nil {
			return e.Type.String() + "::" + e.EnumVariant
		}
	}
	if e.Function != nil {
		return functionSignature(e.Function)
	}
	if e.Kind == kry.ExCall {
		if b, ok := checker.Env.Builtins[e.Name]; ok {
			return b.Signature
		}
	}
	if e.Kind == kry.ExField && e.Type != nil {
		return e.Field + ": " + e.Type.String()
	}
	if e.Kind == kry.ExStruct && e.Type != nil {
		return e.StructName + " = " + e.Type.String()
	}
	if e.Kind == kry.ExEnum && e.Type != nil {
		return e.EnumType + " = " + e.Type.String()
	}
	if e.Kind == kry.ExVar && e.Type != nil {
		return e.Name + ": " + e.Type.String()
	}
	if e.Kind == kry.ExCall && e.Type != nil {
		return e.Name + "(...) -> " + e.Type.String()
	}
	return ""
}

func functionSignature(f *kry.Function) string {
	var b strings.Builder
	b.WriteString("fn ")
	if f.Receiver != nil {
		b.WriteString(kry.TypeSpecString(f.Receiver))
		b.WriteByte('.')
	}
	b.WriteString(f.Name)
	if len(f.TypeParams) > 0 {
		b.WriteByte('[')
		for i, p := range f.TypeParams {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(p.Name)
			if p.Constraint != "" {
				b.WriteString(": " + p.Constraint)
			}
		}
		b.WriteByte(']')
	}
	b.WriteByte('(')
	for i, p := range f.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.Name + ": " + kry.TypeSpecString(p.Type))
		if p.Default != nil {
			b.WriteString(" = …")
		}
	}
	b.WriteString(") -> ")
	b.WriteString(kry.TypeSpecString(f.Return))
	return b.String()
}

func (s *Server) completion(uri string, pos Position) (any, error) {
	doc, _, ok := s.snapshot(uri)
	if !ok {
		return map[string]any{"isIncomplete": false, "items": []any{}}, nil
	}
	prefix := prefixAt(doc.Text, pos)
	items := map[string]map[string]any{}
	add := func(label, detail string, kind int) {
		if !strings.HasPrefix(label, prefix) {
			return
		}
		if _, exists := items[label]; !exists {
			items[label] = map[string]any{"label": label, "kind": kind, "detail": detail}
		}
	}
	for _, keyword := range []string{"fn", "let", "mut", "const", "if", "else", "while", "for", "in", "return", "match", "enum", "struct", "import", "pub", "private", "defer", "unsafe", "break", "continue", "true", "false", "nil"} {
		add(keyword, "keyword", 14)
	}
	for _, name := range []string{"Int", "UInt8", "UInt16", "UInt32", "UInt64", "Float", "Bool", "String", "Bytes", "Json", "Nil", "Option", "Result", "Array", "Map", "Set", "Channel", "Thread", "Shared", "Actor", "TaskGroup"} {
		add(name, "Kryndel type", 25)
	}
	for name, builtin := range kry.Builtins() {
		add(name, builtin.Signature, 3)
	}
	sources := map[string]string{doc.Path: doc.Text}
	s.mu.RLock()
	for _, open := range s.docs {
		sources[open.Path] = open.Text
	}
	s.mu.RUnlock()
	if prog, d := kry.LoadProgramWithSources(doc.Path, sources, kry.DefaultLimits()); d == nil {
		for _, fn := range prog.Functions {
			if fn.Public || fn.VisibilityScope == prog.VisibilityScope {
				add(fn.Name, functionSignature(fn), 3)
			}
		}
		for _, typ := range prog.Structs {
			if typ.Public || typ.VisibilityScope == prog.VisibilityScope {
				add(typ.Name, "struct", 22)
			}
		}
		for _, typ := range prog.Enums {
			if typ.Public || typ.VisibilityScope == prog.VisibilityScope {
				add(typ.Name, "enum", 13)
			}
		}
	}
	labels := make([]string, 0, len(items))
	for label := range items {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	ordered := make([]any, 0, len(labels))
	for _, label := range labels {
		ordered = append(ordered, items[label])
	}
	return map[string]any{"isIncomplete": false, "items": ordered}, nil
}

func (s *Server) format(uri string) (any, error) {
	doc, _, ok := s.snapshot(uri)
	if !ok {
		return []any{}, nil
	}
	src := &kry.Source{Name: doc.Path, Text: doc.Text}
	formatted, d := kry.FormatSource(src, kry.DefaultLimits())
	if d != nil {
		return []any{}, nil
	}
	if formatted == doc.Text {
		return []any{}, nil
	}
	end := positionAt(doc.Text, len(doc.Text))
	return []any{map[string]any{"range": Range{Start: Position{}, End: end}, "newText": formatted}}, nil
}

func tokenAt(text string, pos Position) (kry.Token, bool) {
	src := &kry.Source{Name: "<buffer>", Text: text}
	tokens, d := kry.Lex(src, kry.DefaultLimits())
	if d != nil {
		return kry.Token{}, false
	}
	offset := offsetAt(text, pos)
	for _, token := range tokens {
		if token.Kind == kry.EOF {
			continue
		}
		if offset >= token.Start && offset < token.Start+token.Length && token.Kind == kry.ID {
			return token, true
		}
	}
	return kry.Token{}, false
}

func prefixAt(text string, pos Position) string {
	src := &kry.Source{Name: "<buffer>", Text: text}
	tokens, d := kry.Lex(src, kry.DefaultLimits())
	if d != nil {
		return ""
	}
	offset := offsetAt(text, pos)
	for _, token := range tokens {
		if token.Kind == kry.ID && offset >= token.Start && offset <= token.Start+token.Length {
			return text[token.Start:offset]
		}
	}
	return ""
}

func exprMatchesToken(e *kry.Expr, selected kry.Token) bool {
	if e == nil || selected.Source == nil {
		return false
	}
	tokens := [...]kry.Token{e.NameToken, e.Tok, e.VariantToken}
	for _, tok := range tokens {
		if tok.Source != nil && tok.Source.Name == selected.Source.Name && tok.Start == selected.Start && tok.Length == selected.Length {
			return true
		}
	}
	return false
}

func walkProgram(p *kry.Program, visit func(*kry.Expr)) {
	if p == nil {
		return
	}
	for _, stmt := range p.Statements {
		walkStmt(stmt, visit)
	}
	for _, fn := range p.Functions {
		for _, param := range fn.Params {
			walkExpr(param.Default, visit)
		}
		for _, stmt := range fn.Body {
			walkStmt(stmt, visit)
		}
	}
}

func walkStmt(s *kry.Stmt, visit func(*kry.Expr)) {
	if s == nil {
		return
	}
	walkExpr(s.Init, visit)
	walkExpr(s.Expr, visit)
	walkExpr(s.Target, visit)
	walkExpr(s.Value, visit)
	walkExpr(s.Cond, visit)
	walkExpr(s.Iter, visit)
	walkExpr(s.Return, visit)
	walkExpr(s.Scrutinee, visit)
	for _, list := range [][]*kry.Stmt{s.Then, s.Else, s.Body} {
		for _, child := range list {
			walkStmt(child, visit)
		}
	}
	for _, arm := range s.Arms {
		for _, child := range arm.Body {
			walkStmt(child, visit)
		}
	}
}

func walkExpr(e *kry.Expr, visit func(*kry.Expr)) {
	if e == nil {
		return
	}
	visit(e)
	for _, child := range []*kry.Expr{e.Left, e.Right, e.Operand, e.Base, e.Receiver} {
		walkExpr(child, visit)
	}
	for _, child := range e.Args {
		walkExpr(child, visit)
	}
	for _, child := range e.Items {
		walkExpr(child, visit)
	}
	for _, child := range e.MapKeys {
		walkExpr(child, visit)
	}
	for _, child := range e.Values {
		walkExpr(child, visit)
	}
}

func tokenRange(tok kry.Token) Range {
	text := ""
	if tok.Source != nil {
		text = tok.Source.Text
	}
	start := tok.Start
	if start < 0 || start > len(text) {
		start = offsetFromLineColumn(text, tok.Line, tok.Column)
	}
	end := start + tok.Length
	if end < start || end > len(text) {
		end = start
		if end < len(text) {
			_, size := utf8.DecodeRuneInString(text[end:])
			end += size
		}
	}
	return Range{Start: positionAt(text, start), End: positionAt(text, end)}
}

func offsetFromLineColumn(text string, line, column int) int {
	return offsetAt(text, Position{Line: uint32(max(0, line-1)), Character: uint32(max(0, column-1))})
}

func positionAt(text string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	var line, character uint32
	for i := 0; i < offset; {
		r, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			break
		}
		if r == '\n' {
			line++
			character = 0
		} else if r != '\r' {
			character += uint32(len(utf16.Encode([]rune{r})))
		}
		i += size
	}
	return Position{Line: line, Character: character}
}

func offsetAt(text string, pos Position) int {
	lineStart := 0
	for line := uint32(0); line < pos.Line; line++ {
		next := strings.IndexByte(text[lineStart:], '\n')
		if next < 0 {
			return len(text)
		}
		lineStart += next + 1
	}
	lineEnd := len(text)
	if next := strings.IndexByte(text[lineStart:], '\n'); next >= 0 {
		lineEnd = lineStart + next
	}
	if lineEnd > lineStart && text[lineEnd-1] == '\r' {
		lineEnd--
	}
	units := uint32(0)
	for i := lineStart; i < lineEnd; {
		if units >= pos.Character {
			return i
		}
		r, size := utf8.DecodeRuneInString(text[i:lineEnd])
		if size == 0 {
			break
		}
		width := uint32(len(utf16.Encode([]rune{r})))
		if units+width > pos.Character {
			return i
		}
		units += width
		i += size
	}
	return lineEnd
}

func pathFromURI(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" || u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("unsupported document URI")
	}
	path := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == filepath.Separator && path[2] == ':' {
		path = path[1:]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func uriFromPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	path = filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && len(path) >= 2 && path[1] == ':' {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func (s *Server) document(uri string) (document, bool) {
	s.mu.RLock()
	doc, ok := s.docs[uri]
	s.mu.RUnlock()
	return doc, ok
}

var _ io.ReadWriteCloser = (*stdio)(nil)
