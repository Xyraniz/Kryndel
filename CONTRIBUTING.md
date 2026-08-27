# Contributing to Kryndel

Kryndel has one deliberately small native implementation. A syntax change must consider the lexer, parser, checker, runtime, diagnostics, examples, tests, and documentation. Keep the execution pipeline inside the native implementation; do not add a parallel interpreter or a runtime dependency in another language.

## Local development

A C11 compiler is required to build the executable:

```bash
make test
```

The command-line tool can be exercised directly:

```bash
./tools/kry check examples/hello.kry
./tools/kry run examples/fibonacci.kry
./tools/kry build examples/hello.kry -o /tmp/hello.kexe
./tools/kry run /tmp/hello.kexe
```

## Language changes

Implement a coherent vertical slice. The lexer must produce stable positions, the parser must reject incomplete syntax, the checker must reject invalid programs before execution, and the runtime must emit deterministic English diagnostics. New behavior needs a positive example and focused regression coverage. Changes to the CLI or artifact format must also update the end-to-end integration test.

Preserve determinism. Do not introduce dates, random identifiers, absolute paths in generated output, locale-dependent formatting, or network-dependent tests. Do not commit binaries, `build/`, `.kexe` artifacts, temporary files, or tracked Python source.

## Review checklist

Before opening a change, run `make test`, `make test-sanitized`, `make test-thread-sanitized`, `make test-static`, and `make check-docs`. Review `git diff --check`, confirm that the working tree contains only intentional changes, and verify that the documentation describes the implemented behavior rather than a planned feature. Thread or system changes must update the matching guides in `docs/concurrency.md`, `docs/memory.md`, and `docs/system.md`.

## License

Contributions are distributed under the repository MIT license.
