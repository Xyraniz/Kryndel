# Changelog

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
