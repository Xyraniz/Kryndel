# Kryndel testing guide

Tests use only Python's standard library and run without network, services,
locale assumptions, or a desktop session.

```bash
PYTHONPATH=. python3 -m py_compile kryndel/*.py tests/test_kryndel.py
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The suite contains 78 tests and covers:

| Layer | Contract |
| --- | --- |
| Lexer/parser | comments, literals, spans, precedence, recovery, enum payload syntax, match syntax |
| Checker | nominal structs/enums, payload arity/types, bindings, imports, module interfaces, visibility, diagnostics, exhaustiveness |
| Compiler/VM | deterministic `MAKE_ENUM`, matching, extraction, equality, immutable `BytesValue`, strict UTF-8, octet validation, Bytes concatenation/indexing, assertions, malformed metadata, runtime errors |
| Packages and modules | manifest subset, semver, local/path registry, transitives, lock ordering, cycles, checksums, traversal, nested module resolution, ambiguity, exports, private symbols, deterministic linking |
| CLI/artifacts | human/JSON errors, exit codes, init/add/install/list/tree, build/run/inspect, KEXE checksums, `host-report`, `doc`, `pack`, and structured `kry test` results |

Failure tests assert stable codes and useful spans rather than entire prose
paragraphs. Happy-path tests run through `compile_source` or the project-aware
`compile_project` and `VM` so parser, checker, compiler, module linker, and
runtime mismatches are visible together. Module tests create only local path
packages, refresh their SHA-256 checksum, install offline, and assert concrete
qualified function names and output. Bytes tests execute the visible constructors,
strict decoder, octet indexing, range errors, and source-level stdlib wrappers;
they also compare deterministic value fixtures and compiled bytecode. Testing
assertions are available through `stdlib/testing/testing.kry`; failures use
`KRY6401` and `KRY6402`. `kry test --format json` runs each test in a fresh
compiled VM and reports deterministic per-test status without tracebacks.
Determinism tests compile identical source and resolve identical package/module
graphs twice.

Temporary package registries are created under temporary directories. Tests
never modify a user's package registry or install Python dependencies. The
module graph tests additionally assert `KRY5013` through `KRY5016` and
`KRY3050` through `KRY3052`, including package/module context for import errors.

## Tests written in Kryndel

A project may place source tests below `tests/` and mark zero-argument functions
with `@test`:

```kryndel
@test
fn test_answer() -> Void {
    let answer: Int = 40 + 2
    println(answer)
}
```

`kry test` discovers files in deterministic path order and functions in source
order, then executes each function through the VM. The discovery format and
result names are language-independent even though the initial runner is still
implemented by the Python bootstrap. The runner isolates each source file,
continues through known compile/runtime failures, returns exit code 1 if any
case fails, and can emit a versioned JSON result document. Unknown host errors
are not converted into success and remain visible to the caller.
