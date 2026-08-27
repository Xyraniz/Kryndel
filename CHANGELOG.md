# Changelog

## 1.2.0 — Safe runtime and self-contained artifacts

The native checker now rejects unresolved container types, validates Bool and Nil match coverage, and restricts worker functions to parameters plus explicitly shareable channels. Copy-safe values crossing channels are deeply cloned, bounded channels use queue-based condition variables, and cooperative cancellation and timed joins return typed `Result` values without detaching workers.

The builtin registry now covers numeric, UTF-8 text, bytes, collection, filesystem, and concurrency operations through shared checker/runtime contracts. Native artifacts use the versioned `KRYNATIVE2` format with compiler and target metadata, deterministic source ordering, SHA-256 content hashes, embedded module sources, strict decoder validation, and atomic writes. The CLI exposes JSON diagnostics, the launcher discovers `cc`, GCC, or Clang and caches compiler configuration, and the REPL preserves bindings across lines with `:reset`, `:load`, and multiline input.

## 1.1.0 — Strict native toolchain

Kryndel now has a real static checker shared by `check`, `run`, and `build`. Type annotations are resolved for `Int`, `Float`, `Bool`, `String`, `Bytes`, homogeneous `Array[T]`, `Nil`, `Option[T]`, `Result[T, E]`, `Channel[T]`, `Thread[T]`, structs, and enums. Immutable bindings are enforced by default, Boolean conditions are strict, numeric operations are checked, and conversions reject malformed input.

The native runtime now uses a per-invocation arena, deterministic categorized diagnostics with source excerpts, one authoritative builtin registry, relative public modules with cycle and traversal checks, exhaustive enum, option, and result matching, a mutex-protected channel runtime, OS-backed worker threads with joins and bounded receives, typed UTF-8 file access, environment lookup, a REPL, a deterministic formatter, and expanded `doctor` readiness checks. The `KRYNATIVE1` artifact contract remains deterministic and stores the exact validated source payload.

The repository prose was migrated to English, the verification matrix was expanded for GCC, Clang, AddressSanitizer, UndefinedBehaviorSanitizer, LeakSanitizer, ThreadSanitizer, and documentation, and the removed Python bootstrap remains absent.

## 1.0.0 — Native core

Kryndel was moved to one native command-line implementation in `tools/kry`, backed by `native/kry.c`. The bootstrap interpreter, historical interpreter module directory, package metadata, and fixtures dependent on that route were removed.
