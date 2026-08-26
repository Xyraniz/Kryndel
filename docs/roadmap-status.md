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
| 4. Kryndel-native kry.toml reader | Partial source implementation; differential integration pending | `stdlib/core/manifest.kry` parses the strict subset over `fs.read_text` and returns nominal `ManifestResult`; Python `packages.py` remains oracle and UTF-8/error parity is not yet complete |
| 5. Kryndel-native SHA-256 or verifier | Not implemented | Requires Bytes iteration/bitwise arithmetic or a verifier input contract in Kryndel |
| 6. Controlled IO/filesystem | Bootstrap API boundary implemented; native implementation pending | `fs.*` signatures, `FileMetadata`, source wrappers, explicit VM capability, VFS/rooted adapters, fixture, and security tests exist; the implementation path remains Python |
| 7. Kryndel-native lexer | Not implemented | Requires a self-contained Bytes/string reader and token value representation |
| 8. Kryndel-native parser/AST | Not implemented | Depends on task 7 and a nominal AST serialization contract |
| 9. Kryndel-native checker/module/compiler | Not implemented | Depends on parser/AST, type identity, module graph, and bytecode reproduction fixtures |
| 10. Kryndel-native runtime | Not implemented | Current VM is Python and no independent Kryndel runtime reads bytecode v1 |
| 11. Formatter, test runner, docs, pack and CLI | Partial bootstrap milestone | `kry test --format json`, `kry doc`, `kry pack`, formatter, package commands, and CLI exist in Python; source-level replacements remain |
| 12. Self-contained bundle | Not implemented | No compiler/runtime/bundle executable without Python, Rust, Node.js or external services |
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
| Source manifest reader | `stdlib/core/manifest.kry`, `Manifest`, `Dependency`, `ManifestResult` | Kryndel source executed by Python VM; differential parity pending |
| Data-core source seam | `stdlib/core/data.kry`, `data-core-v1.json`, data-core regression test | Kryndel source executed by Python VM; native reader/value runtime pending |

The source-level filesystem boundary is now implemented as a verified bootstrap API, a first strict manifest reader exists in Kryndel source over that boundary, and `stdlib/core/data.kry` now provides bounded readers, a balanced string builder, and nominal toolchain records. These are executable compatibility seams under the Python VM, not native implementations. The next technical gate is byte-for-byte differential parity with Python for valid and invalid manifests, including UTF-8 mapping and version requirements, before replacing the remaining package tooling.
The bootstrap manifest parser remains the reference implementation and the
project must not claim independence from Python.
