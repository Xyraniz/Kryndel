# Native implementation

The shipped toolchain is implemented completely in Go under `cmd/kry` and `internal/kry`. `tools/kry` is only a launcher for an already-built executable; it never searches for or invokes another compiler.

```bash
make
./tools/kry --version
./tools/kry check examples/control_flow.kry
./tools/kry run examples/fibonacci.kry
```

Kryndel exposes three distinct products. `kry run` interprets source on the portable Go runtime. `kry build --format=kexe` writes a portable `KRYNATIVE5` bundle containing checked source files; it is not a machine-code executable. `kry build --format=elf-direct` emits machine code directly for its explicitly supported Linux amd64 subset. The native AOT formats (`elf`, `exe`, and `pe`) use the generated-C backend and require an external C compiler. C AOT accepts Linux amd64/arm64 and Windows amd64 targets; target OS and architecture are validated before C lowering. `--format=c` emits C source without invoking a compiler.

The executable does not require C, Python, Rust, Node.js, or an equivalent runtime to execute interpreted Kryndel programs. Building the Go toolchain requires Go and the modules listed in `go.mod`; Go builds include those dependencies in the resulting executable. Windows webcam capture additionally uses the external `ffmpeg` executable. Host integrations that need Go libraries or platform frameworks are rejected by native backends with their builtin name instead of embedding or invoking the VM.

On Windows, Linux/amd64 native builds use `x86_64-linux-gnu-gcc` and Linux/arm64 builds use `aarch64-linux-gnu-gcc` when the selected compiler is on `PATH`. If it is missing and WSL2 has the `Ubuntu` distribution with that compiler, the builder automatically invokes it and translates temporary paths into `/mnt/<drive>/...`; `KRY_WSL_DISTRO` selects another distribution. Windows PE builds currently support amd64 and use the configured MinGW-capable `gcc` on Windows. The test suite executes the ELF AOT parity tests on Linux/amd64 and validates PE/ELF headers on Windows.

## Command contract

| Invocation | Behavior | Success code |
| --- | --- | ---: |
| `check source.kry` | Read, lex, parse, resolve modules, and type-check without effects. | `0` |
| `run source.kry` | Check and execute source. | `0` |
| `run file.kexe` | Validate the container and execute its source payload. | `0` |
| `build source.kry` | Check and write a deterministic `KRYNATIVE5` bundle. | `0` |
| `build source.kry --format=kexe` | Write a portable bundle containing validated source; no compiler is invoked. | `0` |
| `build source.kry --format=exe --target=windows-x64` | Check and write a real PE32+ entrypoint for the selected target. | `0` |
| `build source.kry --format=elf --target=linux-x64` | Check and write an ELF64 executable with the generated-C AOT backend; an external C compiler is required. | `0` |
| `build source.kry --format=elf-direct --target=linux-x64` | Use the dependency-free direct machine backend for the documented scalar-output, immutable String-pointer, dynamic String-concatenation, immutable qword-Array literal/index/push/get/concat, tagged Option/Result construction and inspection, Int/Bool/UInt assignment/control-flow, and scalar/pointer-function slice; no C compiler is invoked. | `0` |
| `build source.kry --format=elf-direct --target=linux-x64 --no-external-toolchain` | Require the direct backend and forbid builds that need an external compiler. | `0` |
| `build source.kry --format=elf --no-external-toolchain` | Reject before backend code generation; the C AOT backend requires an external C compiler. | `2` |
| `build source.kry --format=c` | Check and emit the generated C source without compiling. | `0` |
| `emit source.kry --format=kry-ir [--target=TARGET]` | Emit deterministic, versioned checked KIR JSON without executing source; `TARGET` defaults to the host. | `0` |
| `inspect binary` | Inspect PE/ELF headers and reject unknown binary formats. | `0` |
| `fmt [--check\|-w] source.kry` | Check and format valid source deterministically. | `0` |
| `lsp` | Serve LSP requests over standard input/output until the client exits. | `0` after `shutdown` and `exit` |
| `repl` | Run the interactive read-evaluate-print loop. | `0` |
| `doctor` | Report native installation readiness. | `0` |
| `version` | Print the compiler version. | `0` |
| Invalid usage or a forbidden external toolchain | Print a categorized CLI error naming the requested format and required dependency. | `2` |
| Source, type, runtime, artifact, or I/O failure | Print a categorized diagnostic. | `1` |
| Missing built executable | Print an actionable build message. | `69` |

Diagnostics use the stable form `error[category]: file:line:column`, followed by a short message, source excerpt, and caret when source is available. `--json` emits one machine-readable object with a stable `KRY001`–`KRY008` code, category, severity, source, line, column, and message.

`--format=llvm-ir` is rejected explicitly until Kryndel has a real lowering to LLVM's typed SSA model. It never emits placeholder IR.

## KRYNATIVE5 format

The artifact is a deterministic, self-contained bundle. It is written through a temporary file, flushed and synchronized, then renamed atomically:

```text
KRYNATIVE5\0

u32 format version (=5)
u64 compiler-identity length, UTF-8 bytes
u64 language-version length, UTF-8 bytes (currently `1.0.0`)
three-byte header tag `KRY`
u64 target-identity length, UTF-8 bytes
u32 source-entry count
repeat source-entry count:
  u64 logical path length, UTF-8 bytes
  u64 package visibility scope length, UTF-8 bytes
  u64 source length, exact UTF-8 source bytes
  32-byte SHA-256 of those source bytes
```

The `<root>` entry is always first; all remaining entries are sorted by canonical UTF-8 logical path. Module paths and visibility scopes are relative, traversal-safe identifiers. The decoder rejects incompatible compiler or target metadata, truncated fields, integer-size inconsistencies, duplicate entries, invalid hashes, unsafe paths, trailing bytes, and source artifacts masquerading as modules. `run file.kexe` reuses the normal parse, module, checker, and runtime pipeline over the embedded sources, so a missing external module cannot change execution. Package visibility metadata preserves private access between the source files of the same manifest-backed package.

The reader also accepts KRYNATIVE4 version 4 artifacts and legacy KRYNATIVE3 version 3 files with compiler identity `kryndel-go-1.2.0`, treating KRYNATIVE3 as language version 1.0.0. KRYNATIVE4 artifacts retain file-local module visibility because they predate package-scope metadata. The former `KRYNATIVE1` single-source container is rejected.

## Native binary formats

`internal/kry/native.go` produces real, runnable executables through a C-based
ahead-of-time backend. The checked program is lowered to C by
`internal/kry/codegen.go`, linked against the embedded runtime in
`internal/kry/cruntime.go`, and compiled by the host C toolchain:

| Target | Compiler | Output |
| --- | --- | --- |
| `linux-x64` | `cc`/`gcc`/`clang` | ELF64 executable |
| `linux-arm64` | `aarch64-linux-gnu-gcc` | ELF64 executable |
| `windows-x64` | MinGW-capable `gcc` | PE32+ executable |

Windows arm64 PE output is not supported by the C AOT backend yet. A target
alias being accepted by the CLI does not imply that every output format can
produce it; unsupported format/target pairs fail before C generation.

`kry capabilities` prints the canonical output-format and target matrix;
`kry --json capabilities` emits the same rows as JSON. The matrix is generated
from the target policy used by native build validation. `supported` means the
backend implements that format/target pair, while `toolchain` names any
external compiler requirement; it does not claim that compiler is installed
on the current host. `partial` marks the direct ELF backend's language subset.
The `feature_scope` field summarizes the backend, but this matrix does not yet
probe every language construct and builtin independently.

Use `kry capabilities --builtins` for a generated row for each registered
builtin and declared target, with interpreter, C AOT, direct ELF, and current
self-hosted subset status. JSON consumers can run `kry --json capabilities
--builtins`. Native statuses are derived from generated backend dispatch
inventories; `partial` marks the documented direct-ELF subset or current
self-hosted frontend subset.

> [!IMPORTANT]
> `supported` describes backend support for a format and target; it does not
> confirm that the required external compiler is installed. Run `kry doctor`
> to check the current host.

Use `kry capabilities --features` for the combined language matrix. It lists
every registered builtin, expression kind, statement kind, pattern kind,
unary and binary operator, and declared language type for every native target.
The JSON form is `kry --json capabilities --features`. The generator reads the
Go backend dispatch and the Stage 3 source compiler's syntax and builtin
recognition; native builds consult the same generated inventories before
emitting C or machine-code output. `supported` means the backend has a lowering
or runtime handler for that item and the target is allowed; `partial` means a
bounded implementation exists; `unsupported` means no handler is advertised
for that backend/target. These inventories describe dispatch coverage and do
not by themselves prove semantic parity across all input combinations.

The `KRY_CC` environment variable overrides the compiler. Successful builds
print the selected backend and build-time toolchain dependency. Use
`--no-external-toolchain` to make a no-compiler requirement explicit; `elf`,
`exe`, and `pe` fail before C generation or process execution. `elf-direct` is
available only for its documented subset and rejects unsupported source before
emitting an executable. `--format=c` emits C source for inspection or later use
with a separately managed toolchain; this command itself does not invoke one.

The C AOT runtime implements immutable values with an arena budget, checked
arithmetic, and per-block `defer` unwinding for its accepted subset. Those
implementation choices do not establish parity for every interpreter input;
the capability matrix reports lowering coverage, and parity still depends on
the targeted regression tests for each operation. The backend supports
functions, recursion, `if`/`else`, `while`,
`for`, `match`, structs, enums, arrays, maps, sets, strings, bytes, `Option`,
`Result`, the `?` operator, and the pure, filesystem, environment, JSON, crypto,
process and timing builtins. It also supports the deterministic concurrency
surface: `shared_new`/`shared_read`/`shared_write`/`shared_swap`, actor
mailboxes (`actor_channel`, `actor_send`, `actor_try_receive`,
`actor_receive_timeout`, `actor_close`), task groups (`task_group`,
`task_spawn`, `task_group_cancel`, `task_group_wait`), worker threads
(`thread_spawn`, `await`, `await_timeout`) and runtime polymorphism
(`poly_register`, `poly_reorder`, `poly_dispatch`). Because values are
immutable, shared cells are heap pointers and actor mailboxes are FIFO queues;
worker threads are not executed concurrently but are run on demand by `await`,
which preserves the observable result for the deterministic subset. Host-
integrated builtins are added to the native support matrix only when the
generated C runtime has an implementation for the selected target; until then
they are rejected with a categorized diagnostic instead of silently degrading.
Output is never mislabeled as native merely because a file has a native-looking
suffix.

### Cryptography in the native runtime

The C runtime implements the full crypto surface so native executables do not
depend on the interpreter: SHA-256/384/512/1, MD5, HMAC-SHA-256, AES-256-GCM,
PBKDF2-HMAC-SHA-256, HKDF-SHA-256, constant-time comparison, XOR and
base64/base64url. Each primitive is verified against published vectors and
against the Go implementation, and the test suite asserts byte-for-byte parity
between the interpreter, the Linux ELF and the Windows PE for the whole crypto
surface. Two subtle bugs were found and fixed during this work: the SHA-1 block
used a right-rotation where the algorithm requires a left-rotation, and the GCM
counter started at `IV||1` instead of `IV||2` (the first data block must use
`J0 + 1`). Both are now covered by regression tests.

### String-literal obfuscation

`kry build --obfuscate` lowers string literals through `k_obf_str`, which
reverses a position-dependent XOR mask at startup. The plaintext literal never
appears in the binary, so a `strings` scan does not reveal messages or secrets,
while the program's observable output is unchanged. Obfuscation complements
encryption; it does not replace it.

### Sealed artifacts

A sealed artifact (`KRYSEAL1`) is the plain `KRYNATIVE5` byte stream wrapped in
AES-256-GCM with a PBKDF2-HMAC-SHA-256 key derived from a passphrase. The
container is self-describing (magic, version, iteration count, salt, nonce,
length) so the KDF or cipher can be revised without breaking old files. The
engine decrypts a sealed artifact in memory before decoding it, so the plaintext
artifact is never written to disk. A wrong passphrase, a truncated file, or any
tampering fails the GCM tag check and is reported as a categorized artifact
diagnostic.

## Portability

The implementation uses Go fixed-width arithmetic helpers, explicit bounds checks, standard-library filesystem and process APIs, and deterministic locale-independent output. The release workflow cross-builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 binaries with `CGO_ENABLED=0`; each artifact receives a SHA-256 manifest.
