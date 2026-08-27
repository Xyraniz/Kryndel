# Native architecture

Kryndel is distributed as one native executable. The source pipeline is deliberately direct:

```text
UTF-8 source
    -> lexer
    -> parser and AST
    -> module resolver
    -> static type checker
    -> tree-walk evaluator and synchronized thread runtime
    -> output, diagnostics, or versioned KRYNATIVE2 bundle
```

`check`, `run`, and `build` share the lexer, parser, module resolver, and checker. `check` stops before evaluation; `build` stops before evaluation and stores the exact validated root and imported sources; `run` evaluates the checked program or embedded artifact graph. The formatter also parses and checks before producing output, so invalid source is never silently rewritten.

| Component | Responsibility | Runtime dependency |
| --- | --- | --- |
| Lexer | UTF-8 validation, comments, identifiers, literals, operators, and positions. | C standard library and process memory. |
| Parser | Expressions, bindings, functions, modules, data declarations, blocks, and patterns. | Native AST. |
| Type checker | Type inference, annotations, calls, operators, mutability, returns, conditions, and exhaustiveness. | Native type model and source locations. |
| Module resolver | Relative source lookup, public exports, cycle detection, and traversal rejection. | Explicit filesystem paths only. |
| Runtime | Values, scopes, calls, recursion, collections, UTF-8, options, results, control flow, channels, and workers. | C standard library, pthreads, and stdout/stderr. |
| Artifact reader/writer | Versioned KRYNATIVE2 metadata, deterministic source bundle, SHA-256 hashes, atomic writes, and strict replay validation. | Binary file I/O and POSIX sync/rename. |
| CLI | `check`, `run`, `build`, `fmt`, `repl`, `doctor`, `version`, and help. | Explicit command-line and filesystem inputs. |

## Ownership and values

All compiler and runtime allocations are tracked by a per-invocation arena. The arena is released after successful checking, runtime failure, parser failure, module failure, malformed artifact input, and every other ordinary command path. The launcher and CLI keep only file buffers that they explicitly free after the invocation.

Kryndel values are immutable after construction. Bindings carry a mutability bit, and assignments are permitted only for bindings declared with `let mut`. Arrays, bytes, and strings are not mutated in place; concatenation and `array_push` create new storage, while function calls pass value representations that are safe to share because collection elements cannot be assigned. Struct fields, enum tags, option contents, and result contents are also immutable.

Lexical scopes are represented by parent-linked environments. A declaration is local to the current scope and may shadow a parent name. Lookup and assignment walk from the innermost scope outward; an assignment to an outer immutable binding is rejected by both checker and runtime.

## Concurrency and cleanup

`Channel[T]` is a FIFO queue guarded by a pthread mutex and two condition variables. Send and receive operations use predicate-based waits, try/timed variants return typed status values, and close broadcasts to both wait sets. `Thread[T]` owns an OS thread handle. Worker functions are named zero-argument functions, execute in a channel-only worker-safe scope, and report their first diagnostic through `thread_join`; worker safety is propagated through the reachable call graph. The parent runtime requests cooperative cancellation, closes channels, and joins outstanding workers on every ordinary exit path, including an earlier runtime failure. Worker-local arenas release their allocations after execution; channel synchronization primitives are destroyed after all workers have joined.

## Numeric behavior

`Int` is a signed 64-bit value. Addition, subtraction, multiplication, unary negation, division, remainder, and `abs` use explicit boundary checks. Division and remainder by zero, `Int` minimum negation, `Int` minimum absolute value, and the `Int` minimum divided by `-1` are deterministic errors. Float literals and results must be finite; float division by positive or negative zero is rejected. Allocation byte counts are checked before multiplication or addition.

## Artifact format

`build` accepts a source file, validates it, and writes a deterministic KRYNATIVE2 bundle with a fixed magic, semantic version, compiler/target metadata, exact payload length, an ordered `<root>` plus imported source entries, and a SHA-256 hash for every source. The writer uses a temporary file, flush/sync, and atomic rename. The reader rejects incompatible metadata, truncated or oversized fields, duplicate or unsafe paths, invalid hashes, trailing bytes, and any embedded source that fails the ordinary lexer, parser, module, or checker pipeline. Building the same source twice produces identical bytes. The artifact contains no instructions produced by another compiler and no hidden host-language interpreter.
