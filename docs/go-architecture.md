# Go toolchain architecture

Kryndel is implemented by one coherent Go 1.22 toolchain. Go was selected over Zig because the repository needs a portable standard-library implementation of UTF-8 handling, process cancellation, filesystem operations, deterministic serialization, fuzzing, and cross-compilation without C headers, POSIX threads, or a host interpreter. Released binaries are statically linked where the target platform permits and require no Go installation or external runtime.

The implementation is split into source, diagnostics, lexer, parser/AST, types and copy analysis, modules, control-flow checking, builtin registry, validated bytecode, VM, sandbox filesystem, concurrency, artifacts, formatter, REPL, and CLI packages under `internal/kry`. Compiler and execution state is explicit per invocation; no mutable package-level program state is used.

The frontend produces a validated bytecode program. The VM executes only validated instructions and carries an `ExecContext` with cancellation, instruction, wall-clock, call-depth, stack, memory, source, and output budgets. Every host-facing operation returns a typed error. Go panics are not used for language failures and are converted at the CLI boundary only for unexpected host failures.

`Copy` analysis is memoized with `unknown`, `visiting`, `copyable`, and `non-copyable` states. Recursive structural values are conservatively non-copyable. Type equality, display, layout, and serialization use the same recursion/depth guard. Channel send APIs call one runtime and checker transferability predicate, including try and timed variants.

The filesystem policy rejects absolute paths, NUL bytes, parent traversal, symlink components, and reparse/junction-like escapes. Reads and writes resolve relative to the configured root and use `Lstat` checks for every component; writes use a temporary file created in the trusted parent and an atomic rename only after revalidation. The implementation documents the residual limitation of a portable Go user-space check on platforms without an openat-equivalent API and never presents that check as a race-free kernel capability.

Artifacts use `KRYNATIVE3`: a fixed header, explicit root entry, canonical remaining entry order, exact byte lengths, SHA-256 hashes, compiler and target identities, and strict trailing-byte rejection. An artifact is a source container that is rechecked by the same frontend before execution. Atomic replacement uses `os.CreateTemp`, sync, close, and rename in the same directory.

Workers are result-bearing `Thread[T]` values. Worker code runs in a child execution context with cancellation and budgets. Channel waits select on cancellation and bounded deadlines. Joins never detach a worker. A timed join returns a typed timeout result and leaves the worker joinable; normal shutdown requests cancellation, closes channels, and joins within the configured shutdown deadline.

The compatibility namespace accepts existing unqualified public imports and also accepts `module::name` qualified references. Imports merge only public declarations; private declarations remain module-local. Public signatures are checked for inaccessible private types.
