# Native implementation

The shipped toolchain is implemented completely in Go under `cmd/kry` and `internal/kry`. `tools/kry` is only a launcher for an already-built executable; it never searches for or invokes another compiler.

```bash
make
./tools/kry --version
./tools/kry check examples/control_flow.kry
./tools/kry run examples/fibonacci.kry
```

Kryndel exposes three distinct products. `kry run` interprets source on the portable Go runtime. `kry build --format=kexe` writes a portable `KRYNATIVE3` bundle containing checked source files; it is not a machine-code executable. `kry build --format=elf-direct` emits machine code directly for its explicitly supported subset. The default native AOT formats (`elf`, `exe`, and `pe`) use the generated-C backend and require an external C compiler. `--format=c` emits C source without invoking a compiler.

The executable does not require C, Python, Rust, Node.js, or an equivalent runtime to execute interpreted Kryndel programs. The only source-build dependency for the Go toolchain is Go and its standard library, plus the external `ffmpeg` executable when Windows webcam capture is requested. Host integrations that need Go libraries or platform frameworks are rejected by native backends with their builtin name instead of embedding or invoking the VM.

On Windows, Linux/amd64 native builds use `x86_64-linux-gnu-gcc` when it is on `PATH`. If it is missing and WSL2 has the `Ubuntu` distribution with that compiler, the builder automatically invokes it and translates temporary paths into `/mnt/<drive>/...`; `KRY_WSL_DISTRO` selects another distribution. Windows PE builds continue to use the configured MinGW-capable `gcc` on Windows. The test suite executes the ELF AOT parity tests on Linux/amd64 and validates PE/ELF headers on Windows.

## Command contract

| Invocation | Behavior | Success code |
| --- | --- | ---: |
| `check source.kry` | Read, lex, parse, resolve modules, and type-check without effects. | `0` |
| `run source.kry` | Check and execute source. | `0` |
| `run file.kexe` | Validate the container and execute its source payload. | `0` |
| `build source.kry` | Check and write a deterministic `KRYNATIVE3` bundle. | `0` |
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
| `repl` | Run the interactive read-evaluate-print loop. | `0` |
| `doctor` | Report native installation readiness. | `0` |
| `version` | Print the compiler version. | `0` |
| Invalid usage or a forbidden external toolchain | Print a categorized CLI error naming the requested format and required dependency. | `2` |
| Source, type, runtime, artifact, or I/O failure | Print a categorized diagnostic. | `1` |
| Missing built executable | Print an actionable build message. | `69` |

Diagnostics use the stable form `error[category]: file:line:column`, followed by a short message, source excerpt, and caret when source is available. `--json` emits one machine-readable object with a stable `KRY001`–`KRY008` code, category, severity, source, line, column, and message.

`--format=llvm-ir` is rejected explicitly until Kryndel has a real lowering to LLVM's typed SSA model. It never emits placeholder IR.

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

## Native binary formats

`internal/kry/native.go` produces real, runnable executables through a C-based
ahead-of-time backend. The checked program is lowered to C by
`internal/kry/codegen.go`, linked against the embedded runtime in
`internal/kry/cruntime.go`, and compiled by the host C toolchain:

| Target | Compiler | Output |
| --- | --- | --- |
| `linux-x64` | `cc`/`gcc`/`clang` | ELF64 executable |
| `windows-x64` | `x86_64-w64-mingw32-gcc` | PE32+ executable |

The `KRY_CC` environment variable overrides the compiler. Successful builds
print the selected backend and build-time toolchain dependency. Use
`--no-external-toolchain` to make a no-compiler requirement explicit; `elf`,
`exe`, and `pe` fail before C generation or process execution. `elf-direct` is
available only for its documented subset and rejects unsupported source before
emitting an executable. `--format=c` emits C source for inspection or later use
with a separately managed toolchain; this command itself does not invoke one.

The generated code mirrors the interpreter's semantics exactly: values are
immutable and arena-allocated with a memory budget, arithmetic is checked,
display and float formatting are byte-for-byte identical, and `defer` unwinds
per block. The backend supports functions, recursion, `if`/`else`, `while`,
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

A sealed artifact (`KRYSEAL1`) is the plain `KRYNATIVE3` byte stream wrapped in
AES-256-GCM with a PBKDF2-HMAC-SHA-256 key derived from a passphrase. The
container is self-describing (magic, version, iteration count, salt, nonce,
length) so the KDF or cipher can be revised without breaking old files. The
engine decrypts a sealed artifact in memory before decoding it, so the plaintext
artifact is never written to disk. A wrong passphrase, a truncated file, or any
tampering fails the GCM tag check and is reported as a categorized artifact
diagnostic.

## Portability

The implementation uses Go fixed-width arithmetic helpers, explicit bounds checks, standard-library filesystem and process APIs, and deterministic locale-independent output. The release workflow cross-builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 binaries with `CGO_ENABLED=0`; each artifact receives a SHA-256 manifest.
