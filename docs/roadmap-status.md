# Kryndel roadmap status

This status is an implementation audit, not a claim of self-hosting. A task is
marked complete only when the repository contains executable behavior, observable
output, tests, and documentation. The Python bootstrap remains temporary and is
not counted as Kryndel-native implementation.

| Prompt task | Status after this iteration | Evidence or blocking contract |
| --- | --- | --- |
| 1. Bytes and strict UTF-8 | Complete in the bootstrap | `BytesValue`, visible conversions, `bytes-v1.json`, runtime tests, ABI and language docs |
| 1a. Toolchain data core | Compatibility seam complete under bootstrap; native ownership pending | `stdlib/core/data.kry`, `data-core-v1.json`, bounded String/Bytes readers, balanced builder, nominal records, and VM regression tests; primitive values still execute through Python |
| 2. Reduce hidden VM stdlib | Partial | `stdlib/core`, `stdlib/string`, `stdlib/collections`, and `stdlib/testing` expose real wrappers; implementations remain VM Python |
| 3. Measure host boundary | Complete for current VM intrinsics | `kry host-report`, `host-boundary-v1.json`, dispatch/signature/metadata consistency test |
| 4. Kryndel-native kry.toml reader | Source parity expanded; native ownership pending | `stdlib/core/manifest.kry` now covers exact/caret/tilde/range classification, stable version error codes, and source-vs-Python differential tests; UTF-8 mapping and full byte-for-byte diagnostic parity remain pending |
| 4a. Kryndel-native lockfile writer | Compatibility writer complete under bootstrap; resolver/hash/install pending | Nominal `LockEntry`/`Lockfile`, canonical JSON writer, checksum-shape validation, `manifest-lockfile-v1.json`, and byte-for-byte comparison with Python `Lockfile.dumps()`; SHA-256, graph resolution, and staging remain Python |
| 5. Kryndel-native SHA-256 or verifier | Structural verifier source seam implemented; native reader/checksum pending | `stdlib/core/bytecode.kry` validates normalized v1 records and returns `KRY6305`; Python still parses JSON/KEXE and computes SHA-256 |
| 6. Controlled IO/filesystem | Bootstrap API boundary implemented; native implementation pending | `fs.*` signatures, `FileMetadata`, source wrappers, explicit VM capability, VFS/rooted adapters, fixture, and security tests exist; the implementation path remains Python |
| 7. Kryndel-native lexer | Source compatibility seam implemented; typed-token/native ownership pending | `stdlib/core/lexer.kry` lexes current fixture syntax with nested comments, recovery, spans, and `KRY1001`/`KRY1002`/`KRY1004`/`KRY1005`/`KRY1006`; Python still owns typed token payloads and production execution |
| 8. Kryndel-native parser/AST | Source compatibility seam implemented for a tested subset; full AST/native ownership pending | `stdlib/core/parser.kry` consumes source lexer tokens for structs, lets, literals, members, calls, and struct literals; root kinds/spans compare with `parser-v1.json`, while full precedence/declarations/recovery remain Python |
| 9. Kryndel-native checker/module/compiler | Source checker and resolver seam implemented for tested subset; compiler pending | `stdlib/core/checker.kry` validates tested AST bindings/types and resolves normalized module records with `KRY5014`/`KRY5015`/`KRY5016`; full type system, imports, visibility, and compiler remain Python |
| 10. Kryndel-native runtime | Source runtime seam implemented for compiler subset; complete native ownership pending | `stdlib/core/runtime.kry` executes the emitted subset end to end with stack/locals, structs, calls, and stable `KRY7001`–`KRY7005`; full opcode set, KEXE input, host IO, and independent executable remain Python |
| 11. Formatter, test runner, docs, pack and CLI | Source formatter seam added; remaining tools remain bootstrap-owned | `stdlib/core/format.kry` matches the conservative whitespace/final-newline contract; source test runner, docs, pack, package resolver, and production CLI replacements remain |
| 12. Self-contained bundle | Seed boundary only; bundle not implemented | `stdlib/core/backend.kry` emits deterministic x86_64 Linux exit-0 assembly for an empty main; no complete object/linker/runtime bundle exists |
| 13. Formal self-hosting gate | Not passed | The required compiler, runtime, package manager, formatter and two rebuilds without Python do not yet exist |

## Verified bootstrap increments after `eb51d99`

The following nine increments are implemented, tested, and documented in the checkout as bootstrap compatibility seams or staged source contracts. They do not change the native-status claims in the table above.

| Increment | Evidence | Native status |
| --- | --- | --- |
| Core-v1 fixture audit | `kry core-report`, canonical fixture hashes, `docs/specs/core-v1.md` | Python bootstrap |
| Controlled filesystem | `VirtualFileSystem`, `RootedFileSystem`, `fs.*` source wrappers, `filesystem-v1.json`, traversal/symlink and source API tests, `docs/specs/host-boundary-v1.md` | host adapter plus Kryndel-visible API; native runtime pending |
| Nominal wire records | `Token.as_dict`, `Node.as_dict`, `records-v1.json` | Python serialization boundary |
| Manifest filesystem seam | `read_manifest_from_filesystem`, `manifest-reader-v1.json` | Python parser |
| Bytecode verifier | shared opcode set and `bytecode-verifier-v1.json` | Python verifier |
| Lexer snapshot | `kry lex --fixture`, `lexer-v1.json` | Python lexer oracle |
| Parser/AST snapshot | `kry parse --fixture`, `parser-v1.json` | Python parser oracle |
| Graph/compiler snapshots | `kry graph`, `kry compiler-report`, `graph-v1.json`, `compiler-v1.json` | Python graph/compiler |
| Source manifest reader | `stdlib/core/manifest.kry`, `Manifest`, `Dependency`, `ManifestResult` | Kryndel source executed by Python VM; range/error differential parity is covered, full UTF-8/diagnostic parity pending |
| Source lockfile writer | `stdlib/core/manifest.kry`, `LockEntry`, `Lockfile`, `LockfileResult` | Kryndel source executed by Python VM; canonical JSON parity is covered, SHA-256/resolver/install remain Python |
| Data-core source seam | `stdlib/core/data.kry`, `data-core-v1.json`, data-core regression test | Kryndel source executed by Python VM; native reader/value runtime pending |
| Source bytecode verifier seam | `stdlib/core/bytecode.kry`, `bytecode-native-verifier-v1.json`, structural verifier regression test | Kryndel source executed by Python VM; JSON/KEXE reader and checksum path remain Python |
| Source lexer seam | `stdlib/core/lexer.kry`, `lexer-v1.json`, source lexer regression test | Kryndel source executed by Python VM; typed literal values, native reader, and production CLI path remain Python |
| Source parser seam | `stdlib/core/parser.kry`, `parser-v1.json`, source parser regression test | Kryndel source executed by Python VM; full AST, precedence, diagnostics, and production CLI path remain Python |
| Source checker/resolver seam | `stdlib/core/checker.kry`, `checker-v1.md`, checker/resolver regression test | Kryndel source executed by Python VM; full type system, imports, visibility, and native module loading remain Python |
| Source compiler seam | `stdlib/core/compiler.kry`, `bytecode-v1.md`, compiler/verifier regression test | Kryndel source lowers the tested AST subset to verifiable records; full compiler, typed constants, linking, and serialization remain Python |
| Source runtime seam | `stdlib/core/runtime.kry`, `value-runtime-v1.md`, compiler-to-runtime regression test | Kryndel source executes emitted subset through the Python VM; complete opcode coverage, KEXE input, host IO, and native runtime remain pending |
| Direct backend seed seam | `stdlib/core/backend.kry`, `backend-v1.md`, backend determinism regression test | Kryndel source emits x86_64 Linux exit-0 assembly for an empty main; complete native backend, object format, linking, and runtime integration remain pending |
| Source formatter seam | `stdlib/core/format.kry`, `formatter-v1.md`, formatter parity regression test | Kryndel source formats text through the Python VM; native file CLI and full toolchain integration remain pending |

The source-level filesystem boundary is now implemented as a verified bootstrap API, a first strict manifest reader exists in Kryndel source over that boundary, `stdlib/core/data.kry` provides bounded readers, a balanced string builder, and nominal toolchain records, the source manifest module emits canonical lockfile JSON for validated entries, `stdlib/core/bytecode.kry` verifies normalized bytecode records, `stdlib/core/lexer.kry` reproduces the published lexer snapshot's kinds, text, order, spans, and recovery cases, `stdlib/core/parser.kry` consumes those tokens for a tested AST subset, `stdlib/core/checker.kry` validates that subset plus dependency traversal, `stdlib/core/compiler.kry` lowers that subset to bytecode records accepted by the source verifier, and `stdlib/core/runtime.kry` executes the emitted subset end to end. These are executable compatibility seams under the Python VM, not native implementations. The next technical gate is typed token values plus full AST/precedence/checking parity, followed by full-language bytecode reproduction, complete opcode/runtime coverage, a native byte-oriented reader, and SHA-256 or full KEXE verification before replacing the remaining package tooling.
The bootstrap manifest parser remains the reference implementation and the
project must not claim independence from Python. Commits after the initial
published checkpoints are intentionally local until the user authorizes a later
batch push.
