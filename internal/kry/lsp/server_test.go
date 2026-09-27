package lsp

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/Xyraniz/Kryndel/internal/kry"
	"go.lsp.dev/jsonrpc2"
)

type diagnosticsPacket struct {
	URI         string `json:"uri"`
	Version     int    `json:"version"`
	Diagnostics []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Range   Range  `json:"range"`
	} `json:"diagnostics"`
}

func TestServerLSPRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.kry")
	libPath := filepath.Join(dir, "lib.kry")
	if err := os.WriteFile(mainPath, []byte("fn on_disk() -> Nil {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	libText := "pub struct LibType { field: Int }\npub enum LibStatus { Ready }\npub fn from_lib(value: Int) -> Int { return value + 1 }\nimpl LibType { pub fn convert(value: Int) -> Int { let local = value; return local } }\n"
	if err := os.WriteFile(libPath, []byte(libText), 0o600); err != nil {
		t.Fatal(err)
	}
	mainURI := uriFromPath(mainPath)
	libURI := uriFromPath(libPath)
	text := "import \"lib\"\nlet answer: Int = from_lib(1)\nprintln(answer)\n"

	serverSide, clientSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	server := NewServer()
	serverDone := make(chan struct {
		status int
		err    error
	}, 1)
	go func() {
		status, err := server.serve(ctx, jsonrpc2.NewStream(serverSide))
		serverDone <- struct {
			status int
			err    error
		}{status, err}
	}()

	client := jsonrpc2.NewConn(jsonrpc2.NewStream(clientSide))
	diagnostics := make(chan diagnosticsPacket, 8)
	client.Go(ctx, func(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
		if req.Method() == "textDocument/publishDiagnostics" {
			var packet diagnosticsPacket
			if err := json.Unmarshal(req.Params(), &packet); err != nil {
				return err
			}
			diagnostics <- packet
		}
		return nil
	})
	t.Cleanup(func() { _ = client.Close() })

	var initialized map[string]any
	callLSP(t, ctx, client, "initialize", map[string]any{"processId": nil, "rootUri": uriFromPath(dir)}, &initialized)
	capabilities, ok := initialized["capabilities"].(map[string]any)
	if !ok || capabilities["definitionProvider"] != true || capabilities["referencesProvider"] != true || capabilities["documentSymbolProvider"] != true || capabilities["hoverProvider"] != true || capabilities["documentFormattingProvider"] != true {
		t.Fatalf("initialize did not advertise expected features: %#v", initialized)
	}
	notifyLSP(t, ctx, client, "initialized", map[string]any{})
	notifyLSP(t, ctx, client, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": libURI, "languageId": "kryndel", "version": 1, "text": libText,
	}})
	libInitial := readDiagnosticsFor(t, diagnostics, libURI, 1)
	if len(libInitial.Diagnostics) != 0 {
		t.Fatalf("unsaved imported source should be checked from its open buffer: %#v", libInitial)
	}
	notifyLSP(t, ctx, client, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": mainURI, "languageId": "kryndel", "version": 1, "text": text,
	}})
	_ = readDiagnosticsFor(t, diagnostics, libURI, 1)
	initial := readDiagnosticsFor(t, diagnostics, mainURI, 1)
	if initial.URI != mainURI || initial.Version != 1 || len(initial.Diagnostics) != 0 {
		t.Fatalf("valid unsaved source should have no diagnostics: %#v", initial)
	}

	var location map[string]any
	callLSP(t, ctx, client, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 19},
	}, &location)
	if location["uri"] != libURI {
		t.Fatalf("definition did not jump to imported module: %#v", location)
	}
	rangeData, _ := json.Marshal(location["range"])
	var gotRange Range
	if err := json.Unmarshal(rangeData, &gotRange); err != nil {
		t.Fatal(err)
	}
	if gotRange.Start.Line != 2 || gotRange.Start.Character != 7 {
		t.Fatalf("definition range should select the imported function name: %#v", gotRange)
	}
	var importedReferences []map[string]any
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 19},
		"context":      map[string]any{"includeDeclaration": false},
	}, &importedReferences)
	if len(importedReferences) != 1 || importedReferences[0]["uri"] != mainURI {
		t.Fatalf("references without declaration should include only the imported call: %#v", importedReferences)
	}
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 19},
		"context":      map[string]any{"includeDeclaration": true},
	}, &importedReferences)
	if len(importedReferences) != 2 {
		t.Fatalf("references with declaration should include the imported declaration and call: %#v", importedReferences)
	}
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": libURI},
		"position":     Position{Line: 2, Character: 9},
		"context":      map[string]any{"includeDeclaration": false},
	}, &importedReferences)
	if len(importedReferences) != 1 || importedReferences[0]["uri"] != mainURI {
		t.Fatalf("references from the imported declaration should find the open caller: %#v", importedReferences)
	}
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": libURI},
		"position":     Position{Line: 2, Character: 9},
		"context":      map[string]any{"includeDeclaration": true},
	}, &importedReferences)
	if len(importedReferences) != 2 {
		t.Fatalf("references from the imported declaration should include its declaration when requested: %#v", importedReferences)
	}
	var localReferences []map[string]any
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 2, Character: 9},
		"context":      map[string]any{"includeDeclaration": false},
	}, &localReferences)
	if len(localReferences) != 1 || localReferences[0]["uri"] != mainURI {
		t.Fatalf("local references should exclude the binding declaration by default: %#v", localReferences)
	}
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 2, Character: 9},
		"context":      map[string]any{"includeDeclaration": true},
	}, &localReferences)
	if len(localReferences) != 2 {
		t.Fatalf("local references should include the binding declaration when requested: %#v", localReferences)
	}
	var mainSymbols []map[string]any
	callLSP(t, ctx, client, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
	}, &mainSymbols)
	if !hasDocumentSymbol(mainSymbols, "answer", 13) || hasDocumentSymbol(mainSymbols, "from_lib", 12) {
		t.Fatalf("main document symbols should contain its local binding, not the import's declaration: %#v", mainSymbols)
	}
	var libSymbols []map[string]any
	callLSP(t, ctx, client, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": libURI},
	}, &libSymbols)
	for _, expected := range []struct {
		name string
		kind int
	}{{"LibType", 23}, {"field", 8}, {"LibStatus", 10}, {"Ready", 22}, {"from_lib", 12}, {"value", 13}, {"convert", 6}, {"local", 13}} {
		if !hasDocumentSymbol(libSymbols, expected.name, expected.kind) {
			t.Fatalf("imported document symbols should contain %q (kind %d): %#v", expected.name, expected.kind, libSymbols)
		}
	}
	for _, parentChild := range [][2]string{{"LibType", "field"}, {"LibStatus", "Ready"}, {"from_lib", "value"}, {"convert", "local"}} {
		parent := findDocumentSymbol(libSymbols, parentChild[0], -1)
		child := findDocumentSymbol(libSymbols, parentChild[1], -1)
		if parent == nil || child == nil {
			t.Fatalf("missing symbol range pair %q/%q: %#v", parentChild[0], parentChild[1], libSymbols)
		}
		assertDocumentSymbolContains(t, parent, child)
	}
	var localLocation map[string]any
	callLSP(t, ctx, client, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 2, Character: 9},
	}, &localLocation)
	localRangeData, _ := json.Marshal(localLocation["range"])
	var localRange Range
	if err := json.Unmarshal(localRangeData, &localRange); err != nil {
		t.Fatal(err)
	}
	if localRange.Start.Line != 1 || localRange.Start.Character != 4 {
		t.Fatalf("local definition should point to the let binding: %#v", localRange)
	}

	var hover map[string]any
	callLSP(t, ctx, client, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 19},
	}, &hover)
	hoverContents, _ := hover["contents"].(map[string]any)
	hoverValue, _ := hoverContents["value"].(string)
	if !strings.Contains(hoverValue, "fn from_lib(value: Int) -> Int") {
		t.Fatalf("hover did not show the resolved function signature: %#v", hover)
	}

	var completion map[string]any
	callLSP(t, ctx, client, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 2, Character: 8},
	}, &completion)
	completionJSON, _ := json.Marshal(completion["items"])
	if !strings.Contains(string(completionJSON), `"label":"from_lib"`) || !strings.Contains(string(completionJSON), `"label":"println"`) {
		t.Fatalf("completion omitted imported or builtin symbols: %s", completionJSON)
	}

	badText := "import \"lib\"\nlet answer: Int = from_lib(1)\nprintln(answer)\nlet broken: Int = missing(1)\n"
	notifyLSP(t, ctx, client, "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": mainURI, "version": 2},
		"contentChanges": []any{map[string]any{"text": badText}},
	})
	_ = readDiagnosticsFor(t, diagnostics, libURI, 1)
	changed := readDiagnosticsFor(t, diagnostics, mainURI, 2)
	if changed.Version != 2 || len(changed.Diagnostics) != 1 || !strings.Contains(changed.Diagnostics[0].Message, "unknown function 'missing'") {
		t.Fatalf("didChange should report the current type error: %#v", changed)
	}
	if changed.Diagnostics[0].Range.Start.Line != 3 {
		t.Fatalf("diagnostic range should point into the changed buffer: %#v", changed.Diagnostics[0].Range)
	}
	var definitionAfterError map[string]any
	callLSP(t, ctx, client, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 20},
	}, &definitionAfterError)
	if definitionAfterError["uri"] != libURI {
		t.Fatalf("definition should remain available before a later type error: %#v", definitionAfterError)
	}
	var referencesAfterError []map[string]any
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 6},
		"context":      map[string]any{"includeDeclaration": false},
	}, &referencesAfterError)
	if len(referencesAfterError) != 1 || referencesAfterError[0]["uri"] != mainURI {
		t.Fatalf("references should remain available in a document with a later type error: %#v", referencesAfterError)
	}
	callLSP(t, ctx, client, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"position":     Position{Line: 1, Character: 6},
		"context":      map[string]any{"includeDeclaration": true},
	}, &referencesAfterError)
	if len(referencesAfterError) != 2 {
		t.Fatalf("references should include declaration and use with a later type error: %#v", referencesAfterError)
	}
	var symbolsAfterError []map[string]any
	callLSP(t, ctx, client, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
	}, &symbolsAfterError)
	if !hasDocumentSymbol(symbolsAfterError, "answer", 13) {
		t.Fatalf("document symbols should remain available for a parsed document with a type error: %#v", symbolsAfterError)
	}

	unformatted := "import \"lib\"\nlet answer:Int=from_lib(1)\n"
	notifyLSP(t, ctx, client, "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": mainURI, "version": 3},
		"contentChanges": []any{map[string]any{"text": unformatted}},
	})
	_ = readDiagnosticsFor(t, diagnostics, libURI, 1)
	if formattedDiagnostics := readDiagnosticsFor(t, diagnostics, mainURI, 3); len(formattedDiagnostics.Diagnostics) != 0 {
		t.Fatalf("valid source should stay valid before formatting: %#v", formattedDiagnostics)
	}
	var edits []map[string]any
	callLSP(t, ctx, client, "textDocument/formatting", map[string]any{
		"textDocument": map[string]any{"uri": mainURI},
		"options":      map[string]any{"tabSize": 4, "insertSpaces": true},
	}, &edits)
	if len(edits) != 1 || !strings.Contains(edits[0]["newText"].(string), "let answer: Int = from_lib(1)") {
		t.Fatalf("formatting should return a whole-document edit with canonical spacing: %#v", edits)
	}

	notifyLSP(t, ctx, client, "textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": mainURI}})
	closed := readDiagnosticsFor(t, diagnostics, mainURI, 0)
	if len(closed.Diagnostics) != 0 {
		t.Fatalf("didClose should clear diagnostics: %#v", closed)
	}

	var shutdown any
	callLSP(t, ctx, client, "shutdown", nil, &shutdown)
	notifyLSP(t, ctx, client, "exit", nil)
	select {
	case outcome := <-serverDone:
		if outcome.err != nil || outcome.status != 0 {
			t.Fatalf("orderly shutdown returned status %d, error %v", outcome.status, outcome.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after the exit notification")
	}
}

func hasDocumentSymbol(symbols []map[string]any, name string, kind int) bool {
	return findDocumentSymbol(symbols, name, kind) != nil
}

func findDocumentSymbol(symbols []map[string]any, name string, kind int) map[string]any {
	for _, symbol := range symbols {
		if symbol["name"] == name && (kind < 0 || symbol["kind"] == float64(kind)) {
			return symbol
		}
		children, _ := symbol["children"].([]any)
		for _, child := range children {
			if nested, ok := child.(map[string]any); ok {
				if nested["name"] == name && (kind < 0 || nested["kind"] == float64(kind)) {
					return nested
				}
				if found := findDocumentSymbol([]map[string]any{nested}, name, kind); found != nil {
					return found
				}
			}
		}
	}
	return nil
}

func assertDocumentSymbolContains(t *testing.T, parent, child map[string]any) {
	t.Helper()
	var parentRange, childSelection Range
	parentRangeJSON, _ := json.Marshal(parent["range"])
	childSelectionJSON, _ := json.Marshal(child["selectionRange"])
	if err := json.Unmarshal(parentRangeJSON, &parentRange); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(childSelectionJSON, &childSelection); err != nil {
		t.Fatal(err)
	}
	beforeOrEqual := func(left, right Position) bool {
		return left.Line < right.Line || left.Line == right.Line && left.Character <= right.Character
	}
	if !beforeOrEqual(parentRange.Start, childSelection.Start) || !beforeOrEqual(childSelection.End, parentRange.End) {
		t.Fatalf("parent symbol range should contain its child selection: parent=%#v child=%#v", parentRange, childSelection)
	}
}

func TestOffsetUsesUTF16Characters(t *testing.T) {
	text := "x😀y\r\nz"
	if got := offsetAt(text, Position{Line: 0, Character: 3}); got != len("x😀") {
		t.Fatalf("UTF-16 position mapped to byte offset %d; want %d", got, len("x😀"))
	}
	if got := positionAt(text, len("x😀")); got != (Position{Line: 0, Character: 3}) {
		t.Fatalf("byte offset mapped to %#v; want line 0, UTF-16 column 3", got)
	}
	if got := offsetAt(text, Position{Line: 1, Character: 0}); got != len("x😀y\r\n") {
		t.Fatalf("second-line start mapped to byte offset %d", got)
	}
}

func TestDiagnosticOffsetsUseRuneColumnsAndUTF16Positions(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		match string
	}{
		{name: "BMP before diagnostic with CRLF", text: "let label = \"☀\"; missing()\r\n", match: "missing"},
		{name: "supplementary before diagnostic with CRLF", text: "let label = \"😀\"; missing()\r\n", match: "missing"},
		{name: "EOF at end of line", text: "let label = \"😀\"", match: "<eof>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &kry.Source{Name: "diagnostic.kry", Text: tc.text}
			tokens, diagnostic := kry.Lex(source, kry.DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic)
			}
			var selected kry.Token
			found := false
			for _, token := range tokens {
				if (tc.match == "<eof>" && token.Kind == kry.EOF) || (token.Kind == kry.ID && token.Text() == tc.match) {
					selected, found = token, true
					break
				}
			}
			if !found {
				t.Fatalf("test token %q not found", tc.match)
			}

			gotOffset := offsetFromLineColumn(tc.text, selected.Line, selected.Column)
			if gotOffset != selected.Start {
				t.Fatalf("diagnostic line/column mapped to byte %d; want token byte %d", gotOffset, selected.Start)
			}
			got := positionAt(tc.text, gotOffset)
			prefix := tc.text[:selected.Start]
			lineStart := strings.LastIndex(prefix, "\n") + 1
			linePrefix := strings.TrimSuffix(prefix[lineStart:], "\r")
			want := Position{
				Line:      uint32(strings.Count(prefix, "\n")),
				Character: uint32(len(utf16.Encode([]rune(linePrefix)))),
			}
			if got != want {
				t.Fatalf("diagnostic position = %#v, want UTF-16 position %#v", got, want)
			}
		})
	}
}

func TestImportedDiagnosticsUseOpenSourceVersionAndUTF16Range(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kry.toml"), []byte("name = \"lsp-diagnostic-test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "src")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(srcDir, "main.kry")
	libPath := filepath.Join(srcDir, "lib.kry")
	mainText := "import \"lib\"\nfn main() -> Int { return imported() }\n"
	libText := "pub fn imported() -> Int { let marker: String = \"😀\"; let broken: Int = \"wrong\"; return 1 }\r\n"
	program, diagnostic := kry.LoadProgramWithSources(mainPath, map[string]string{
		mainPath: mainText,
		libPath:  libText,
	}, kry.DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("load imported source: %v", diagnostic)
	}
	_, checkDiagnostic := kry.Check(program, kry.DefaultLimits())
	if checkDiagnostic == nil || checkDiagnostic.Source != libPath {
		t.Fatalf("checker diagnostic = %#v, want source %s", checkDiagnostic, libPath)
	}

	source := &kry.Source{Name: libPath, Text: libText}
	tokens, lexDiagnostic := kry.Lex(source, kry.DefaultLimits())
	if lexDiagnostic != nil {
		t.Fatal(lexDiagnostic)
	}
	byteOffset := -1
	// The mismatch is reported at the declaration token; resolve that exact
	// lexer location to make the expected editor range independent of UTF-8 byte lengths.
	for _, token := range tokens {
		if token.Line == checkDiagnostic.Line && token.Column == checkDiagnostic.Column {
			byteOffset = token.Start
			break
		}
	}
	if byteOffset < 0 {
		t.Fatalf("no imported-source token at diagnostic %d:%d", checkDiagnostic.Line, checkDiagnostic.Column)
	}

	mainURI, libURI := uriFromPath(mainPath), uriFromPath(libPath)
	server := NewServer()
	mainDoc := document{URI: mainURI, Path: mainPath, Text: mainText, Version: 8}
	server.docs[mainURI] = mainDoc
	server.docs[libURI] = document{URI: libURI, Path: libPath, Text: libText, Version: 13}
	serverSide, clientSide := net.Pipe()
	server.conn = jsonrpc2.NewConn(jsonrpc2.NewStream(serverSide))
	client := jsonrpc2.NewConn(jsonrpc2.NewStream(clientSide))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	defer server.conn.Close()
	defer client.Close()
	packets := make(chan diagnosticsPacket, 1)
	client.Go(ctx, func(_ context.Context, _ jsonrpc2.Replier, request jsonrpc2.Request) error {
		if request.Method() == "textDocument/publishDiagnostics" {
			var packet diagnosticsPacket
			if err := json.Unmarshal(request.Params(), &packet); err != nil {
				return err
			}
			packets <- packet
		}
		return nil
	})
	if err := server.publishDiagnostic(ctx, mainURI, mainDoc, checkDiagnostic); err != nil {
		t.Fatal(err)
	}
	select {
	case packet := <-packets:
		if packet.URI != libURI || packet.Version != 13 {
			t.Fatalf("imported diagnostic destination/version = %s/%d, want %s/13", packet.URI, packet.Version, libURI)
		}
		if len(packet.Diagnostics) != 1 {
			t.Fatalf("imported diagnostic count = %d, want 1", len(packet.Diagnostics))
		}
		want := positionAt(libText, byteOffset)
		if got := packet.Diagnostics[0].Range.Start; got != want {
			t.Fatalf("imported diagnostic range starts at %#v, want %#v", got, want)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for imported diagnostic: %v", ctx.Err())
	}
}

func TestCompletionIncludesLocalBindingsAndParameters(t *testing.T) {
	cases := []struct {
		name, source, typed string
		want                string
	}{
		{
			name:   "local binding",
			source: `fn render(prefix: String) -> Nil { let message: String = prefix; println(mes) }`,
			typed:  "mes)",
			want:   "message",
		},
		{
			name:   "parameter",
			source: `fn render(prefix: String) -> Nil { println(pre) }`,
			typed:  "pre)",
			want:   "prefix",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := NewServer()
			path := filepath.Join(t.TempDir(), "main.kry")
			uri := uriFromPath(path)
			server.docs[uri] = document{URI: uri, Path: path, Text: tc.source}
			offset := strings.Index(tc.source, tc.typed) + len(tc.typed) - 1
			result, err := server.completion(uri, Position{Character: uint32(offset)})
			if err != nil {
				t.Fatal(err)
			}
			if !completionHasLabel(result, tc.want) {
				t.Fatalf("completion omitted %q: %#v", tc.want, result)
			}
		})
	}
}

func TestCompletionIncludesStructFields(t *testing.T) {
	source := `struct Point { x: Int, name: String }
fn render() -> Nil { let point: Point = Point { x: 1, name: "origin" }; println(point.) }`
	server := NewServer()
	path := filepath.Join(t.TempDir(), "main.kry")
	uri := uriFromPath(path)
	server.docs[uri] = document{URI: uri, Path: path, Text: source}
	offset := strings.Index(source, "point.)") + len("point.")
	result, err := server.completion(uri, Position{Line: 1, Character: uint32(offset - strings.LastIndex(source[:offset], "\n") - 1)})
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"x", "name"} {
		if !completionHasLabel(result, label) {
			t.Fatalf("completion omitted struct field %q: %#v", label, result)
		}
	}

	partial := strings.Replace(source, "point.)", "point.na)", 1)
	server.docs[uri] = document{URI: uri, Path: path, Text: partial}
	offset = strings.Index(partial, "point.na)") + len("point.na")
	result, err = server.completion(uri, Position{Line: 1, Character: uint32(offset - strings.LastIndex(partial[:offset], "\n") - 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !completionHasLabel(result, "name") {
		t.Fatalf("completion omitted the field matching a partial name: %#v", result)
	}
}

func completionHasLabel(result any, want string) bool {
	response, ok := result.(map[string]any)
	if !ok {
		return false
	}
	items, ok := response["items"].([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if ok && entry["label"] == want {
			return true
		}
	}
	return false
}

func TestUnsavedImportedModuleIsMergedWithoutDiskFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kry.toml"), []byte("name = \"overlay-test\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(dir, "src")
	if err := os.Mkdir(srcDir, 0o700); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(srcDir, "main.kry")
	libPath := filepath.Join(srcDir, "newlib.kry")
	program, diagnostic := kry.LoadProgramWithSources(mainPath, map[string]string{
		mainPath: `import "newlib"
fn main() -> Int { return from_newlib() }`,
		libPath: `pub fn from_newlib() -> Int { return 42 }`,
	}, kry.DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("loading unsaved imported module failed: %v", diagnostic)
	}
	if _, diagnostic = kry.Check(program, kry.DefaultLimits()); diagnostic != nil {
		t.Fatalf("checker could not resolve function from unsaved imported module: %v", diagnostic)
	}
}

func TestFileURIPathRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "space name.kry")
	got, err := pathFromURI(uriFromPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("file URI round trip got %q, want %q", got, path)
	}
	parsed, err := url.Parse(uriFromPath(path))
	if err != nil || parsed.Scheme != "file" {
		t.Fatalf("invalid file URI: %#v, %v", parsed, err)
	}
}

func TestLSPNavigatesCapturedClosureBindings(t *testing.T) {
	text := "fn make() -> fn() -> Int {\n    let captured: Int = 1\n    return fn() -> Int { return captured }\n}\n"
	path := filepath.Join(t.TempDir(), "closure.kry")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := uriFromPath(path)
	server := NewServer()
	if err := server.open(context.Background(), uri, text, 1); err != nil {
		t.Fatal(err)
	}
	position := positionAt(text, strings.LastIndex(text, "captured"))

	definition, err := server.definition(uri, position)
	if err != nil {
		t.Fatal(err)
	}
	location, ok := definition.(map[string]any)
	if !ok || location["uri"] != uri {
		t.Fatalf("captured binding definition = %#v, want location in %s", definition, uri)
	}
	rng, ok := location["range"].(Range)
	if !ok || rng.Start.Line != 1 || rng.Start.Character != 8 {
		t.Fatalf("captured binding range = %#v, want declaration at line 1, character 8", location["range"])
	}

	hover, err := server.hover(uri, position)
	if err != nil {
		t.Fatal(err)
	}
	hoverResult, ok := hover.(map[string]any)
	if !ok {
		t.Fatalf("captured binding hover = %#v", hover)
	}
	contents, ok := hoverResult["contents"].(map[string]any)
	if !ok || !strings.Contains(contents["value"].(string), "captured: Int") {
		t.Fatalf("captured binding hover contents = %#v", hoverResult["contents"])
	}

	references, err := server.references(uri, position, true)
	if err != nil {
		t.Fatal(err)
	}
	if locations, ok := references.([]any); !ok || len(locations) != 2 {
		t.Fatalf("captured binding references = %#v, want declaration and use", references)
	}
}

func callLSP(t *testing.T, ctx context.Context, client jsonrpc2.Conn, method string, params, result any) {
	t.Helper()
	if _, err := client.Call(ctx, method, params, result); err != nil {
		t.Fatalf("LSP request %s failed: %v", method, err)
	}
}

func notifyLSP(t *testing.T, ctx context.Context, client jsonrpc2.Conn, method string, params any) {
	t.Helper()
	if err := client.Notify(ctx, method, params); err != nil {
		t.Fatalf("LSP notification %s failed: %v", method, err)
	}
}

func readDiagnosticsFor(t *testing.T, ch <-chan diagnosticsPacket, uri string, version int) diagnosticsPacket {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case packet := <-ch:
			if packet.URI == uri && packet.Version == version {
				return packet
			}
		case <-deadline:
			t.Fatalf("timed out waiting for diagnostics for %s version %d", uri, version)
			return diagnosticsPacket{}
		}
	}
}
