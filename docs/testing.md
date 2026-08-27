# Testing

The test suite runs from the repository root and requires no package environment. `make test` builds the native executable with strict warnings, checks the shipped examples, and runs the same integration executable exposed to users.

```bash
make test
make test-sanitized
make test-thread-sanitized
make test-static
make fuzz-smoke
make coverage
make benchmark
make check-docs
```

The integration test covers recursive functions, `if`, `while`, mutable bindings, checked operators, homogeneous arrays, Unicode strings including embedded NUL bytes, bytes, assertions, static diagnostics, modules, enums, exhaustive options and results, bounded threads and channels, worker failure propagation, deterministic artifacts, formatter behavior, and malformed input. The feature regression suite additionally covers deep transfer after join, worker call-graph isolation, forward-reference rejection, Bool/Nil coverage, artifact dependency removal and version mutation, sandbox traversal, input limits, long REPL lines, and formatter idempotence. Sanitizer execution uses AddressSanitizer, UndefinedBehaviorSanitizer, and LeakSanitizer without disabling leak detection.

| Area | Required coverage |
| --- | --- |
| Lexer | UTF-8 source, line and block comments, nested comments, escapes, malformed literals, invalid characters, and exact positions. |
| Parser | Incomplete expressions, malformed blocks, declarations, imports, patterns, precedence, and nested scopes. |
| Type checker | Unknown types, mismatched declarations, immutable assignment, invalid operators, non-Boolean conditions, unknown functions, arity, return mismatches, worker resolution, and thread transfer types. |
| Mutability | Immutable rejection, mutable success, shadowing, branch scopes, loop scopes, and function scopes. |
| Runtime | Division by zero, out-of-bounds indexing, invalid UTF-8, embedded NUL values, conversions, assertions, recursion, control-flow misuse, channel transfer, worker joins, shutdown, and worker failures. |
| Numeric safety | Overflow, underflow, minimum integer negation, `abs(Int minimum)`, literal overflow, and allocation-size checks. |
| Builtins | Every registry entry, valid signatures, invalid signatures, conversion edges, and deterministic output. |
| Modules | Relative resolution, public exports, duplicate names, cycles, missing files, traversal rejection, and deterministic behavior. |
| Artifacts | Deterministic builds, KRYNATIVE2 metadata, exact lengths, SHA-256 hashes, embedded dependencies, path safety, trailing bytes, truncated payloads, version mismatch, invalid payloads, and replay after dependency removal. |
| Security and resources | Restricted-root traversal and symlink checks, NUL paths, invalid UTF-8 environment values, source/artifact input limits, and clean controlled failures. |
| Tooling | Persistent function/type state, 5 KiB REPL lines, formatter idempotence, JSON diagnostics, fuzz-smoke timeouts, coverage generation, and portable benchmark targets. |
| CLI | Help, version, invalid arguments, exit codes, REPL, formatter, doctor, and missing compiler. |
| Memory | Representative success and failure paths, worker-local arenas, channel cleanup, and thread joins under all available sanitizers. |
| Documentation | English-only audit and synchronization between documented and implemented commands and builtins. |

GCC and Clang are used where available. ThreadSanitizer runs the worker and channel fixtures, including the feature regression suite, separately from AddressSanitizer, UndefinedBehaviorSanitizer, and LeakSanitizer. Fuzz smoke uses fixed byte patterns and a two-second timeout per malformed input. Tests do not require network access. A syntax change must include a positive example and a stable negative assertion where appropriate. An artifact change must include identical builds, dependency removal, version mutation, and a length or payload mutation.
