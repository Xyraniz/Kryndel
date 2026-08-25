# Kryndel testing guide

Tests use only Python's standard library and run without network, services,
locale assumptions, or a desktop session.

```bash
PYTHONPATH=. python3 -m py_compile kryndel/*.py tests/test_kryndel.py
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The suite contains more than forty tests and covers:

| Layer | Contract |
| --- | --- |
| Lexer/parser | comments, literals, spans, precedence, recovery, enum payload syntax, match syntax |
| Checker | nominal structs/enums, payload arity/types, bindings, imports, diagnostics, exhaustiveness |
| Compiler/VM | deterministic `MAKE_ENUM`, matching, extraction, equality, malformed metadata, runtime errors |
| Packages | manifest subset, semver, local/path registry, transitives, lock ordering, cycles, checksums, traversal |
| CLI/artifacts | human/JSON errors, exit codes, init/add/install/list/tree, build/run/inspect, KEXE checksums |

Failure tests assert stable codes and useful spans rather than entire prose
paragraphs. Happy-path tests run through `compile_source` and `VM` so parser,
checker, compiler, and runtime mismatches are visible together. Determinism
tests compile identical source with different filenames and resolve identical
package graphs twice.

Temporary package registries are created under temporary directories. Tests
never modify a user's package registry or install Python dependencies.
