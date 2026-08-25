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
| Option/Result | `Option.None`, `Option.Some(Int)`, `Result.Ok(Int)`, `Result.Error(String)` | enum compiler/VM paths | existing enum diagnostics; future `KRY6204` | `stdlib/core/*.kry` | 100% | Kryndel enum/value runtime |
| Conversion | `str(Any)`, `int(Any)`, `float(Any)` | `VM.builtin` conversion branches | CLI-normalized host errors | string/conversion tests | 100% | Kryndel conversion functions |
| Output | `print(Any)`, `println(Any)` | `VM.emit_output` | IO mapping not yet frozen in bootstrap | CLI/example runs | 100% | minimal output host primitive |
| Clock | `clock() -> Float` | `time.monotonic()` | not yet stable in bootstrap | `types.py`, `vm.py` | 100% | monotonic-clock host primitive with test seam |
| UI text tree | `ui.*` signatures | `UINode` host object/render | `KRY6000` runtime wrapper | `examples/ui_tree.kry` | 100% | explicit portable tree value or host capability |
| Bytecode read/verify | `kry verify-bytecode` | Python JSON parser/verifier | `KRY6002`, `KRY6305` | `tooling.py` | 100% | Kryndel reader/verifier |
| Compiler/front end | source -> v1 module | Python lexer/parser/checker/compiler | KRY1xxx–KRY3xxx | full suite | 100% | staged Kryndel toolchain |

`Bytes` is intentionally absent from executable rows: it is frozen in
`docs/specs/value-runtime-v1.md` but has no current builtin or semantic host
representation. Adding it requires a nominal runtime value, byte fixtures,
invalid UTF-8 fixtures, and differential tests before marking it implemented.
