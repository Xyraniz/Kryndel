# Kryndel standard library boundary

The standard library is being moved into Kryndel source. The bootstrap VM
still supplies the host boundary, but the first core, string, and collection
contracts below are compiled and executed as ordinary Kryndel modules.

| Area | Current bootstrap behavior | Future Kryndel-owned direction |
| --- | --- | --- |
| Core values | Primitive values, nominal structs, enums, `ArrayValue`, and `TupleValue` are represented by the VM | `stdlib/core/option.kry` and `stdlib/core/result.kry` define the current non-generic Option/Result contract, including source-level constructors, predicates, and total fallback accessors |
| Conversion | `str`, `int`, and `float` are VM builtins | Move signatures and semantics into `stdlib/core` and `stdlib/string` |
| Collections | `len`, `MAKE_ARRAY`, `MAKE_TUPLE`, and `INDEX` are checked VM boundaries | `stdlib/collections/sequences.kry` exposes deterministic length helpers |
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

The fallback accessors are total and therefore do not yet define a value-absent
runtime error. A partial `unwrap` API will be added only after a serializable
program-error contract is specified. The remaining directories will be added
only with real APIs and host-boundary tests, rather than empty declarations.

Kryndel is not independent of Python yet. A future native runtime must replace
the bootstrap implementation while preserving the documented bytecode, module,
ABI, and runtime-error contracts.
