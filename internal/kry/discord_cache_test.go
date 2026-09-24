package kry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func requireDiscordCacheJSON(t *testing.T, cache *discordObjectCache, kind, id string) map[string]any {
	t.Helper()
	value, ok := cache.get(kind, id)
	if !ok {
		t.Fatalf("expected %s %s to be cached", kind, id)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		t.Fatalf("cached %s %s is not a JSON object: %v", kind, id, err)
	}
	return object
}

func requireDiscordCacheMiss(t *testing.T, cache *discordObjectCache, kind, id string) {
	t.Helper()
	if value, ok := cache.get(kind, id); ok {
		t.Fatalf("expected %s %s to be absent, got %s", kind, id, value)
	}
}

func TestDiscordCacheIndexesAndPreservesGuildMemberFields(t *testing.T) {
	cache := newDiscordObjectCache(64, time.Minute)
	err := cache.ingest("GUILD_CREATE", `{"id":"100","members":[{"user":{"id":"104","username":"member","avatar":"user-avatar"},"nick":"before","avatar":"member-avatar","flags":3,"pending":true,"joined_at":"2026-01-01T00:00:00Z","roles":["103"],"deaf":false,"mute":false}]}`)
	if err != nil {
		t.Fatal(err)
	}
	member := requireDiscordCacheJSON(t, cache, "member", "100:104")
	if member["nick"] != "before" || member["avatar"] != "member-avatar" || member["pending"] != true {
		t.Fatalf("GUILD_CREATE member fields were not preserved: %#v", member)
	}
	if member["guild_id"] != "100" || member["id"] != "104" {
		t.Fatalf("GUILD_CREATE member was not indexed with its guild and nested user IDs: %#v", member)
	}
	requireDiscordCacheJSON(t, cache, "user", "104")

	err = cache.ingest("GUILD_MEMBER_UPDATE", `{"guild_id":"100","user":{"id":"104","username":"member-updated","avatar":"user-avatar-updated","global_name":"Display"},"nick":"after","avatar":"member-avatar-updated","flags":7,"pending":false,"premium_since":"2026-09-24T00:00:00Z","communication_disabled_until":"2026-09-25T00:00:00Z","roles":["103"],"deaf":true,"mute":false}`)
	if err != nil {
		t.Fatal(err)
	}
	member = requireDiscordCacheJSON(t, cache, "member", "100:104")
	for key, want := range map[string]any{
		"guild_id":                     "100",
		"id":                           "104",
		"nick":                         "after",
		"avatar":                       "member-avatar-updated",
		"flags":                        float64(7),
		"pending":                      false,
		"premium_since":                "2026-09-24T00:00:00Z",
		"communication_disabled_until": "2026-09-25T00:00:00Z",
		"joined_at":                    "2026-01-01T00:00:00Z",
		"deaf":                         true,
	} {
		if got := member[key]; got != want {
			t.Errorf("member field %q = %#v, want %#v (full payload: %#v)", key, got, want, member)
		}
	}
	user := requireDiscordCacheJSON(t, cache, "user", "104")
	if user["global_name"] != "Display" || user["avatar"] != "user-avatar-updated" {
		t.Fatalf("member update did not refresh the cached user payload: %#v", user)
	}
}

func TestDiscordCacheMergesPartialGatewayUpdates(t *testing.T) {
	cache := newDiscordObjectCache(64, time.Minute)
	for _, event := range []struct{ name, payload string }{
		{"GUILD_CREATE", `{"id":"100","name":"before","owner_id":"900","channels":[{"id":"101","guild_id":"100","name":"general","topic":"keep this","type":0}],"threads":[{"id":"102","guild_id":"100","name":"thread","parent_id":"101","archived":false}],"roles":[{"id":"103","name":"role","permissions":"8"}]}`},
		{"MESSAGE_CREATE", `{"id":"301","channel_id":"101","guild_id":"100","content":"before","author":{"id":"104","username":"member","global_name":"Preserve"},"attachments":[{"id":"501","filename":"a.txt"}],"flags":0}`},
		{"GUILD_UPDATE", `{"id":"100","name":"after","premium_tier":2}`},
		{"CHANNEL_UPDATE", `{"id":"101","name":"announcements"}`},
		{"THREAD_UPDATE", `{"id":"102","name":"renamed","archived":true}`},
		{"GUILD_ROLE_UPDATE", `{"guild_id":"100","role":{"id":"103","name":"renamed"}}`},
		{"MESSAGE_UPDATE", `{"id":"301","content":"after","edited_timestamp":"2026-09-24T00:00:00Z","author":{"id":"104","username":"updated"}}`},
	} {
		if err := cache.ingest(event.name, event.payload); err != nil {
			t.Fatalf("ingest %s: %v", event.name, err)
		}
	}
	for _, test := range [][4]string{
		{"guild", "100", "name", "after"},
		{"channel", "101", "guild_id", "100"},
		{"channel", "101", "topic", "keep this"},
		{"thread", "102", "parent_id", "101"},
		{"thread", "102", "archived", "true"},
		{"role", "100:103", "permissions", "8"},
		{"message", "301", "channel_id", "101"},
		{"message", "301", "guild_id", "100"},
		{"message", "301", "content", "after"},
	} {
		kind, id, key, want := test[0], test[1], test[2], test[3]
		object := requireDiscordCacheJSON(t, cache, kind, id)
		if kind == "thread" && key == "archived" {
			if object[key] != true {
				t.Errorf("%s %s field %s = %#v, want true", kind, id, key, object[key])
			}
			continue
		}
		if object[key] != want {
			t.Errorf("%s %s field %s = %#v, want %q", kind, id, key, object[key], want)
		}
	}
	message := requireDiscordCacheJSON(t, cache, "message", "301")
	attachments, ok := message["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("partial message update dropped attachments: %#v", message["attachments"])
	}
	author, ok := message["author"].(map[string]any)
	if !ok || author["username"] != "updated" || author["global_name"] != "Preserve" {
		t.Fatalf("partial message update did not deep-merge its author: %#v", message["author"])
	}
}

func TestDiscordCacheDoesNotStoreOversizedReencodedEvents(t *testing.T) {
	cache := newDiscordObjectCache(8, time.Minute)
	payload := `{"id":"301","content":"` + strings.Repeat("<", 400_000) + `"}`
	if len(payload) >= 1<<20 {
		t.Fatalf("test payload must be under the Gateway ingestion limit, got %d bytes", len(payload))
	}
	err := cache.ingest("MESSAGE_CREATE", payload)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("expected re-encoded cache-size diagnostic, got %v", err)
	}
	requireDiscordCacheMiss(t, cache, "message", "301")
}

func TestDiscordCacheGuildDeletePurgesGuildStateButKeepsUnavailableGuild(t *testing.T) {
	cache := newDiscordObjectCache(64, time.Minute)
	for _, payload := range []string{
		`{"id":"100","channels":[{"id":"101","guild_id":"100"}],"threads":[{"id":"102","guild_id":"100"}],"roles":[{"id":"103","name":"guild-a-role"}],"members":[{"user":{"id":"104","username":"guild-a-user"},"roles":[]}],"guild_scheduled_events":[{"id":"105","guild_id":"100"}]}`,
		`{"id":"200","channels":[{"id":"201","guild_id":"200"}],"roles":[{"id":"203","name":"guild-b-role"}],"members":[{"user":{"id":"204","username":"guild-b-user"},"roles":[]}]}`,
	} {
		if err := cache.ingest("GUILD_CREATE", payload); err != nil {
			t.Fatal(err)
		}
	}
	for _, event := range []struct{ name, payload string }{
		{"MESSAGE_CREATE", `{"id":"301","guild_id":"100","channel_id":"101","content":"guild message"}`},
		{"MESSAGE_CREATE", `{"id":"302","guild_id":"200","channel_id":"201","content":"other guild message"}`},
		{"VOICE_STATE_UPDATE", `{"guild_id":"100","user_id":"104","channel_id":"101"}`},
	} {
		if err := cache.ingest(event.name, event.payload); err != nil {
			t.Fatal(err)
		}
	}

	if err := cache.ingest("GUILD_DELETE", `{"id":"100","unavailable":true}`); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ kind, id string }{
		{"guild", "100"}, {"channel", "101"}, {"thread", "102"}, {"role", "100:103"},
		{"member", "100:104"}, {"scheduled_event", "105"}, {"message", "301"}, {"voice_state", "100:104"},
	} {
		requireDiscordCacheJSON(t, cache, item.kind, item.id)
	}

	if err := cache.ingest("GUILD_DELETE", `{"id":"100"}`); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ kind, id string }{
		{"guild", "100"}, {"channel", "101"}, {"thread", "102"}, {"role", "100:103"},
		{"member", "100:104"}, {"scheduled_event", "105"}, {"message", "301"}, {"voice_state", "100:104"},
	} {
		requireDiscordCacheMiss(t, cache, item.kind, item.id)
	}
	for _, item := range []struct{ kind, id string }{
		{"guild", "200"}, {"channel", "201"}, {"role", "200:203"},
		{"member", "200:204"}, {"message", "302"}, {"user", "104"}, {"user", "204"},
	} {
		requireDiscordCacheJSON(t, cache, item.kind, item.id)
	}
}
