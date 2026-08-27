# Standard library and builtins

Kryndel keeps its standard library small and explicit. The current release provides a native builtin registry shared by the parser-facing checker, runtime, and documentation. Builtins are ordinary calls and are checked before execution.

| Builtin | Signature | Behavior |
| --- | --- | --- |
| `print` | `(value)` | Write one deterministic value without a newline. |
| `println` | `(value)` | Write one deterministic value and a newline. |
| `len` | `(String \| Array[T] \| Bytes) -> Int` | Count UTF-8 code points, elements, or octets. |
| `bytes` | `(Array[Int]) -> Bytes` | Convert integers in `0..255` to bytes. |
| `string_to_bytes` | `(String) -> Bytes` | Preserve valid UTF-8 bytes. |
| `bytes_to_string` | `(Bytes) -> String` | Decode only valid UTF-8. |
| `array_push` | `(Array[T], T) -> Array[T]` | Return a new array with the value appended. |
| `int` | `(Int \| Float \| Bool \| String) -> Int` | Explicit conversion with range and complete-parse checks. |
| `float` | `(Int \| Float \| String) -> Float` | Explicit finite conversion. |
| `str` | `(value) -> String` | Produce deterministic display text. |
| `bool` | `(value) -> Bool` | Explicit convenience conversion; conditions still require Bool. |
| `assert` | `(Bool) -> Nil` | Fail when the condition is false. |
| `assert_eq` | `(T, T) -> Nil` | Fail when values are not equal. |
| `abs` | `(Int \| Float) -> same numeric type` | Checked absolute value. |
| `sqrt` | `(Int \| Float) -> Float` | Finite square root of a non-negative value. |
| `some` / `none` | `T -> Option[T]` / `() -> Option[T]` | Construct explicit option values. |
| `ok` / `err` | `T -> Result[T, E]` / `E -> Result[T, E]` | Construct explicit result values. |

String-to-number conversion rejects whitespace-dependent partial parses and inputs such as `"12xyz"`. Float values and results must be finite. Integer arithmetic and `abs(Int minimum)` are checked. `Bytes` conversion never applies an implicit text encoding to arbitrary values.

The native registry is the authoritative list. Adding a builtin requires a registry entry, checker behavior, runtime behavior, documentation, and positive and negative tests.
