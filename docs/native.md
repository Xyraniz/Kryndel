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

Diagnostics use the stable form `error[category]: file:line:column`, followed by a short message, source excerpt, and caret when source is available.

## KRYNATIVE1 format

The format is intentionally simple and deterministic:

```text
11 bytes:  KRYNATIVE1\n
8 bytes:   unsigned little-endian payload length
N bytes:   exact Kryndel source payload
```

The length must equal the remaining file size. A truncated header, inconsistent length, or trailing byte is rejected before evaluation. The payload is checked again through the ordinary native source pipeline during `run`.

## Portability

The source uses fixed-width integer types for arithmetic and artifact fields, explicit overflow checks, standard C and POSIX file APIs, and no locale-dependent output. GCC and Clang static checks are part of the test matrix. Release workflows build native binaries for supported platforms without fabricating artifacts on platforms that are unavailable to the local environment.
