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
| 5. Kryndel-native SHA-256 or verifier | Structural verifier, KEXE framing, and standalone SHA-256 source seams; integration pending | `stdlib/core/bytecode.kry` validates normalized v1 records, `stdlib/core/artifact.kry` validates KEXE magic/length, and `stdlib/core/sha256.kry` matches known vectors; Python still parses JSON/KEXE and owns production artifact/package checksums |
| 6. Controlled IO/filesystem | Bootstrap API boundary implemented; native implementation pending | `fs.*` signatures, `FileMetadata`, source wrappers, explicit VM capability, VFS/rooted adapters, fixture, and security tests exist; the implementation path remains Python |
| 7. Kryndel-native lexer | Source compatibility seam plus tagged typed-token payloads; native ownership pending | `stdlib/core/lexer.kry` lexes current fixture syntax with nested comments, recovery, spans, and `KRY1001`/`KRY1002`/`KRY1004`/`KRY1005`/`KRY1006`; `typed-token-v1.json` freezes int/float/bool/string/nil/EOF categories, while Python still owns the differential oracle and production execution |
| 8. Kryndel-native parser/AST | Source compatibility seam implemented for a tested subset; full AST/native ownership pending | `stdlib/core/parser.kry` consumes source lexer tokens for structs, lets, literals, members, calls, and struct literals; root kinds/spans compare with `parser-v1.json`, while full precedence/declarations/recovery remain Python |
| 9. Kryndel-native checker/module/compiler | Source checker and resolver seam expanded with typed literal assignments; compiler pending | `stdlib/core/checker.kry` consumes tagged AST literals for Int/Float/Bool/String/Void assignment checks and resolves normalized module records with `KRY5014`/`KRY5015`/`KRY5016`; full type system, imports, visibility, and compiler remain Python |
| 10. Kryndel-native runtime | Source runtime seam expanded with typed constants; complete native ownership pending | `stdlib/core/runtime.kry` decodes tagged Int/Float/Bool/String constants, `PUSH_NIL`, and reports `KRY7006` for unsupported categories in addition to stack/locals, structs, and calls; full opcode set, KEXE input, host IO, and independent executable remain Python |
| 11. Formatter, test runner, docs, pack and CLI | Source formatter plus narrow no-Python formatter CLI verified; remaining tools remain bootstrap-owned | `stdlib/core/format.kry` and `tools/kry-format` implement the conservative whitespace/final-newline contract; source test runner, docs, pack, package resolver, and production CLI replacements remain |
| 12. Self-contained bundle | Seed-only offline boundary verified; bundle not implemented | `tools/kry-seed-check` isolates PATH/HOME, exercises a spaced output path, checks deterministic ELF bytes and executes the raw x86_64 Linux exit-0 seed; no complete object/linker/runtime bundle exists |
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
| Source KEXE framing reader | `stdlib/core/artifact.kry`, `kexe-reader-v1.json`, framing regression test | Kryndel source executed by Python VM; framing is source-owned for the tested bytes, while SHA-256 integration, JSON/module decode, and production artifact CLI remain Python |
| Source SHA-256 seam | `stdlib/core/sha256.kry`, `sha256-v1.json`, known-vector and mismatch regression | Kryndel source computes/verifies SHA-256 through arithmetic simulation under the Python VM; KEXE/package file integration and production checksum paths remain Python |
| Source lexer seam | `stdlib/core/lexer.kry`, `lexer-v1.json`, `typed-token-v1.json`, source lexer regressions | Kryndel source executed by Python VM; tagged literal values now exist for the tested subset, while native reader and production CLI path remain Python |
| Source parser seam | `stdlib/core/parser.kry`, `parser-v1.json`, source parser regression test | Kryndel source executed by Python VM; full AST, precedence, diagnostics, and production CLI path remain Python |
| Source checker/resolver seam | `stdlib/core/checker.kry`, `checker-v1.md`, `typed-checker-v1.json`, checker/resolver regressions | Kryndel source executed by Python VM; tagged literal assignment checks are covered, while full type system, imports, visibility, and native module loading remain Python |
| Source compiler seam | `stdlib/core/compiler.kry`, `bytecode-v1.md`, `typed-bytecode-v1.json`, compiler/verifier regression tests | Kryndel source lowers the tested AST subset and records literal categories in `PUSH_CONST.text`; full compiler, linking, and serialization remain Python |
| Source runtime seam | `stdlib/core/runtime.kry`, `value-runtime-v1.md`, `typed-bytecode-v1.json`, compiler-to-runtime regression tests | Kryndel source executes emitted subset through the Python VM and decodes typed constants; complete opcode coverage, KEXE input, host IO, and native runtime remain pending |
| Direct backend seed seam | `stdlib/core/backend.kry`, `backend-v1.md`, backend determinism regression test | Kryndel source emits x86_64 Linux exit-0 assembly for an empty main; complete native backend, object format, linking, and runtime integration remain pending |
| Source formatter seam | `stdlib/core/format.kry`, `formatter-v1.md`, formatter parity regression test | Kryndel source formats text through the Python VM; `tools/kry-format` is a separate conservative no-Python CLI; full toolchain integration remains pending |
| Seed-only offline verification | `tools/kry-seed-check`, `seed-offline-v1.md`, isolated PATH/HOME regression test | Raw empty-main ELF generation and execution are independent of Python; compiler, runtime, KEXE, packages, checksums, and full CLI remain Python-owned |

The source-level filesystem boundary is now implemented as a verified bootstrap API, a first strict manifest reader exists in Kryndel source over that boundary, `stdlib/core/data.kry` provides bounded readers, a balanced string builder, and nominal toolchain records, the source manifest module emits canonical lockfile JSON for validated entries, `stdlib/core/bytecode.kry` verifies normalized bytecode records, `stdlib/core/lexer.kry` reproduces the published lexer snapshot's kinds, text, order, spans, recovery cases, and tagged typed-literal payloads, `stdlib/core/parser.kry` consumes those tokens for a tested AST subset, `stdlib/core/checker.kry` validates typed literal assignments in that subset plus dependency traversal, `stdlib/core/compiler.kry` lowers that subset to bytecode records accepted by the source verifier, and `stdlib/core/runtime.kry` executes the emitted subset end to end. `tools/kry-format` and `tools/kry-seed-check` provide narrow no-Python host utilities, but the source modules remain compatibility seams under the Python VM, not native implementations. The next technical gate is typed AST literal propagation plus full AST/precedence/checking parity, followed by full-language bytecode reproduction, complete opcode/runtime coverage, a native byte-oriented reader, and SHA-256 or full KEXE verification before replacing the remaining package tooling.
The bootstrap manifest parser remains the reference implementation and the
project must not claim independence from Python. The seed-only checker verifies
only the raw empty-main artifact and must not be described as a product bundle.
Commits after the initial
published checkpoints are intentionally local until the user authorizes a later
batch push.
