# Native implementation

`native/kry.c` is the complete implementation of the shipped toolchain. It compiles as a C11 executable named `build/kry`; `tools/kry` only performs an on-demand build and forwards all arguments.

```bash
make
./tools/kry --version
./tools/kry check examples/control_flow.kry
./tools/kry run examples/fibonacci.kry
```

The executable does not load modules from another language and does not require Python, Rust, Node, or an equivalent runtime to execute Kryndel programs. The only external build dependency is a C11 compiler.

## Command contract

| Invocation | Behavior | Success code |
| --- | --- | ---: |
| `check source.kry` | Read, lex, parse, resolve modules, and type-check without effects. | `0` |
| `run source.kry` | Check and execute source. | `0` |
| `run file.kexe` | Validate the container and execute its source payload. | `0` |
| `build source.kry` | Check and write a deterministic artifact. | `0` |
| `fmt [--check\|-w] source.kry` | Check and format valid source deterministically. | `0` |
| `repl` | Run the interactive read-evaluate-print loop. | `0` |
| `doctor` | Report native installation readiness. | `0` |
| `version` | Print the compiler version. | `0` |
| Invalid usage | Print a categorized CLI error. | `2` |
| Source, type, runtime, artifact, or I/O failure | Print a categorized diagnostic. | `1` |
| Missing C11 compiler in launcher | Print an actionable installation message. | `69` |

Diagnostics use the stable form `error[category]: file:line:column`, followed by a short message, source excerpt, and caret when source is available. `--json` emits one machine-readable object with a stable `KRY001`–`KRY008` code, category, severity, source, line, column, and message.

## KRYNATIVE2 format

The artifact is a deterministic, self-contained bundle. It is written through a temporary file, flushed and synchronized, then renamed atomically:

```text
KRYNATIVE2\n
u32 format version
u32 compiler-version length, bytes
u32 target length, bytes
u32 source-entry count
u64 payload byte length
repeat source-entry count:
  u32 logical path length, bytes
  u64 source length, exact UTF-8 source bytes
  32-byte SHA-256 of those source bytes
```

Entries are ordered by logical path; the first entry is `<root>` and module entries are relative, traversal-safe paths. The decoder rejects incompatible compiler or target metadata, truncated fields, integer-size inconsistencies, duplicate entries, invalid hashes, unsafe paths, trailing bytes, and source artifacts masquerading as modules. `run file.kexe` reuses the normal parse, module, checker, and runtime pipeline over the embedded sources, so a missing external module cannot change execution.

The former `KRYNATIVE1` single-source container is intentionally rejected as an incompatible artifact rather than silently interpreted.

## Portability

The source uses fixed-width integer types for arithmetic and artifact fields, explicit overflow checks, standard C and POSIX file APIs, and no locale-dependent output. GCC and Clang static checks are part of the test matrix. Release workflows build native binaries for supported platforms without fabricating artifacts on platforms that are unavailable to the local environment.
