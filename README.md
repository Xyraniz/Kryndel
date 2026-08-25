# Kryndel

> A strongly typed language for building native desktop software.

Kryndel is an experimental programming language and compiler toolkit written from scratch in Python's standard library. It is designed around explicit types, predictable semantics, readable diagnostics, deterministic bytecode, and a small declarative UI model.

The project is intentionally self-contained. It does not require third-party Python packages, a package manager, a web service, or a downloaded compiler toolchain. The current implementation compiles Kryndel source into a portable Kryndel bytecode module and executes it in the included virtual machine.

## Project status

Kryndel is an early language implementation, not a finished replacement for Rust, C++, or Qt. The front end, type checker, compiler, bytecode format, virtual machine, command-line interface, artifact checksum validation, diagnostics, tests, and declarative UI tree are implemented in this repository.

The native desktop layer is deliberately staged. The current `ui` module builds and renders a deterministic UI tree in the bundled runtime. It does not yet open operating-system windows. That boundary is explicit so the language semantics can become stable before platform-specific windowing code is added.

## A first program

Create `hello.kry`:

```kryndel
let greeting: String = "Hello from Kryndel"
println(greeting)
```

Run it directly from a checkout:

```bash
PYTHONPATH=. python3 -m kryndel run hello.kry
```

The output is:

```text
Hello from Kryndel
```

## Language sample

Kryndel uses immutable bindings by default. Reassignment requires `let mut`. Function parameters and return types are explicit.

```kryndel
fn fibonacci(n: Int) -> Int {
    if n <= 1 {
        return n
    }

    let mut previous: Int = 0
    let mut current: Int = 1
    let mut index: Int = 2

    while index <= n {
        let next: Int = previous + current
        previous = current
        current = next
        index = index + 1
    }

    return current
}

println(fibonacci(10))
```

The compiler rejects invalid programs before execution:

```kryndel
let count: Int = "not an integer"
```

The diagnostic includes a source location, a stable error code, a highlighted span, and a correction hint.

## Declarative UI model

The first UI layer is platform-neutral and deterministic. It provides a stable language-level shape for windows, containers, labels, buttons, callbacks, and rendering. The current runtime renders the tree as text, which keeps tests independent of a desktop session.

```kryndel
let window: UiNode = ui.window("Kryndel Dashboard", 960, 600)
let content: UiNode = ui.vbox(window)
let title: UiNode = ui.label(content, "Welcome to Kryndel")
let button: UiNode = ui.button(content, "Continue")
ui.on_click(button, "on_continue")
ui.show(window)
ui.run()
```

Current output:

```text
Window (title='Kryndel Dashboard' width=960 height=600)
  VBox
    Label (text='Welcome to Kryndel')
    Button (text='Continue') [on_click=on_continue]
```

The API is kept deliberately small until event ownership, layout lifetime, thread rules, resource loading, and platform backends are specified rather than guessed.

## What is implemented

| Area | Current behavior |
| --- | --- |
| Lexer | Identifiers, keywords, integers, floating-point values, strings, nested block comments, line comments, operators, and source spans. |
| Parser | Recursive-descent parser with precedence-aware expressions, functions, blocks, conditions, loops, returns, `break`, and `continue`. |
| Static typing | `Int`, `Float`, `Bool`, `String`, `UiNode`, `Void`, immutable bindings, mutable bindings, function signatures, assignment checks, and operator checks. |
| Compiler | AST-to-bytecode compiler with lexical compiler scopes, functions, calls, jumps, short-circuit boolean operators, and deterministic constants. |
| Runtime | Stack-based virtual machine, function calls, recursion, arithmetic, comparisons, conversions, strings, clock access, and guarded stack operations. |
| Diagnostics | Stable `KRY` error codes, line and column locations, highlighted source spans, notes, and help text. |
| UI | Platform-neutral window and widget tree with containers, text, buttons, callbacks, deterministic rendering, and runtime validation. |
| Artifacts | Checksummed `KEXE` portable Kryndel executable packages with deterministic JSON bytecode payloads. |
| Tooling | `check`, `run`, `build`, `inspect`, `dump`, and `--version` commands. |

## Type rules

The initial type system favors explicit failures over implicit conversions. An integer can be promoted to a floating-point value where a floating-point operand is expected. Other incompatible assignments and calls are rejected.

```kryndel
let whole: Int = 7
let decimal: Float = whole
let enabled: Bool = true
let title: String = "Kryndel"
```

`/` returns `Float`, even when both operands are integers. This makes division behavior visible in function signatures and avoids silently presenting fractional results as integers.

The current implementation intentionally does not claim Rust's ownership, borrowing, lifetime, trait, or memory-safety model. Those features require a separate language specification and a larger verification effort.

## Command-line interface

All commands work from the repository root without installation:

```bash
# Parse and type-check a source file
PYTHONPATH=. python3 -m kryndel check examples/hello.kry

# Compile and execute a source file
PYTHONPATH=. python3 -m kryndel run examples/fibonacci.kry

# Print deterministic bytecode JSON
PYTHONPATH=. python3 -m kryndel dump examples/hello.kry

# Build a checksummed Kryndel artifact
PYTHONPATH=. python3 -m kryndel build examples/hello.kry -o hello.kexe

# Inspect an artifact without executing it
PYTHONPATH=. python3 -m kryndel inspect hello.kexe

# Show the compiler version
PYTHONPATH=. python3 -m kryndel --version
```

## KEXE artifacts

A `.kexe` file is a portable Kryndel executable package, not a Windows PE or Linux ELF binary. It contains a versioned header, a SHA-256 checksum, and the Kryndel bytecode module. The bundled runtime validates the header, payload length, and checksum before loading it.

This format gives Kryndel a stable packaging boundary without pretending that a virtual-machine artifact is already native machine code. A future native backend can preserve the source and module contracts while adding platform-specific executable emission.

## Repository layout

```text
Kryndel/
├── kryndel/
│   ├── __init__.py          Public package surface.
│   ├── __main__.py          `python -m kryndel` entry point.
│   ├── artifact.py          Checksummed KEXE package format.
│   ├── ast.py               Abstract syntax tree nodes.
│   ├── bytecode.py          Portable bytecode data model and JSON format.
│   ├── cli.py               Command-line interface.
│   ├── compiler.py          Source-to-bytecode compiler.
│   ├── diagnostics.py       Source diagnostics and rendering.
│   ├── parser.py            Recursive-descent parser.
│   ├── source.py            Source files and span calculation.
│   ├── tokens.py            Lexer and token definitions.
│   ├── type_checker.py      Static name and type checker.
│   ├── types.py             Type definitions and function signatures.
│   ├── version.py           Version metadata.
│   └── vm.py                Bytecode virtual machine and standard library.
├── examples/                Small runnable Kryndel programs.
├── tests/                   Standard-library-only automated tests.
├── docs/                    Design and language notes.
├── tools/                   Local development helpers.
├── .github/workflows/       Continuous integration definition.
├── LICENSE                  MIT license.
└── README.md                Project documentation.
```

## Reproducible testing

Kryndel uses Python's built-in `unittest` runner. No test dependency is required.

```bash
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The suite covers lexing, comments and literals, precedence, conversions, recursion, loops, short-circuit behavior, type failures, immutable assignment, runtime stack traces, UI rendering, bytecode determinism, and KEXE round trips with checksum validation.

## Design boundaries

Kryndel keeps the following boundaries explicit:

1. The lexer owns characters and source positions.
2. The parser owns syntax and produces an AST without executing user code.
3. The type checker owns names, mutability, signatures, and operator legality.
4. The compiler owns the translation from typed AST nodes to bytecode.
5. The VM owns runtime values, calls, stack safety, standard functions, and UI nodes.
6. The artifact writer owns serialization, checksums, and package validation.

This separation makes it possible to replace one backend without rewriting the language front end. It also gives tests a stable place to detect regressions.

## Roadmap

The next milestones are ordered by semantic risk rather than visual complexity:

- Add explicit `struct`, `enum`, `Option`, and `Result` types.
- Introduce a typed intermediate representation between the AST and bytecode.
- Add first-class closures with an explicit capture model.
- Specify ownership and borrowing rules before implementing them.
- Add source maps and a debugger protocol.
- Define a platform backend contract for native executable generation.
- Implement a real desktop backend behind the platform-neutral UI API.
- Add a formatter, language server, package manifest, and cross-platform release process.

## Contributing

Changes should preserve the separation between language phases and include a regression test for every new semantic rule. Keep public behavior documented in `README.md` and the files under `docs/`. Do not add a third-party dependency for functionality that can remain small, deterministic, and maintainable in the standard library.

## License

Kryndel is released under the MIT License. See [`LICENSE`](LICENSE).
