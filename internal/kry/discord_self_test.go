package kry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func loadDiscordSelfTestProgram(t *testing.T, source string) (*Program, *Checker) {
	t.Helper()
	project := t.TempDir()
	for _, name := range []string{"discord", "discord-self"} {
		if err := copyPackageTree(filepath.Join("..", "..", "packages", name), filepath.Join(project, "vendor", name)); err != nil {
			t.Fatalf("stage %s package: %v", name, err)
		}
	}
	envSource, err := os.ReadFile(filepath.Join("..", "..", "std", "env.kry"))
	if err != nil {
		t.Fatalf("read standard environment module: %v", err)
	}
	stdDir := filepath.Join(project, "std")
	if err := os.MkdirAll(stdDir, 0o755); err != nil {
		t.Fatalf("create staged std directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stdDir, "env.kry"), envSource, 0o644); err != nil {
		t.Fatalf("stage standard environment module: %v", err)
	}
	root := filepath.Join(project, "main.kry")
	if err := os.WriteFile(root, []byte(source), 0o600); err != nil {
		t.Fatalf("write Discord self-client program: %v", err)
	}
	program, diagnostic := LoadProgram(root, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord self-client program: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("type-check Discord self-client program: %s", diagnostic.Message)
	}
	return program, checker
}

func TestDiscordSelfFetchGuildsWithCounts(t *testing.T) {
	queries := []string{"with_counts=true", "with_counts=false", "with_counts=false"}
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v10/users/@me/guilds" {
			t.Errorf("Discord guild request = %s %s, want GET /api/v10/users/@me/guilds", request.Method, request.URL.RequestURI())
		}
		if got, want := request.URL.RawQuery, queries[requestCount]; got != want {
			t.Errorf("Discord guild query = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("Authorization"), "synthetic-user-token"; got != want {
			t.Errorf("Discord user authorization = %q, want the unprefixed user token", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read Discord guild request body: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("Discord guild request body = %q, want empty", body)
		}
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	defer server.Close()

	source := `
import "discord-self"
fn main() -> Result[Nil, String] {
    let client: Client = self_client("synthetic-user-token")?
    let defaults: Json = client.fetch_guilds()?
    assert_eq(json_kind(defaults), "array")
    let omitted: Json = client.fetch_guilds(false)?
    assert_eq(json_kind(omitted), "array")
    let compatible: Json = client.guilds()?
    assert_eq(json_kind(compatible), "array")
    return ok(nil)
}
main()
`
	program, checker := loadDiscordSelfTestProgram(t, source)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord self-client runtime: %s", diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord self-client program: %s", diagnostic.Message)
	}
	if requestCount != len(queries) {
		t.Fatalf("Discord self-client sent %d guild requests, want %d", requestCount, len(queries))
	}
}

func TestDiscordSelfAdditionalRESTMethods(t *testing.T) {
	type expectedRequest struct {
		uri      string
		response string
	}
	want := []expectedRequest{
		{"/api/v10/invites/abc_123?with_counts=true&with_expiration=true&with_permissions=true&with_profile=true", `{"id":"invite"}`},
		{"/api/v10/guilds/123/widget.json", `{"id":"widget"}`},
		{"/api/v10/stickers/456", `{"id":"sticker"}`},
		{"/api/v10/sticker-packs/789", `{"id":"pack"}`},
		{"/api/v10/guilds/123/member-verification?with_guild=false&invite_code=abc_123", `{"id":"verification"}`},
		{"/api/v10/join-requests/555", `{"id":"request"}`},
		{"/api/v10/users/@me/join-request-guilds", `[]`},
		{"/api/v10/users/@me/settings-proto/1", `{"settings":{"theme":"dark"}}`},
		{"/api/v10/users/@me/consent", `{"personalization":false}`},
		{"/api/v10/friend-suggestions", `[]`},
	}
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if requestCount >= len(want) {
			t.Errorf("unexpected Discord request: %s %s", request.Method, request.URL.RequestURI())
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if request.Method != http.MethodGet || request.URL.RequestURI() != want[requestCount].uri {
			t.Errorf("Discord request = %s %s, want GET %s", request.Method, request.URL.RequestURI(), want[requestCount].uri)
		}
		if got := request.Header.Get("Authorization"); got != "synthetic-user-token" {
			t.Errorf("Discord user authorization = %q, want the unprefixed user token", got)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read Discord request body: %v", err)
		}
		if len(body) != 0 {
			t.Errorf("Discord GET request body = %q, want empty", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, want[requestCount].response)
		requestCount++
	}))
	defer server.Close()

	source := `
import "discord-self"
fn main() -> Result[Nil, String] {
    let client: Client = self_client("synthetic-user-token")?
    let invalid_invite: Result[Json, String] = client.fetch_invite("bad/code")
    match invalid_invite { ok(_) => { assert_eq(true, false) } err(_) => {} }
    let invalid_widget: Result[Json, String] = client.fetch_widget("not-a-snowflake")
    match invalid_widget { ok(_) => { assert_eq(true, false) } err(_) => {} }
    let invite: Json = client.fetch_invite("abc_123")?
    assert_eq(json_kind(invite), "object")
    let widget: Json = client.fetch_widget("123")?
    assert_eq(json_kind(widget), "object")
    let sticker: Json = client.fetch_sticker("456")?
    assert_eq(json_kind(sticker), "object")
    let pack: Json = client.fetch_sticker_pack("789")?
    assert_eq(json_kind(pack), "object")
    let verification: Json = client.fetch_member_verification("123", false, "abc_123")?
    assert_eq(json_kind(verification), "object")
    let request: Json = client.fetch_join_request("555")?
    assert_eq(json_kind(request), "object")
    let requested_guilds: Json = client.join_request_guilds()?
    assert_eq(json_kind(requested_guilds), "array")
    let settings: Json = client.fetch_settings()?
    let theme: Json = result_unwrap(json_object_get(settings, "theme"))
    assert_eq(result_unwrap(json_string(theme)), "dark")
    let tracking: Json = client.fetch_tracking_settings()?
    assert_eq(json_kind(tracking), "object")
    let suggestions: Json = client.friend_suggestions()?
    assert_eq(json_kind(suggestions), "array")
    return ok(nil)
}
main()
`
	program, checker := loadDiscordSelfTestProgram(t, source)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord self-client runtime: %s", diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord self-client REST methods: %s", diagnostic.Message)
	}
	if requestCount != len(want) {
		t.Fatalf("Discord self-client sent %d REST requests, want %d", requestCount, len(want))
	}
}

func TestDiscordSelfExampleTypeChecks(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "discord_self.kry"))
	if err != nil {
		t.Fatal(err)
	}
	loadDiscordSelfTestProgram(t, string(source))
}
