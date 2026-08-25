# Kryndel standard library boundary

The standard library is being moved into Kryndel source. The bootstrap VM
still supplies the host boundary, but the first core, string, and collection
contracts below are compiled and executed as ordinary Kryndel modules.

| Area | Current bootstrap behavior | Future Kryndel-owned direction |
| --- | --- | --- |
| Core values | Primitive values, nominal structs, enums, `ArrayValue`, `TupleValue`, and `BytesValue` are represented by the VM | `stdlib/core/option.kry` and `stdlib/core/result.kry` define the current non-generic Option/Result contract, including source-level constructors, predicates, and total fallback accessors |
| Conversion | `str`, `int`, `float`, `bytes`, `string_to_bytes`, and `bytes_to_string` are VM boundaries with visible signatures | Move signatures and semantics into `stdlib/core` and `stdlib/string`; UTF-8 wrappers now live in `stdlib/string/utf8.kry` |
| Collections | `len`, `MAKE_ARRAY`, `MAKE_TUPLE`, `INDEX`, and `Bytes + Bytes` are checked VM boundaries | `stdlib/collections/sequences.kry` and `stdlib/collections/bytes.kry` expose deterministic collection helpers |
| Math | `abs` and `sqrt` are VM builtins | Provide checked Kryndel-visible math APIs |
| IO and UI | `print`, `println`, and textual UI helpers cross the host boundary | Keep only controlled IO primitives in the VM and express policies in Kryndel |
| Time/process/filesystem | `clock` and future host bridges remain outside the language | Freeze serializable errors and portable APIs before moving implementations |

The source tree currently contains `stdlib/core`, `stdlib/string`, and
`stdlib/collections`; each existing file has executable signatures and tests.
The core modules now expose these non-generic APIs directly in Kryndel:

| Module | Source-level operations | Boundary status |
| --- | --- | --- |
| `core/option` | `none`, `some`, `is_some`, `is_none`, `unwrap_or`, `get_or` | No new VM builtin; matching and assignment use existing language and bytecode operations |
| `core/result` | `ok`, `error`, `is_ok`, `is_error`, `unwrap_or`, `get_or` | No new VM builtin; matching and assignment use existing language and bytecode operations |
| `string/utf8` | `encode`, `decode`, `length`, `octet_length`, `append`, `octet` | Source wrappers over the visible UTF-8/Bytes boundary; strict decode reports `KRY6201` |
| `collections/bytes` | `from_array`, `length`, `octet`, `join` | Source wrappers over `bytes(Array)`, `len`, `INDEX`, and `Bytes + Bytes` |

The fallback accessors are total and therefore do not yet define a value-absent
runtime error. A partial `unwrap` API will be added only after a serializable
program-error contract is specified. The remaining directories will be added
only with real APIs and host-boundary tests, rather than empty declarations. The
Bytes wrappers are tested by `tests/test_kryndel.py` and the deterministic
`tests/fixtures/bytes-v1.json` contract.

Kryndel is not independent of Python yet. A future native runtime must replace
the bootstrap implementation while preserving the documented bytecode, module,
ABI, and runtime-error contracts.
