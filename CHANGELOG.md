# Changelog

## [Unreleased]

### Added

- Structured diagnostics with stable JSON output, primary/secondary spans,
  notes, help, and conservative suggestions.
- Parser recovery coverage for independent syntax failures.
- Positional enum payloads for primitive, struct, enum, and nested values.
- Structural enum equality, deterministic payload `MAKE_ENUM`, safe VM payload
  validation, and the first exhaustive enum `match` with local bindings and `_`.
- Strict standard-library-only `kry.toml` parsing, semantic version
  requirements, local/offline registry resolution, path dependencies,
  transitive dependency graphs, deterministic `kry.lock`, checksums, staged
  installation, and traversal/cycle/incompatibility protections.
- `kry init`, `add`, `remove`, `install`, `update`, `list`, `tree`, project
  imports, `--format human|json`, and project-aware `check`, `build`, `run`,
  and `inspect` behavior.
- Versioned bytecode v1 and manifest/lockfile specifications for the future
  self-hosting boundary.
- Regression tests expanded from 28 to 43.

### Explicit limitations

- The bootstrap compiler and runtime are still Python implementations.
- The registry is local/offline only; no remote transport is claimed.
- Imports validate declared package names but do not yet expose imported
  symbols or resolve a complete module graph.
- Match supports only enum variant patterns, positional bindings, blocks, and
  `_`; arbitrary patterns, guards, OR patterns, and struct destructuring remain
  future language work.
- `.kexe` remains a portable Kryndel VM artifact, not a native executable.

## [0.1.0] - 2026-08-25

### Added

- First Light bootstrap with lexer, recursive-descent parser, static checker,
  deterministic bytecode VM, structs, unit enums, UI tree, KEXE artifacts,
  CLI, and a 28-test standard-library-only suite.
