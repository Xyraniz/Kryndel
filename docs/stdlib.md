# Kryndel standard library boundary

The standard library is not yet a Kryndel-native implementation. The current
Python VM exposes a small set of host primitives and deterministic helpers so
the bootstrap can run the language while the self-hosting boundary is
stabilized.

| Area | Current bootstrap behavior | Future Kryndel-owned direction |
| --- | --- | --- |
| Core values | Primitive values, nominal structs, and enums are represented by the VM | Define native `String`, arrays, tuples, `Option`, and `Result` layouts |
| Conversion | `str`, `int`, and `float` are VM builtins | Move signatures and semantics into `stdlib/core` and `stdlib/string` |
| Collections | `len` currently accepts the existing string runtime value | Add arrays, safe indexing, iteration, length, concatenation, and defined comparison |
| Math | `abs` and `sqrt` are VM builtins | Provide checked Kryndel-visible math APIs |
| IO and UI | `print`, `println`, and textual UI helpers cross the host boundary | Keep only controlled IO primitives in the VM and express policies in Kryndel |
| Time/process/filesystem | `clock` and future host bridges remain outside the language | Freeze serializable errors and portable APIs before moving implementations |

The intended directory layout is `stdlib/core`, `stdlib/io`,
`stdlib/string`, `stdlib/collections`, `stdlib/math`, `stdlib/process`,
`stdlib/filesystem`, and `stdlib/testing`. These directories are a planned
source-level boundary, not placeholders that pretend the APIs already exist.
The next safe additions are explicit conversion and string contracts, followed
by arrays and recoverable `Option`/`Result` values with tests and stable
representations.

Kryndel is not independent of Python yet. A future native runtime must replace
the bootstrap implementation while preserving the documented bytecode, module,
ABI, and runtime-error contracts.
