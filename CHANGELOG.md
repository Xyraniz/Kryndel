# Changelog

## [Unreleased]

### Added

- Added the source-level controlled filesystem API: `fs.read_bytes`, `fs.read_text`, `fs.write_bytes`, `fs.list_dir`, and `fs.stat`, with nominal `FileMetadata`, explicit VM capability roots, deterministic VFS/rooted adapters, `filesystem-v1.json`, executable `stdlib/core/filesystem.kry` wrappers, and security/error tests. This remains a Python bootstrap boundary until a native runtime replaces the adapter.
- Added `stdlib/core/data.kry` with bounded Unicode String/Bytes slices, a divide-and-conquer `StringBuilder`, and nominal `SpanRecord`, `TokenRecord`, `AstRecord`, and `DiagnosticRecord` layouts. Added `data-core-v1.json` and VM regression coverage for deterministic values and `KRY6104`/`KRY6202` bounds errors. This is a source compatibility seam under the Python bootstrap, not a native runtime.
- Extended `stdlib/core/manifest.kry` with source-level version-range parity and nominal `LockEntry`/`Lockfile` values. Its canonical lockfile writer is compared byte for byte with Python `Lockfile.dumps()` and rejects malformed checksum metadata; SHA-256 calculation, resolution, staging, and installation remain bootstrap boundaries.
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
- Regression tests expanded to 93, including nested modules, public/private
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
