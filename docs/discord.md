# Discord integration

`packages/discord` is a bot-account client for Discord API v10. It exposes bounded REST helpers, interaction callbacks, command registration, a Gateway client with reconnect/resume support, configurable single-shard connections, and a bounded event cache. It never automates normal user accounts.

## Create a bot

```kry
import "std/env"
import "packages/discord"

fn reply_to_ping(message_json: String) -> String {
    let message: Json = result_unwrap(json_parse(message_json))
    let raw_content: Result[Json, String] = json_object_get(message, "content")
    match raw_content {
        ok(value) => {
            let content: String = result_unwrap(json_string(value))
            if content == "!ping" { return "Pong!" }
        }
        err(_) => {}
    }
    return ""
}

fn main() -> Result[Nil, String] {
    let token: Result[String, String] = require("DISCORD_TOKEN")
    match token {
        ok(secret) => {
            let intents: Array[String] = ["Guilds", "GuildMessages", "MessageContent"]
            let created: Result[Bot, String] = bot(secret, intents)
            match created {
                ok(client) => {
                    let handler: Result[Nil, String] = poly_register("discord.on_message", "reply_to_ping", 0)
                    match handler { ok(_) => { return client.run() } err(problem) => { return err(problem) } }
                }
                err(problem) => { return err(problem) }
            }
        }
        err(problem) => { return err(problem) }
    }
}
```

`bot(token, intents)` rejects empty or header-unsafe tokens, unknown intent names, and duplicate intents. Supported Gateway intents are `Guilds`, `GuildMembers`, `GuildModeration`, `GuildExpressions`, `GuildIntegrations`, `GuildWebhooks`, `GuildInvites`, `GuildVoiceStates`, `GuildPresences`, `GuildMessages`, `GuildMessageReactions`, `GuildMessageTyping`, `DirectMessages`, `DirectMessageReactions`, `DirectMessageTyping`, `MessageContent`, `GuildScheduledEvents`, `AutoModerationConfiguration`, `AutoModerationExecution`, `GuildMessagePolls`, and `DirectMessagePolls`. Discord's privileged intents must also be enabled in the Developer Portal.

`Bot.with_shard(shard_id, shard_count)` validates shard configuration. Each `Bot.run()` opens one shard; run one configured Bot process per shard. The client discovers the Gateway, validates its secure Discord host, sends Identify or Resume, tracks sequence numbers, maintains heartbeats, and reconnects when Discord asks it to. It uses `resume_gateway_url`, `session_id`, and the last dispatch sequence when a session can be resumed. Permanent Gateway close codes and handshake rejections are returned as errors instead of being retried indefinitely.

Every dispatch reaches `discord.on_event` as `{"name":"READY","data":{...}}`. `MESSAGE_CREATE` additionally reaches `discord.on_message` when the generic handler returns an empty string; a non-empty return sends a message reply. Bot-authored messages are ignored for automatic replies. Kryndel installs low-priority no-op handlers so an unused callback does not stop the connection. Since Kryndel has named dispatch slots rather than function decorators or closures, register top-level `String -> String` functions with `poly_register`.

## Interactions and application commands

Incoming interaction requests should be verified with `Bot.verify_interaction(public_key, timestamp, raw_body, signature)` before parsing the payload. Invalid signatures and timestamps older than five minutes return `ok(false)`; invalid key or signature encodings return an error. Interaction callback and webhook methods do not send the bot Authorization header:

- `Bot.respond_interaction` posts an initial callback.
- `Bot.create_followup`, `Bot.get_followup`, `Bot.edit_followup`, and `Bot.delete_followup` manage follow-up messages.
- `Bot.edit_original_response` and `Bot.delete_original_response` manage the initial response.
- `Bot.create_global_command`, `Bot.upsert_global_command`, `Bot.list_global_commands`, and `Bot.delete_global_command` manage global application commands.
- `Bot.bulk_overwrite_global_commands` replaces a global command set.
- `Bot.create_guild_command`, `Bot.update_guild_command`, `Bot.list_guild_commands`, and `Bot.delete_guild_command` manage guild commands; `Bot.bulk_overwrite_guild_commands` replaces a guild command set.

Command definitions and callback payloads are JSON strings and are validated before they are sent. Register `discord.on_event` to dispatch incoming `INTERACTION_CREATE` events to the application code.

## Webhooks

`Bot.create_webhook(channel_id, name)` creates a webhook with bot authentication and returns a token-authenticated `Webhook` when Discord includes its token in the create response. `webhook_from_json(value)` validates and converts a JSON webhook object that includes both its ID and token; `webhook(id, token)` constructs one directly. Version 1.5.0 adds `webhook_from_url(value)`, which accepts exactly `https://discord.com/api/webhooks/{id}/{token}` and rejects other hosts, HTTP, extra path segments, query strings, and invalid IDs or tokens. The token is a credential; keep it out of source control and logs. Token-authenticated `Webhook` requests use the token in the fixed Discord API path and never send a bot `Authorization` header.

`Bot.list_channel_webhooks`, `Bot.list_guild_webhooks`, `Bot.fetch_webhook`, `Bot.edit_webhook_json`, and `Bot.delete_webhook` cover management routes that use bot authentication. Edit payloads remain raw JSON so Discord can add webhook fields without waiting for new wrappers.

- `Webhook.send(content)` sends plain text, waits for the created message, escapes JSON, enforces Discord's 2,000 UTF-16-unit content limit, and disables mentions by default.
- `Webhook.execute_json(payload_json, wait, thread_id)` exposes Discord's JSON execute payload, including embeds, components, polls, and future fields. Set `wait` to receive the created message; an empty `thread_id` targets the webhook's default channel. This method preserves the caller's `allowed_mentions` payload, so specify it when the payload could contain mentions.
- `Webhook.upload_files_json(payload_json, filenames, file_data, thread_id)` sends one to ten files as `files[0]` through `files[9]` multipart fields and waits for the created message. Pass matching filename and `Bytes` arrays, include corresponding `attachments` entries in the JSON payload, and use an empty thread ID for the default channel. Uploads use the same runtime size limit as other Discord API bodies.
- `fetch()` and `edit_json(payload_json)` retrieve or edit the webhook; `delete()` removes it.
- `fetch_message`, `edit_message_json`, and `delete_message` operate on a webhook message. Pass an empty `thread_id` for a normal message or a valid snowflake for a thread message.

JSON methods retain Discord's evolving request schema without pretending Kryndel has typed models for every embed, component, poll, and attachment variant. The client validates the JSON body, route, IDs, and request/response size before sending it.

## REST, attachments, and rate limits

`Bot.request(method, route, body)` exposes the full REST API v10 route surface on a fixed `https://discord.com/api/v10` origin with Bot authentication. Common helpers include users, guilds, channels, messages, roles, moderation actions, and application commands. Snowflake IDs are checked by typed helpers, message bodies are JSON-escaped, message length is limited to 2,000 UTF-16 code units, and implicit mentions are disabled by default.

`Bot.upload(method, route, payload_json, filename, data)` sends one bounded file as a multipart request with `payload_json`. Request bodies are replayed safely across retries. The runtime uses Discord's rate-limit bucket headers and global 429 information to coordinate waits across routes and worker runtimes. It makes an initial request and allows up to five retries, with a five-minute maximum wait per retry. API response sizes and upload sizes are limited by the runtime's configured input limit; credentials and long interaction tokens are redacted from returned HTTP errors.

## Object cache

`Bot.cache_get(kind, id)`, `Bot.cache_put(kind, id, object_json)`, `Bot.cache_delete(kind, id)`, and `Bot.cache_clear()` access a runtime-local cache capped at 10,000 objects with a 30-minute sliding lifetime. The Gateway automatically caches recent users, guilds, channels, threads, roles, members, messages, voice states, and other supported dispatch objects. Use `guild_id:user_id` for member and role cache keys. Cache misses and expired objects return an error result.

Partial Gateway updates merge fields into cached guilds, channels, threads, roles, members, messages, and users; nested objects merge recursively while arrays and explicit `null` values replace the old value. If an encoded object exceeds the 1 MiB cache-entry limit, Kryndel evicts that entry and reports the cache update error instead of retaining stale data. `GUILD_DELETE` with `unavailable: true` preserves the guild cache for a possible Gateway recovery. A permanent `GUILD_DELETE` removes the guild, its guild-scoped objects, and cached messages belonging to its channels; globally cached user objects remain available. Members received through `GUILD_CREATE`, `GUILD_MEMBER_ADD`, and `GUILD_MEMBER_UPDATE` are indexed by `guild_id:user_id`, preserve all received fields, and refresh the global user entry.

## Voice

Gateway `VOICE_STATE_UPDATE` and `VOICE_SERVER_UPDATE` events are available through `discord.on_event` and the event cache. Connecting to a voice channel, UDP/Opus media transport, and DAVE end-to-end media encryption are not implemented. DAVE is required for current non-stage voice calls; the client does not report voice support when the cryptographic negotiation and media transport are absent.

## Runtime support

`discord_api_request` performs bounded API v10 requests with Bot authentication, JSON validation, a shared rate-limit scheduler, and bounded retries. `discord_interaction_request` calls interaction and webhook routes without Bot authentication. `discord_api_upload` supports one Bot-authenticated multipart file and `Webhook.upload_files_json` supports up to ten token-authenticated webhook files. `discord_verify_interaction` checks Ed25519 signatures and replay age, and `discord_cache_*` provides bounded storage and dispatch ingestion. Text and binary WebSocket helpers support fragmented frames, payload limits, and timeout-safe reads; binary helpers provide the framing needed to build protocol integrations. These primitives do not implement the DAVE cryptographic session.

Load bot tokens from environment variables through `std/env.kry`; never write them into source code or commit them. Self-bots are unsupported.
