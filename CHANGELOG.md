# Changelog

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
