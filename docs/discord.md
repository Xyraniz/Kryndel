# Discord integration

`packages/discord` is a bot-account client for Discord API v10. It exposes bounded REST helpers, application-command builders and sync, Gateway interaction routing, basic prefix-command callbacks, autocomplete responses, asynchronous Gateway member queries, configurable single-shard connections, and a bounded event cache. It never automates normal user accounts.

The separate `discord-self` package exposes the user-token API under the distinct `discord-self` import path. Discord forbids automating ordinary user accounts; use a bot account for supported integrations. `discord-self` is an opt-in compatibility experiment and does not include stealth or client-fingerprint spoofing.

## User-account client (`discord-self`)

Install it alongside the regular client with `kry add discord-self ^1.2.0`; it depends on `discord ^2.3.0`. Import both `discord` and `discord-self` when an application needs both APIs. The modules have separate package paths. `discord-self` exposes `fetch_user`, `fetch_guild`, `fetch_guilds(with_counts)`, `fetch_guild_member`, `list_guild_members`, `search_guild_members`, `fetch_channel`, `fetch_message`, `send_files`, message send/edit/delete, DM creation, plus Gateway presence and voice-state updates. `fetch_guilds()` includes approximate counts by default; pass `false` to omit them. `guilds()` remains an alias that omits counts.

Register `discord_self.on_event` before `client.run()`. Its callback receives an object containing the Gateway event `name` and `data`. Inside that callback, the package-level `set_presence_json(...)` and `set_voice_state(...)` functions send opcode 3 and 4 updates through the active session. `leave_voice(guild_id)` sends a null channel ID. `rpc_activity_json(name, activity_type, state, details)` builds a basic activity; pass complete activity objects through `set_presence_json` when richer fields are needed.

> [!WARNING]
> Discord explicitly prohibits automating normal user accounts outside the bot API. It can terminate accounts used this way. This package is for isolated experimentation; it does not attempt to hide automation. See [Discord's API guidance](https://discord.com/developers/docs/topics/oauth2).

`examples/discord_self.kry` sets `DISCORD_USER_TOKEN` from the environment and changes presence after `READY`. The self-client uses the Gateway connection shared with `discord`; only one Gateway session can be active per Kryndel runtime.

The package source is split into `models.kry`, `validation.kry`, `rest.kry`, `interactions.kry`, `application_commands.kry`, `gateway.kry`, and `cache.kry`; `main.kry` imports these modules as the package entry point. `Bot` and `Webhook` credential fields are private, so callers must use their validated constructors and methods. This privacy change is a breaking API change and was released as package version 2.0.0.

## Create a bot

Create a project with `kry new my-bot`, then install the package with `kry install discord`. Use this import in `main.kry`:

```kry
import "std/env"
import "discord"

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

`Bot.with_shard(shard_id, shard_count)` validates shard configuration. Each `Bot.run()` opens one shard; run one configured Bot process per shard. The client discovers the Gateway, validates its secure Discord host, sends Identify or Resume, tracks sequence numbers, maintains heartbeats, and reconnects when Discord asks it to. It uses `resume_gateway_url`, `session_id`, and the last dispatch sequence when a session can be resumed. Invalid credentials and unsupported shard/intent configurations stop with an error; reconnectable close codes trigger the recovery path.

> [!WARNING]
> The CLI stops programs at its default ten-second wall-clock limit. Start a
> persistent bot in an installed project with `kry --max-wall-ms 0 run main.kry`;
> network operations still have individual ten-second timeouts. Press Ctrl+C
> to stop it. The repository's copyable example runs from
> `examples/discord_bot.kry`.

Every dispatch reaches `discord.on_event` as `{"name":"READY","data":{...}}`. `MESSAGE_CREATE` additionally reaches `discord.on_message` when the generic handler returns an empty string; a non-empty return sends a message reply. If that callback returns an empty string, a Bot configured with `with_prefixes(prefixes)` also checks the message for the longest matching non-empty prefix and dispatches it as a prefix command. Prefixes default to disabled; configure the `MessageContent` intent in code and in the Developer Portal. Bot-authored messages are ignored. Kryndel installs low-priority no-op handlers so an unused callback does not stop the connection. Since Kryndel has named dispatch slots rather than function decorators or closures, register top-level `String -> String` functions with `poly_register`.

Prefix callbacks use `discord.on_prefix_command.<name>` and receive JSON with `message`, `prefix`, `command`, `arguments` (the existing whitespace-separated strings), `argument_text` (normalized from those tokens), and `raw_argument_text` (the original text after the command, preserving internal whitespace, line breaks, and quotes). The additional `parsed_arguments` field removes matching single, double, or paired Unicode quotation marks and keeps quoted text in one argument. A malformed quoted argument sets `parse_error` and routes to `discord.on_prefix_command_error`; its callback may return a reply string. If no named callback exists, `discord.on_prefix_command` receives the context. `examples/discord_bot.kry` uses this route for `!ping`. This layer does not yet provide converters, command groups, checks, cooldowns, or a typed `Context`; regular callbacks return a reply string.

Gateway member requests are asynchronous because the Gateway must keep reading events while Discord sends the chunks. Call `query_guild_members(guild_id, prefix, limit, presences, nonce)` for a prefix query, `request_guild_members_by_ids(guild_id, user_ids, presences, nonce)` for 1–100 unique user IDs, or `chunk_guild_members(guild_id, presences, nonce)` to request the guild member list. Each returns a `Result[String, String]` containing the correlation nonce; pass `""` to generate one. Prefix queries accept limits from 1 to 100. Requesting the full list requires the privileged `GuildMembers` intent, and requesting presences requires `GuildPresences`; enable privileged intents in the Developer Portal as well. Every raw `GUILD_MEMBERS_CHUNK` still reaches `discord.on_event` and updates the member cache. Once all chunks for a request arrive, `discord.on_guild_members_complete` receives JSON with `guild_id`, `nonce`, and the combined `members` array, plus `not_found` and `presences` when provided by Discord. `discord.on_guild_members_error` receives failed queries with `guild_id`, `nonce`, and `reason`; rate-limit errors include `retry_after`. Kryndel limits full-list requests to one per guild every 30 seconds and applies a conservative 110-request outbound Gateway budget per connection per 60-second window. Requests expire after ten minutes, at most 64 can be pending, and aggregate results are capped at 32 MiB. A reconnect reports pending queries as `session_ended`.

## Interactions and application commands

Incoming interaction requests should be verified with `Bot.verify_interaction(public_key, timestamp, raw_body, signature)` before parsing the payload. Invalid signatures and timestamps older than five minutes return `ok(false)`; invalid key or signature encodings return an error. Interaction callback and webhook methods do not send the bot Authorization header:

- `Bot.respond_interaction` posts an initial callback.
- `Bot.create_followup`, `Bot.get_followup`, `Bot.edit_followup`, and `Bot.delete_followup` manage follow-up messages.
- `Bot.edit_original_response` and `Bot.delete_original_response` manage the initial response.
- `Bot.create_global_command`, `Bot.upsert_global_command`, `Bot.list_global_commands`, and `Bot.delete_global_command` manage global application commands.
- `Bot.bulk_overwrite_global_commands` replaces a global command set.
- `Bot.create_guild_command`, `Bot.update_guild_command`, `Bot.list_guild_commands`, and `Bot.delete_guild_command` manage guild commands; `Bot.bulk_overwrite_guild_commands` replaces a guild command set.

`CommandTree` keeps a local set of command JSON definitions. Use `command_tree(application_id)` for global commands or `guild_command_tree(application_id, guild_id)` for a guild, add definitions with `tree.add(command_json)`, inspect the set with `tree.commands_json()`, and replace the remote set with `tree.sync(bot)`. It validates command and option structure and rejects duplicate name/type pairs before sending. `slash_command_json(name, description, options_json)`, `slash_option_json(name, description, type, required, properties_json)`, `slash_subcommand_json(name, description, options_json)`, `slash_subcommand_group_json(name, description, subcommands_json)`, `user_context_command_json(name)`, and `message_context_command_json(name)` build common payloads. `slash_option_json` takes a Discord option type (3–11) and JSON properties such as `choices`, `autocomplete`, `channel_types`, and min/max values or lengths. Names use the lowercase ASCII subset; Discord may accept additional Unicode names that Kryndel cannot classify yet.

For interactive message components, `button_json(label, custom_id, style, disabled)` builds labeled button styles 1–4; `button_with_emoji_json` also supports emoji-only buttons and validates the emoji object. `link_button_json(label, url, disabled)` builds style 5 link buttons, and `premium_button_json(sku_id)` builds style 6 purchase buttons. `button_row_json(buttons_json)` validates rows of up to five buttons, including mixed interactive, link, and premium buttons. `string_select_option_json` and `string_select_json` build text-select menus; `string_select_option_with_emoji_json` adds a validated emoji to an option, and `string_select_row_json` wraps a menu in an action row. `user_select_json`, `role_select_json`, `mentionable_select_json`, and `channel_select_json` build entity selectors; `entity_select_row_json` wraps one in an action row. Their optional default values are passed as JSON and checked against the selector type. `interaction_message_components_response_json(content, rows_json, ephemeral)` creates an initial response with up to five legacy rows containing buttons or a select menu. Component `custom_id` values must be unique within the message. `interaction_component_values_json(interaction_json)` reads selected strings or entity IDs from a message component interaction; `interaction_component_resolved_json(interaction_json)` returns the resolved entity maps for a user, role, mentionable, or channel selection. For component interactions, `interaction_defer_update_response_json()` builds a type 6 deferred message update, and `interaction_message_update_response_json(content, rows_json)` builds a type 7 response that edits the original message. `interaction_message_response_visibility_json(content, ephemeral)` builds a type 4 text response with an explicit visibility flag; the existing `interaction_message_response_json` remains public by default. These response builders do not determine whether an interaction is compatible with a callback type; use update response types for component interactions.

`text_input_json`, `modal_text_input_label_json`, `modal_string_select_json`, `modal_entity_select_json`, `modal_select_label_json`, and `interaction_modal_response_json` build text inputs and select menus inside Discord's current Label layout. Text inputs also accept the legacy Action Row layout still emitted by discord.py; prefer Labels for new modals. `interaction_modal_text_value(interaction_json, input_custom_id)` reads submitted text, while `interaction_modal_select_values_json(interaction_json, component_custom_id)` reads submitted select values and `interaction_modal_resolved_json` returns the associated entity maps. Gateway routes modal submissions through `discord.on_component.<modal_custom_id>`. File uploads and newer modal controls such as file upload, radio groups, and checkboxes remain available through raw JSON. Components V2 can also be sent as raw JSON through the existing request methods.

For Gateway interactions, `Bot.run()` routes each `INTERACTION_CREATE` to a named callback. Register `discord.on_command.<name>` for a root command or `discord.on_command.<name>.<subcommand>` / `discord.on_command.<name>.<group>.<subcommand>` for its selected leaf. The leaf slot is tried first; if it has no handler, the root slot is tried. Autocomplete uses the matching `discord.on_autocomplete` slots; `interaction_autocomplete_focused_option_json(interaction_json)` returns the focused leaf option. `interaction_command_options_json(interaction_json)` returns the selected leaf's option array, and `interaction_command_option_value_json(interaction_json, option_name)` returns one matched option value encoded as JSON. For context-menu commands, `interaction_context_target_user_json`, `interaction_context_target_member_json`, and `interaction_context_target_message_json` resolve the selected target from the interaction's `resolved` data. Register `discord.on_component.<custom_id>` for a component or modal submit. The callback receives the raw interaction JSON and returns a JSON callback object. When no named slot has a callback, `discord.on_interaction` receives the event as a catch-all. A non-empty string returned by `discord.on_event` for `INTERACTION_CREATE` is treated as the initial callback body and takes priority over named routing.

> [!IMPORTANT]
> Discord requires an initial interaction acknowledgement within three
> seconds. For work that may take longer, call `interaction_defer(...)` first,
> then send the result with an edit or follow-up.

`interaction_followup_files_json` sends multipart file attachments, while `interaction_edit_original_response_files_json` and `interaction_edit_followup_files_json` upload files when editing a response. `interaction_original_response_json` fetches the original response and `interaction_delete_original_response` removes it. The `interaction_get_followup_json`, `interaction_edit_followup_json`, and `interaction_delete_followup` helpers manage follow-up messages by their message ID. These helpers use the interaction credentials and omit Bot authorization. `interaction_defer_response_json(ephemeral)` builds a type 5 callback body when returning a deferred acknowledgement is sufficient. Use `autocomplete_response_json(choices_json)` for autocomplete. `string_choice_json`, `integer_choice_json`, and `number_choice_json` build validated choice objects; autocomplete responses accept at most 25 choices. Use either a generic `discord.on_event` interaction handler or a named slot for a given interaction so it is acknowledged only once.

For a plain-text initial response, `interaction_message_response_json(content)` builds a type 4 callback, escapes content, disables implicit mentions, and enforces the 2,000 UTF-16-unit limit. Other callback types can be returned as raw JSON. This routing layer does not infer parameter types from function signatures, perform Python-style option conversion, or implement local command checks, cooldowns, transformers, and decorators. Callback functions can validate and read the raw `data` and `options` values with Kryndel's JSON helpers. `CommandTree` is a JSON command set with bulk sync; it does not generate the JSON from typed declarations.

`examples/discord_interactions.kry` demonstrates a guild-scoped command tree, nested group and subcommand routing, option extraction, interactive and link buttons, string-select and user-select callbacks, text-input modals, and autocomplete. Set `DISCORD_TOKEN`, `DISCORD_APPLICATION_ID`, and `DISCORD_GUILD_ID` before running it. The example syncs by bulk overwrite, so the tree must contain every command that should remain registered in that guild.

`Bot.sync_global_commands(application_id, commands_json)` and the global/guild bulk-overwrite methods remain available for applications that manage command JSON directly instead of using `CommandTree`.

### Global command contexts and permissions

`slash_command_with_settings_json(name, description, options_json, settings_json)` adds optional top-level application command fields while preserving the validated name, description, and options. It validates `default_member_permissions`, `integration_types` (`0` guild install, `1` user install), `contexts` (`0` guild, `1` bot DM, `2` private channel), and `nsfw`; other API fields in the settings object pass through. `CommandTree` rejects `integration_types` and `contexts` in guild-scoped trees because Discord supports those fields only for global commands. Per-command permission overwrites require an authorized Bearer token; the package's Bot-token methods cannot call those endpoints.

### Channel type filters

Discord channel options and channel select components accept the documented `channel_types` values `0`, `1`, `2`, `3`, `4`, `5`, and `10` through `16`. Kryndel validates these values when building either payload.

## Webhooks

`Bot.create_webhook(channel_id, name)` creates a webhook with bot authentication and returns a token-authenticated `Webhook` when Discord includes its token in the create response. `webhook_from_json(value)` validates and converts a JSON webhook object that includes both its ID and token; `webhook(id, token)` constructs one directly. `webhook_from_url(value)` accepts exactly `https://discord.com/api/webhooks/{id}/{token}` and rejects other hosts, HTTP, extra path segments, query strings, and invalid IDs or tokens. The token is a credential; keep it out of source control and logs. Token-authenticated `Webhook` requests use the token in the fixed Discord API path and never send a bot `Authorization` header.

`examples/discord_webhook_live.kry` is an opt-in live probe. Set `KRYND_DISCORD_WEBHOOK_URL` in the environment and run `kry run examples/discord_webhook_live.kry`; it creates one plain-text message, edits and fetches it, then deletes it and verifies the API returns 404. It does not delete or modify the webhook itself. Windows double-click launch requires the `.kry` association installed by `cmd/kry-installer`.

`Bot.list_channel_webhooks`, `Bot.list_guild_webhooks`, `Bot.fetch_webhook`, `Bot.edit_webhook_json`, and `Bot.delete_webhook` cover management routes that use bot authentication. Edit payloads remain raw JSON so Discord can add webhook fields without waiting for new wrappers.

- `Webhook.send(content)` sends plain text, waits for the created message, escapes JSON, enforces Discord's 2,000 UTF-16-unit content limit, and disables mentions by default.
- `Webhook.send_files(content, filenames, file_data, thread_id)` sends text with one to ten uploaded files, creates the attachment metadata, escapes content and filenames, waits for the created message, and disables mentions by default. Empty content is allowed when files are attached.
- `Webhook.execute_json(payload_json, wait, thread_id)` exposes Discord's JSON execute payload, including embeds, components, polls, and future fields. Set `wait` to receive the created message; an empty `thread_id` targets the webhook's default channel. This method preserves the caller's `allowed_mentions` payload, so specify it when the payload could contain mentions.
- `Webhook.execute_files_json(payload_json, filenames, file_data, wait, thread_id)` exposes the multipart execute route with the same wait control. Its JSON payload can include `thread_name` and `applied_tags` to create a forum or media post with files. With `wait=false`, Discord may return an empty success body; use `upload_files_json` or `send_files` when the created message body is needed.
- `Webhook.upload_files_json(payload_json, filenames, file_data, thread_id)` sends one to ten files as `files[0]` through `files[9]` multipart fields and waits for the created message. Pass matching filename and `Bytes` arrays, include corresponding `attachments` entries in the JSON payload, and use an empty thread ID for the default channel. Uploads use the same runtime size limit as other Discord API bodies.
- `fetch()` and `edit_json(payload_json)` retrieve or edit the webhook; `delete()` removes it.
- `fetch_message`, `edit_message_json`, and `delete_message` operate on a webhook message. Pass an empty `thread_id` for a normal message or a valid snowflake for a thread message. `edit_message_files_json(message_id, payload_json, filenames, file_data, thread_id)` edits a message with new multipart files; include the retained and new attachment metadata in `payload_json`.

JSON methods retain Discord's evolving request schema without pretending Kryndel has typed models for every embed, component, poll, and attachment variant. The client validates the JSON body, route, IDs, and request/response size before sending it.

## REST, attachments, and rate limits

`Bot.request(method, route, body)` exposes the full REST API v10 route surface on a fixed `https://discord.com/api/v10` origin with Bot authentication. Common helpers include users, guilds, channels, messages, roles, moderation actions, and application commands. Snowflake IDs are checked by typed helpers, message bodies are JSON-escaped, message length is limited to 2,000 UTF-16 code units, and implicit mentions are disabled by default.

`Bot.request_with_audit_reason(method, route, body, reason)` adds Discord's `X-Audit-Log-Reason` header to a JSON API request. `Bot.upload_with_audit_reason` and `Bot.upload_files_with_audit_reason` provide the same support for multipart requests. The reason is percent-encoded from UTF-8 and must fit Discord's 512-character encoded limit; pass an empty string to omit the header. Typed variants are available as `ban_with_reason`, `ban_with_options_and_reason`, `bulk_ban_with_reason`, `unban_with_reason`, `kick_with_reason`, `edit_guild_member_json_with_reason`, `add_member_role_with_reason`, and `remove_member_role_with_reason`.

`Bot.get_guild_member(guild_id, user_id)` fetches one member. `Bot.list_guild_members(guild_id, after, limit)` fetches one page; pass `""` for `after` on the first call, then use the last member's `user.id` to request the next page. `Bot.search_guild_members(guild_id, query, limit)` searches usernames and nicknames by prefix. Both list and search limits must be between 1 and 1000. Discord requires the privileged `GUILD_MEMBERS` intent for the list endpoint, enabled both in the Developer Portal and in the `Bot` configuration. This REST pagination complements cached Gateway member events; it does not request Gateway chunks or combine pages automatically.

`Bot.edit_guild_member_json(guild_id, user_id, payload_json)` forwards a JSON object to the member edit route, including nickname, roles, mute/deaf, voice channel, and timeout fields. `Bot.list_guild_bans(guild_id, before, after, limit)` fetches a page of bans; use empty strings for absent cursors. `Bot.get_guild_ban(guild_id, user_id)` fetches one ban. `Bot.ban_with_options` and `Bot.bulk_ban` accept message deletion seconds from 0 to 604800; bulk ban accepts 1 to 200 unique user IDs and returns Discord's banned/failed ID lists. The existing `Bot.ban` keeps its zero-second deletion behavior. The corresponding `*_with_reason` methods put the moderation reason in Discord's audit log. Listing and managing bans require `BAN_MEMBERS`; bulk ban also requires `MANAGE_GUILD`; kicking a member requires `KICK_MEMBERS`. Member edits and role changes require the endpoint-specific permissions.

`Bot.upload(method, route, payload_json, filename, data)` sends one bounded file as a multipart request with `payload_json`. Request bodies are replayed safely across retries. The runtime uses Discord's rate-limit bucket headers and global 429 information to coordinate waits across routes and worker runtimes. It makes an initial request and allows up to five retries, with a five-minute maximum wait per retry. API response sizes and upload sizes are limited by the runtime's configured input limit; credentials and long interaction tokens are redacted from returned HTTP errors.

`Bot.upload_files(method, route, payload_json, filenames, file_data)` sends one to ten files to a caller-selected Bot-authenticated API v10 route; the JSON payload must include matching attachment metadata. `Bot.send_files(channel_id, content, filenames, file_data)` sends a channel message with those files and suppresses implicit mentions. Both require equally sized filename and byte arrays, use `files[0]` through `files[9]`, and honor the same total upload limit and REST retry policy.

## Object cache

`Bot.cache_get(kind, id)`, `Bot.cache_put(kind, id, object_json)`, `Bot.cache_delete(kind, id)`, and `Bot.cache_clear()` access a runtime-local cache capped at 10,000 objects with a 30-minute sliding lifetime. The Gateway automatically caches recent users, guilds, channels, threads, roles, members, messages, voice states, and other supported dispatch objects. A `MESSAGE_CREATE` event also caches its author as a user when that user is not already present. Use `guild_id:user_id` for member and role cache keys. Cache misses and expired objects return an error result.

Partial Gateway updates merge fields into cached guilds, channels, threads, roles, members, messages, and users; nested objects merge recursively while arrays and explicit `null` values replace the old value. If an encoded object exceeds the 1 MiB cache-entry limit, Kryndel evicts that entry and reports the cache update error instead of retaining stale data. `GUILD_DELETE` with `unavailable: true` preserves the guild cache for a possible Gateway recovery. A permanent `GUILD_DELETE` removes the guild, its guild-scoped objects, and cached messages belonging to its channels; globally cached user objects remain available. Members received through `GUILD_CREATE`, `GUILD_MEMBER_ADD`, `GUILD_MEMBER_UPDATE`, and `GUILD_MEMBERS_CHUNK` are indexed by `guild_id:user_id`, preserve all received fields, and refresh the global user entry. Gateway member queries retain raw member and presence objects in the completion payload while also updating the bounded cache.

## Voice

Gateway `VOICE_STATE_UPDATE` and `VOICE_SERVER_UPDATE` events are available through `discord.on_event` and the event cache. Connecting to a voice channel, UDP/Opus media transport, and DAVE end-to-end media encryption are not implemented. DAVE is required for current non-stage voice calls; the client does not report voice support when the cryptographic negotiation and media transport are absent.

`discord-self` exposes Gateway presence and voice-state updates while its client is running. `set_presence_json` accepts a status plus an array of activity objects; `rpc_activity_json(name, type, state, details)` builds a basic activity, and arbitrary activity fields can be supplied as JSON. `set_voice_state(guild_id, channel_id, self_mute, self_deaf)` joins, leaves (empty channel ID), and changes self-mute/deaf flags. These calls update Gateway state only: voice websocket/media, audio codecs, and DAVE are not implemented.

## Runtime support

`discord_api_request` performs bounded API v10 requests with Bot authentication, JSON validation, a shared rate-limit scheduler, and bounded retries. `discord_interaction_request` calls interaction and webhook routes without Bot authentication. `discord_api_upload` supports one Bot-authenticated multipart file; `Bot.upload_files`, `Bot.send_files`, and `Webhook.upload_files_json` support up to ten files. `discord_verify_interaction` checks Ed25519 signatures and replay age, `discord_cache_*` provides bounded storage and dispatch ingestion, and `discord_gateway_*` sends and aggregates Gateway member requests during `Bot.run()`. Text and binary WebSocket helpers support fragmented frames, payload limits, and timeout-safe reads; binary helpers provide the framing needed to build protocol integrations. These primitives do not implement the DAVE cryptographic session.

Load bot tokens from environment variables through `std/env.kry`; never write them into source code or commit them. The `discord-self` example reads `DISCORD_USER_TOKEN` the same way and keeps it out of logs.
