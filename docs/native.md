# Native Kryndel core

Kryndel now includes a native execution path for its productive core. The implementation lives in `native/kry.c` and is compiled as one C11 translation unit. Once `build/kry` has been produced, the executable uses only the platform C runtime; it does not import Python, invoke Rust, load a scripting runtime, or start a subprocess to execute a Kryndel program.

This is an independent runtime and parser, not a wrapper around `kryndel/*.py`. The existing Python implementation remains in the repository as the stage-0 reference implementation for the broader language and its historical bytecode contracts. It is not required by the native command path.

## Build and run

A C compiler is required once to build the executable. GCC is the default compiler, and another compiler can be selected with `CC`:

```bash
make native
./build/kry check examples/hello.kry
./build/kry run examples/fibonacci.kry
./build/kry build examples/hello.kry -o /tmp/hello.kexe
./build/kry run /tmp/hello.kexe
```

For a convenient source checkout workflow, `tools/kry-native` compiles the binary on demand and then forwards the command without Python:

```bash
./tools/kry-native run examples/hello.kry
./tools/kry-native --version
```

The native artifact format is deliberately small and self-contained. It starts with `KRYNATIVE1`, stores the source length as a little-endian 64-bit integer, and stores the original UTF-8 source bytes. The native runtime reparses the source when loading it. This format is separate from the bootstrap KEXE v1 container; the native command must not be described as a reader for historical Python-produced KEXE files.

## Supported core

The native core supports the useful first programming loop: UTF-8 strings, integers, floats, booleans, `nil`, immutable arrays, indexing, `len`, `array_push`, arithmetic, comparisons, short-circuit boolean operators, `let` and `let mut` syntax, reassignment, functions with typed-looking parameter and return annotations, recursion, `if`/`else`, `while`, `break`, `continue`, `return`, `print`, `println`, assertions, `abs`, `sqrt`, and strict byte conversions through `Bytes` builtins.

The parser accepts the existing statement-oriented syntax without requiring semicolons. Type annotations are retained as part of the source language syntax, while the native core currently reports runtime type errors rather than reproducing the full bootstrap static checker. This keeps the native implementation small and useful while preserving a clear boundary for the next native compiler milestone.

The following program runs without Python:

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

## Explicit boundaries

Structs, enums, pattern matching, imports, package resolution, the complete static checker, the full v1 bytecode compiler, and the full host capability table remain on the stage-0 route. When the native core encounters one of those declarations it fails with a source diagnostic instead of silently delegating to Python. This makes unsupported functionality visible and keeps the native path honest.

The native runtime is a tree-walk interpreter rather than a self-hosted compiler. It is therefore the first independent product path, not the final self-hosting gate. The next replacement milestones are a native AST/type checker, bytecode emitter, module loader, and a native implementation of the established KEXE contract.
