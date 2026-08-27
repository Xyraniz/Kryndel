# Changelog

## 1.1.0 — Strict native toolchain

Kryndel now has a real static checker shared by `check`, `run`, and `build`. Type annotations are resolved for `Int`, `Float`, `Bool`, `String`, `Bytes`, homogeneous `Array[T]`, `Nil`, `Option[T]`, `Result[T, E]`, structs, and enums. Immutable bindings are enforced by default, Boolean conditions are strict, numeric operations are checked, and conversions reject malformed input.

The native runtime now uses a per-invocation arena, deterministic categorized diagnostics with source excerpts, one authoritative builtin registry, relative public modules with cycle and traversal checks, exhaustive enum matching, a REPL, a deterministic formatter, and `doctor` readiness checks. The `KRYNATIVE1` artifact contract remains deterministic and stores the exact validated source payload.

The repository prose was migrated to English, the verification matrix was expanded for GCC, Clang, sanitizers, and documentation, and the removed Python bootstrap remains absent.

## 1.0.0 — Native core

Kryndel was moved to one native command-line implementation in `tools/kry`, backed by `native/kry.c`. The bootstrap interpreter, historical interpreter module directory, package metadata, and fixtures dependent on that route were removed.
