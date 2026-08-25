# Host dependency inventory v1

This inventory measures implementation-path ownership, not an optimistic
estimate of future self-hosting. Percentages are the share of the currently
executed implementation owned by the Python bootstrap. At this milestone every
executable compiler/runtime path remains Python-owned; Kryndel source modules
provide API fixtures and signatures only.

| Capability | Kryndel-visible signature | Current host operation | Serializable error | Evidence | Python share | Replacement |
| --- | --- | --- | --- | --- | ---: | --- |
| String length/index | `len(String)`, `String[Int]` | `VM.builtin("len")`, `VM.index` | `KRY6102`, `KRY6103`, `KRY6104`, `KRY6105` | sequence tests | 100% | Kryndel `StringValue` + byte reader |
| Array/Tuple layout | `[values]`, `(values)` | `MAKE_ARRAY`, `MAKE_TUPLE`, `ArrayValue`, `TupleValue` | `KRY6101`–`KRY6105` | bytecode/runtime tests | 100% | Kryndel nominal sequence runtime |
| Bytes value/conversion | `bytes(Array)`, `string_to_bytes(String)`, `bytes_to_string(Bytes)`, `Bytes[Int]`, `Bytes + Bytes` | `BytesValue`, `VM.builtin`, `VM.index`, `VM.binary` | `KRY6102`, `KRY6104`, `KRY6201`, `KRY6202`, `KRY6203`, `KRY6105` | `tests/fixtures/bytes-v1.json`, Bytes runtime tests | 100% | Kryndel nominal octet value and UTF-8 decoder |
| Testing assertions | `assert(Bool)`, `assert_eq(Any, Any)` | `VM.builtin`, `tooling.run_kryndel_tests` | `KRY6401`, `KRY6402` | `tests/fixtures/stdlib-testing-v1.json`, `stdlib/testing/testing.kry` | 100% | Kryndel-native assertion values and runner |
| Option/Result | `Option.None`, `Option.Some(Int)`, `Result.Ok(Int)`, `Result.Error(String)` | enum compiler/VM paths | existing enum diagnostics; future `KRY6204` | `stdlib/core/*.kry` | 100% | Kryndel enum/value runtime |
| Conversion | `str(Any)`, `int(Any)`, `float(Any)` | `VM.builtin` conversion branches | CLI-normalized host errors | string/conversion tests | 100% | Kryndel conversion functions |
| Output | `print(Any)`, `println(Any)` | `VM.emit_output` | IO mapping not yet frozen in bootstrap | CLI/example runs | 100% | minimal output host primitive |
| Clock | `clock() -> Float` | `time.monotonic()` | `KRY6301` | `types.py`, `vm.py` | 100% | monotonic-clock host primitive with test seam |
| UI text tree | `ui.*` signatures | `UINode` host object/render | `KRY6000` runtime wrapper | `examples/ui_tree.kry` | 100% | explicit portable tree value or host capability |
| Bytecode read/verify | `kry verify-bytecode` | Python JSON parser/verifier | `KRY6002`, `KRY6305` | `tooling.py` | 100% | Kryndel reader/verifier |
| Compiler/front end | source -> v1 module | Python lexer/parser/checker/compiler | KRY1xxx–KRY3xxx | full suite, `compiler-v1.json` | 100% | staged Kryndel toolchain |
| Controlled filesystem | `read_bytes`, `write_bytes`, `list_dir`, `stat` | `RootedFileSystem` temporary adapter; `VirtualFileSystem` test seam | `KRY6301`–`KRY6304` | `host-boundary-v1.md`, filesystem tests, `manifest-reader-v1.json` | 100% host adapter | Kryndel-visible filesystem capability |

The eight verified increments add executable compatibility seams for core fixture audits, controlled filesystem access, nominal wire records, manifest reading, bytecode verification, lexer snapshots, parser/AST snapshots, and graph/compiler snapshots. These seams are evidence for differential development only; they do not retire Python ownership.

Bytes and testing assertions are executable bootstrap values. Their language-level
signatures, nominal immutable storage, deterministic fixtures, and
positive/negative runtime tests are implemented. The 100% Python share is an
implementation-path measurement, not a claim of self-hosting; the replacement
is a Kryndel-native value, assertion runtime, and runner that must reproduce the
fixtures before these rows can be retired. `kry host-report` verifies that every
VM dispatch name has a visible signature, error metadata, fixture, and
replacement entry; an inventory mismatch is a failing command.
