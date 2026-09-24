package kry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func loadDiscordApplicationCommandTestProgram(t *testing.T, source string) (*Program, *Checker) {
	t.Helper()
	fixture, err := os.CreateTemp(filepath.Join("..", "..", "examples"), ".discord-app-command-*.kry")
	if err != nil {
		t.Fatal(err)
	}
	fixturePath := fixture.Name()
	t.Cleanup(func() { _ = os.Remove(fixturePath) })
	if _, err := fixture.WriteString(source); err != nil {
		_ = fixture.Close()
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}

	program, diagnostic := LoadProgram(fixturePath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord application-command test program: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("type-check Discord application-command test program at %d:%d: %s", diagnostic.Line, diagnostic.Column, diagnostic.Message)
	}
	return program, checker
}

func runDiscordApplicationCommandTestProgram(t *testing.T, source string) {
	t.Helper()
	program, checker := loadDiscordApplicationCommandTestProgram(t, source)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord application-command test runtime: %s", diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord application-command test program: %s", diagnostic.Message)
	}
}

func TestDiscordApplicationCommandBuildersValidateAndEncodePayloads(t *testing.T) {
	tooManyOptions := "[" + strings.TrimSuffix(strings.Repeat("{},", 26), ",") + "]"
	source := `
import "packages/discord"
fn main() -> Result[Nil, String] {
    let slash: String = slash_command_json("ping", "Ping", "[{}]")?
    assert_eq(slash, "{\"name\":\"ping\",\"type\":1,\"description\":\"Ping\",\"options\":[{}]}")
    assert_eq(user_context_command_json("Open Profile")?, "{\"name\":\"Open Profile\",\"type\":2}")
    assert_eq(message_context_command_json("Summarize Message")?, "{\"name\":\"Summarize Message\",\"type\":3}")

    assert_eq(is_err(slash_command_json("Ping", "Description", "[]")), true)
    assert_eq(is_err(slash_command_json("can't", "Description", "[]")), true)
    assert_eq(is_err(slash_command_json("this-name-is-longer-than-thirty-two-characters", "Description", "[]")), true)
    assert_eq(is_err(slash_command_json("ping", "", "[]")), true)
    assert_eq(is_err(slash_command_json("ping", "Description", "{}")), true)
    assert_eq(is_err(slash_command_json("ping", "Description", "[null]")), true)
    assert_eq(is_err(slash_command_json("ping", "Description", ` + strconv.Quote(tooManyOptions) + `)), true)
    assert_eq(is_err(user_context_command_json("")), true)
    assert_eq(is_err(message_context_command_json("bad\nname")), true)
    return ok(nil)
}
main()
`
	runDiscordApplicationCommandTestProgram(t, source)
}

func TestDiscordSyncGlobalCommandsUsesBulkOverwriteRoute(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestCount++
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read Discord command-sync request body: %v", err)
		}
		if request.Method != http.MethodPut {
			t.Errorf("Discord command-sync method = %s, want PUT", request.Method)
		}
		if request.URL.Path != "/api/v10/applications/123/commands" {
			t.Errorf("Discord command-sync path = %s, want /api/v10/applications/123/commands", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bot synthetic-token" {
			t.Errorf("Discord command-sync authorization = %q, want Bot token", request.Header.Get("Authorization"))
		}
		if got, want := string(body), `[{"name":"ping","type":1,"description":"Ping","options":[]}]`; got != want {
			t.Errorf("Discord command-sync payload = %s, want %s", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"456","name":"ping","type":1}]`)
	}))
	defer server.Close()

	source := `
import "packages/discord"
fn main() -> Result[Nil, String] {
    let client: Bot = bot("synthetic-token", [])?
    let command: String = slash_command_json("ping", "Ping", "[]")?
    let synced: Json = client.sync_global_commands("123", "[" + command + "]")?
    assert_eq(json_kind(synced), "array")
    assert_eq(is_err(client.sync_global_commands("123", "{}")), true)
    assert_eq(is_err(client.sync_global_commands("123", "[null]")), true)
    assert_eq(is_err(client.sync_global_commands("not-a-snowflake", "[]")), true)
    return ok(nil)
}
main()
`
	program, checker := loadDiscordApplicationCommandTestProgram(t, source)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord command-sync runtime: %s", diagnostic.Message)
	}
	runtime.discordAPIBaseURL = server.URL + "/api/v10"
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord command-sync program: %s", diagnostic.Message)
	}
	if requestCount != 1 {
		t.Fatalf("Discord command-sync sent %d requests, want one validated bulk overwrite", requestCount)
	}
}
