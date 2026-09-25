# Kryndel Design Record

This record defines the stable execution and semantic contract for the current Kryndel toolchain. It is intentionally explicit about the boundary between implemented behavior and future extensions.

## Execution

The development and reference backend is the modular Go toolchain in `cmd/kry` and `internal/kry`. `kry check`, `kry run`, `kry build`, module loading, formatting validation, the REPL, and artifact execution share the same lexer, parser, module resolver, type checker, validated IR, value representation, and runtime. A `.kexe` file is a deterministic source container, not a machine-code executable. Released binaries are built with `CGO_ENABLED=0` and require no external runtime.

## Values and ownership

`Int`, `Float`, `Bool`, `Nil`, enum tags, and immutable handles are represented as values. `String`, `Bytes`, arrays, structs, options, and results are immutable logical values whose storage is allocated within one invocation arena. The evaluator copies value descriptors when binding or passing arguments; collection payloads are immutable and are copied when an operation creates a new collection. The arena releases all allocations when a source or REPL evaluation ends. SQLite databases, TCP/UDP sockets, regular expressions, seeded random generators, and FFI libraries/symbols/buffers are explicit non-Copy handles with dedicated close operations where the host resource requires one. Child processes and native pointers never become implicit source values.

This model makes ordinary source evaluation deterministic and prevents accidental in-place mutation of collection payloads. It does not claim general ownership checking for external resources; each host handle has a bounded API and returns a typed error when the platform or resource rejects an operation.

## Mutability

`let` creates an immutable binding and `let mut` creates a mutable binding. Binding mutability is stored in both the checker environment and runtime environment. Assignment resolves the nearest visible binding and succeeds only when that binding is mutable and the assigned value has the same checked type. Child blocks may shadow names; assignment never changes a parent binding merely because a child scope exists. Collection update operations return new values rather than mutating an immutable collection in place. Struct fields are currently immutable values because field assignment is outside the stable syntax.

## Threads and shared state

The stable language provides OS-backed `Thread[T]` workers and bounded FIFO `Channel[T]` values through explicit `thread_*` builtins. Channels have a default capacity of 64; `thread_channel_with_capacity` accepts a configured positive capacity. A worker is spawned by the name of a zero-argument function, receives a private child scope containing only global channel handles, and reports its first diagnostic through `thread_join`. `thread_send` permits only recursively Copy values: primitives, strings, bytes, enums, and arrays, options, or results composed of Copy values. Structs and synchronization handles are not transferable.

Channel operations use Go synchronization primitives and predicate-based waits. Send and receive wait on state predicates, and close wakes blocked operations. The parent runtime closes channels and joins outstanding workers at shutdown, including after an earlier runtime failure. This is a deliberately small concurrency model; async scheduling, atomics, read-write locks, cancellation tokens, and arbitrary shared mutable state are outside the stable API.

## Errors and unsafe boundaries

Lexical, parse, type, artifact, runtime, I/O, CLI, and resource failures are represented by deterministic diagnostics and non-zero process status. User-visible operations that can fail are reported rather than silently coerced. Checked integer arithmetic rejects overflow, division by zero, minimum-integer negation, and invalid absolute value. Floating-point literals and computed results must remain finite. `Option[T]` and `Result[T, E]` are explicit tagged values in the current expression and pattern surface. OS resource wrappers return these tagged values instead of hiding permission, capability, or transport failures.

The stable language has no general `unsafe` block or raw-pointer dereference. FFI is an explicit, checker-visible boundary: callers load a library, resolve a symbol, declare a restricted integer/pointer ABI signature, and pass opaque buffer address tokens. Invalid signatures, closed handles, and unsupported platforms return errors; a semantically incorrect C declaration remains the caller's responsibility and is documented as an unsafe native contract.

## Modules and packaging

Imports are quoted relative paths resolved relative to the importing source file. The `.kry` suffix is optional. Absolute paths and parent traversal are rejected, canonical paths are confined to the root program directory, duplicate declarations are rejected, public declarations are exported, and import cycles are diagnosed. Module resolution is source-only and deterministic. Package manifests, dependency selection, lock files, and versioned packages are outside the stable command set until their validation and reproducible-build semantics are specified.

## System access

The stable source standard library consists of the authoritative global builtin registry documented in `docs/stdlib.md`. It provides pure value operations, output, assertions, conversions, collection/text/byte primitives, regex and seeded random handles, UUID/time/platform/dotenv helpers, bounded thread/channel APIs, UTF-8 file operations, environment lookup, process metadata, HTTP/WebSocket requests, SQLite, raw TCP/UDP, FFI, screen/camera capture, IP geolocation, and Windows input/host APIs. Every host-facing operation is typed, bounded where data can grow, and reports its capability or permission failure rather than fabricating a result.

## Compatibility

The stable syntax is `let`, `let mut`, `fn`, `if`, `else`, `while`, `return`, `break`, `continue`, `struct`, `enum`, `match`, quoted imports, `Option`, `Result`, and `Array[T]`. Top-level executable statements and explicit function declarations use the same checker and evaluator path. Bare `Array` remains accepted for compatibility and is inferred from a non-empty initializer; new code should use `Array[T]`. Any incompatible syntax change requires a design record, a migration path, positive and negative tests, and a documented user benefit.

## Performance

The current performance claim is limited to a small Go toolchain with checked execution and deterministic startup behavior. The runtime executes a checked, bounded intermediate representation; no Kryndel program-level native-code performance claim is made. Benchmarks should measure startup, integer loops, function calls, collections, source checking, and artifact replay separately.

## Stability rule

Documentation describes only behavior exercised by tests and available through the CLI. Planned language areas are labeled as future design work rather than presented as stable commands or primitives. The single Go implementation remains authoritative; a second backend would require the same semantic definitions and differential test suite.
