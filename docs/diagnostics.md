# Diagnostics

Kryndel reports errors in English using a stable category and source location:

```text
error[type-mismatch]: examples/main.kry:4:14
  assignment to 'count' expected Int, found String
  count = "seven"
         ^
```

The categories are `lex`, `parse`, `type-mismatch`, `runtime`, `artifact`, `cli`, `io`, and `resource`. Stable codes are `KRY001` through `KRY008`, assigned by category in that order. A diagnostic contains the filename, one-based line and column, a concise message, and a source excerpt with a caret whenever source text is available. The first error is retained so output remains deterministic.

`check` and `build` report lexical, parse, module, and static errors before any user expression is evaluated. `run` reports the same preflight errors and then runtime failures such as checked integer overflow, division by zero, invalid UTF-8, failed assertions, and out-of-bounds indexing. Artifact failures are reported before payload parsing.

| Exit code | Meaning |
| ---: | --- |
| `0` | The requested operation completed successfully. |
| `1` | Source, static, runtime, artifact, or I/O failure. |
| `2` | Invalid command-line usage. |
| `69` | The built executable is missing; run `make build` for a source checkout. |

Diagnostics never include Python tracebacks, implementation-debug output, locale-dependent formatting, or silent fallback behavior.
