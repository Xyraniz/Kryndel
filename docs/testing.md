# Testing

The test suite runs from the repository root and requires no package environment. `make test` builds the native executable with strict warnings, checks the shipped examples, and runs the same integration executable exposed to users.

```bash
make test
make test-sanitized
make test-static
make check-docs
```

The integration test covers recursive functions, `if`, `while`, mutable bindings, checked operators, homogeneous arrays, Unicode strings, bytes, assertions, static diagnostics, modules, enums, deterministic artifacts, formatter behavior, and malformed input. Sanitizer execution uses AddressSanitizer, UndefinedBehaviorSanitizer, and LeakSanitizer without disabling leak detection.

| Area | Required coverage |
| --- | --- |
| Lexer | UTF-8 source, line and block comments, nested comments, escapes, malformed literals, invalid characters, and exact positions. |
| Parser | Incomplete expressions, malformed blocks, declarations, imports, patterns, precedence, and nested scopes. |
| Type checker | Unknown types, mismatched declarations, immutable assignment, invalid operators, non-Boolean conditions, unknown functions, arity, and return mismatches. |
| Mutability | Immutable rejection, mutable success, shadowing, branch scopes, loop scopes, and function scopes. |
| Runtime | Division by zero, out-of-bounds indexing, invalid UTF-8, conversions, assertions, recursion, and control-flow misuse. |
| Numeric safety | Overflow, underflow, minimum integer negation, `abs(Int minimum)`, literal overflow, and allocation-size checks. |
| Builtins | Every registry entry, valid signatures, invalid signatures, conversion edges, and deterministic output. |
| Modules | Relative resolution, public exports, duplicate names, cycles, missing files, traversal rejection, and deterministic behavior. |
| Artifacts | Deterministic builds, exact header, exact length, trailing bytes, truncated payloads, invalid payloads, and replay. |
| CLI | Help, version, invalid arguments, exit codes, REPL, formatter, doctor, and missing compiler. |
| Memory | Representative success and failure paths under all available sanitizers. |
| Documentation | English-only audit and synchronization between documented and implemented commands and builtins. |

GCC and Clang are used where available. Tests do not require network access. A syntax change must include a positive example and a stable negative assertion where appropriate. An artifact change must include identical builds and a length or payload mutation.
