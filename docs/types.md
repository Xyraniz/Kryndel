# Type system

Kryndel checks every program before evaluation. A declaration with an initializer infers its type when no annotation is present; an annotation constrains the initializer and must name a supported type. Unknown names, malformed generic arity, incompatible assignments, invalid operators, wrong function arguments, and return mismatches are errors.

| Type family | Static policy |
| --- | --- |
| `Int` | Signed 64-bit integer. Arithmetic is checked and never wraps. |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | Fixed-width unsigned integer. Arithmetic wraps modulo `2^N`; bitwise operands must have the same width. |
| `Float` | Finite floating-point value. It does not implicitly combine with `Int`. |
| `Bool` | The only type accepted by conditions and boolean operators. |
| `String` | Valid UTF-8 text with concatenation and code-point indexing. |
| `Bytes` | Byte sequence with explicit, validated UTF-8 decoding. |
| `Array[T]` | Homogeneous collection. Empty literals require an annotation. |
| `Nil` | Explicit nil value, not a general error or truthiness escape hatch. |
| `Option[T]` | `some(value)` or `none()`, with the expected type resolving `none`. |
| `Result[T, E]` | `ok(value)` or `err(value)`, with the annotation resolving the counterpart type. |
| `Channel[T]` | Synchronized FIFO channel carrying `T`, with capacity 64 by default or a configured positive capacity. |
| `Thread[T]` | OS-backed worker handle with result type `T`. |
| `fn(T1, T2) -> R` | Typed function value. Lambdas close over lexical bindings; function values are not `Copy`, const-safe, or comparable. |
| `Box[T]` | User-defined nominal struct instance with checked, substituted field types and compile-time checked type arguments. |
| Struct and enum | Nominal declarations with checked fields or finite variants. Generic enums are not supported. |

Numeric conversion is explicit. `float(3)` produces a `Float`, `int(3.5)` truncates toward zero only when the result is representable, and `u8/u16/u32/u64` perform checked conversions from `Int` or another `UInt`. String conversions require a complete decimal input; `int("12xyz")` is rejected. There are no implicit `Int`/`Float` or unsigned-width conversions and no implicit condition conversions. `thread_send` requires a recursively Copy type: primitives, strings, bytes, enums, arrays, options, results, and structs whose every field is Copy-safe. Channels and thread handles do not satisfy Copy.

Bare `Array` is retained as a compatibility form for existing examples. Its initializer must still be homogeneous, and new code should use `Array[T]`. `array_push` checks the element type and returns a new collection. Collection values are not mutable through indexing or field assignment.

Function types can appear in parameters, returns, local bindings, and supported
generic substitutions. Lambda parameters require explicit types and cannot have
default values. A closure keeps referenced bindings alive after their declaring
scope exits; mutable captures share the original binding. Function values do
not satisfy `Copy`, so channel and actor sends reject them, including when nested
inside a composite value.

Generic structs use the form `struct Box[T: Copy] { value: T }` and must be
instantiated with all type arguments, such as `Box[Int]`. The checker enforces
the declaration's constraints and substitutes arguments into field types.
Methods in `impl Box[T]` share the receiver's substitutions and may declare
additional method type parameters. Generic structs are serialized with their
parameter and field metadata in KIR v5; generic enums and generic associated
constants remain unsupported.

Non-generic traits provide static method contracts. `impl Render for North`
must define each method declared by `trait Render` exactly once with the same
explicit parameter and return types. A generic function can declare one user
trait bound, such as `fn render[T: Render](value: T) -> String`; the checker
requires a concrete implementation and records the resolved method target.
Concrete instances of generic structs, such as `Box[Int]`, may implement a
trait. The interpreter dispatches to the checked implementation and C AOT
monomorphizes generic functions and emits a direct call for that type.

This does not add trait values or dynamic dispatch. Generic traits or methods,
default methods, associated types or constants, generic or blanket impls,
multiple bounds, and implementations for non-struct targets are unsupported.
The frontend reports these forms with diagnostics. KIR v5 stores trait
declarations, implementation targets, symbolic bound-method calls, and
implementation function references; KRYNATIVE6 embeds this validated KIR next
to its source bundle.

The checker is effect-free. It never evaluates expressions, calls builtins, writes user output, starts threads, or mutates files. `run` and `build` invoke it before runtime evaluation or artifact creation. Thread worker names are resolved and must refer to zero-argument functions; worker-safe restrictions propagate through every reachable helper and expose only explicit parameters and global channel capabilities.
