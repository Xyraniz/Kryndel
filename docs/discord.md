# Discord integration

`packages/discord` is a bot-account client for Discord API v10. It exposes bounded REST helpers, an application-command set and sync flow, Gateway interaction routing, command callbacks, autocomplete responses, configurable single-shard connections, and a bounded event cache. It never automates normal user accounts.

The package source is split into `models.kry`, `validation.kry`, `rest.kry`, `interactions.kry`, `application_commands.kry`, `gateway.kry`, and `cache.kry`; `main.kry` imports these modules as the package entry point. `Bot` and `Webhook` credential fields are private, so callers must use their validated constructors and methods. This privacy change is a breaking API change and was released as package version 2.0.0.

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

`Bot.with_shard(shard_id, shard_count)` validates shard configuration. Each `Bot.run()` opens one shard; run one configured Bot process per shard. The client discovers the Gateway, validates its secure Discord host, sends Identify or Resume, tracks sequence numbers, maintains heartbeats, and reconnects when Discord asks it to. It uses `resume_gateway_url`, `session_id`, and the last dispatch sequence when a session can be resumed. Invalid credentials and unsupported shard/intent configurations stop with an error; reconnectable close codes trigger the recovery path. The CLI's normal ten-second execution limit still applies to all programs. Run a persistent bot with `kry --max-wall-ms 0 run examples/discord_bot.kry` to disable the process-wide wall-clock limit; individual network operations retain a ten-second timeout. Ctrl+C stops the process.

Every dispatch reaches `discord.on_event` as `{"name":"READY","data":{...}}`. `MESSAGE_CREATE` additionally reaches `discord.on_message` when the generic handler returns an empty string; a non-empty return sends a message reply. Bot-authored messages are ignored for automatic replies. Kryndel installs low-priority no-op handlers so an unused callback does not stop the connection. Since Kryndel has named dispatch slots rather than function decorators or closures, register top-level `String -> String` functions with `poly_register`.

## Interactions and application commands

Incoming interaction requests should be verified with `Bot.verify_interaction(public_key, timestamp, raw_body, signature)` before parsing the payload. Invalid signatures and timestamps older than five minutes return `ok(false)`; invalid key or signature encodings return an error. Interaction callback and webhook methods do not send the bot Authorization header:

- `Bot.respond_interaction` posts an initial callback.
- `Bot.create_followup`, `Bot.get_followup`, `Bot.edit_followup`, and `Bot.delete_followup` manage follow-up messages.
- `Bot.edit_original_response` and `Bot.delete_original_response` manage the initial response.
- `Bot.create_global_command`, `Bot.upsert_global_command`, `Bot.list_global_commands`, and `Bot.delete_global_command` manage global application commands.
- `Bot.bulk_overwrite_global_commands` replaces a global command set.
- `Bot.create_guild_command`, `Bot.update_guild_command`, `Bot.list_guild_commands`, and `Bot.delete_guild_command` manage guild commands; `Bot.bulk_overwrite_guild_commands` replaces a guild command set.

`CommandTree` keeps a local set of command JSON definitions. Use `command_tree(application_id)` for global commands or `guild_command_tree(application_id, guild_id)` for a guild, add definitions with `tree.add(command_json)`, inspect the set with `tree.commands_json()`, and replace the remote set with `tree.sync(bot)`. It rejects invalid command names/types and duplicate name/type pairs before sending. `slash_command_json(name, description, options_json)`, `user_context_command_json(name)`, and `message_context_command_json(name)` build common command payloads. Slash options remain JSON definitions, so Discord validates their detailed schemas.

For Gateway interactions, `Bot.run()` routes each `INTERACTION_CREATE` to a named callback. Register `discord.on_command.<name>` for an application command, `discord.on_autocomplete.<name>` for its autocomplete interactions, and `discord.on_component.<custom_id>` for a component or modal submit. The callback receives the raw interaction JSON and returns a JSON callback object. When a named slot has no callback, `discord.on_interaction` receives the event as a catch-all. A non-empty string returned by `discord.on_event` for `INTERACTION_CREATE` is treated as the initial callback body and takes priority over named routing. Return promptly: Discord requires the initial acknowledgement within three seconds. For slow work, call `interaction_defer(interaction_json, ephemeral)` before doing it, then send the result with `interaction_followup_json(interaction_json, body)`; both helpers use the interaction credentials and omit Bot authorization. `interaction_defer_response_json(ephemeral)` builds a type 5 callback body when returning a deferred acknowledgement is sufficient. Use `autocomplete_response_json(choices_json)` for autocomplete. `string_choice_json`, `integer_choice_json`, and `number_choice_json` build validated choice objects; autocomplete responses accept at most 25 choices. Use either a generic `discord.on_event` interaction handler or a named slot for a given interaction so it is acknowledged only once.

For a plain-text initial response, `interaction_message_response_json(content)` builds a type 4 callback, escapes content, disables implicit mentions, and enforces the 2,000 UTF-16-unit limit. Other callback types can be returned as raw JSON. This routing layer does not infer parameter types from function signatures, perform Python-style option conversion, or implement local command checks, cooldowns, transformers, and decorators. Callback functions can validate and read the raw `data` and `options` values with Kryndel's JSON helpers. `CommandTree` is a JSON command set with bulk sync; it does not generate the JSON from typed declarations.

`examples/discord_interactions.kry` demonstrates a guild-scoped command tree, a command callback, and autocomplete. Set `DISCORD_TOKEN`, `DISCORD_APPLICATION_ID`, and `DISCORD_GUILD_ID` before running it. The example syncs by bulk overwrite, so the tree must contain every command that should remain registered in that guild.

`Bot.sync_global_commands(application_id, commands_json)` and the global/guild bulk-overwrite methods remain available for applications that manage command JSON directly instead of using `CommandTree`.

## Webhooks

`Bot.create_webhook(channel_id, name)` creates a webhook with bot authentication and returns a token-authenticated `Webhook` when Discord includes its token in the create response. `webhook_from_json(value)` validates and converts a JSON webhook object that includes both its ID and token; `webhook(id, token)` constructs one directly. `webhook_from_url(value)` accepts exactly `https://discord.com/api/webhooks/{id}/{token}` and rejects other hosts, HTTP, extra path segments, query strings, and invalid IDs or tokens. The token is a credential; keep it out of source control and logs. Token-authenticated `Webhook` requests use the token in the fixed Discord API path and never send a bot `Authorization` header.

`examples/discord_webhook_live.kry` is an opt-in live probe. Set `KRYND_DISCORD_WEBHOOK_URL` in the environment and run `kry run examples/discord_webhook_live.kry`; it creates one plain-text message, edits and fetches it, then deletes it and verifies the API returns 404. It does not delete or modify the webhook itself. Windows double-click launch requires the `.kry` association installed by `cmd/kry-installer`.

`Bot.list_channel_webhooks`, `Bot.list_guild_webhooks`, `Bot.fetch_webhook`, `Bot.edit_webhook_json`, and `Bot.delete_webhook` cover management routes that use bot authentication. Edit payloads remain raw JSON so Discord can add webhook fields without waiting for new wrappers.

- `Webhook.send(content)` sends plain text, waits for the created message, escapes JSON, enforces Discord's 2,000 UTF-16-unit content limit, and disables mentions by default.
- `Webhook.send_files(content, filenames, file_data, thread_id)` sends text with one to ten uploaded files, creates the attachment metadata, escapes content and filenames, waits for the created message, and disables mentions by default. Empty content is allowed when files are attached.
- `Webhook.execute_json(payload_json, wait, thread_id)` exposes Discord's JSON execute payload, including embeds, components, polls, and future fields. Set `wait` to receive the created message; an empty `thread_id` targets the webhook's default channel. This method preserves the caller's `allowed_mentions` payload, so specify it when the payload could contain mentions.
- `Webhook.upload_files_json(payload_json, filenames, file_data, thread_id)` sends one to ten files as `files[0]` through `files[9]` multipart fields and waits for the created message. Pass matching filename and `Bytes` arrays, include corresponding `attachments` entries in the JSON payload, and use an empty thread ID for the default channel. Uploads use the same runtime size limit as other Discord API bodies.
- `fetch()` and `edit_json(payload_json)` retrieve or edit the webhook; `delete()` removes it.
- `fetch_message`, `edit_message_json`, and `delete_message` operate on a webhook message. Pass an empty `thread_id` for a normal message or a valid snowflake for a thread message. `edit_message_files_json(message_id, payload_json, filenames, file_data, thread_id)` edits a message with new multipart files; include the retained and new attachment metadata in `payload_json`.

JSON methods retain Discord's evolving request schema without pretending Kryndel has typed models for every embed, component, poll, and attachment variant. The client validates the JSON body, route, IDs, and request/response size before sending it.

## REST, attachments, and rate limits

`Bot.request(method, route, body)` exposes the full REST API v10 route surface on a fixed `https://discord.com/api/v10` origin with Bot authentication. Common helpers include users, guilds, channels, messages, roles, moderation actions, and application commands. Snowflake IDs are checked by typed helpers, message bodies are JSON-escaped, message length is limited to 2,000 UTF-16 code units, and implicit mentions are disabled by default.

`Bot.upload(method, route, payload_json, filename, data)` sends one bounded file as a multipart request with `payload_json`. Request bodies are replayed safely across retries. The runtime uses Discord's rate-limit bucket headers and global 429 information to coordinate waits across routes and worker runtimes. It makes an initial request and allows up to five retries, with a five-minute maximum wait per retry. API response sizes and upload sizes are limited by the runtime's configured input limit; credentials and long interaction tokens are redacted from returned HTTP errors.

`Bot.upload_files(method, route, payload_json, filenames, file_data)` sends one to ten files to a caller-selected Bot-authenticated API v10 route; the JSON payload must include matching attachment metadata. `Bot.send_files(channel_id, content, filenames, file_data)` sends a channel message with those files and suppresses implicit mentions. Both require equally sized filename and byte arrays, use `files[0]` through `files[9]`, and honor the same total upload limit and REST retry policy.

## Object cache

`Bot.cache_get(kind, id)`, `Bot.cache_put(kind, id, object_json)`, `Bot.cache_delete(kind, id)`, and `Bot.cache_clear()` access a runtime-local cache capped at 10,000 objects with a 30-minute sliding lifetime. The Gateway automatically caches recent users, guilds, channels, threads, roles, members, messages, voice states, and other supported dispatch objects. Use `guild_id:user_id` for member and role cache keys. Cache misses and expired objects return an error result.

Partial Gateway updates merge fields into cached guilds, channels, threads, roles, members, messages, and users; nested objects merge recursively while arrays and explicit `null` values replace the old value. If an encoded object exceeds the 1 MiB cache-entry limit, Kryndel evicts that entry and reports the cache update error instead of retaining stale data. `GUILD_DELETE` with `unavailable: true` preserves the guild cache for a possible Gateway recovery. A permanent `GUILD_DELETE` removes the guild, its guild-scoped objects, and cached messages belonging to its channels; globally cached user objects remain available. Members received through `GUILD_CREATE`, `GUILD_MEMBER_ADD`, and `GUILD_MEMBER_UPDATE` are indexed by `guild_id:user_id`, preserve all received fields, and refresh the global user entry.

## Voice

Gateway `VOICE_STATE_UPDATE` and `VOICE_SERVER_UPDATE` events are available through `discord.on_event` and the event cache. Connecting to a voice channel, UDP/Opus media transport, and DAVE end-to-end media encryption are not implemented. DAVE is required for current non-stage voice calls; the client does not report voice support when the cryptographic negotiation and media transport are absent.

## Runtime support

`discord_api_request` performs bounded API v10 requests with Bot authentication, JSON validation, a shared rate-limit scheduler, and bounded retries. `discord_interaction_request` calls interaction and webhook routes without Bot authentication. `discord_api_upload` supports one Bot-authenticated multipart file; `Bot.upload_files`, `Bot.send_files`, and `Webhook.upload_files_json` support up to ten files. `discord_verify_interaction` checks Ed25519 signatures and replay age, and `discord_cache_*` provides bounded storage and dispatch ingestion. Text and binary WebSocket helpers support fragmented frames, payload limits, and timeout-safe reads; binary helpers provide the framing needed to build protocol integrations. These primitives do not implement the DAVE cryptographic session.

Load bot tokens from environment variables through `std/env.kry`; never write them into source code or commit them. Self-bots are unsupported.
