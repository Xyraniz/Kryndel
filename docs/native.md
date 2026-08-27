# Native implementation

The shipped toolchain is implemented completely in Go under `cmd/kry` and `internal/kry`. `tools/kry` is only a launcher for an already-built executable; it never searches for or invokes another compiler.

```bash
make
./tools/kry --version
./tools/kry check examples/control_flow.kry
./tools/kry run examples/fibonacci.kry
```

The executable does not load modules from another language and does not require C, Python, Rust, Node.js, or an equivalent runtime to execute Kryndel programs. The only source-build dependency is the documented Go toolchain and standard library.

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
| Missing built executable | Print an actionable build message. | `69` |

Diagnostics use the stable form `error[category]: file:line:column`, followed by a short message, source excerpt, and caret when source is available. `--json` emits one machine-readable object with a stable `KRY001`–`KRY008` code, category, severity, source, line, column, and message.

## KRYNATIVE3 format

The artifact is a deterministic, self-contained bundle. It is written through a temporary file, flushed and synchronized, then renamed atomically:

```text
KRYNATIVE3\0

u32 format version (=3)
u64 compiler-identity length, UTF-8 bytes
three-byte header tag `KRY`
u64 target-identity length, UTF-8 bytes
u32 source-entry count
repeat source-entry count:
  u64 logical path length, UTF-8 bytes
  u64 source length, exact UTF-8 source bytes
  32-byte SHA-256 of those source bytes
```

The `<root>` entry is always first; all remaining entries are sorted by canonical UTF-8 logical path. Module entries are relative, traversal-safe paths. The decoder rejects incompatible compiler or target metadata, truncated fields, integer-size inconsistencies, duplicate entries, invalid hashes, unsafe paths, trailing bytes, and source artifacts masquerading as modules. `run file.kexe` reuses the normal parse, module, checker, and runtime pipeline over the embedded sources, so a missing external module cannot change execution.

The former `KRYNATIVE1` single-source container is intentionally rejected as an incompatible artifact rather than silently interpreted.

## Portability

The implementation uses Go fixed-width arithmetic helpers, explicit bounds checks, standard-library filesystem and process APIs, and deterministic locale-independent output. The release workflow cross-builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 binaries with `CGO_ENABLED=0`; each artifact receives a SHA-256 manifest.
