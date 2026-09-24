package kry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type discordWebhookRequest struct {
	method string
	path   string
	body   string
}

func TestDiscordPackageArchiveMatchesRegistryIndex(t *testing.T) {
	const version = "1.3.0"
	archivePath := filepath.Join("..", "..", "registry", "packages", "discord-"+version+".tar.gz")
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join("..", "..", "registry", "index", "discord.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Name     string `json:"name"`
		Versions []struct {
			Version string `json:"version"`
			SHA256  string `json:"sha256"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatal(err)
	}
	wantHash := ""
	for _, candidate := range index.Versions {
		if candidate.Version == version {
			wantHash = candidate.SHA256
			break
		}
	}
	if index.Name != "discord" || wantHash == "" {
		t.Fatalf("Discord registry index is missing release %s", version)
	}
	digest := sha256.Sum256(archive)
	if got := hex.EncodeToString(digest[:]); got != wantHash {
		t.Fatalf("Discord archive SHA-256 = %s, registry index says %s", got, wantHash)
	}

	extracted := t.TempDir()
	if err := extractPackage(archive, extracted); err != nil {
		t.Fatalf("extract published Discord package: %v", err)
	}
	manifest, err := ReadManifest(extracted)
	if err != nil || manifest.Name != "discord" || manifest.Version != version {
		t.Fatalf("published Discord package manifest = %#v, %v", manifest, err)
	}

	project := t.TempDir()
	vendorPackage := filepath.Join(project, "vendor", "discord")
	if err := os.MkdirAll(vendorPackage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractPackage(archive, vendorPackage); err != nil {
		t.Fatalf("install published Discord package into project fixture: %v", err)
	}
	root := filepath.Join(project, "main.kry")
	if err := os.WriteFile(root, []byte("import \"discord\"\nfn main() -> Result[Nil, String] {\n    let hook: Webhook = webhook(\"123\", \"token\")?\n    return hook.delete()\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(root, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord Webhook through published package: %s", diagnostic.Message)
	}
	if _, diagnostic := Check(program, DefaultLimits()); diagnostic != nil {
		t.Fatalf("type-check Discord Webhook through published package: %s", diagnostic.Message)
	}

	rebuilt := filepath.Join(t.TempDir(), "discord-"+version+".tar.gz")
	if err := PackageArchive(filepath.Join("..", "..", "packages", "discord"), rebuilt); err != nil {
		t.Fatal(err)
	}
	rebuiltArchive, err := os.ReadFile(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rebuiltArchive, archive) {
		t.Fatal("published Discord package archive is not reproducible from packages/discord")
	}
}

func TestDiscordWebhookMethodsUseUnauthenticatedRoutesAndSafeTextDefaults(t *testing.T) {
	var seen []discordWebhookRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read Discord webhook request body: %v", err)
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("webhook token request unexpectedly used bot Authorization: %q", request.Header.Get("Authorization"))
		}
		seen = append(seen, discordWebhookRequest{method: request.Method, path: request.URL.RequestURI(), body: string(body)})

		switch {
		case request.Method == http.MethodPost && request.URL.Query().Get("wait") == "true":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"789","content":"hello <@456>"}`)
		case request.Method == http.MethodPost && request.URL.Query().Get("wait") == "false":
			w.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/v10/webhooks/124/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"rejected synthetic-webhook-token-with-more-than-thirty-two-characters"}`)
		case request.Method == http.MethodGet && strings.Contains(request.URL.Path, "/messages/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"789","content":"hello"}`)
		case request.Method == http.MethodPatch && strings.Contains(request.URL.Path, "/messages/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"789","content":"edited"}`)
		case request.Method == http.MethodGet || request.Method == http.MethodPatch:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"123","name":"test"}`)
		case request.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected Discord webhook request: %s %s", request.Method, request.URL.RequestURI())
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	modulePath := filepath.Join("..", "..", "packages", "discord", "main.kry")
	module, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(module) + `
fn main() -> Result[Nil, String] {
    let invalid_webhook: Result[Webhook, String] = webhook("not-a-snowflake", "token")
    assert_eq(is_err(invalid_webhook), true)
    let hook: Webhook = webhook("123", "token")?
    let invalid_thread: Result[String, String] = hook.execute_json("{}", true, "bad")
    assert_eq(is_err(invalid_thread), true)
    let invalid_message: Result[Json, String] = hook.fetch_message("bad", "")
    assert_eq(is_err(invalid_message), true)

    let sent: Json = hook.send("hello <@456>")?
    let sent_id: String = json_string(result_unwrap(json_object_get(sent, "id")))?
    assert_eq(sent_id, "789")
    let executed: String = hook.execute_json("{\"embeds\":[]}", true, "456")?
    assert_eq(executed, "{\"id\":\"789\",\"content\":\"hello <@456>\"}")
    let fire_and_forget: String = hook.execute_json("{\"content\":\"no wait\"}", false, "")?
    assert_eq(fire_and_forget, "")
    let fetched: Json = hook.fetch()?
    let edited: Json = hook.edit_json("{\"name\":\"renamed\"}")?
    let message: Json = hook.fetch_message("789", "456")?
    let changed: Json = hook.edit_message_json("789", "{\"content\":\"edited\"}", "456")?
    hook.delete_message("789", "456")?
    hook.delete()?
    let private_hook: Webhook = webhook("124", "synthetic-webhook-token-with-more-than-thirty-two-characters")?
    let private_error: Result[Json, String] = private_hook.fetch()
    assert_eq(is_err(private_error), true)
    let error_text: String = unwrap_or(result_error(private_error), "")
    assert_eq(contains(error_text, "Discord API HTTP 404"), true)
    assert_eq(contains(error_text, "synthetic-webhook-token-with-more-than-thirty-two-characters"), false)
    return ok(nil)
}
`
	p, diagnostic := Parse(&Source{Name: "discord_webhook_test.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("parse Discord module and webhook fixture: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(p, DefaultLimits())
	if diagnostic != nil {
		lines := strings.Split(source, "\n")
		t.Fatalf("type-check Discord module and webhook fixture at %d:%d (%s): %s", diagnostic.Line, diagnostic.Column, lines[diagnostic.Line-1], diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord webhook fixture: %s", diagnostic.Message)
	}

	want := []discordWebhookRequest{
		{method: "POST", path: "/api/v10/webhooks/123/token?wait=true", body: `{"content":"hello <@456>","allowed_mentions":{"parse":[]}}`},
		{method: "POST", path: "/api/v10/webhooks/123/token?wait=true&thread_id=456", body: `{"embeds":[]}`},
		{method: "POST", path: "/api/v10/webhooks/123/token?wait=false", body: `{"content":"no wait"}`},
		{method: "GET", path: "/api/v10/webhooks/123/token", body: ""},
		{method: "PATCH", path: "/api/v10/webhooks/123/token", body: `{"name":"renamed"}`},
		{method: "GET", path: "/api/v10/webhooks/123/token/messages/789?thread_id=456", body: ""},
		{method: "PATCH", path: "/api/v10/webhooks/123/token/messages/789?thread_id=456", body: `{"content":"edited"}`},
		{method: "DELETE", path: "/api/v10/webhooks/123/token/messages/789?thread_id=456", body: ""},
		{method: "DELETE", path: "/api/v10/webhooks/123/token", body: ""},
		{method: "GET", path: "/api/v10/webhooks/124/synthetic-webhook-token-with-more-than-thirty-two-characters", body: ""},
	}
	if !reflect.DeepEqual(seen, want) {
		got, _ := json.MarshalIndent(seen, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Discord webhook requests differ\ngot:\n%s\nwant:\n%s", got, wantJSON)
	}
}
