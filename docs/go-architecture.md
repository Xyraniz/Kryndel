# Go toolchain architecture

Kryndel is implemented by one coherent Go toolchain with a minimum version of 1.27.1. Go was selected over Zig because the repository needs a portable standard-library implementation of UTF-8 handling, process cancellation, filesystem operations, deterministic serialization, fuzzing, and cross-compilation without C headers, POSIX threads, or a host interpreter. Released binaries are statically linked where the target platform permits and require no Go installation or external runtime.

## Supported toolchains and build modes

These workflows share source code, but they have different toolchain and proof
requirements. Passing one does not imply that another mode or target is
supported.

| Mode | Host toolchain | Command / CI job | Supported scope and what it proves |
| --- | --- | --- | --- |
| Normal compiler build and tests | Go 1.27.1 or newer, as declared by `go.mod`; CI uses Go `1.27.x`. Go modules must be available locally or downloadable. | `go build ./cmd/kry`, `go test ./...`, or `make test-static`; CI `go` and `windows` jobs. `make test` additionally needs a C compiler for its C-backed ELF smoke build. | Builds and tests the Go reference compiler. CI tests it on Linux and Windows amd64. This does not verify the self-hosted compiler. |
| Bootstrap verification | Exactly Go `1.27.1`, Linux amd64, as pinned by `selfhost/bootstrap.lock.json`; `CGO_ENABLED=0` for the locked Stage 0 build. | `KRY_REQUIRE_LOCKED_BOOTSTRAP=1 bash scripts/bootstrap-stage3.sh`; CI `bootstrap` job. | Rebuilds the locked Stage 0 compiler and verifies the tested Stage 1–3 Linux amd64 source-compiler slice, including byte-identical Stage 2/3 artifacts. The integrated Stage 36 graph test needs 14 GiB of currently available memory plus swap; it skips below that locally and fails fast when `KRY_REQUIRE_LOCKED_BOOTSTRAP=1`. The source frontend and target coverage remain bounded; this is not full-language self-hosting. The bootstrap KIR limit is 192 MiB. The latest measured 168,377,923-byte KIR emission used 697,904 KB peak RSS; a Stage 0 dynamic-lowering run reached 1,546,468 KB RSS before completion. |
| Release build | Go 1.27.1 or newer (`1.27.x` in the release workflow), `CGO_ENABLED=0`; cross-compilation runs on Ubuntu. | `make release` locally, or the release workflow's verification and target matrix. | Produces Go compiler binaries for Linux amd64/arm64, macOS amd64/arm64, and Windows amd64. The self-host bootstrap is a separate release prerequisite. This matrix does not claim that Kryndel-generated native binaries support the same targets. |
| Self-host development | The checked-in Go `kry` executable is Stage 0 for initial KIR emission and for running the current source compiler during development. The tested source-compiler output is Linux amd64 ELF. | Start with the commands in [`selfhost/README.md`](../selfhost/README.md); `bash scripts/bootstrap-stage3.sh` runs the locked verification. | After Stage 0 has produced and launched Stage 1, the tested Stage 1–3 fixture path does not invoke Go, C, an assembler, or a linker. The current proof covers a subset and does not establish full compiler or Windows native-output parity. |

Keep changes to these modes explicit: a Go version change must update the
module declaration and relevant CI/release jobs, while a bootstrap lock change
must be reproduced with its pinned host and target before it is accepted.

The compiler and execution subsystems remain in `internal/kry`. Bounded camera and display capture live in `internal/platform`; generated C runtime fragments live in `internal/cruntime`. Both are leaf packages with no dependency on compiler or interpreter state. `internal/cruntime` assembles the standalone C runtime in dependency order, with separate source files for string building, float formatting, display, equality, arithmetic, canonical JSON, collections, and host integrations. These extractions follow testable dependency boundaries instead of mechanically splitting files. Further compiler/runtime package extraction should use the same rule. Compiler and execution state is explicit per invocation; no mutable package-level program state is used.

The frontend produces checker-annotated source trees, and `CompileMIR` now lowers those trees directly into flat typed arena rows. It does not build a recursive KIR document or round-trip JSON. `DecodeMIR` parses and validates the portable document once, then builds the same arena. Neither path retains the recursive wire document in `ValidatedMIR`. Native target and feature preflight walks arena indexes directly. The PE backend validates and lowers both static and dynamic subsets through arena references; ELF static output and dynamic lowering do the same. C AOT follows arena indexes and keeps only scalar per-row views. The interpreter resolves expression, statement, function, parameter, and match references through shallow scalar adapters and preflights its executable subset before running; it does not materialize a recursive compatibility document. Production entrypoints start from `ValidatedMIR`, and they do not lower the source AST. The Engine interpreter retains its executor and global scope across REPL snippets. C AOT, direct ELF, and direct PE reject unsupported forms without an AST fallback. The bounded direct KIR entrypoint also supports differential and backend-subset tests, whose reference runtime uses the retained AST evaluator explicitly. The runtime carries an `ExecContext` with cancellation, instruction, wall-clock, call-depth, stack, memory, source, and output budgets. Every host-facing operation returns a typed error. Go panics are not used for language failures and are converted at the CLI boundary only for unexpected host failures.

This Go `ValidatedMIR` storage change does not mean the self-hosted compiler
shares the same typed table layout, and it does not finish the lowering
boundary. KIR v6 carries canonical binding IDs and UTF-8 byte spans. Its JSON
schema is checked against Go wire fields, and a shared corpus compares the Go
and selfhost validators on representative valid, malformed, and v6 metadata
documents. Selfhost validates decoded documents, converts them to schema-shaped
typed tables, and its ELF/PE lowerers consume checked arena views. Selfhost
source compilation still serializes and reparses KIR. The schema does not
generate Kry row definitions or prove identical in-memory layouts. Treat the
corpus as a wire-contract and validator-parity guard, not proof of a shared
generated representation or full selfhosting.

`Copy` analysis is memoized with `unknown`, `visiting`, `copyable`, and `non-copyable` states. Recursive structural values are conservatively non-copyable. Type equality, display, layout, and serialization use the same recursion/depth guard. Channel send APIs call one runtime and checker transferability predicate, including try and timed variants.

The filesystem policy rejects absolute paths, NUL bytes, parent traversal, symlink components, and reparse/junction-like escapes. Reads and writes resolve relative to the configured root and use `Lstat` checks for every component; writes use a temporary file created in the trusted parent and an atomic rename only after revalidation. The implementation documents the residual limitation of a portable Go user-space check on platforms without an openat-equivalent API and never presents that check as a race-free kernel capability.

Artifacts use `KRYNATIVE6`: a fixed header, language-version metadata, explicit root entry, canonical remaining entry order, each source's package visibility scope, target-neutral checked KIR, exact byte lengths, SHA-256 hashes, compiler identity, and strict trailing-byte rejection. The reader accepts KRYNATIVE5, KRYNATIVE4, and KRYNATIVE3 artifacts. It validates KIR and compares it with the representation regenerated from the embedded sources; older versions have KIR regenerated at load time. Atomic replacement uses `os.CreateTemp`, sync, close, and rename in the same directory.

Workers are result-bearing `Thread[T]` values. Worker code runs in a child execution context with cancellation and budgets. Channel waits select on cancellation and bounded deadlines. Joins never detach a worker. A timed join returns a typed timeout result and leaves the worker joinable; normal shutdown requests cancellation, closes channels, and joins within the configured shutdown deadline.

The compatibility namespace accepts existing unqualified public imports and also accepts `module::name` qualified references. Imports merge only public declarations; standalone files retain file-local visibility, while files under one `kry.toml` share package-private access. Public signatures are checked for inaccessible private types.
