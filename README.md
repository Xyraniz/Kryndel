# Kryndel

Kryndel is a small, readable language for structured programming. This repository ships one native C11 implementation in `native/kry.c`; it lexes, parses, statically checks, and evaluates Kryndel source without a Python bootstrap, a hidden interpreter, or a second runtime.

> The normal user-facing entry point is `tools/kry`. The repository contains no bootstrap directory and no tracked Python source.

## Quick start

A C11 compiler is required only for the first native build. After that, the launcher rebuilds `build/kry` when the source is newer and forwards every command to the native executable.

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
| `kry build file.kry [-o output.kexe]` | Check and package the root and imported source graph into a deterministic bundle. | `0` |
| `kry fmt [--check\|-w] file.kry` | Format valid source with deterministic whitespace, indentation, and final newline rules. | `0` |
| `kry repl` | Start an interactive read-evaluate-print loop. | `0` |
| `kry doctor` | Report native compiler, source, output-directory, and locale readiness. | `0` |
| `kry version` | Print the compiler version. | `0` |
| `kry --help` | Print complete command help. | `0` |
| `kry --json <command>` | Emit machine-readable diagnostics with stable code and category fields. | command-dependent |
| `kry --restricted ROOT <command>` | Restrict filesystem APIs to an existing sandbox root. | command-dependent |
| `kry --max-source BYTES <command>` | Reject source input above the configured byte limit. | command-dependent |
| `kry --max-artifact BYTES <command>` | Reject artifact input above the configured byte limit. | command-dependent |

Invalid command usage returns `2`; source, static, runtime, artifact, and I/O failures return `1`; the launcher returns `69` when no C11 compiler can be found.

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

The initial type system contains `Int`, `Float`, `Bool`, `String`, `Bytes`, homogeneous `Array[T]`, `Nil`, `Option[T]`, `Result[T, E]`, `Channel[T]`, `Thread[T]`, structs, and enums. The checker validates declarations, assignments, function calls, return types, operators, indexing, builtins, control-flow conditions, worker names, and thread transfer types before runtime begins. Bare `Array` remains accepted for compatibility and receives a homogeneous element type from its initializer; new code should prefer `Array[T]`.

## Builtins and safety

The authoritative builtin registry covers output, conversion, Option/Result inspection, UTF-8 text, bytes, immutable arrays, checked mathematics, FIFO channels, cooperative cancellation, typed filesystem access, and environment lookup. Conversions are explicit, complete, and deterministic: partial numeric parses such as `int("12xyz")` are rejected, UTF-8 conversions validate their input, and `abs(Int minimum)` fails instead of overflowing. Each registered builtin has an effect, ownership, failure, checker, runtime, documentation, and version contract validated by `doctor`.

Integer addition, subtraction, multiplication, negation, division, remainder, literal parsing, and allocation-size calculations are checked. Floating-point literals and results must be finite. Arrays, bytes, strings, structs, options, and results are immutable values; concatenation and `array_push` allocate new collections, while function calls pass value representations whose immutable collection storage cannot be modified in place. `fs_read_text` and `fs_write_text` provide typed UTF-8 file access, and `env_get` returns an explicit `Option[String]`; process, networking, terminal, and FFI operations remain outside the stable boundary.

## Modules and data types

A source file may import a relative `.kry` module. Resolution is deterministic relative to the importing file, absolute paths and `..` traversal are rejected, cycles are diagnosed, and only declarations marked `pub` are exported. Structs have checked named fields. Enums have checked variants and support exhaustive `match` statements. `Option[T]` and `Result[T, E]` are explicit tagged values rather than untyped error signaling.

## Threads

Kryndel provides OS-backed named worker functions and FIFO channels with configurable capacity. A worker is declared as a zero-argument function and started with `thread_spawn("worker")`; `thread_send`, `thread_try_send`, timed send, receive, and timed receive synchronize through the channel, while `thread_join_timeout` and `thread_cancel` make shutdown bounded and cooperative. Only recursively Copy values cross a channel, and the checker propagates worker-safe restrictions through the reachable call graph. See [the language reference](docs/language.md), [the concurrency contract](docs/concurrency.md), and [the design record](docs/design.md).

## Native execution and artifacts

The implementation is intentionally a small tree-walk toolchain. `check` and `build` share the lexer, parser, module loader, and type checker with `run`, but `check` and `build` never evaluate user expressions. `build` writes a deterministic, atomically replaced `KRYNATIVE2` bundle containing the exact root and imported source bytes, logical paths, compiler/target metadata, and SHA-256 hashes; `run` validates every field before executing the embedded graph without consulting external modules.

## Repository structure

| Path | Purpose |
| --- | --- |
| `native/kry.c` | Native lexer, parser, checker, runtime, modules, formatter, REPL, doctor, and CLI. |
| `native/kry_artifacts.inc` | Single included artifact reader/writer component with strict KRYNATIVE2 validation. |
| `tools/kry` | Portable on-demand C11 builder and launcher. |
| `examples/` | Positive Kryndel programs. |
| `tests/` | Integration, static, sanitizer, and documentation checks. |
| `docs/` | Language, architecture, module, type, diagnostics, standard-library, testing, and release contracts. |

## Verification

```bash
make test
make test-sanitized
make test-static
make check-docs
```

The test matrix uses the same executable exposed to users, checks representative success and failure paths, verifies deterministic artifacts, exercises module and enum behavior, and keeps the native implementation free of a Python bootstrap or a parallel interpreter.
