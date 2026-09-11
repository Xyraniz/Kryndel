# Kryndel

Kryndel is a small, readable language for structured programming. This repository ships one coherent Go implementation; it lexes, parses, statically checks, validates an intermediate representation, and executes Kryndel source without C, Python, Rust, Node.js, a hidden interpreter, or a second runtime.

> The normal user-facing entry point is `tools/kry`. The repository contains no bootstrap directory and no tracked Python source.

## Quick start

A Go 1.22+ toolchain is required only to build from source. Released executables are self-contained and do not require Go or any external runtime. The repository launcher forwards commands to the built executable and never searches for a host-language compiler.

```bash
make
./tools/kry run examples/hello.kry
./tools/kry run examples/fibonacci.kry
./tools/kry check examples/control_flow.kry
./tools/kry build examples/hello.kry -o /tmp/hello.kexe
./tools/kry run /tmp/hello.kexe
./tools/kry doctor
```

| Command | Behavior | Success code |
| --- | --- | ---: |
| `kry check file.kry` | Lex, parse, resolve modules, and statically validate without executing user code. | `0` |
| `kry run file.kry` | Check and execute a source file. | `0` |
| `kry run file.kexe` | Validate and execute a deterministic native artifact. | `0` |
| `kry build file.kry [-o output.kexe]` | Check and package the root and imported source graph into a deterministic bundle; `--format=exe|pe|elf` emits a native PE/ELF entrypoint for supported targets. | `0` |
| `kry emit file.kry --format=llvm-ir` | Emit checked LLVM-compatible textual IR for inspection and downstream toolchains. | `0` |
| `kry inspect binary` | Inspect PE or ELF headers without executing the binary. | `0` |
| `kry new PROJECT` / `kry init` | Create a manifest and starter source tree. | `0` |
| `kry add/remove PACKAGE [VERSION]` | Edit `[dependencies]` deterministically. | `0` |
| `kry install` / `kry update` | Resolve registry versions, verify SHA-256 archives, vendor sources, and write `kry.lock`. | `0` |
| `kry search TERM` | Search the configured HTTP registry. | `0` |
| `kry package` / `kry publish` | Build a reproducible archive or upload it to the configured registry. | `0` |
| `kry cache clean` / `kry registry serve ROOT` | Manage the local cache or serve a local registry. | `0` |
| `kry fmt [--check\|-w] file.kry` | Format valid source with deterministic whitespace, indentation, and final newline rules. | `0` |
| `kry repl` | Start an interactive read-evaluate-print loop. | `0` |
| `kry doctor` | Verify the executable, standard-library registry, limits, and runtime capabilities. | `0` |
| `kry version` | Print the compiler version. | `0` |
| `kry --help` | Print complete command help. | `0` |
| `kry --json <command>` | Emit machine-readable diagnostics with stable code and category fields. | command-dependent |
| `kry --restricted ROOT <command>` | Restrict filesystem APIs to an existing sandbox root. | command-dependent |
| `kry --max-source BYTES <command>` | Reject source input above the configured byte limit. | command-dependent |
| `kry --max-artifact BYTES <command>` | Reject artifact input above the configured byte limit. | command-dependent |

Invalid command usage returns `2`; source, static, runtime, artifact, resource, and I/O failures return `1`. A missing built executable returns `69`; distributed binaries do not depend on a compiler.

## Language overview

Kryndel uses braces for blocks and familiar infix expressions. Bindings are immutable by default, while `let mut` is required for assignment. Function parameters and return types are explicit, ordinary declarations infer their types, and `if`, `while`, `assert`, `&&`, and `||` require `Bool` rather than applying implicit truthiness.

```kryndel
fn factorial(n: Int) -> Int {
    if n <= 1 {
        return 1
    }
    return n * factorial(n - 1)
}

let answer: Int = factorial(6)
println("factorial = " + str(answer))
assert(answer == 720)
```

The type system contains `Int`, `Float`, `Bool`, `String`, `Bytes`, homogeneous `Array[T]`, immutable `Map[K,V]`, immutable `Set[T]`, validated `Json`, non-copyable `WebSocket`, `Nil`, `Option[T]`, `Result[T, E]`, `Channel[T]`, `Thread[T]`, structs, and enums. The checker validates declarations, assignments, function calls, return types, operators, indexing, builtins, control-flow conditions, worker names, method receivers, loop iterables, unsafe boundaries, and thread transfer types before runtime begins. Bare `Array` remains accepted for compatibility and receives a homogeneous element type from its initializer; new code should prefer `Array[T]`.

## Builtins and safety

The authoritative builtin registry covers output, conversion, Option/Result inspection, UTF-8 text, bytes, immutable arrays, maps, sets, checked mathematics, FIFO channels, cooperative cancellation, typed filesystem access, environment lookup, validated JSON, bounded HTTP/TLS, authenticated HTTP, RFC 6455 WebSockets, and shell-free process launch. Conversions are explicit, complete, and deterministic: partial numeric parses such as `int("12xyz")` are rejected, UTF-8 conversions validate their input, and `abs(Int minimum)` fails instead of overflowing. Network responses, JSON documents, process output, collection sizes, and WebSocket frames are bounded by runtime limits. Each registered builtin has an effect, ownership, failure, checker, runtime, documentation, and version contract validated by `doctor`.

Integer addition, subtraction, multiplication, negation, division, remainder, literal parsing, and allocation-size calculations are checked. Floating-point literals and results must be finite. Arrays, bytes, strings, structs, options, and results are immutable values; concatenation and `array_push` allocate new collections, while function calls pass value representations whose immutable collection storage cannot be modified in place. `fs_read_text` and `fs_write_text` provide typed UTF-8 file access, and `env_get` returns an explicit `Option[String]`. `defer { ... }` executes cleanup in reverse registration order on normal return, propagated errors, runtime failures, and cancellation. `unsafe { ... }` is an explicit diagnostic boundary. `value?` unwraps `Option`/`Result` only when the enclosing function has the matching success/error type; it never silently converts unrelated values.

## Modules and data types

A source file may import a relative `.kry` module or a vendored package whose entrypoint is `vendor/<name>/main.kry`. Resolution is deterministic relative to the importing file, absolute paths and `..` traversal are rejected, cycles are diagnosed, and only declarations marked `pub` are exported. Structs have checked named fields, `impl Type { ... }` provides receiver methods, and `for item in iterable { ... }` iterates arrays, strings, maps, and sets. Enums have checked variants and support exhaustive `match` statements. `Option[T]` and `Result[T, E]` are explicit tagged values rather than untyped error signaling.

## Threads

Kryndel provides OS-backed named worker functions and FIFO channels with configurable capacity. A worker is declared as a zero-argument function and started with `thread_spawn("worker")`; `thread_send`, `thread_try_send`, timed send, receive, and timed receive synchronize through the channel, while `thread_join_timeout` and `thread_cancel` make shutdown bounded and cooperative. Only recursively Copy values cross a channel, and the checker propagates worker-safe restrictions through the reachable call graph. See [the language reference](docs/language.md), [the concurrency contract](docs/concurrency.md), and [the design record](docs/design.md).

## Native execution and artifacts

The implementation is a modular toolchain. `check` and `build` share the lexer, parser, module loader, type checker, and validated IR with `run`, but `check` and `build` never evaluate user expressions. The default `build` writes a deterministic, atomically replaced `KRYNATIVE3` bundle containing the exact root and imported source bytes, logical paths, compiler/target metadata, and SHA-256 hashes; `run` validates every field before executing the embedded graph without consulting external modules. Native output currently provides a real minimal PE32+ Windows entrypoint for `windows-x64`/`windows-arm64` metadata and an ELF64 Linux `linux-x64` entrypoint; unsupported Mach-O targets fail explicitly rather than producing mislabeled text.

## Repository structure

| Path | Purpose |
| --- | --- |
| `cmd/kry` | Portable Go CLI, REPL, formatter, doctor, and release entry point. |
| `internal/kry` | Modular source, lexer, parser, AST, types, checker, IR, runtime, sandbox, modules, artifacts, builtins, native backend, package manager, WebSocket client, and Windows boundary. |
| `std/` | Typed standard-library wrappers for environment, JSON, and HTTP. |
| `packages/discord` | Real Discord Gateway/API package using HTTPS bearer requests and RFC 6455 WebSockets; tokens are never printed. |
| `tools/kry` | Thin launcher for the already-built self-contained executable. |
| `examples/` | Positive Kryndel programs. |
| `tests/` | Integration, static, sanitizer, and documentation checks. |
| `docs/` | Language, architecture, module, type, diagnostics, standard-library, testing, and release contracts. |

## Verification

```bash
make test
make test-static
make test-race
make coverage
make release
```

The test matrix checks representative success and failure paths, deterministic artifacts, modules, enums, concurrency, sandbox boundaries, malformed inputs, JSON diagnostics, formatter idempotence, fuzz smoke cases, race detection, coverage, and cross-compilation. The implementation and build graph contain no production C, Python, Rust, or Node.js dependency.
