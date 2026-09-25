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
	if err := os.WriteFile(libPath, []byte("pub fn from_lib(value: String) -> String { return value }"), 0o600); err != nil {
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
	if !ok || capabilities["definitionProvider"] != true || capabilities["hoverProvider"] != true || capabilities["documentFormattingProvider"] != true {
		t.Fatalf("initialize did not advertise expected features: %#v", initialized)
	}
	notifyLSP(t, ctx, client, "initialized", map[string]any{})
	libText := "pub fn from_lib(value: Int) -> Int { return value + 1 }"
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
	if gotRange.Start.Line != 0 || gotRange.Start.Character != 7 {
		t.Fatalf("definition range should select the imported function name: %#v", gotRange)
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

	badText := "import \"lib\"\nlet answer: Int = missing(1)\n"
	notifyLSP(t, ctx, client, "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": mainURI, "version": 2},
		"contentChanges": []any{map[string]any{"text": badText}},
	})
	_ = readDiagnosticsFor(t, diagnostics, libURI, 1)
	changed := readDiagnosticsFor(t, diagnostics, mainURI, 2)
	if changed.Version != 2 || len(changed.Diagnostics) != 1 || !strings.Contains(changed.Diagnostics[0].Message, "unknown function 'missing'") {
		t.Fatalf("didChange should report the current type error: %#v", changed)
	}
	if changed.Diagnostics[0].Range.Start.Line != 1 {
		t.Fatalf("diagnostic range should point into the changed buffer: %#v", changed.Diagnostics[0].Range)
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
