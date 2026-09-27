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
				"referencesProvider":         true,
				"documentSymbolProvider":     true,
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
	case "textDocument/references":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
			Context  struct {
				IncludeDeclaration bool `json:"includeDeclaration"`
			} `json:"context"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.references(p.TextDocument.URI, p.Position, p.Context.IncludeDeclaration)
	case "textDocument/documentSymbol":
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := decodeParams(req, &p); err != nil {
			return nil, invalidParams(err)
		}
		return s.documentSymbols(p.TextDocument.URI)
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
	doc, prog, _, _ := s.analysis(uri)
	if doc.URI == "" || prog == nil {
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

func (s *Server) references(uri string, pos Position, includeDeclaration bool) (any, error) {
	doc, prog, checker, _ := s.analysis(uri)
	if doc.URI == "" || prog == nil {
		return []any{}, nil
	}
	selected, ok := tokenAt(doc.Text, pos)
	if !ok {
		return []any{}, nil
	}
	selected.Source.Name = doc.Path

	var target kry.Token
	var targetType *kry.Type
	walkProgram(prog, func(e *kry.Expr) {
		if target.Source == nil && exprMatchesToken(e, selected) {
			if sameToken(e.VariantToken, selected) {
				target = e.VariantDefinition
			} else {
				target = e.Definition
			}
		}
	})
	if target.Source == nil && checker != nil {
		targetType = resolvedTypeAt(prog, checker, selected)
	}
	if target.Source == nil && targetType != nil {
		target = typeDeclarationToken(targetType)
	} else if target.Source == nil && declarationTokenExists(prog, selected) {
		target = selected
	}
	if target.Source == nil {
		return []any{}, nil
	}
	programs := []struct {
		program *kry.Program
		checker *kry.Checker
	}{{program: prog, checker: checker}}
	seenPrograms := map[string]bool{doc.Path: true}
	for _, openURI := range s.openURIs() {
		openDoc, openProgram, openChecker, _ := s.analysis(openURI)
		if openDoc.URI == "" || openProgram == nil || seenPrograms[openDoc.Path] {
			continue
		}
		seenPrograms[openDoc.Path] = true
		programs = append(programs, struct {
			program *kry.Program
			checker *kry.Checker
		}{program: openProgram, checker: openChecker})
	}

	tokens := make([]kry.Token, 0)
	seen := make(map[string]bool)
	add := func(token kry.Token) {
		if token.Source == nil {
			return
		}
		key := fmt.Sprintf("%s:%d:%d", token.Source.Name, token.Start, token.Length)
		if seen[key] {
			return
		}
		seen[key] = true
		tokens = append(tokens, token)
	}
	if includeDeclaration {
		add(target)
	}
	for _, candidate := range programs {
		walkProgram(candidate.program, func(e *kry.Expr) {
			if sameToken(e.Definition, target) {
				add(expressionNameToken(e))
			}
			if sameToken(e.VariantDefinition, target) {
				add(e.VariantToken)
			}
		})
		if targetType != nil && candidate.checker != nil && candidate.checker.Env != nil {
			walkTypeSpecs(candidate.program, func(spec *kry.TypeSpec) {
				if sameToken(typeDeclarationToken(candidate.checker.Env.Types[spec.Name]), target) {
					add(spec.Tok)
				}
			})
			walkPatterns(candidate.program, func(pattern kry.Pattern) {
				if pattern.TypeName != "" && sameToken(typeDeclarationToken(candidate.checker.Env.Types[pattern.TypeName]), target) && pattern.Tok.Text() == pattern.TypeName {
					add(pattern.Tok)
				}
			})
		}
	}
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].Source.Name != tokens[j].Source.Name {
			return tokens[i].Source.Name < tokens[j].Source.Name
		}
		return tokens[i].Start < tokens[j].Start
	})
	locations := make([]any, 0, len(tokens))
	for _, token := range tokens {
		locations = append(locations, map[string]any{"uri": uriFromPath(token.Source.Name), "range": tokenRange(token)})
	}
	return locations, nil
}

func (s *Server) openURIs() []string {
	s.mu.RLock()
	uris := make([]string, 0, len(s.docs))
	for uri := range s.docs {
		uris = append(uris, uri)
	}
	s.mu.RUnlock()
	sort.Strings(uris)
	return uris
}

func (s *Server) documentSymbols(uri string) (any, error) {
	doc, prog, _, _ := s.analysis(uri)
	if doc.URI == "" || prog == nil {
		return []any{}, nil
	}
	return documentSymbolsFor(prog, doc.Path), nil
}

func expressionNameToken(e *kry.Expr) kry.Token {
	if e.NameToken.Source != nil {
		return e.NameToken
	}
	return e.Tok
}

func sameToken(a, b kry.Token) bool {
	return a.Source != nil && b.Source != nil && a.Source.Name == b.Source.Name && a.Start == b.Start && a.Length == b.Length
}

func declarationTokenExists(program *kry.Program, selected kry.Token) bool {
	found := false
	walkDeclarationTokens(program, func(token kry.Token) {
		if sameToken(token, selected) {
			found = true
		}
	})
	return found
}

func walkDeclarationTokens(program *kry.Program, visit func(kry.Token)) {
	if program == nil {
		return
	}
	for _, function := range program.Functions {
		visit(function.NameToken)
		for _, parameter := range function.TypeParams {
			visit(parameter.Tok)
		}
		for _, parameter := range function.Params {
			visit(parameter.Tok)
		}
	}
	for _, declaration := range program.Structs {
		visit(declaration.NameToken)
		for _, field := range declaration.Fields {
			visit(field.Tok)
		}
	}
	for _, declaration := range program.Enums {
		visit(declaration.NameToken)
		for _, variant := range declaration.VariantTokens {
			visit(variant)
		}
	}
	var walkStmtDeclarations func(*kry.Stmt)
	walkStmtDeclarations = func(stmt *kry.Stmt) {
		if stmt == nil {
			return
		}
		if stmt.NameToken.Source != nil {
			visit(stmt.NameToken)
		}
		for _, child := range [][]*kry.Stmt{stmt.Then, stmt.Else, stmt.Body} {
			for _, nested := range child {
				walkStmtDeclarations(nested)
			}
		}
		for _, arm := range stmt.Arms {
			if arm.Pattern.BindingTok.Source != nil {
				visit(arm.Pattern.BindingTok)
			}
			for _, nested := range arm.Body {
				walkStmtDeclarations(nested)
			}
		}
	}
	for _, stmt := range program.Statements {
		walkStmtDeclarations(stmt)
	}
	for _, function := range program.Functions {
		for _, stmt := range function.Body {
			walkStmtDeclarations(stmt)
		}
	}
}

func typeDeclarationToken(typ *kry.Type) kry.Token {
	if typ == nil {
		return kry.Token{}
	}
	if typ.Struct != nil {
		return typ.Struct.NameToken
	}
	if typ.Enum != nil {
		return typ.Enum.NameToken
	}
	return kry.Token{}
}

func resolvedTypeAt(program *kry.Program, checker *kry.Checker, selected kry.Token) *kry.Type {
	if checker == nil || checker.Env == nil {
		return nil
	}
	if typ := checker.Env.Types[selected.Text()]; typ != nil && sameToken(typeDeclarationToken(typ), selected) {
		return typ
	}
	var found *kry.Type
	walkTypeSpecs(program, func(spec *kry.TypeSpec) {
		if sameToken(spec.Tok, selected) {
			found = checker.Env.Types[spec.Name]
		}
	})
	if found == nil {
		walkPatterns(program, func(pattern kry.Pattern) {
			if sameToken(pattern.Tok, selected) && pattern.TypeName != "" {
				found = checker.Env.Types[pattern.TypeName]
			}
		})
	}
	return found
}

func walkTypeSpecs(program *kry.Program, visit func(*kry.TypeSpec)) {
	if program == nil {
		return
	}
	var walkType func(*kry.TypeSpec)
	walkType = func(spec *kry.TypeSpec) {
		if spec == nil {
			return
		}
		visit(spec)
		for _, parameter := range spec.Params {
			walkType(parameter)
		}
	}
	var walkStmtTypes func(*kry.Stmt)
	walkStmtTypes = func(stmt *kry.Stmt) {
		if stmt == nil {
			return
		}
		walkType(stmt.Annotation)
		for _, child := range [][]*kry.Stmt{stmt.Then, stmt.Else, stmt.Body} {
			for _, nested := range child {
				walkStmtTypes(nested)
			}
		}
		for _, arm := range stmt.Arms {
			for _, nested := range arm.Body {
				walkStmtTypes(nested)
			}
		}
	}
	for _, function := range program.Functions {
		walkType(function.Receiver)
		walkType(function.Return)
		for _, parameter := range function.Params {
			walkType(parameter.Type)
		}
		for _, stmt := range function.Body {
			walkStmtTypes(stmt)
		}
	}
	for _, declaration := range program.Structs {
		for _, field := range declaration.Fields {
			walkType(field.Spec)
		}
	}
	for _, stmt := range program.Statements {
		walkStmtTypes(stmt)
	}
}

func walkPatterns(program *kry.Program, visit func(kry.Pattern)) {
	if program == nil {
		return
	}
	var walkStmtPatterns func(*kry.Stmt)
	walkStmtPatterns = func(stmt *kry.Stmt) {
		if stmt == nil {
			return
		}
		for _, child := range [][]*kry.Stmt{stmt.Then, stmt.Else, stmt.Body} {
			for _, nested := range child {
				walkStmtPatterns(nested)
			}
		}
		for _, arm := range stmt.Arms {
			visit(arm.Pattern)
			for _, nested := range arm.Body {
				walkStmtPatterns(nested)
			}
		}
	}
	for _, stmt := range program.Statements {
		walkStmtPatterns(stmt)
	}
	for _, function := range program.Functions {
		for _, stmt := range function.Body {
			walkStmtPatterns(stmt)
		}
	}
}

func documentSymbolsFor(program *kry.Program, path string) []any {
	if program == nil {
		return []any{}
	}
	var symbols []any
	for _, declaration := range program.Structs {
		if declaration.NameToken.Source == nil || declaration.NameToken.Source.Name != path {
			continue
		}
		item := newDocumentSymbolRange(declaration.Name, "", 23, declaration.NameToken, declaration.EndToken)
		for _, field := range declaration.Fields {
			if field.Tok.Source != nil && field.Tok.Source.Name == path {
				addDocumentSymbolChild(item, newDocumentSymbol(field.Name, kry.TypeSpecString(field.Spec), 8, field.Tok))
			}
		}
		symbols = append(symbols, item)
	}
	for _, declaration := range program.Enums {
		if declaration.NameToken.Source == nil || declaration.NameToken.Source.Name != path {
			continue
		}
		item := newDocumentSymbolRange(declaration.Name, "", 10, declaration.NameToken, declaration.EndToken)
		for i, variant := range declaration.Variants {
			if i < len(declaration.VariantTokens) && declaration.VariantTokens[i].Source != nil && declaration.VariantTokens[i].Source.Name == path {
				addDocumentSymbolChild(item, newDocumentSymbol(variant, "", 22, declaration.VariantTokens[i]))
			}
		}
		symbols = append(symbols, item)
	}
	for _, function := range program.Functions {
		if function.NameToken.Source == nil || function.NameToken.Source.Name != path {
			continue
		}
		if function.Receiver != nil {
			symbols = append(symbols, functionDocumentSymbol(function, 6, function.Name, path))
			continue
		}
		symbols = append(symbols, functionDocumentSymbol(function, 12, function.Name, path))
	}
	for _, statement := range program.Statements {
		walkBindingDocumentSymbols(statement, path, func(symbol map[string]any) { symbols = append(symbols, symbol) })
	}
	sort.SliceStable(symbols, func(i, j int) bool {
		leftSymbol, _ := symbols[i].(map[string]any)
		rightSymbol, _ := symbols[j].(map[string]any)
		left, _ := leftSymbol["range"].(Range)
		right, _ := rightSymbol["range"].(Range)
		if left.Start.Line != right.Start.Line {
			return left.Start.Line < right.Start.Line
		}
		return left.Start.Character < right.Start.Character
	})
	return symbols
}

func newDocumentSymbol(name, detail string, kind int, token kry.Token) map[string]any {
	return newDocumentSymbolRange(name, detail, kind, token, token)
}

func newDocumentSymbolRange(name, detail string, kind int, start, end kry.Token) map[string]any {
	selectionRange := tokenRange(start)
	rangeValue := tokenSpanRange(start, end)
	symbol := map[string]any{
		"name":           name,
		"kind":           kind,
		"range":          rangeValue,
		"selectionRange": selectionRange,
	}
	if detail != "" {
		symbol["detail"] = detail
	}
	return symbol
}

func addDocumentSymbolChild(parent map[string]any, child map[string]any) {
	children, _ := parent["children"].([]any)
	parent["children"] = append(children, child)
}

func functionDocumentSymbol(function *kry.Function, kind int, name, path string) map[string]any {
	symbol := newDocumentSymbolRange(name, functionSignature(function), kind, function.NameToken, function.EndToken)
	for _, parameter := range function.TypeParams {
		if parameter.Tok.Source != nil && parameter.Tok.Source.Name == path {
			addDocumentSymbolChild(symbol, newDocumentSymbol(parameter.Name, parameter.Constraint, 26, parameter.Tok))
		}
	}
	for _, parameter := range function.Params {
		if parameter.Tok.Source != nil && parameter.Tok.Source.Name == path {
			addDocumentSymbolChild(symbol, newDocumentSymbol(parameter.Name, kry.TypeSpecString(parameter.Type), 13, parameter.Tok))
		}
	}
	for _, statement := range function.Body {
		walkBindingDocumentSymbols(statement, path, func(child map[string]any) { addDocumentSymbolChild(symbol, child) })
	}
	return symbol
}

func walkBindingDocumentSymbols(statement *kry.Stmt, path string, add func(map[string]any)) {
	if statement == nil {
		return
	}
	if binding := bindingDocumentSymbol(statement, path); binding != nil {
		add(binding)
	}
	for _, child := range [][]*kry.Stmt{statement.Then, statement.Else, statement.Body} {
		for _, nested := range child {
			walkBindingDocumentSymbols(nested, path, add)
		}
	}
	for _, arm := range statement.Arms {
		if arm.Pattern.BindingTok.Source != nil && arm.Pattern.BindingTok.Source.Name == path {
			add(newDocumentSymbol(arm.Pattern.Binding, "pattern binding", 13, arm.Pattern.BindingTok))
		}
		for _, nested := range arm.Body {
			walkBindingDocumentSymbols(nested, path, add)
		}
	}
}

func bindingDocumentSymbol(statement *kry.Stmt, path string) map[string]any {
	if statement.NameToken.Source == nil || statement.NameToken.Source.Name != path {
		return nil
	}
	if statement.Kind == kry.StConst {
		return newDocumentSymbolRange(statement.Name, "", 14, statement.NameToken, statement.EndToken)
	}
	if statement.Kind == kry.StLet || statement.Kind == kry.StFor {
		return newDocumentSymbolRange(statement.Name, "", 13, statement.NameToken, statement.EndToken)
	}
	return nil
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
	fieldContext, hasFieldContext := fieldCompletionAt(doc.Text, pos)
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
	if hasFieldContext {
		sources[doc.Path] = fieldContext.source
	}
	if prog, d := kry.LoadProgramWithSources(doc.Path, sources, kry.DefaultLimits()); d == nil {
		// Keep the partial checker annotations even if the identifier under the
		// cursor has an unknown-name diagnostic; they still carry its lexical scope.
		_, _ = kry.Check(prog, kry.DefaultLimits())
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
		if hasFieldContext {
			if receiver := expressionAtToken(prog, doc.Path, fieldContext.receiver); receiver != nil && receiver.Type != nil && receiver.Type.Kind == kry.TyStruct && receiver.Type.Struct != nil {
				for _, field := range receiver.Type.Struct.Fields {
					if field.Public || receiver.Scope != nil && receiver.Type.Struct.VisibilityScope == receiver.Scope.VisibilityScope {
						add(field.Name, "field: "+field.Type.String(), 5)
					}
				}
			}
		} else if selected, found := identifierAt(doc.Text, pos); found {
			if expression := expressionAtToken(prog, doc.Path, selected); expression != nil && expression.Scope != nil {
				addLocalCompletions(add, expression.Scope, doc.Path, selected.Start)
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

type fieldCompletionContext struct {
	receiver kry.Token
	source   string
}

func fieldCompletionAt(text string, pos Position) (fieldCompletionContext, bool) {
	src := &kry.Source{Name: "<buffer>", Text: text}
	tokens, d := kry.Lex(src, kry.DefaultLimits())
	if d != nil {
		return fieldCompletionContext{}, false
	}
	offset := offsetAt(text, pos)
	var selected *kry.Token
	for i := range tokens {
		token := &tokens[i]
		if token.Kind == kry.ID && offset >= token.Start && offset <= token.Start+token.Length {
			selected = token
			break
		}
	}
	replaceEnd := offset
	if selected != nil {
		replaceEnd = selected.Start + selected.Length
	}
	var result fieldCompletionContext
	found := false
	for i, dot := range tokens {
		if dot.Kind != kry.DOT || dot.Start+dot.Length > offset || i == 0 || tokens[i-1].Kind != kry.ID {
			continue
		}
		dotEnd := dot.Start + dot.Length
		if selected != nil && selected.Start < dotEnd {
			continue
		}
		gapEnd := offset
		if selected != nil {
			gapEnd = selected.Start
		}
		if gapEnd < dotEnd || strings.TrimSpace(text[dotEnd:gapEnd]) != "" {
			continue
		}
		valid := true
		for j := i + 1; j < len(tokens); j++ {
			token := tokens[j]
			if token.Kind == kry.EOF || token.Start >= offset {
				break
			}
			if selected != nil && token.Kind == kry.ID && token.Start == selected.Start {
				continue
			}
			valid = false
			break
		}
		if !valid {
			continue
		}
		result = fieldCompletionContext{
			receiver: tokens[i-1],
			source:   text[:dot.Start] + text[replaceEnd:],
		}
		found = true
	}
	return result, found
}

func identifierAt(text string, pos Position) (kry.Token, bool) {
	src := &kry.Source{Name: "<buffer>", Text: text}
	tokens, d := kry.Lex(src, kry.DefaultLimits())
	if d != nil {
		return kry.Token{}, false
	}
	offset := offsetAt(text, pos)
	for _, token := range tokens {
		if token.Kind == kry.ID && offset >= token.Start && offset <= token.Start+token.Length {
			return token, true
		}
	}
	return kry.Token{}, false
}

func expressionAtToken(program *kry.Program, path string, token kry.Token) *kry.Expr {
	var found *kry.Expr
	walkProgram(program, func(expression *kry.Expr) {
		if expression.Kind == kry.ExVar && expression.Tok.Source != nil && expression.Tok.Source.Name == path && expression.Tok.Start == token.Start && expression.Tok.Length == token.Length {
			found = expression
		}
	})
	return found
}

func addLocalCompletions(add func(string, string, int), scope *kry.Scope, path string, offset int) {
	seen := map[string]bool{}
	for current := scope; current != nil; current = current.Parent {
		for name, binding := range current.Values {
			if seen[name] || binding.Global || name != "self" && (binding.Token.Source == nil || binding.Token.Source.Name != path || binding.Token.Start > offset) {
				continue
			}
			seen[name] = true
			kind := "local"
			if binding.Const {
				kind = "const"
			}
			add(name, kind+": "+binding.Type.String(), 6)
		}
	}
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

func tokenSpanRange(start, end kry.Token) Range {
	if start.Source == nil || end.Source == nil || start.Source.Name != end.Source.Name || end.Start < start.Start {
		return tokenRange(start)
	}
	text := start.Source.Text
	spanEnd := end.Start + end.Length
	if spanEnd < end.Start || spanEnd > len(text) {
		return tokenRange(start)
	}
	return Range{Start: positionAt(text, start.Start), End: positionAt(text, spanEnd)}
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
