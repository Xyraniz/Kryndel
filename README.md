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
| `kry build file.kry [-o output.kexe]` | Check and package the exact source payload. | `0` |
| `kry fmt [--check\|-w] file.kry` | Format valid source by removing trailing whitespace and enforcing a final newline. | `0` |
| `kry repl` | Start an interactive read-evaluate-print loop. | `0` |
| `kry doctor` | Report native compiler, source, output-directory, and locale readiness. | `0` |
| `kry version` | Print the compiler version. | `0` |
| `kry --help` | Print complete command help. | `0` |

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

The initial type system contains `Int`, `Float`, `Bool`, `String`, `Bytes`, homogeneous `Array[T]`, `Nil`, `Option[T]`, `Result[T, E]`, structs, and enums. The checker validates declarations, assignments, function calls, return types, operators, indexing, builtins, and control-flow conditions before runtime begins. Bare `Array` remains accepted for compatibility and receives a homogeneous element type from its initializer; new code should prefer `Array[T]`.

## Builtins and safety

The authoritative builtin registry defines `print`, `println`, `len`, `bytes`, `string_to_bytes`, `bytes_to_string`, `array_push`, `int`, `float`, `str`, `bool`, `assert`, `assert_eq`, `abs`, `sqrt`, `some`, `none`, `ok`, and `err`. Conversions are explicit, complete, and deterministic: partial numeric parses such as `int("12xyz")` are rejected, UTF-8 conversions validate their input, and `abs(Int minimum)` fails instead of overflowing.

Integer addition, subtraction, multiplication, negation, division, remainder, literal parsing, and allocation-size calculations are checked. Floating-point literals and results must be finite. Arrays, bytes, strings, structs, options, and results are immutable values; concatenation and `array_push` allocate new collections, while function calls pass value representations whose immutable collection storage cannot be modified in place.

## Modules and data types

A source file may import a relative `.kry` module. Resolution is deterministic relative to the importing file, absolute paths and `..` traversal are rejected, cycles are diagnosed, and only declarations marked `pub` are exported. Structs have checked named fields. Enums have checked variants and support exhaustive `match` statements. `Option[T]` and `Result[T, E]` are explicit tagged values rather than untyped error signaling.

## Native execution and artifacts

The implementation is intentionally a small tree-walk toolchain. `check` and `build` share the lexer, parser, module loader, and type checker with `run`, but `check` and `build` never evaluate user expressions. `build` writes the exact source into a deterministic `KRYNATIVE1` container; `run` validates the header, little-endian payload length, and exact file length before parsing the payload through the same native pipeline.

## Repository structure

| Path | Purpose |
| --- | --- |
| `native/kry.c` | Native lexer, parser, checker, runtime, modules, artifacts, formatter, REPL, doctor, and CLI. |
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
