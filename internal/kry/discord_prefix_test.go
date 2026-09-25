package kry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiscordPrefixQuotedArgumentsAndParseErrors(t *testing.T) {
	packageDir := filepath.Join("..", "..", "packages", "discord")
	fixtureDir := t.TempDir()
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("list Discord package modules: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (filepath.Ext(name) != ".kry" && name != "kry.toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatalf("read Discord package %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDir, name), data, 0o600); err != nil {
			t.Fatalf("copy Discord package %s: %v", name, err)
		}
	}

	fixturePath := filepath.Join(fixtureDir, "prefix_test.kry")
	const source = `import "main"

fn expect_prefix_word(words: Array[String], index: Int, expected: String) -> Result[Nil, String] {
    match array_get(words, index) {
        some(word) => {
            if word != expected { return err("prefix word did not match expected value") }
            return ok(nil)
        }
        none => { return err("prefix word was missing") }
    }
}

fn handle_prefix_error(context_json: String) -> String {
    return "argument error"
}

fn main() -> Result[Nil, String] {
    let words: Array[String] = discord_prefix_parse_words("echo \"hello world\" 'two words' “三つ の語”")?
    assert_eq(len(words), 4)
    expect_prefix_word(words, 0, "echo")?
    expect_prefix_word(words, 1, "hello world")?
    expect_prefix_word(words, 2, "two words")?
    expect_prefix_word(words, 3, "三つ の語")?
    assert_eq(is_err(discord_prefix_parse_words("echo \"unfinished")), true)
    assert_eq(is_err(discord_prefix_parse_words("echo \"joined\"text")), true)
    assert_eq(is_err(discord_prefix_parse_words("bad\"quote")), true)
    assert_eq(valid_link_button_url("https://example.com/path?q=1#top"), true)
    assert_eq(valid_link_button_url("HTTPS://Example.com/path"), true)
    assert_eq(valid_link_button_url("https://?x"), false)
    assert_eq(valid_link_button_url("https:///path"), false)
    assert_eq(valid_link_button_url("https://bad..host/path"), false)
    let tab_url: String = "https://example.com/bad" + "\t" + "path"
    assert_eq(valid_link_button_url(tab_url), false)

    let message_json: String = "{\"content\":" + json_quote("!echo \"hello world\" there")? + "}"
    let message: Json = json_parse(message_json)?
    let context: Json = json_parse(discord_prefix_command_context(message, ["!"])?)?
    let old_arguments: Json = json_object_get(context, "arguments")?
    let parsed_arguments: Json = json_object_get(context, "parsed_arguments")?
    assert_eq(json_stringify(old_arguments), "[\"\\\"hello\",\"world\\\"\",\"there\"]")
    assert_eq(json_stringify(parsed_arguments), "[\"hello world\",\"there\"]")
    assert_eq(json_string(json_object_get(context, "parse_error")?)?, "")

    let registration: Result[Nil, String] = poly_register("discord.on_prefix_command_error", "handle_prefix_error", 0)
    registration?
    let malformed_json: String = "{\"content\":" + json_quote("!echo \"unfinished")? + "}"
    let malformed: Json = json_parse(malformed_json)?
    let malformed_context: Json = json_parse(discord_prefix_command_context(malformed, ["!"])?)?
    assert_eq(json_string(json_object_get(malformed_context, "parse_error")?)? != "", true)
    assert_eq(discord_dispatch_prefix_command(malformed, ["!"])?, "argument error")
    return ok(nil)
}
main()
`
	if err := os.WriteFile(fixturePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write Discord prefix test fixture: %v", err)
	}
	program, diagnostic := LoadProgram(fixturePath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord prefix test fixture: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("type-check Discord prefix test fixture: %s", diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord prefix test runtime: %s", diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord prefix test fixture: %s", diagnostic.Message)
	}
}

func TestDiscordAuditLogReasonURLencodesUTF8AndReservedCharacters(t *testing.T) {
	got, err := encodeDiscordAuditLogReason("spam user/a? café")
	if err != nil {
		t.Fatalf("encode audit-log reason: %v", err)
	}
	const want = "spam%20user%2Fa%3F%20caf%C3%A9"
	if got != want {
		t.Fatalf("encoded audit-log reason = %q, want %q", got, want)
	}

	if got, err := encodeDiscordAuditLogReason(strings.Repeat("é", 85)); err != nil || len(got) != 510 {
		t.Fatalf("encode 510-character audit-log reason: got length %d, err %v", len(got), err)
	}
	if _, err := encodeDiscordAuditLogReason(strings.Repeat("é", 86)); err == nil {
		t.Fatal("expected 516-character encoded audit-log reason to be rejected")
	}
}

func TestDiscordMemberChunkFailureQueuesErrorCallback(t *testing.T) {
	state := newDiscordGatewayState()
	state.chunks["request-1"] = &discordMemberChunkRequest{
		guildID:    "123",
		chunkCount: 1,
		chunks:     make(map[int]discordMemberChunkPart),
		createdAt:  time.Now(),
	}
	runtime := &Runtime{discordGateway: state}
	_, diagnostic := runtime.discordGatewayMemberChunkResult(`{"guild_id":"456","nonce":"request-1","chunk_index":0,"chunk_count":2,"members":[]}`)
	if diagnostic != nil {
		t.Fatalf("process mismatched member chunk: %s", diagnostic.Message)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.failures) != 1 {
		t.Fatalf("queued member-query failures = %d, want 1", len(state.failures))
	}
	var failure discordGatewayMemberQueryFailure
	if err := json.Unmarshal([]byte(state.failures[0]), &failure); err != nil {
		t.Fatalf("decode member-query failure: %v", err)
	}
	if failure.GuildID != "123" || failure.Nonce != "request-1" || failure.Reason != "chunk_metadata_mismatch" {
		t.Fatalf("member-query failure = %+v, want request metadata mismatch", failure)
	}
}
