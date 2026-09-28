# Native implementation

The shipped toolchain is implemented completely in Go under `cmd/kry` and `internal/kry`. `tools/kry` is only a launcher for an already-built executable; it never searches for or invokes another compiler.

```bash
make
./tools/kry --version
./tools/kry check examples/control_flow.kry
./tools/kry run examples/fibonacci.kry
```

Kryndel exposes three distinct products. `kry run` interprets source on the portable Go runtime. `kry build --format=kexe` writes a portable `KRYNATIVE6` bundle containing checked source files and canonical typed KIR; it is not a machine-code executable. The loader validates the KIR and requires it to match the KIR regenerated from the embedded sources. `kry build --format=elf-direct` emits Linux amd64 machine code directly for its explicitly supported subset. The native AOT formats (`elf`, `exe`, and `pe`) use the generated-C backend by default. On a Windows amd64 target, `kry build --format=exe --no-external-toolchain` selects the C-free `pe-direct` machine backend; `--format=pe-direct` requests it explicitly. This backend has an explicit bounded language subset and fails on unsupported constructs. C AOT accepts Linux amd64/arm64 and Windows amd64 targets; target OS and architecture are validated before C lowering. `--format=c` emits C source without invoking a compiler. To compile a supported Windows PE with the Kryndel-authored compiler rather than the Go direct backend, run `bash scripts/build-selfhost-pe.sh SOURCE.kry [OUTPUT.exe]` from Linux amd64 or WSL; this invokes the checked-in Stage 1 ELF directly, without Go, C, an assembler, or a linker.

The executable does not require C, Python, Rust, Node.js, or an equivalent runtime to execute interpreted Kryndel programs. Building the Go toolchain requires Go and the modules listed in `go.mod`; Go builds include those dependencies in the resulting executable. Windows webcam capture additionally uses the external `ffmpeg` executable. Host integrations that need Go libraries or platform frameworks are rejected by native backends with their builtin name instead of embedding or invoking the VM.

On Windows, Linux/amd64 native builds use `x86_64-linux-gnu-gcc` and Linux/arm64 builds use `aarch64-linux-gnu-gcc` when the selected compiler is on `PATH`. If it is missing and WSL2 has the `Ubuntu` distribution with that compiler, the builder automatically invokes it and translates temporary paths into `/mnt/<drive>/...`; `KRY_WSL_DISTRO` selects another distribution. Windows PE builds currently support amd64 and use the configured MinGW-capable `gcc` on Windows. The test suite executes the ELF AOT parity tests on Linux/amd64 and validates PE/ELF headers on Windows.

## Command contract

| Invocation | Behavior | Success code |
| --- | --- | ---: |
| `check source.kry` | Read, lex, parse, resolve modules, and type-check without effects. | `0` |
| `run source.kry` | Check and execute source. | `0` |
| `run file.kexe` | Validate embedded KIR against the checked source payload, then execute the program. | `0` |
| `build source.kry` | Check and write a deterministic `KRYNATIVE6` bundle with checked source and typed KIR. | `0` |
| `build source.kry --format=kexe` | Write a portable source-and-KIR bundle; no external compiler is invoked. | `0` |
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

## KRYNATIVE6 format

The artifact is a deterministic, self-contained bundle. It is written through a temporary file, flushed and synchronized, then renamed atomically:

```text
KRYNATIVE6\0

u32 format version (=6)
u64 compiler-identity length, UTF-8 bytes
u64 language-version length, UTF-8 bytes (currently `1.0.0`)
three-byte header tag `KRY`
u64 target identity length, UTF-8 bytes (`portable/any` in KRYNATIVE6)
u32 source-entry count
repeat source-entry count:
  u64 logical path length, UTF-8 bytes
  u64 package visibility scope length, UTF-8 bytes
  u64 source length, exact UTF-8 source bytes
  32-byte SHA-256 of those source bytes
u64 canonical KIR length
32-byte SHA-256 of those KIR bytes
exact canonical KIR JSON bytes
```

The `<root>` entry is always first; all remaining entries are sorted by canonical UTF-8 logical path. Module paths and visibility scopes are relative, traversal-safe identifiers. KRYNATIVE6 uses a target-neutral KIR so the same checked artifact can run across supported operating systems and architectures; older versions retain their recorded host target. The decoder rejects incompatible compiler or target metadata, truncated fields, integer-size inconsistencies, duplicate entries, invalid hashes, unsafe paths, trailing bytes, and source artifacts masquerading as modules. It decodes and validates the typed KIR, then the engine checks that it is byte-for-byte the canonical KIR regenerated from the embedded source files. Missing external modules therefore cannot alter execution. Package visibility metadata preserves private access between the source files of the same manifest-backed package.

The reader also accepts KRYNATIVE5 version 5, KRYNATIVE4 version 4, and legacy KRYNATIVE3 version 3 files; v3 uses compiler identity `kryndel-go-1.2.0` and language version 1.0.0. V3–v5 artifacts are rechecked from their source entries and have KIR regenerated at load time. KRYNATIVE4 and earlier artifacts retain file-local module visibility because they predate package-scope metadata. The former `KRYNATIVE1` single-source container is rejected.

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
on the current host. `partial` marks a bounded backend subset.
The `feature_scope` field summarizes the backend, but this matrix does not yet
probe every language construct and builtin independently.

Use `kry capabilities --builtins` for a generated row for each registered
builtin and declared target, with interpreter, C AOT, direct ELF, direct PE,
and current self-hosted subset status. JSON consumers can run `kry --json capabilities
--builtins`. Native statuses are derived from generated backend dispatch
inventories; `partial` marks bounded implementations such as the direct
machine-code subsets, plain-HTTP C AOT, and the current self-hosted frontend.
The direct ELF `str` subset covers the scalar output types plus `Nil`; direct
PE `str` covers `Int`, unsigned integers, `Bool`, and `String`. Both remain
`partial` because they do not implement every type accepted by interpreter
`str`.

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

C AOT JSON support includes `json_parse`, `json_stringify`, `json_kind`,
`json_object_get`, `json_array_len`, `json_array_get`, `json_string`,
`json_int`, `json_uint`, `json_float`, `json_bool`, and `json_is_null`. Accessor
results retain the interpreter's typed errors and integer range behavior,
including the full UInt64 range. Both implementations replace invalid UTF-8
and unpaired surrogate escapes with U+FFFD, keep the last duplicate object
key, and reject documents nested deeper than 256 containers with the same
`Result` error. The JSON builtins are unsupported by `elf-direct`; capabilities
reports that explicitly, and a build using one fails with a builtin-specific
diagnostic. Differential regression tests execute the interpreter and C AOT
output on Linux amd64, including accessor errors, number boundaries, escaped
duplicate keys, large objects, nesting limits, and invalid UTF-8 through the C
runtime. The generated C implementation is advertised for Linux amd64, Linux
arm64, and Windows amd64; the differential suite does not claim runtime
verification on arm64 or Windows.

C AOT also implements the TCP client builtins `tcp_connect`, `tcp_send`,
`tcp_receive`, and `tcp_close` for Linux amd64/arm64 and Windows amd64.
Connections use nonblocking sockets; connect, send, and receive wait against a
single absolute deadline captured once at the start of each operation. Partial
writes and readiness retries reuse that deadline, so they cannot extend a send;
`tcp_send` returns the full byte count on success and an error if the deadline
expires. Receive sizes use the configured source-size limit. Closed-handle
operations, invalid ports, EOF, and unclosed-socket diagnostics are explicit;
the C runtime closes remaining sockets on exit. Operating-system connection
errors retain their broad cause but can differ in detail from Go's resolver and
socket messages. Host lookup uses synchronous `getaddrinfo`/WinSock resolution,
which the native deadline cannot interrupt. `tcp_listen`, `tcp_accept`,
`tcp_local_port`, UDP, and WebSockets remain unsupported by C AOT; the direct
ELF and direct PE backends still reject all TCP builtins. C AOT implements
`http_request` for plain `http://` URLs on Linux amd64/arm64 and Windows amd64.
It shares one `MaxWallTimeMS` deadline across connect, send, and response read,
limits response bodies to `MaxSourceBytes`, handles Content-Length and chunked
bodies, and matches the interpreter's HTTP status, invalid UTF-8, size-limit,
and timeout results. HTTPS, URL user information, custom headers, and the
separate `http_get` and `http_request_auth` builtins are not supported by this
subset; HTTPS is returned as an explicit error. Redirect responses are returned
as HTTP status errors instead of being followed. Hostname resolution is
synchronous and cannot be interrupted by the deadline. Differential execution
tests cover Linux amd64 and Windows amd64, not Linux arm64. Windows C output
links WinSock (`ws2_32`); add `-lws2_32` when compiling emitted C source
manually.

### SQLite in C AOT

The generated-C AOT backend implements `sqlite_open`, `sqlite_exec`,
`sqlite_query`, and `sqlite_close` on Linux amd64/arm64 and Windows amd64. It
loads SQLite at program startup and does not need SQLite development headers:

- Linux tries `libsqlite3.so.0`, then `libsqlite3.so`; manually compiling
  emitted C should link with `-ldl` on systems where `dlopen` is not in libc.
- Windows tries `winsqlite3.dll`, then `sqlite3.dll`.
- If neither library is available, `sqlite_open` returns an `Err` that names
  the missing runtime library. Programs can still be built without SQLite
  installed, but need one of these libraries to open a database.

`:memory:` databases and file paths are supported. Relative file paths resolve
from the process working directory and remain subject to host file permissions.
Queries return `Array[Array[String]]`: `NULL` becomes an empty string, integers
use decimal text, floats use shortest round-trip text, and TEXT/BLOB values
preserve their bytes in a Kryndel `String`. Result row count and columns per row
are each bounded by `MaxArrayElements`; allocations for rows, cells, and copied
values count against `MaxMemoryBytes`. SQLite's own internal allocations are
managed by the loaded SQLite library. A 5-second SQLite busy timeout is set on
each handle. SQLite diagnostics discard the driver's class prefix and trailing
numeric result-code suffix. The remaining message comes from the loaded SQLite
library, so wording can still vary between SQLite versions and platforms.

The C runtime finalizes active statements and closes open database handles on
runtime errors, and reports an unclosed handle at normal exit. Direct ELF and
direct PE do not implement SQLite and reject these builtins before emitting an
executable. SQLite file access is not confined by a native-runtime sandbox;
restricted interpreter mode continues to deny SQLite because database
`ATTACH` paths cannot be safely confined by the current authorizer.
Differential execution tests cover Windows amd64 and Linux amd64/WSL; Linux
arm64 is listed as a supported C AOT target but is not runtime-tested by this
matrix.

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

A sealed artifact (`KRYSEAL1`) is the plain `KRYNATIVE6` byte stream wrapped in
AES-256-GCM with a PBKDF2-HMAC-SHA-256 key derived from a passphrase. The
container is self-describing (magic, version, iteration count, salt, nonce,
length) so the KDF or cipher can be revised without breaking old files. The
engine decrypts a sealed artifact in memory before decoding it, so the plaintext
artifact is never written to disk. A wrong passphrase, a truncated file, or any
tampering fails the GCM tag check and is reported as a categorized artifact
diagnostic.

## Portability

The implementation uses Go fixed-width arithmetic helpers, explicit bounds checks, standard-library filesystem and process APIs, and deterministic locale-independent output. The release workflow cross-builds Linux amd64/arm64, macOS amd64/arm64, and Windows amd64 binaries with `CGO_ENABLED=0`; each artifact receives a SHA-256 manifest.
