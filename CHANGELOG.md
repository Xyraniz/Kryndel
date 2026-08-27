# Changelog

## [Unreleased]

### Added

- Added the independent native core in `native/kry.c`, a C11 lexer, parser, tree-walk runtime, and CLI for the productive language subset. `make native` builds `build/kry`; `tools/kry-native` builds on demand. The native path executes functions, recursion, control flow, arrays, UTF-8 strings, Bytes, assertions, and native source artifacts without Python or Rust. Unsupported structs, enums, imports, and full static-checker features fail explicitly instead of delegating to the bootstrap. See `docs/native.md` and `tests/native-core.sh`.

- Added eight bounded bootstrap verification checkpoints: strict JSON decoding that rejects duplicate keys and non-finite numbers; exact instruction/function/module record validation; portable scalar constant checks; structural and callable validation with `KRY7001`–`KRY7007`; branch-aware operand-stack preflight with bounded states and stack depth under `KRY7008`; KEXE regular-file, symlink, size, checksum, and schema guards; CLI verification before VM execution with JSON diagnostics; and the `verification-boundary-v1.json` fixture plus documentation. These changes harden the Python bootstrap path and do not claim a native runtime, self-hosting, or a final bundle.

- Added `kry autonomy-audit` and `docs/specs/autonomy-audit-v1.md`. The deterministic report exposes the normal Python bootstrap route, exact repository invocations, pending replacements, and a four-state matrix distinguishing `Kryndel-native`, `host capability nativa mínima`, `bootstrap Python`, and `no implementado`. This is an audit boundary, not a native or self-hosting claim.
- Extended `stdlib/core/bytecode.kry` with explicit per-opcode operand-stack requirements/deltas and a bounded branch-aware `verify_execution` entry point. The source seam rejects reachable stack underflow, declared-call arity mismatches, and reachable fallthrough without `RETURN` while preserving structural schema fixtures; it remains executed by the Python bootstrap and does not replace the production verifier.
- Added `tools/kry-kexe-check`, a no-Python host-capability checker for KEXE v1 framing, payload length, SHA-256, missing files, and symlink rejection. It is a bounded artifact checkpoint; it does not decode bytecode, execute modules, or form part of the final bundle.
- Added `tools/kry-bundle-check` and `bundle-audit-v1.json`, a no-Python pre-release policy gate that rejects forbidden runtimes/toolchains, caches, generated artifacts, and symlinks in candidate bundle directories. It validates packaging policy but does not create a bundle.
- Extended `stdlib/core/runtime.kry` with source-level `bytes`, immutable `array_push`, `assert`, and `assert_eq` builtins, plus `runtime-builtins-v1.json` positive/negative coverage. Strict UTF-8 conversion, clock, and filesystem remain explicit bootstrap boundaries; `abs` and bounded `sqrt` are source-runtime operations, and the source runtime is not yet native.
- Extended `stdlib/core/backend.kry` with `emit_elf_program`, a bounded direct x86_64 ELF generator for integer addition. Its output executes from a spaced path without assembler or linker and rejects unsupported shapes/overflow; general compiler-to-backend lowering remains pending.

- Added the source-level controlled filesystem API: `fs.read_bytes`, `fs.read_text`, `fs.write_bytes`, `fs.list_dir`, and `fs.stat`, with nominal `FileMetadata`, explicit VM capability roots, deterministic VFS/rooted adapters, `filesystem-v1.json`, executable `stdlib/core/filesystem.kry` wrappers, and security/error tests. This remains a Python bootstrap boundary until a native runtime replaces the adapter.
- Added `stdlib/core/data.kry` with bounded Unicode String/Bytes slices, a divide-and-conquer `StringBuilder`, and nominal `SpanRecord`, `TokenRecord`, `AstRecord`, and `DiagnosticRecord` layouts. Added `data-core-v1.json` and VM regression coverage for deterministic values and `KRY6104`/`KRY6202` bounds errors. This is a source compatibility seam under the Python bootstrap, not a native runtime.
- Extended `stdlib/core/manifest.kry` with source-level version-range parity and nominal `LockEntry`/`Lockfile` values. Its canonical lockfile writer is compared byte for byte with Python `Lockfile.dumps()` and rejects malformed checksum metadata; SHA-256 calculation, resolution, staging, and installation remain bootstrap boundaries.
- Added `stdlib/core/bytecode.kry` with nominal instruction/function/module records and a structural verifier returning serializable `KRY6305` errors. Added normalized valid/invalid fixtures and regression coverage; `stdlib/core/artifact.kry` now also validates KEXE v1 framing and extracts checksum/payload bytes, while JSON/module decoding and production checksum integration remain Python-owned.
- Added `stdlib/core/sha256.kry` with arithmetic-only SHA-256 over nominal `Bytes`, known vectors for empty/single-block/multi-block inputs, and `KRY6205` digest verification. Connected it in a source-level regression to `stdlib/core/artifact.kry` checksum extraction, including payload tamper rejection; KEXE JSON/module decoding and package production paths remain Python-owned.
- Added `stdlib/core/json.kry` with recursive source parsing for nominal null, boolean, integer, float, string, array, and object values. Numeric validation and malformed subset cases return `KRY6304`; its bounded `decode_bytecode` schema path now builds verifier-compatible scalar-constant records for `PUSH_CONST`, `PUSH_NIL`, `LOAD`, `STORE`, `JUMP`, `JUMP_IF_FALSE`, `POP`, and `RETURN`, and `decode_bytecode_file`/`decode_bytecode_bytes` compose that path with controlled input. The new `kexe-source-pipeline-v1.md` regression reads a real KEXE, verifies its checksum, decodes and verifies the module, and runs it through the source runtime; full schema parity and production KEXE decoding remain Python-owned.
- Added `stdlib/core/lexer.kry` with source-level scanning of current keywords, identifiers, numbers, strings, escapes, comments, operators, delimiters, EOF, spans, and recovery diagnostics. It reproduces the published snapshot's normalized token text and now attaches a tagged `LiteralValue` for tested int/float/bool/string/nil/EOF categories; production execution remains Python-owned. The `typed-token-v1.json` fixture freezes the payload layout.
- Added `stdlib/core/parser.kry` with a source-level AST subset over lexer tokens for struct declarations, typed lets, literals, members, calls, and struct literals. Root AST kinds and spans are compared with `parser-v1.json`; literal nodes now preserve tagged `LiteralValue` payloads under `typed-ast-v1.json`, while full precedence and production ownership remain Python-owned.
- Added `stdlib/core/checker.kry` with source-level binding/type checks for the parser subset and deterministic dependency-first module resolution. It now consumes tagged literal payloads for Int/Float/Bool/String/Void assignments; `typed-checker-v1.json` freezes valid and mismatch cases, while the full type system and native loader remain Python-owned.
- Added `stdlib/core/compiler.kry` with source-level lowering of the migrated AST subset into normalized bytecode records and a regression that validates its instruction sequence through the source verifier. `typed-bytecode-v1.json` now freezes canonical textual constants, typed `PUSH_CONST` categories, and `PUSH_NIL`; full-language compilation, linking, and serialization remain Python-owned.
- Added `stdlib/core/runtime.kry` with a source-level stack/local runtime for the compiler subset, including typed Int/Float/Bool/String/Nil constant decoding, stores, struct values, fields, builtin print calls, returns, and stable runtime errors including `KRY7006` for unsupported decoder categories. The end-to-end path still executes through the Python VM and does not claim native runtime independence.
- Added `stdlib/core/backend.kry` with a deterministic x86_64 Linux seed backend for an empty `main`, emitting exit-status assembly for codes 0..255, a bounded `PUSH_CONST`/`RETURN` lowering path, one fixed conditional-jump seed with `je`/`jmp` labels, and `emit_elf_exit` for a direct 132-byte ELF64 `Bytes` image with a patched status. Unsupported targets, shapes, and constants return `KRY8001`–`KRY8005`; it remains a narrow direct-backend seam, not a complete object/linker pipeline.
- Added `tools/kry-seed`, a local no-Python raw ELF64 seed wrapper with an optional validated exit status. It emits a statically valid x86_64 Linux binary without relying on `as`, `ld`, or Python and is covered by executable integration tests. Added `tools/kry-seed-check` and `seed-offline-v1.md` to verify that seed only with isolated `PATH`/`HOME`, a spaced output directory, deterministic bytes, and direct execution; this is not a complete toolchain bundle.
- Added `tools/kry-native-run` and `tools/kry-native-run-check` with `docs/specs/native-seed-runtime-v1.md`. The fixed x86_64 native seed reads a bounded `KRYSEED1` module, validates exact framing, writes payload bytes to stdout, returns the first payload byte, and rejects malformed or missing files under an isolated environment. The launcher still uses POSIX shell utilities to materialize the image, so this is a native-runtime checkpoint rather than a final Kryndel bundle or replacement for the Python CLI.
- Added `stdlib/core/format.kry` with the conservative formatter contract: trailing spaces/tabs are removed, trailing blank lines are dropped, the final newline is canonicalized, and idempotence is tested. Added `tools/kry-format`, a no-Python check/rewrite CLI with empty-file coverage; the regular compiler and VM remain Python-bootstrap components.
- Added the first toolchain-oriented immutable collection primitive, `array_push(Array, Any) -> Array`, with the `stdlib/collections/sequences.kry` wrapper, deterministic fixture, stable `KRY6203` error, and regression tests. The operation still executes in the bootstrap VM.
- Added `stdlib/core/manifest.kry`, a real strict-subset manifest parser over `fs.read_text`, with nominal `Dependency`, `Manifest`, and `ManifestResult` values and valid/invalid execution coverage. The Python manifest parser remains the differential oracle until UTF-8 diagnostics and all version-requirement edge cases match byte for byte.
- Added the offline `kry core-report` contract audit. It canonicalizes and validates the value/runtime, Bytes, testing, and host-boundary v1 fixtures and reports stable byte lengths and SHA-256 checksums without claiming self-hosting. The contract is documented in `docs/specs/core-v1.md`.
- Added `VirtualFileSystem` and `RootedFileSystem` under the host-boundary v1 contract. Relative-path normalization, deterministic listings, byte IO, missing-path diagnostics, traversal rejection, and symlink rejection are executable and tested without touching a user home or network.
- Added deterministic nominal wire records for `Token`, AST nodes, and `Span`, including finite-value checks, source-order preservation, and the `records-v1.json` fixture used by differential tests. The bootstrap explicitly rejects arbitrary host objects at this boundary.
- Routed manifest reading and writing through the controlled filesystem boundary. `parse_manifest_text` and `read_manifest_from_filesystem` preserve manifest diagnostics, reject invalid UTF-8 with `KRY6304`, and keep physical paths stable for offline dependency resolution; `manifest-reader-v1.json` freezes the VFS input/output pair.
- Hardened bytecode v1 verification with a shared opcode set, exact parameter metadata, string and no-argument checks, nominal struct/enum metadata validation, and deterministic negative cases in `bytecode-verifier-v1.json`.
- Added `kry lex` with deterministic token/diagnostic snapshots and `--fixture` comparison, plus Unicode-aware `lexer-input.kry`/`lexer-v1.json` evidence. The command remains a bootstrap oracle and does not claim a native lexer.
- Added `kry parse` with deterministic nominal AST records, parser/lexer diagnostics, and `--fixture` comparison. `parser-input.kry` and `parser-v1.json` freeze a valid struct/let/call program without claiming a Kryndel-native parser.
- Added `kry graph` and `kry compiler-report` snapshots for module IDs, relative paths, public interfaces, and bytecode v1. `graph-v1.json` and `compiler-v1.json` demonstrate path-independent, repeatable bootstrap outputs without claiming a Kryndel-native checker/compiler.
- Updated `docs/roadmap-status.md` and `docs/host-dependency-inventory.md` to record the source-level filesystem API and retain explicit Python ownership and self-hosting limitations.

- Versioned value/runtime v1 contract for strings, bytes, sequences, core
  enums, Void/nil, frames, calls, serializable errors, and the temporary host
  boundary, with deterministic valid/invalid fixtures and a measured host
  dependency inventory. The contract now has an executable bootstrap
  `BytesValue` implementation.
- Kryndel-native immutable array and tuple literals, deterministic `MAKE_ARRAY`,
  `MAKE_TUPLE`, and `INDEX` bytecode, safe indexing, concatenation, and stable
  runtime errors KRY6101–KRY6105.
- Executable `bytes(Array)`, `string_to_bytes(String)`, and
  `bytes_to_string(Bytes)` APIs, strict canonical UTF-8 validation with
  offset/sequence diagnostics KRY6201, octet validation KRY6202, immutable
  `Bytes + Bytes`, octet length/indexing, deterministic Bytes fixtures, and
  source-level wrappers in `stdlib/core/bytes.kry`, `stdlib/string/utf8.kry`,
  and `stdlib/collections/bytes.kry`.
- Source-level non-generic `Option` and `Result` constructors, predicates, and
  total fallback accessors in `stdlib/core`, with regression coverage proving
  the APIs compile without new hidden VM builtins.
- Boolean literal tokens now carry runtime Boolean values rather than their
  source spelling, preserving the existing `Bool` static type at execution.
- Executable `stdlib/core`, `stdlib/string`, and `stdlib/collections` source
  fixtures, including the non-generic `Option` and `Result` enum contracts and
  differential bootstrap tests.

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
- Deterministic recursive module resolution for `src/lib.kry`, child `.kry`,
  `mod.kry`, and compatibility `lib.kry` modules, including missing,
  ambiguous, undeclared, and circular-import diagnostics.
- Minimal visibility syntax with private-by-default declarations and `pub fn`,
  `pub struct`, and `pub enum`; exported functions are checker-validated and
  linked under qualified module names in bytecode v1.
- Versioned bytecode v1, the linked-module ABI, and manifest/lockfile
  specifications for the future self-hosting boundary.
- `@test` discovery and the initial `kry test`, conservative `kry fmt`,
  `kry reproducible`, `inspect-bytecode`, `verify-bytecode`,
  `verify-artifact`, `abi`, `host-report`, `new`, and `clean` commands.
- Executable `assert(Bool)` and `assert_eq(Any, Any)` primitives, typed
  `stdlib/testing/testing.kry` wrappers, KRY6401/KRY6402 diagnostics, and
  deterministic `kry test --format json` results with failure continuation.
- Deterministic `kry doc` source declarations and `kry pack` `.krypkg` source
  archives with fixed metadata, offline operation, and SHA-256 checksums; source
  files are never executed while documenting or packaging.
- Regression tests expanded to 121, including nested modules, public/private
  symbols, deterministic linking, cycles, ambiguity, project-aware execution,
  source-level core APIs, runtime Boolean values, executable Bytes/UTF-8
  behavior, host-boundary inventory, and structured test failures.

### Explicit limitations

- The bootstrap compiler and runtime are still Python implementations.
- The registry is local/offline only; no remote transport is claimed.
- Imported functions are linked and checked, but aliases, reexports, imported
  nominal struct/enum types, traits, and generics are not implemented.
- Match supports only enum variant patterns, positional bindings, blocks, and
  `_`; arbitrary patterns, guards, OR patterns, and struct destructuring remain
  future language work.
- `.kexe` remains a portable Kryndel VM artifact, not a native executable.

## [0.1.0] - 2026-08-25

### Added

- First Light bootstrap with lexer, recursive-descent parser, static checker,
  deterministic bytecode VM, structs, unit enums, UI tree, KEXE artifacts,
  CLI, and a 28-test standard-library-only suite.
