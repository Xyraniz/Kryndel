# Contributing to Kryndel

Kryndel is a language project. A small syntax change can affect diagnostics, bytecode, runtime behavior, examples, and future compatibility, so contributions should explain the semantic rule before changing implementation code.

## Development environment

The project requires Python 3.10 or newer and uses only the standard library at runtime. From a checkout, the test command is:

```bash
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The examples can be checked with:

```bash
PYTHONPATH=. python3 -m kryndel check examples/hello.kry
PYTHONPATH=. python3 -m kryndel check examples/fibonacci.kry
PYTHONPATH=. python3 -m kryndel check examples/ui_tree.kry
```

## Change process

Begin with a short design note in the relevant file under `docs/`. Describe the syntax, type behavior, runtime behavior, failure behavior, and compatibility impact. If the feature is not fully specified, keep it out of the parser rather than accepting syntax with undefined semantics.

Implement the smallest coherent vertical slice: tokens, AST, type checking, compiler, runtime, documentation, and tests. Keep phase boundaries intact. The lexer should not perform type checks, the parser should not execute code, the type checker should not perform side effects, and the VM should not reinterpret source syntax.

Every bug fix needs a regression test. Valid programs should be accompanied by at least one invalid program when the change affects diagnostics or type rules. Tests should assert stable diagnostic codes and important message fragments instead of depending on an entire rendered paragraph.

## Code style

Use the standard library and clear type annotations. Prefer small functions with one responsibility. Keep public names descriptive and avoid hidden global state. Preserve deterministic output: no timestamps, random identifiers, machine-specific paths, or locale-dependent formatting should enter bytecode or artifacts.

Comments should explain an invariant or a non-obvious tradeoff. They should not restate the next line of code. Public behavior belongs in the language reference or README rather than only in comments.

## Pull requests

A pull request should state what changed, why the semantic rule is correct, and how it was tested. Include the complete test command and any example output that helps reviewers understand user-visible behavior. Do not claim native executable generation, memory safety, ownership, or production readiness unless the implementation and tests actually support the claim.

## License

By contributing, you agree that your contribution is available under the MIT License in this repository.
