# Kryndel roadmap status

This status is an implementation audit, not a claim of self-hosting. A task is
marked complete only when the repository contains executable behavior, observable
output, tests, and documentation. The Python bootstrap remains temporary and is
not counted as Kryndel-native implementation.

| Prompt task | Status after this iteration | Evidence or blocking contract |
| --- | --- | --- |
| 1. Bytes and strict UTF-8 | Complete in the bootstrap | `BytesValue`, visible conversions, `bytes-v1.json`, runtime tests, ABI and language docs |
| 2. Reduce hidden VM stdlib | Partial | `stdlib/core`, `stdlib/string`, `stdlib/collections`, and `stdlib/testing` expose real wrappers; implementations remain VM Python |
| 3. Measure host boundary | Complete for current VM intrinsics | `kry host-report`, `host-boundary-v1.json`, dispatch/signature/metadata consistency test |
| 4. Kryndel-native kry.toml reader | Not implemented | Requires source file IO, strings, diagnostics, and a frozen Kryndel parser/runtime boundary |
| 5. Kryndel-native SHA-256 or verifier | Not implemented | Requires Bytes iteration/bitwise arithmetic or a verifier input contract in Kryndel |
| 6. Controlled IO/filesystem | Not implemented as a language API | Current package implementation is host-only; project-root, symlink, traversal, and serializable IO contracts still need a source-level boundary |
| 7. Kryndel-native lexer | Not implemented | Requires a self-contained Bytes/string reader and token value representation |
| 8. Kryndel-native parser/AST | Not implemented | Depends on task 7 and a nominal AST serialization contract |
| 9. Kryndel-native checker/module/compiler | Not implemented | Depends on parser/AST, type identity, module graph, and bytecode reproduction fixtures |
| 10. Kryndel-native runtime | Not implemented | Current VM is Python and no independent Kryndel runtime reads bytecode v1 |
| 11. Formatter, test runner, docs, pack and CLI | Partial bootstrap milestone | `kry test --format json`, `kry doc`, `kry pack`, formatter, package commands, and CLI exist in Python; source-level replacements remain |
| 12. Self-contained bundle | Not implemented | No compiler/runtime/bundle executable without Python, Rust, Node.js or external services |
| 13. Formal self-hosting gate | Not passed | The required compiler, runtime, package manager, formatter and two rebuilds without Python do not yet exist |

## Verified bootstrap increments after `eb51d99`

The following eight increments are implemented, tested, documented, committed, and pushed as bootstrap compatibility seams. They do not change the native-status claims in the table above.

| Increment | Evidence | Native status |
| --- | --- | --- |
| Core-v1 fixture audit | `kry core-report`, canonical fixture hashes, `docs/specs/core-v1.md` | Python bootstrap |
| Controlled filesystem | `VirtualFileSystem`, `RootedFileSystem`, traversal/symlink tests, `docs/specs/host-boundary-v1.md` | host adapter plus VFS seam |
| Nominal wire records | `Token.as_dict`, `Node.as_dict`, `records-v1.json` | Python serialization boundary |
| Manifest filesystem seam | `read_manifest_from_filesystem`, `manifest-reader-v1.json` | Python parser |
| Bytecode verifier | shared opcode set and `bytecode-verifier-v1.json` | Python verifier |
| Lexer snapshot | `kry lex --fixture`, `lexer-v1.json` | Python lexer oracle |
| Parser/AST snapshot | `kry parse --fixture`, `parser-v1.json` | Python parser oracle |
| Graph/compiler snapshots | `kry graph`, `kry compiler-report`, `graph-v1.json`, `compiler-v1.json` | Python graph/compiler |

The next technically bounded native task remains to implement the source-level filesystem boundary required by the manifest reader. It should reproduce relative project paths, deterministic directory order, symlink rejection, `KRY6301`–`KRY6304` errors, and the fixtures before attempting to rewrite the manifest parser in Kryndel.
Until that boundary exists, the bootstrap manifest parser must remain the
reference implementation and the project must not claim independence from
Python.
