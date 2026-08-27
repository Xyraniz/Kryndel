# Type system

Kryndel checks every program before evaluation. A declaration with an initializer infers its type when no annotation is present; an annotation constrains the initializer and must name a supported type. Unknown names, malformed generic arity, incompatible assignments, invalid operators, wrong function arguments, and return mismatches are errors.

| Type family | Static policy |
| --- | --- |
| `Int` | Signed 64-bit integer. Arithmetic is checked and never wraps. |
| `Float` | Finite floating-point value. It does not implicitly combine with `Int`. |
| `Bool` | The only type accepted by conditions and boolean operators. |
| `String` | Valid UTF-8 text with concatenation and code-point indexing. |
| `Bytes` | Byte sequence with explicit, validated UTF-8 decoding. |
| `Array[T]` | Homogeneous collection. Empty literals require an annotation. |
| `Nil` | Explicit nil value, not a general error or truthiness escape hatch. |
| `Option[T]` | `some(value)` or `none()`, with the expected type resolving `none`. |
| `Result[T, E]` | `ok(value)` or `err(value)`, with the annotation resolving the counterpart type. |
| `Channel[T]` | Synchronized FIFO channel carrying `T`, unbounded by default or bounded by explicit capacity. |
| `Thread[T]` | OS-backed worker handle with result type `T`. |
| Struct and enum | Nominal declarations with checked fields or finite variants. |

Numeric conversion is explicit. `float(3)` produces a `Float`, and `int(3.5)` truncates toward zero only when the result is representable. String conversions require a complete decimal input; `int("12xyz")` is rejected. There are no implicit `Int`/`Float` conversions and no implicit condition conversions. `thread_send` requires a recursively Copy type: primitives, strings, bytes, enums, arrays, options, results, and structs whose every field is Copy-safe. Channels and thread handles do not satisfy Copy.

Bare `Array` is retained as a compatibility form for existing examples. Its initializer must still be homogeneous, and new code should use `Array[T]`. `array_push` checks the element type and returns a new collection. Collection values are not mutable through indexing or field assignment.

The checker is effect-free. It never evaluates expressions, calls builtins, writes user output, starts threads, or mutates files. `run` and `build` invoke it before runtime evaluation or artifact creation. Thread worker names are resolved and must refer to zero-argument functions; worker-safe restrictions propagate through every reachable helper and expose only explicit parameters and global channel capabilities.
