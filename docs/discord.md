# Discord integration

The `packages/discord` package provides a small, explicit bot integration. `bot(token, intents)` validates the token locally and constructs a `Bot`; `Bot.run()` opens a secure WebSocket to Discord Gateway v10, sends an Identify payload with the token and intent mask, receives the first Gateway frame, and closes cooperatively. `Bot.on_message(command)` is the typed registration point for commands in the initial surface; the API does not invent events or simulate responses.

| Element | Type | Semantics |
| --- | --- | --- |
| `Bot.token` | `String` | Kept in the program memory and never printed. |
| `Bot.intents` | `Array[String]` | `Guilds`, `GuildMembers`, `GuildMessages`, and `MessageContent` become explicit bits. |
| `Bot.gateway` | `String` | WebSocket URL `wss://gateway.discord.gg/?v=10&encoding=json`. |
| `bot(token, intents)` | `Result[Bot,String]` | Rejects an empty token. |
| `Bot.run()` | `Result[Nil,String]` | Performs a bounded handshake, Identify, read, and close. |
| `Bot.on_message(command)` | `Result[Nil,String]` | Rejects empty registrations and keeps a deterministic contract. |

The WebSocket layer implements the RFC 6455 handshake, validates `Sec-WebSocket-Accept`, uses TLS 1.2 or newer for `wss`, masks client frames, answers ping with pong, rejects frames larger than the input limit, and treats remote closure as an explicit error. `WebSocket` is a non-`Copy` type, so it cannot cross channels or be implicitly duplicated.

Authenticated HTTP calls use `http_request_auth`. The token is placed only in the `Authorization: Bearer ...` header; status and transport diagnostics never include the secret. In production, the token must come from an environment variable read through `std/env.kry`, not be written into source code or stored in the repository.

> The initial integration covers a real Gateway connection and a secure API boundary. It does not hide the complexity of reconnection, rate limits, sharding, sequence persistence, or asynchronous callbacks; those capabilities must be added as explicit contracts and tests against a controlled test server.
