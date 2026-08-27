# Testing

The test suite runs from the repository root with only Go 1.22+ for source builds. `make test` builds the self-contained executable, checks the shipped examples, and runs native Go tests over the compiler and runtime APIs.

```bash
make test
make test-static
make test-race
make fuzz-smoke
make coverage
make benchmark
make check-docs
```

The native Go tests cover recursive functions, `if`, `while`, mutable bindings, checked operators, homogeneous arrays, Unicode strings including embedded NUL bytes, bytes, assertions, static diagnostics, modules, enums, exhaustive options and results, bounded threads and channels, worker failure propagation, deterministic artifacts, formatter behavior, malformed input, sandbox traversal, resource limits, and REPL state. The race target exercises the worker and channel fixtures; fuzz smoke uses bounded deterministic malformed inputs and the Go fuzzing API is available for extension.

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
| Artifacts | Deterministic builds, KRYNATIVE3 metadata, exact lengths, SHA-256 hashes, embedded dependencies, path safety, trailing bytes, truncated payloads, version mismatch, invalid payloads, and replay after dependency removal. |
| Security and resources | Restricted-root traversal and symlink checks, NUL paths, invalid UTF-8 environment values, source/artifact input limits, and clean controlled failures. |
| Tooling | Persistent function/type state, 5 KiB REPL lines, formatter idempotence, JSON diagnostics, fuzz-smoke timeouts, coverage generation, and portable benchmark targets. |
| CLI | Help, version, invalid arguments, exit codes, REPL, formatter, doctor, JSON diagnostics, and missing executable. |
| Memory | Representative success and failure paths, worker-local contexts, channel cleanup, worker joins, and bounded shutdown under the race detector. |
| Documentation | English-only audit and synchronization between documented and implemented commands and builtins. |

Go vet and the race detector are used as the applicable strict and concurrency checks. Fuzz smoke uses bounded malformed inputs and reproducible seeds; full fuzz targets are run with explicit time budgets in CI. Tests do not require network access. A syntax change must include a positive example and a stable negative assertion where appropriate. An artifact change must include identical builds, dependency removal, version mutation, and a length or payload mutation.
