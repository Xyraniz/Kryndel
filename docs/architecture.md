# Native architecture

Kryndel is distributed as one native executable. The source pipeline is deliberately direct:

```text
UTF-8 source
    -> lexer
    -> parser and AST
    -> module resolver
    -> static type checker
    -> tree-walk evaluator
    -> output, diagnostics, or KRYNATIVE1 artifact
```

`check`, `run`, and `build` share the lexer, parser, module resolver, and checker. `check` stops before evaluation; `build` stops before evaluation and stores the exact validated source; `run` evaluates the checked program. The formatter also parses and checks before producing output, so invalid source is never silently rewritten.

| Component | Responsibility | Runtime dependency |
| --- | --- | --- |
| Lexer | UTF-8 validation, comments, identifiers, literals, operators, and positions. | C standard library and process memory. |
| Parser | Expressions, bindings, functions, modules, data declarations, blocks, and patterns. | Native AST. |
| Type checker | Type inference, annotations, calls, operators, mutability, returns, conditions, and exhaustiveness. | Native type model and source locations. |
| Module resolver | Relative source lookup, public exports, cycle detection, and traversal rejection. | Explicit filesystem paths only. |
| Runtime | Values, scopes, calls, recursion, collections, UTF-8, options, results, and control flow. | C standard library and stdout/stderr. |
| Artifact reader/writer | Deterministic header, little-endian length, exact payload, and replay validation. | Binary file I/O. |
| CLI | `check`, `run`, `build`, `fmt`, `repl`, `doctor`, `version`, and help. | Explicit command-line and filesystem inputs. |

## Ownership and values

All compiler and runtime allocations are tracked by a per-invocation arena. The arena is released after successful checking, runtime failure, parser failure, module failure, malformed artifact input, and every other ordinary command path. The launcher and CLI keep only file buffers that they explicitly free after the invocation.

Kryndel values are immutable after construction. Bindings carry a mutability bit, and assignments are permitted only for bindings declared with `let mut`. Arrays, bytes, and strings are not mutated in place; concatenation and `array_push` create new storage, while function calls pass value representations that are safe to share because collection elements cannot be assigned. Struct fields, enum tags, option contents, and result contents are also immutable.

Lexical scopes are represented by parent-linked environments. A declaration is local to the current scope and may shadow a parent name. Lookup and assignment walk from the innermost scope outward; an assignment to an outer immutable binding is rejected by both checker and runtime.

## Numeric behavior

`Int` is a signed 64-bit value. Addition, subtraction, multiplication, unary negation, division, remainder, and `abs` use explicit boundary checks. Division and remainder by zero, `Int` minimum negation, `Int` minimum absolute value, and the `Int` minimum divided by `-1` are deterministic errors. Float literals and results must be finite; float division by positive or negative zero is rejected. Allocation byte counts are checked before multiplication or addition.

## Artifact format

`build` accepts a source file, validates it, and writes the following deterministic container:

| Field | Size | Content |
| --- | ---: | --- |
| Magic | 11 bytes | ASCII `KRYNATIVE1\n`. |
| Length | 8 bytes | Unsigned little-endian payload length. |
| Payload | Variable | Exact Kryndel source bytes, unchanged. |

The reader rejects a truncated header, an impossible length, trailing bytes, and any payload that fails the ordinary lexer, parser, module, or checker pipeline. Building the same source twice produces identical bytes. The artifact contains no instructions produced by another compiler and no hidden host-language interpreter.
