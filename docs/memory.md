# Memory and resource lifecycle

Kryndel uses a per-invocation arena for the source tree, tokens, typed declarations, diagnostics, runtime binding records, immutable collection payloads, channel records, and thread records. The arena is released after checking, successful execution, parser failure, module failure, artifact rejection, formatter validation, and REPL evaluation. The launcher separately frees file buffers and artifact payloads after each command.

## Value categories

`Int`, `Float`, `Bool`, and `Nil` are copied by value. Enum tags are copied by value. Strings and bytes carry a length and immutable storage. Arrays carry an element type, a length, and immutable value storage. Structs, options, and results contain immutable value descriptors and payloads. Passing these values to a function copies the descriptor; operations such as concatenation and `array_push` allocate a new logical value rather than mutating the source.

`Channel[T]` and `Thread[T]` are opaque runtime handles. They are synchronized or joined through explicit builtins and are not Copy values for channel transfer. Raw pointers, arbitrary casts, file descriptors, sockets, and child-process handles are not source-language values.

## Mutation and cleanup

`let` creates an immutable binding and `let mut` creates a mutable binding. Collection elements and struct fields cannot be assigned through the current syntax. A channel owns a mutex, two condition variables, and one value slot; `thread_close` wakes blocked operations, and ordinary runtime shutdown closes every channel before joining every outstanding worker. Thread synchronization objects are destroyed only after their workers have joined.

Every file read closes its `FILE` object on success and all detected failure paths. Typed file writes close their stream and return a `Result`. Worker-local arenas are released after worker execution. The runtime also performs cleanup after a prior execution error, so a failing main program does not leave a worker or channel live beyond the invocation.

## Boundaries

The arena is an implementation lifetime boundary, not a general ownership proof. Source programs cannot retain references after an invocation, access freed storage, or bypass the typed value representation. Future resource families require owned handles and deterministic cleanup rules before they can be added to the stable source API.
