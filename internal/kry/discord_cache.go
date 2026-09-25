package kry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type discordCacheEntry struct {
	value string
	seen  time.Time
}

type discordObjectCache struct {
	mu       sync.Mutex
	entries  map[string]discordCacheEntry
	capacity int
	ttl      time.Duration
}

func newDiscordObjectCache(capacity int, ttl time.Duration) *discordObjectCache {
	if capacity < 1 {
		capacity = 1
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &discordObjectCache{entries: make(map[string]discordCacheEntry), capacity: capacity, ttl: ttl}
}

func validDiscordCacheKind(kind string) bool {
	switch kind {
	case "guild", "channel", "thread", "message", "user", "member", "role", "voice_state", "emoji", "sticker", "stage_instance", "scheduled_event":
		return true
	default:
		return false
	}
}

func validDiscordCacheID(id string) bool {
	parts := strings.Split(id, ":")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if len(part) > 20 || !allDecimalDiscordID(part) {
			return false
		}
	}
	return true
}

func discordCacheKey(kind, id string) string { return kind + ":" + id }

func (c *discordObjectCache) put(kind, id, value string) error {
	if c == nil || !validDiscordCacheKind(kind) || !validDiscordCacheID(id) {
		return fmt.Errorf("invalid Discord cache kind or ID")
	}
	if len(value) > 1<<20 || !json.Valid([]byte(value)) {
		return fmt.Errorf("Discord cache value must be valid JSON under 1 MiB")
	}
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return fmt.Errorf("Discord cache value must be a JSON object")
	}
	if rawID, ok := object["id"]; ok {
		if parsed, ok := discordJSONID(rawID); ok && !strings.Contains(id, ":") && parsed != id {
			return fmt.Errorf("Discord cache ID does not match the object's id")
		}
	}
	c.set(kind, id, value)
	return nil
}

func (c *discordObjectCache) set(kind, id, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(kind, id, value)
}

func (c *discordObjectCache) setLocked(kind, id, value string) {
	if c.entries == nil {
		c.entries = make(map[string]discordCacheEntry)
	}
	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.seen) >= c.ttl {
			delete(c.entries, key)
		}
	}
	key := discordCacheKey(kind, id)
	c.entries[key] = discordCacheEntry{value: value, seen: now}
	for len(c.entries) > c.capacity {
		oldestKey := ""
		var oldest time.Time
		for candidate, entry := range c.entries {
			if oldestKey == "" || entry.seen.Before(oldest) {
				oldestKey, oldest = candidate, entry.seen
			}
		}
		delete(c.entries, oldestKey)
	}
}

func mergeDiscordJSONObject(existing, update map[string]any) {
	for key, value := range update {
		updatedObject, updatedIsObject := value.(map[string]any)
		existingObject, existingIsObject := existing[key].(map[string]any)
		if updatedIsObject && existingIsObject {
			mergeDiscordJSONObject(existingObject, updatedObject)
			continue
		}
		existing[key] = value
	}
}

func (c *discordObjectCache) merge(kind, id, value string) error {
	if c == nil {
		return nil
	}
	var update map[string]any
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&update); err != nil || update == nil {
		return fmt.Errorf("Discord cache update must be a JSON object")
	}
	key := discordCacheKey(kind, id)
	c.mu.Lock()
	defer c.mu.Unlock()
	if previous, ok := c.entries[key]; ok && time.Since(previous.seen) < c.ttl {
		var existing map[string]any
		decoder := json.NewDecoder(strings.NewReader(previous.value))
		decoder.UseNumber()
		if err := decoder.Decode(&existing); err == nil && existing != nil {
			mergeDiscordJSONObject(existing, update)
			update = existing
		}
	}
	encoded, err := json.Marshal(update)
	if err != nil {
		delete(c.entries, key)
		return fmt.Errorf("could not encode Discord cache update: %w", err)
	}
	if len(encoded) > 1<<20 {
		delete(c.entries, key)
		return fmt.Errorf("merged Discord cache object exceeds 1 MiB")
	}
	c.setLocked(kind, id, string(encoded))
	return nil
}

func (c *discordObjectCache) get(kind, id string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := discordCacheKey(kind, id)
	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if time.Since(entry.seen) >= c.ttl {
		delete(c.entries, key)
		return "", false
	}
	entry.seen = time.Now()
	c.entries[key] = entry
	return entry.value, true
}

func (c *discordObjectCache) delete(kind, id string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, discordCacheKey(kind, id))
	c.mu.Unlock()
}

func (c *discordObjectCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[string]discordCacheEntry)
	c.mu.Unlock()
}

func (c *discordObjectCache) deleteGuild(guildID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	channelIDs := make(map[string]struct{})
	for key, entry := range c.entries {
		kind, id, ok := strings.Cut(key, ":")
		if !ok || (kind != "channel" && kind != "thread") {
			continue
		}
		var object map[string]any
		if json.Unmarshal([]byte(entry.value), &object) != nil {
			continue
		}
		if cachedGuildID, ok := discordJSONID(object["guild_id"]); ok && cachedGuildID == guildID {
			channelIDs[id] = struct{}{}
		}
	}

	compositePrefix := guildID + ":"
	for key, entry := range c.entries {
		kind, id, ok := strings.Cut(key, ":")
		if !ok {
			continue
		}
		if (kind == "guild" && id == guildID) ||
			((kind == "member" || kind == "role" || kind == "emoji" || kind == "sticker" || kind == "voice_state") && strings.HasPrefix(id, compositePrefix)) {
			delete(c.entries, key)
			continue
		}
		var object map[string]any
		if json.Unmarshal([]byte(entry.value), &object) != nil {
			continue
		}
		if cachedGuildID, ok := discordJSONID(object["guild_id"]); ok && cachedGuildID == guildID {
			delete(c.entries, key)
			continue
		}
		if kind == "message" {
			if channelID, ok := discordJSONID(object["channel_id"]); ok {
				if _, belongsToGuild := channelIDs[channelID]; belongsToGuild {
					delete(c.entries, key)
				}
			}
		}
	}
}

func discordJSONID(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, allDecimalDiscordID(value)
	case json.Number:
		id := value.String()
		return id, allDecimalDiscordID(id)
	case float64:
		if value < 0 || value != float64(uint64(value)) {
			return "", false
		}
		return strconv.FormatUint(uint64(value), 10), true
	default:
		return "", false
	}
}

func (c *discordObjectCache) ingest(event, payload string) error {
	if c == nil {
		return nil
	}
	if len(payload) > 1<<20 {
		return fmt.Errorf("Discord Gateway event exceeds cache limit")
	}
	if !json.Valid([]byte(payload)) {
		return fmt.Errorf("Discord Gateway event payload is not valid JSON")
	}
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return fmt.Errorf("Discord Gateway event payload must be a JSON object")
	}
	var cacheErr error
	saveObject := func(kind string, value any, merge bool) {
		item, ok := value.(map[string]any)
		if !ok {
			return
		}
		id, ok := discordJSONID(item["id"])
		if !ok {
			return
		}
		encoded, err := json.Marshal(item)
		if err == nil {
			if len(encoded) > 1<<20 {
				c.delete(kind, id)
				cacheErr = fmt.Errorf("Discord cache object exceeds 1 MiB")
				return
			}
			if merge {
				if err := c.merge(kind, id, string(encoded)); err != nil {
					cacheErr = err
				}
			} else {
				c.set(kind, id, string(encoded))
			}
		}
	}
	putObject := func(kind string, value any) { saveObject(kind, value, false) }
	mergeObject := func(kind string, value any) { saveObject(kind, value, true) }
	deleteObject := func(kind string, value any) {
		id, ok := discordJSONID(value)
		if ok {
			c.delete(kind, id)
		}
	}
	compositeID := func(guildValue, itemValue any) string {
		guildID, guildOK := discordJSONID(guildValue)
		itemID, itemOK := discordJSONID(itemValue)
		if !guildOK || !itemOK {
			return ""
		}
		return guildID + ":" + itemID
	}
	saveComposite := func(kind string, guildValue any, value any, merge bool) {
		item, ok := value.(map[string]any)
		if !ok {
			return
		}
		itemID := item["id"]
		if kind == "member" {
			user, ok := item["user"].(map[string]any)
			if !ok {
				return
			}
			itemID = user["id"]
			member := make(map[string]any, len(item)+1)
			for key, field := range item {
				member[key] = field
			}
			member["id"] = itemID
			if _, exists := member["guild_id"]; !exists {
				member["guild_id"] = guildValue
			}
			item = member
		}
		id := compositeID(guildValue, itemID)
		if id == "" {
			return
		}
		encoded, err := json.Marshal(item)
		if err == nil {
			if len(encoded) > 1<<20 {
				c.delete(kind, id)
				cacheErr = fmt.Errorf("Discord cache object exceeds 1 MiB")
				return
			}
			if merge {
				if err := c.merge(kind, id, string(encoded)); err != nil {
					cacheErr = err
				}
			} else {
				c.set(kind, id, string(encoded))
			}
		}
	}
	putComposite := func(kind string, guildValue any, value any) { saveComposite(kind, guildValue, value, false) }
	mergeComposite := func(kind string, guildValue any, value any) { saveComposite(kind, guildValue, value, true) }
	switch event {
	case "READY":
		putObject("user", object["user"])
	case "GUILD_CREATE":
		putObject("guild", object)
		guildID := object["id"]
		for _, field := range []struct{ key, kind string }{{"channels", "channel"}, {"threads", "thread"}, {"roles", "role"}, {"members", "member"}, {"emojis", "emoji"}, {"stickers", "sticker"}, {"stage_instances", "stage_instance"}, {"guild_scheduled_events", "scheduled_event"}} {
			items, _ := object[field.key].([]any)
			for _, item := range items {
				if field.kind == "member" {
					putComposite(field.kind, guildID, item)
					member, _ := item.(map[string]any)
					user, _ := member["user"].(map[string]any)
					putObject("user", user)
				} else if field.kind == "channel" || field.kind == "thread" || field.kind == "stage_instance" || field.kind == "scheduled_event" {
					putObject(field.kind, item)
				} else {
					putComposite(field.kind, guildID, item)
				}
			}
		}
	case "GUILD_UPDATE":
		mergeObject("guild", object)
	case "GUILD_DELETE":
		unavailable, _ := object["unavailable"].(bool)
		if !unavailable {
			if guildID, ok := discordJSONID(object["id"]); ok {
				c.deleteGuild(guildID)
			}
		}
	case "CHANNEL_CREATE":
		putObject("channel", object)
	case "CHANNEL_UPDATE":
		mergeObject("channel", object)
	case "CHANNEL_DELETE":
		deleteObject("channel", object["id"])
	case "THREAD_CREATE":
		putObject("thread", object)
	case "THREAD_UPDATE":
		mergeObject("thread", object)
	case "THREAD_DELETE":
		deleteObject("thread", object["id"])
	case "MESSAGE_CREATE":
		putObject("message", object)
	case "MESSAGE_UPDATE":
		mergeObject("message", object)
	case "MESSAGE_DELETE":
		deleteObject("message", object["id"])
	case "MESSAGE_DELETE_BULK":
		ids, _ := object["ids"].([]any)
		for _, id := range ids {
			deleteObject("message", id)
		}
	case "GUILD_MEMBER_ADD":
		user, _ := object["user"].(map[string]any)
		if user != nil {
			putComposite("member", object["guild_id"], object)
			putObject("user", user)
		}
	case "GUILD_MEMBER_UPDATE":
		user, _ := object["user"].(map[string]any)
		if user != nil {
			mergeComposite("member", object["guild_id"], object)
			mergeObject("user", user)
		}
	case "GUILD_MEMBERS_CHUNK":
		guildID, ok := discordJSONID(object["guild_id"])
		if ok {
			members, _ := object["members"].([]any)
			for _, value := range members {
				putComposite("member", guildID, value)
				member, _ := value.(map[string]any)
				user, _ := member["user"].(map[string]any)
				putObject("user", user)
			}
		}
	case "GUILD_MEMBER_REMOVE":
		user, _ := object["user"].(map[string]any)
		if user != nil {
			if id := compositeID(object["guild_id"], user["id"]); id != "" {
				c.delete("member", id)
			}
		}
	case "USER_UPDATE":
		mergeObject("user", object)
	case "VOICE_STATE_UPDATE":
		if id := compositeID(object["guild_id"], object["user_id"]); id != "" {
			channelID, _ := discordJSONID(object["channel_id"])
			if object["channel_id"] == nil || channelID == "" {
				c.delete("voice_state", id)
			} else {
				encoded, err := json.Marshal(object)
				if err == nil {
					if len(encoded) > 1<<20 {
						c.delete("voice_state", id)
						cacheErr = fmt.Errorf("Discord cache object exceeds 1 MiB")
					} else {
						c.set("voice_state", id, string(encoded))
					}
				}
			}
		}
	case "GUILD_ROLE_CREATE":
		putComposite("role", object["guild_id"], object["role"])
	case "GUILD_ROLE_UPDATE":
		mergeComposite("role", object["guild_id"], object["role"])
	case "GUILD_ROLE_DELETE":
		if id := compositeID(object["guild_id"], object["role_id"]); id != "" {
			c.delete("role", id)
		}
	}
	return cacheErr
}
