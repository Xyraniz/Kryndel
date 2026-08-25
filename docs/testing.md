# Kryndel Testing Guide

Enum tests cover the keyword, AST values, declaration-before-use, nominal checking, equality, ordering rejection, stable spans, deterministic `MAKE_ENUM`, and malformed metadata. `examples/enums.kry` is checked in CI.

Kryndel treats tests as part of the language definition. A feature is not considered implemented because a happy-path example runs; it also needs invalid-input coverage, deterministic behavior, and a clear failure mode.

## Running the suite

From the repository root:

```bash
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The suite uses only Python's standard library. It does not download packages, start a server, open a desktop session, or rely on the host locale.

## Test layers

| Layer | What it verifies |
| --- | --- |
| Lexer | Token kinds, literal values, comments, nested blocks, and source positions. |
| Parser | Precedence, associativity, statement boundaries, function syntax, blocks, struct declarations and constructors, member access, and recovery. |
| Type checker | Unknown names and types, duplicate declarations and fields, mutability, signatures, constructors, field compatibility, member access, operators, conditions, and return values. |
| Compiler | Internal binding slots, function metadata, declaration-ordered `MAKE_STRUCT` lowering, `GET_FIELD`, jump patching, short-circuit paths, and deterministic constants and metadata. |
| VM | Stack discipline, recursion, arithmetic, conversions, `StructValue` construction and field loads, malformed-bytecode failures, call traces, and UI rendering. |
| Artifact | Header, payload length, checksum, serialization round trip, and rejection of invalid packages. |
| CLI | Commands use the same compiler pipeline as the library and return a non-zero code for failures. |

## Regression expectations

Every compiler bug should become a small source program that fails before the fix and passes after it. Prefer tests that exercise the public pipeline:

```python
module = compile_source(source, "case.kry")
result = VM(module).run()
```

This catches mismatches between individual components. Unit-level tests are still useful for a lexer edge case or a bytecode instruction, but they should not replace end-to-end coverage.

## Determinism

Compiling the same source should produce equivalent function and instruction data regardless of the input filename. The module name is allowed to record the filename for diagnostics, but compiler output should not contain timestamps, random identifiers, host paths, or process-specific values.

## Failure tests

Failure tests should assert the stable diagnostic code and the useful part of the message, not an entire paragraph. This allows wording to improve without hiding a regression in the error category.

```python
with self.assertRaises(DiagnosticError) as context:
    compile_source('let value: Int = "wrong"', "types.kry")

self.assertIn("KRY3003", str(context.exception))
```

Runtime failures should include a function name and call stack when the failure occurs inside a call. Struct tests must also verify that malformed `MAKE_STRUCT` or `GET_FIELD` bytecode produces an explicit `RuntimeKryndelError`, never a leaked `KeyError`, `IndexError`, or internal traceback. Artifact tests should mutate bytes and verify that checksum validation rejects the result.

## Adding a language feature

Struct coverage in this repository includes a valid constructor and field access, use before declaration, nested/function use, missing fields, unknown fields with precise spans, duplicate fields, unknown field types, field type mismatches, invalid member assignment, deterministic declaration-order metadata, and malformed-bytecode runtime behavior.

A new feature should be added in this order:

1. Write the semantic rule in `docs/language.md`.
2. Add or update token and AST structures if syntax requires it.
3. Add valid and invalid source cases.
4. Implement type-checking behavior.
5. Implement compiler and VM behavior.
6. Add a focused example when the feature is user-facing.
7. Run the complete suite and inspect the CLI output.

This order keeps syntax from becoming a promise before semantics exist.
