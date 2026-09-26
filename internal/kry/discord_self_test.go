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
