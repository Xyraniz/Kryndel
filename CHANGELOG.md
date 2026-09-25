# Changelog

## Discord package 2.2.0

The Discord package adds a local global/guild `CommandTree`, named Gateway callbacks for application commands, autocomplete and component interactions, asynchronous Gateway member queries, multipart uploads, encoded audit-log reasons, and helpers for message, defer, and autocomplete responses. Prefix callback contexts now include parsed arguments and route malformed quoting to an error callback while preserving the existing fields. Gateway query aggregation reports failed requests through callbacks, link buttons validate their URL host, and member-prefix limits accept 1–100. The current release still does not provide typed declarations, local command checks/cooldowns, or voice media and DAVE support.

## Discord package 2.1.0

The Discord client adds Bot-authenticated multi-file uploads and message sends (up to ten files), builders for slash and context-menu command payloads, and validated global command bulk sync. Gateway close-code handling now retries reconnectable closures and starts a fresh session when the old session is invalid. `kry --max-wall-ms 0` supports persistent programs while network operations remain bounded individually. Previous Discord package archives are removed from the active registry so 2.1.0 is the sole installable release; projects pinned to an exact earlier Discord version must update their dependency constraint. This release still does not provide full discord.py parity: typed `CommandTree` dispatch/UI/prefix command frameworks and voice/DAVE remain unimplemented.

## Unreleased — Discord Gateway, interactions, and REST

The Discord Gateway now retains session IDs and dispatch sequences, resumes sessions after transient disconnects, reconnects after server requests, validates shard configuration, and reports permanent close codes. Gateway dispatches update a bounded cache of recent Discord objects. The Discord package now includes Ed25519 interaction signature checks, unauthenticated interaction callback and follow-up helpers, global and guild command registration, and multipart file uploads. REST rate-limit buckets and global limits are coordinated across routes and worker runtimes, and multipart request bodies are replayed on retries.

The Discord package source is organized into model, validation, REST, interaction, Gateway, and cache modules. `Bot` and `Webhook` credentials are now private fields; this breaking API change is versioned as Discord package 2.0.0. The current archive, package manifest, and registry checksum are kept in sync.

Gateway events and voice-state updates are exposed, but voice media transport and DAVE encryption are not implemented.

The previous Discord Gateway did not reconnect after an explicit reconnect request and did not preserve resumable session state. The earlier REST layer retried 429 responses independently and could resend an empty file body. These issues are fixed. Current permanent Gateway failures and malformed interaction signatures now surface as explicit errors.

## Unreleased — Language contracts and verification matrix

Float, map/set, and Unicode behavior is now documented as an explicit language contract: finite IEEE-754 values, signed-zero handling, subnormal preservation, insertion ordering, structural key equality, code-point indexing, and locale-independent formatting are covered by regression and interpreter/native differential tests. The differential capture path now uses the supported `io.ReadAll` API, and invalid test inputs were corrected to exercise the actual typed syntax.

The Makefile now exposes `benchmark`, `verify-fast`, `verify-full`, and `verify`. Verification records native build logs, runs static, fuzz, documentation, executable smoke, parity, race, coverage, and benchmark checks, and applies bounded test timeouts. The expensive Stage 36 bootstrap retains race coverage with an explicit `KRY_RACE=1` budget rather than failing on race-instrumentation overhead. CI uses the complete `make verify` gate. Documentation is English-only for the package, Discord, Windows, and test guides, and stale or unfinished references are rejected by the documentation test.

## Unreleased — Crypto hardening, executable protection, and Python ergonomics

The cryptography library is substantially expanded and now covers both the
interpreter and the native C backend with byte-for-byte parity. New primitives:
`crypto_sha512`, `crypto_sha384`, `crypto_sha1`, `crypto_md5`,
`crypto_aes_gcm_encrypt`, `crypto_aes_gcm_decrypt`, `crypto_pbkdf2_sha256`,
`crypto_hkdf_sha256`, `crypto_constant_time_equal`, `crypto_xor`,
`base64url_encode` and `base64url_decode`. Every primitive is verified against
published vectors (RFC 5869, RFC 6070, NIST GCM) and against the Go
implementation, and the test suite asserts identical output from the
interpreter, the Linux ELF and the Windows PE.

Two correctness bugs in the native crypto runtime were found and fixed rather
than hidden: the SHA-1 compression block used a right-rotation where the
algorithm requires a left-rotation, and the AES-GCM counter started at `IV||1`
instead of `IV||2` (the first data block must use `J0 + 1`). Both are now
covered by regression tests.

Built executables and artifacts can now be protected. `kry build --encrypt`
wraps an artifact in the authenticated `KRYSEAL1` container — AES-256-GCM under
a PBKDF2-HMAC-SHA-256 key derived from a passphrase — and the plaintext artifact
never touches disk. A wrong passphrase, a truncated file, or any tampering fails
the GCM tag check and is reported as a categorized artifact diagnostic.
`--passphrase` and `--passphrase-file` supply the secret, and running a sealed
`.kexe` interactively prompts for it. `kry build --obfuscate` masks string
literals in the native binary so a `strings` scan does not reveal messages or
secrets, without changing observable output.

Several Python-style ergonomics gaps are closed. Functions may declare default
parameter values (`fn greet(name: String, greeting: String = "Hello")`), with
the checker rejecting a required parameter that follows a defaulted one and a
default of the wrong type. New helpers `string_slice` and `array_slice_range`
provide negative-index slicing with clamped bounds, `string_format` substitutes
`{}` placeholders with `{{`/`}}` escapes, and `array_indices`/`array_zip`
support enumeration and pairing. All are available in the interpreter and the
native backend with verified parity.

## Unreleased — Real native executables

`kry build --format=elf` and `--format=exe` now produce genuine, runnable
executables through a C-based ahead-of-time backend. The checked program is
lowered to C and compiled with the host toolchain (`cc`/`gcc`/`clang` for Linux,
`x86_64-w64-mingw32-gcc` for Windows), replacing the previous stub PE/ELF
emitters that only handled constant top-level output. The backend supports
functions, recursion, `if`/`else`, `while`, `for`, `match`, structs, enums,
arrays, maps, sets, strings, bytes, `Option`, `Result`, the `?` operator and
`defer`, with byte-for-byte identical output to the interpreter. `--format=c`
emits the generated C source. Top-level bindings become file-scope globals so
programs that mix top-level statements with functions compile correctly.

The backend also lowers the concurrency surface: `Shared[T]` cells
(`shared_new`/`shared_read`/`shared_write`/`shared_swap`), isolated `Actor[T]`
mailboxes (`actor_channel`, `actor_with_capacity`, `actor_send`,
`actor_try_receive`, `actor_receive_timeout`, `actor_close`), structured
`TaskGroup` ownership (`task_group`, `task_spawn`, `task_group_cancel`,
`task_group_wait`), `thread_spawn`, `await`/`await_timeout`, and runtime
polymorphic dispatch (`poly_register`, `poly_reorder`, `poly_dispatch`). Handler
names are resolved at runtime through a generated name table, so wrappers that
forward a handler name dynamically still dispatch correctly. Constructs that
remain genuinely unsupported (HTTP, WebSockets, Windows APIs) are rejected with
a clear diagnostic instead of silently degrading.

The native runtime now includes JSON parsing and Go-compatible serialization,
SHA-256 and HMAC-SHA-256, cryptographically secure random bytes, the full
filesystem surface, environment lookup, process execution, sleep and yield.

Thirty additional builtins are now available in both the interpreter and the
native backend with verified parity: `string_repeat`, `string_index_of`,
`string_pad_start`/`string_pad_end`, `string_lines`, `string_chars`,
`string_to_upper`/`string_to_lower`, `array_sort`, `array_index_of`,
`array_sum`, `array_min`/`array_max`, `array_take`/`array_drop`,
`map_contains_key`, `map_values`, `map_remove`, `set_remove`, `set_to_array`,
`tan`, `atan`, `atan2`, `exp`, `log10`, `log2`, `trunc`, `sign` and `clamp`.

Several interpreter builtins that were declared to return `Result` but returned
raw values (`crypto_random_bytes`, `fs_read_dir`, `fs_create_dir`,
`fs_create_dir_all`, `fs_remove_file`, `fs_remove_dir_all`, `fs_copy_file`,
`fs_move_file`, `fs_file_size`, `fs_file_modified_time`, `fs_absolute_path`,
`fs_temp_file`) now return proper `ok`/`err` results, and `json_parse`
preserves integer precision. Top-level `defer` blocks now run at program exit.

## Public registry hardening

The default GitHub-backed registry can now be used from a fresh directory with `kry install discord`, without a local manifest bootstrap or registry configuration. The development registry validates package coordinates and manifests before publication, serializes concurrent writes, uses atomic replacement for archives and indices, and advertises cache-safe static metadata. The checked-in Discord archive and its index remain reproducible and SHA-256 pinned.

The language/runtime now includes compile-time `const` initializers, enforced private struct fields, signature-based overloads, constrained generic functions, isolated `Actor[T]` mailboxes, explicit `await` operations, cancellable sleep/yield effects, and a checked constant-folding fast path. Primitive folding preserves instruction accounting and does not claim a general JIT; native output remains an explicit backend boundary.

Fresh installs also normalize temporary directory names that cannot be represented as package identifiers, while preserving existing project files. Actor and async APIs are now present in the authoritative builtin registry and therefore participate in `doctor`, static checking, and runtime dispatch consistently.

Shared mutable state is now explicit and synchronized through `Shared[T]`, `shared_read`, `shared_write`, and atomic `shared_swap`; only Shared and Channel handles may cross the worker boundary. Const bindings now enforce deep immutability over their entire type graph and reject synchronization, thread, actor, socket, and other runtime handles.

Structured concurrency is now available through `TaskGroup`: child workers are owned by a group, cancellation propagates to all siblings, waits join every child, and the first child failure is returned after sibling cleanup. Static overload resolution now performs complete-argument multiple dispatch and rejects ambiguous matches rather than choosing declaration order.

Direct self-recursive tail calls now use an iterative runtime trampoline. Tail recursion therefore avoids consuming additional logical call-depth budget while non-tail calls retain the ordinary safety limit. A regression test evaluates 10,000 tail calls under the normal depth limit.

Linux x64 native builds now emit a real AOT ELF for compile-time top-level `print`/`println` programs, including constant bindings. The output contains direct x86-64 syscalls and embedded data, has no Kryndel VM or interpreter payload, and rejects unsupported source constructs explicitly.

The package manager now provides `kry uninstall PACKAGE [PACKAGE ...]`. It removes direct manifest entries, prunes their vendored files, preserves still-reachable transitive dependencies, rewrites `kry.lock`, and leaves the shared download cache intact. The registry now publishes expanded `async`, `crypto`, and `fs` libraries at version 1.1.0 and a new `json` library at version 1.0.0.

## 1.3.0 — Collections, propagation, packages, native targets, and platform APIs

Kryndel now supports typed `Map[K,V]` and `Set[T]` values, `for` iteration, receiver methods through `impl`, `defer` cleanup scopes, explicit `unsafe` regions, and strict `Option`/`Result` propagation with `?`. The checker and runtime share deterministic collection equality, cloning, display, bounds, and non-Copy WebSocket ownership rules.

The CLI adds project lifecycle commands, an HTTP registry client/server, reproducible package archives, semver constraints, SHA-256 lockfiles, offline cache reuse, secure vendor extraction, PE/ELF inspection, checked LLVM-compatible IR emission, and native `windows-x64` and `linux-x64` output paths. The runtime adds validated JSON, bounded HTTP/TLS, bearer-authenticated requests, an RFC 6455 client, shell-free process execution, and explicit Windows registry, service, Event Log, Raw Input, and `DeviceIoControl` boundaries.

The repository includes typed `std/env`, `std/json`, and `std/http` wrappers, a real `packages/discord` Gateway example that never prints tokens, new examples and integration tests, and expanded Make/CI/release verification. Unsupported platform capabilities fail explicitly instead of being represented by mock or mislabeled artifacts.

## 1.2.0 — Go toolchain, bounded runtime, and KRYNATIVE3 artifacts

Kryndel now ships one coherent Go 1.22 implementation with a UTF-8 lexer, recursive-descent parser, static checker, validated intermediate representation, bounded runtime, deterministic diagnostics, and a portable CLI. The production tree contains no C, Python, Rust, Node.js, host interpreter, or external runtime dependency.

The checker rejects unresolved container types, validates Bool and Nil match coverage, restricts worker functions to parameters plus explicitly shareable channels, and enforces recursive Copy transfer. The runtime uses checked arithmetic, immutable values, cancelable workers, bounded channels, result-bearing joins, output and resource budgets, and cleanup on every exit path.

Artifacts use versioned `KRYNATIVE3` metadata with deterministic source ordering, SHA-256 content hashes, embedded module sources, strict decoder validation, atomic writes, and replay without external dependencies. The CLI exposes JSON diagnostics, a deterministic formatter, persistent REPL state, restricted filesystem access, `doctor`, cross-platform builds, and release checksums, SBOM, and provenance.

## 1.1.0 — Strict source toolchain

Kryndel now has a real static checker shared by `check`, `run`, and `build`. Type annotations are resolved for `Int`, `Float`, `Bool`, `String`, `Bytes`, homogeneous `Array[T]`, `Nil`, `Option[T]`, `Result[T, E]`, `Channel[T]`, `Thread[T]`, structs, and enums. Immutable bindings are enforced by default, Boolean conditions are strict, numeric operations are checked, and conversions reject malformed input.

The source runtime uses bounded invocation state, deterministic categorized diagnostics with source excerpts, one authoritative builtin registry, relative public modules with cycle and traversal checks, exhaustive enum, option, and result matching, synchronized channels, managed workers with joins and bounded receives, typed UTF-8 file access, environment lookup, a REPL, a deterministic formatter, and expanded `doctor` readiness checks. The former artifact formats are intentionally rejected.

The repository prose was migrated to English, the verification matrix now covers Go vet, race detection, fuzz smoke, coverage, cross-compilation, deterministic artifacts, and documentation, and the removed legacy implementation remains absent.

## 1.0.0 — Native core

Kryndel was moved to one portable command-line implementation in `tools/kry`, backed by the Go module under `cmd/kry` and `internal/kry`. The bootstrap interpreter, historical implementation directory, package metadata, and fixtures dependent on that route were removed.
