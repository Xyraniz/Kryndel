# Kryndel standard library boundary

The standard library is being moved into Kryndel source. The bootstrap VM
still supplies the host boundary, but the first core, string, and collection
contracts below are compiled and executed as ordinary Kryndel modules.

| Area | Current bootstrap behavior | Future Kryndel-owned direction |
| --- | --- | --- |
| Core values | Primitive values, nominal structs, enums, `ArrayValue`, `TupleValue`, and `BytesValue` are represented by the VM | `stdlib/core/option.kry` and `stdlib/core/result.kry` define the current non-generic Option/Result contract, including source-level constructors, predicates, and total fallback accessors |
| Conversion | `str`, `int`, `float`, `bytes`, `string_to_bytes`, and `bytes_to_string` are VM boundaries with visible signatures | Move signatures and semantics into `stdlib/core` and `stdlib/string`; UTF-8 wrappers now live in `stdlib/string/utf8.kry` |
| Collections | `len`, `MAKE_ARRAY`, `MAKE_TUPLE`, `INDEX`, `array_push`, and `Bytes + Bytes` are checked VM boundaries | `stdlib/collections/sequences.kry` exposes immutable `append`; `stdlib/collections/bytes.kry` exposes deterministic Bytes helpers |
| Testing | `assert(Bool)` and `assert_eq(Any, Any)` are visible VM testing primitives | `stdlib/testing/testing.kry` provides typed wrappers; `kry test --format json` emits structured results |
| Math | `abs` and `sqrt` are VM builtins | Provide checked Kryndel-visible math APIs |
| IO and UI | `print`, `println`, and textual UI helpers cross the host boundary | Keep only controlled IO primitives in the VM and express policies in Kryndel |
| Time/process/filesystem | `clock` and controlled `fs.*` bridges cross the temporary host boundary | `stdlib/core/filesystem.kry` exposes `read_bytes`, `write_bytes`, `list_dir`, and `stat`; replace the VM adapter after the native runtime can provide the same capability |

The source tree currently contains `stdlib/core`, `stdlib/string`,
`stdlib/collections`, and `stdlib/testing`; each existing file has executable
signatures and tests. `stdlib/core/data.kry` adds the first toolchain-oriented
bounded readers, a non-quadratic divide-and-conquer string builder, and nominal
record constructors as ordinary Kryndel source.
The core modules now expose these non-generic APIs directly in Kryndel:

| Module | Source-level operations | Boundary status |
| --- | --- | --- |
| `core/option` | `none`, `some`, `is_some`, `is_none`, `unwrap_or`, `get_or` | No new VM builtin; matching and assignment use existing language and bytecode operations |
| `core/bytes` | `new`, `from_string`, `to_string`, `length`, `get`, `concat` | Source wrappers over the visible Bytes boundary |
| `core/result` | `ok`, `error`, `is_ok`, `is_error`, `unwrap_or`, `get_or` | No new VM builtin; matching and assignment use existing language and bytecode operations |
| `string/utf8` | `encode`, `decode`, `length`, `octet_length`, `append`, `octet` | Source wrappers over the visible UTF-8/Bytes boundary; strict decode reports `KRY6201` |
| `collections/bytes` | `from_array`, `length`, `octet`, `join` | Source wrappers over `bytes(Array)`, `len`, `INDEX`, and `Bytes + Bytes` |
| `testing` | `assert_true`, `assert_equal_int`, `assert_equal_string` | Source wrappers over visible assertion primitives; failures use `KRY6401` and `KRY6402` |
| `core/filesystem` | `read_bytes`, `read_text`, `write_bytes`, `list_dir`, `stat` | Source wrappers over explicit `fs.*` capability; metadata uses `FileMetadata` and errors use `KRY6301`–`KRY6304` |
| `core/manifest` | `parse(String) -> ManifestResult`, `read(String) -> ManifestResult`, `lock_entry`, `lockfile`, `lockfile_json` | Source parser now covers version-range classification and canonical lockfile JSON for validated entries; SHA-256 calculation, resolver, staging, and installation remain Python-owned |
| `core/data` | bounded `StringSlice`/`BytesSlice`, balanced `StringBuilder`, `SpanRecord`, `TokenRecord`, `AstRecord`, and `DiagnosticRecord` constructors/accessors | Real source behavior executed by the bootstrap VM; data layouts and bounds errors are frozen by `data-core-v1.json`, but implementation ownership remains Python |
| `core/bytecode` | nominal `InstructionRecord`, `FunctionRecord`, `ModuleRecord`, and `verify` | Source-level structural verifier for normalized v1 records; JSON reading, checksum calculation, and production artifact verification remain Python |
| `core/lexer` | `lex(String) -> LexerResult`, `read(String) -> LexerResult` | Source-level lexer for current keywords, literals, comments, operators, delimiters, spans, recovery, and tagged `LiteralValue` payloads; runtime execution and differential ownership remain bootstrap-owned |
| `core/parser` | `parse(Array) -> ParseResult` over nominal lexer tokens | Source-level parser subset for structs, lets, literals, members, calls, and struct literals; literal nodes preserve tagged `LiteralValue` payloads, while full language AST and typed parser execution remain bootstrap-owned |
| `core/checker` | `check(Array) -> CheckResult`, `module`, `resolve` | Source-level semantic and dependency checks for the parser subset, including tagged Int/Float/Bool/String/Void literal assignments; complete type identity, imports, visibility, and native module loading remain bootstrap-owned |
| `core/compiler` | `compile(String, Array) -> CompileResult` | Source-level lowering for the migrated AST subset into verifiable bytecode records with tagged `PUSH_CONST` categories and `PUSH_NIL`; full compiler, linking, and serialization remain bootstrap-owned |
| `core/runtime` | `run(ModuleRecord) -> RuntimeResult`, `decode_constant(String, String) -> RuntimeResult` | Source-level execution of the compiler subset with stack/locals, struct values, and typed Int/Float/Bool/String/Nil decoding; complete opcode coverage, KEXE input, host IO, and native runtime ownership remain bootstrap-owned |
| `core/artifact` | `read_header(Bytes) -> ReadResult` | Source-level KEXE v1 reader validates magic/length, exposes payload bytes, and canonicalizes checksum text for `core/sha256`; JSON/module decoding and production artifact verification remain Python |
| `core/sha256` | `digest(Bytes) -> DigestResult`, `verify(Bytes, String) -> DigestResult` | Source-level SHA-256 known-vector implementation with `KRY6205` comparison errors; file/package/KEXE production checksum paths remain Python |
| `core/json` | `parse(String) -> JsonResult`, `decode_bytecode(String) -> BytecodeResult`, `decode_bytecode_file(String) -> BytecodeResult`, `decode_bytecode_bytes(Bytes) -> BytecodeResult` | Source-level JSON value parser plus bounded v1 bytecode schema decoder using controlled files or extracted KEXE payload bytes; full schema validation, production KEXE loading, and CLI ownership remain Python |
| `core/backend` | `seed_module()`, `emit(ModuleRecord, String) -> BackendResult`, `emit_exit(ModuleRecord, String, Int) -> BackendResult`, `emit_program(ModuleRecord, String) -> BackendResult` | Deterministic x86_64 Linux exit-status assembly for an empty main, one scalar `PUSH_CONST`/`RETURN` program, and one fixed conditional-jump seed with status 0..255; general native backend, object format, linking, and compiler/runtime integration remain pending |
| `core/format` | `format(String) -> FormatResult` | Conservative source formatting with trailing-whitespace removal and final-newline canonicalization; `tools/kry-format` is a separate no-Python file utility, while the regular CLI remains bootstrap-owned |

The source manifest module's lockfile writer accepts checksums as validated
hexadecimal inputs; it does not pretend to calculate SHA-256. The fallback
accessors are total and therefore do not yet define a value-absent
runtime error. A partial `unwrap` API will be added only after a serializable
program-error contract is specified. The remaining directories will be added
only with real APIs and host-boundary tests, rather than empty declarations. The
Bytes wrappers are tested by `tests/test_kryndel.py` and the deterministic
`tests/fixtures/bytes-v1.json` contract. The filesystem wrapper is covered by `tests/fixtures/filesystem-v1.json` and by execution tests over both the in-memory and rooted adapters. Immutable collection append is covered by `tests/fixtures/collections-v1.json` and its runtime tests.

Kryndel is not independent of Python yet. A future native runtime must replace the bootstrap implementation while preserving the documented bytecode, module,
ABI, data-core, and runtime-error contracts.
