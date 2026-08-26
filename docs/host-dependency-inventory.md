# Host dependency inventory v1

This inventory measures implementation-path ownership, not an optimistic
estimate of future self-hosting. Percentages are the share of the currently
executed implementation owned by the Python bootstrap. At this milestone every
executable compiler/runtime path remains Python-owned; Kryndel source modules
provide API wrappers and signatures, while their host capabilities remain temporary.

| Capability | Kryndel-visible signature | Current host operation | Serializable error | Evidence | Python share | Replacement |
| --- | --- | --- | --- | --- | ---: | --- |
| String length/index | `len(String)`, `String[Int]` | `VM.builtin("len")`, `VM.index` | `KRY6102`, `KRY6103`, `KRY6104`, `KRY6105` | sequence tests | 100% | Kryndel `StringValue` + byte reader |
| Array/Tuple layout | `[values]`, `(values)` | `MAKE_ARRAY`, `MAKE_TUPLE`, `ArrayValue`, `TupleValue` | `KRY6101`–`KRY6105` | bytecode/runtime tests | 100% | Kryndel nominal sequence runtime |
| Immutable array append | `array_push(Array, Any) -> Array` | `VM.builtin`, immutable `ArrayValue` reconstruction | `KRY6203` | `collections-v1.json`, collection runtime tests | 100% | Kryndel-native immutable Array operation |
| Bytes value/conversion | `bytes(Array)`, `string_to_bytes(String)`, `bytes_to_string(Bytes)`, `Bytes[Int]`, `Bytes + Bytes` | `BytesValue`, `VM.builtin`, `VM.index`, `VM.binary` | `KRY6102`, `KRY6104`, `KRY6201`, `KRY6202`, `KRY6203`, `KRY6105` | `tests/fixtures/bytes-v1.json`, Bytes runtime tests | 100% | Kryndel nominal octet value and UTF-8 decoder |
| Testing assertions | `assert(Bool)`, `assert_eq(Any, Any)` | `VM.builtin`, `tooling.run_kryndel_tests` | `KRY6401`, `KRY6402` | `tests/fixtures/stdlib-testing-v1.json`, `stdlib/testing/testing.kry` | 100% | Kryndel-native assertion values and runner |
| Option/Result | `Option.None`, `Option.Some(Int)`, `Result.Ok(Int)`, `Result.Error(String)` | enum compiler/VM paths | existing enum diagnostics; future `KRY6204` | `stdlib/core/*.kry` | 100% | Kryndel enum/value runtime |
| Conversion | `str(Any)`, `int(Any)`, `float(Any)` | `VM.builtin` conversion branches | CLI-normalized host errors | string/conversion tests | 100% | Kryndel conversion functions |
| Output | `print(Any)`, `println(Any)` | `VM.emit_output` | IO mapping not yet frozen in bootstrap | CLI/example runs | 100% | minimal output host primitive |
| Clock | `clock() -> Float` | `time.monotonic()` | `KRY6301` | `types.py`, `vm.py` | 100% | monotonic-clock host primitive with test seam |
| UI text tree | `ui.*` signatures | `UINode` host object/render | `KRY6000` runtime wrapper | `examples/ui_tree.kry` | 100% | explicit portable tree value or host capability |
| Bytecode read/verify | `kry verify-bytecode` | Python JSON parser/verifier | `KRY6002`, `KRY6305` | `tooling.py`, `stdlib/core/bytecode.kry`, `bytecode-native-verifier-v1.json` | 100% normal path | Replace the JSON/KEXE reader and checksum with the source verifier plus a native byte reader |
| Compiler/front end | source -> v1 module | Python lexer/parser/checker/compiler | KRY1xxx–KRY3xxx | full suite, `compiler-v1.json` | 100% | staged Kryndel toolchain |
| Controlled filesystem | `fs.read_bytes(String) -> Bytes`, `fs.read_text(String) -> String`, `fs.write_bytes(String, Bytes) -> Void`, `fs.list_dir(String) -> Array<FileMetadata>`, `fs.stat(String) -> FileMetadata` | `VM.filesystem` explicit capability over `RootedFileSystem` or `VirtualFileSystem`; source wrappers in `stdlib/core/filesystem.kry` | `KRY6301`–`KRY6304` | `host-boundary-v1.md`, `filesystem-v1.json`, filesystem API tests, `manifest-reader-v1.json` | 100% host temporal | Replace the VM adapter with a Kryndel-native capability after the native runtime exists |
| Data-core source records and readers | `string_slice`, `bytes_slice`, `string_builder_*`, `span`, `token`, `ast`, `diagnostic` | `VM` executes `stdlib/core/data.kry`; primitive String/Bytes indexing and arrays still dispatch through Python | `KRY6104`, `KRY6202` | `data-core-v1.json`, data-core source regression tests | 100% runtime path | Reproduce the frozen nominal layouts and bounds behavior in the native value runtime, then retire the bootstrap execution path |

The verified increments add executable compatibility seams for core fixture audits, controlled filesystem access, a source-level filesystem API, nominal wire records, data-core readers/builders/records, manifest reading, bytecode verification, lexer snapshots, parser/AST snapshots, and graph/compiler snapshots. These seams are evidence for differential development only; they do not retire Python ownership.

Bytes and testing assertions are executable bootstrap values. Their language-level
signatures, nominal immutable storage, deterministic fixtures, and
positive/negative runtime tests are implemented. The 100% Python share is an
implementation-path measurement, not a claim of self-hosting; the replacement
is a Kryndel-native value, assertion runtime, and runner that must reproduce the
fixtures before these rows can be retired. `kry host-report` verifies that every
VM dispatch name has a visible signature, error metadata, fixture, and
replacement entry; an inventory mismatch is a failing command.
