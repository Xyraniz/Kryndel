# Kryndel

Kryndel 0.1.0 — First Light is a dependency-free language bootstrap. The
compiler and VM are currently written in Python's standard library. Kryndel is
not self-hosted yet; this repository is building the stable interfaces needed
to replace that bootstrap over time.

## Current language

The front end supports explicit primitive types, immutable/mutable bindings,
functions, recursion, control flow, nominal structs, and nominal algebraic
enums. Structs and enum values are checked before execution and compiled to
deterministic portable bytecode.

Unit and positional enum variants are supported:

```kryndel
enum Message {
    Quit
    Move(Int, Int)
    Text(String)
}

let message: Message = Message.Move(10, 20)
match message {
    Message.Quit => println("quit")
    Message.Move(x, y) => println(x + y)
    Message.Text(text) => println(text)
}
```

`match` supports enum variants, positional bindings, blocks, `_`, ordered
branches, duplicate detection, and exhaustiveness diagnostics. It does not yet
support arbitrary patterns, struct destructuring, guards, OR patterns, macros,
generics, ownership, borrowing, or lifetimes.

The first Kryndel-native collection values are immutable homogeneous arrays and
fixed-width tuples: `[1, 2]`, `(1, "two")`, `len(value)`, `left + right`,
`array_push(value, item)`, and safe `value[index]`. `Bytes` is an immutable nominal octet sequence. The
Kryndel-visible APIs `bytes(Array)`, `string_to_bytes(String)`, and
`bytes_to_string(Bytes)` construct, encode, and strictly decode it; `len` counts
octets, `Bytes[index]` returns an `Int` in `0..255`, and `Bytes + Bytes`
concatenates without normalization or lossy replacement. `Option` and `Result` are ordinary non-generic enums in `stdlib/core`; their Kryndel-native modules
expose constructors, predicates, and total `unwrap_or`/`get_or` fallback
accessors. `stdlib/core/data.kry` also exposes bounded String/Bytes slices, a
balanced string builder, and nominal Span/Token/AST/diagnostic records for the
future toolchain. `stdlib/core/lexer.kry` provides a source-level lexer seam for
the current keywords, literals, comments, operators, spans, and recovery cases.
`stdlib/core/parser.kry` consumes those tokens for a tested AST subset covering
struct declarations, typed lets, literals, members, calls, and struct literals.
`stdlib/core/checker.kry` validates that subset and resolves normalized module
graphs with deterministic missing, duplicate, and cycle diagnostics. `stdlib/core/compiler.kry` lowers the same subset into bytecode records accepted by the source verifier. `stdlib/core/runtime.kry` executes the resulting subset end to end with stack, locals, struct values, and builtin print calls. `tools/kry-seed` emits and runs a raw x86_64 Linux ELF exit-0 seed without Python, `as`, or `ld`. `stdlib/core/format.kry` now provides the conservative trailing-whitespace and final-newline formatter contract in source, and `tools/kry-format` exposes it as a no-Python check/rewrite CLI.
These source modules execute through the Python bootstrap;
the compiler and VM remain Python implementations.


## Diagnostics

Every compiler diagnostic has a stable `KRY` code, severity, source file,
primary span, optional secondary spans, message, notes, help, and an optional
correction suggestion. Lexer, parser, name/type, bytecode/runtime, and package
errors use separate code ranges. Human output remains the default:

```bash
PYTHONPATH=. python3 -m kryndel check examples/enums.kry --format human
PYTHONPATH=. python3 -m kryndel check examples/enums.kry --format json
```

JSON is deterministic and intended for editors and CI. The CLI returns zero on
success and one on compilation, runtime, artifact, or package failure.

## Local Kryndel packages

Kryndel packages are not Python packages and the package manager never calls
pip. A project uses the strict manifest subset documented in
[`docs/specs/manifest-v1.md`](docs/specs/manifest-v1.md):

```toml
[package]
name = "demo"
version = "0.1.0"
edition = "2026"

[dependencies]
request = "1.0.0"
```

The implemented registry is local and offline:

```text
.kryndel/registry/request/1.0.0/kry.toml
.kryndel/registry/request/1.0.0/src/
.kryndel/registry/request/1.0.0/checksum
```

Supported commands are `new`, `init`, `add`, `remove`, `install`, `update`,
`list`, `tree`, `check`, `build`, `run`, `inspect`, `fmt`, `test`, `doc`, `pack`,
`reproducible`, `inspect-bytecode`, `verify-bytecode`, `verify-artifact`,
`abi`, `host-report`, and `clean`:

```bash
PYTHONPATH=. python3 -m kryndel init demo
cd demo
PYTHONPATH=.. python3 -m kryndel add request --path ../request
PYTHONPATH=.. python3 -m kryndel install --offline
PYTHONPATH=.. python3 -m kryndel list
PYTHONPATH=.. python3 -m kryndel tree
```

The resolver supports exact, caret, tilde, and simple range semver, transitive
dependencies, cycles, incompatible versions, duplicate declarations, missing
manifests, invalid names/versions, checksums, and deterministic `kry.lock`.
Installations are staged before replacement. Package source is copied but never
executed; symlink escape, path traversal, malformed manifests, and checksum
errors are rejected. Remote registries are not implemented or implied.

Imports now resolve real installed source modules. The top-level package must be declared in the project manifest and installed offline. A package root is `src/lib.kry`; child modules use a unique `src/name.kry`, `src/name/mod.kry`, or the compatibility form `src/name/lib.kry`. Dotted paths resolve recursively, so `import request.http.client` searches the corresponding nested module path and rejects missing or ambiguous candidates. Imports are traversed deterministically and circular graphs are rejected.

Declarations are private by default. The first visibility form is intentionally small:

```kryndel
pub fn get(value: Int) -> Int {
    return value + 1
}
```

Project compilation links exported functions under their fully qualified module names. A caller can use `request.http.client.get(41)`. Private functions remain available inside their defining module but cannot be called from another module. This milestone does not yet implement aliases, reexports, imported struct/enum types, traits, or generics.

## Artifacts and bytecode

`build` writes a `.kexe` Kryndel artifact. This is a portable Kryndel VM
container, not a Windows PE or native executable. Its header contains a magic
marker, payload length, and SHA-256 checksum. `inspect` validates and reports
the module without executing it. The versioned bytecode contract is in
[`docs/specs/bytecode-v1.md`](docs/specs/bytecode-v1.md).

```bash
PYTHONPATH=. python3 -m kryndel build examples/enums.kry -o examples/enums.kexe
PYTHONPATH=. python3 -m kryndel inspect examples/enums.kexe
PYTHONPATH=. python3 -m kryndel run examples/enums.kexe
```

## Architecture

```text
UTF-8 source
    -> lexer -> tokens and lexical diagnostics
    -> parser -> dataclass AST and recoverable parse diagnostics
    -> checker -> names, types, module interfaces, imports, exhaustiveness
    -> compiler -> deterministic linked bytecode Module
    -> VM/artifact -> checked execution or portable serialization
```

The initial test contract is also language-shaped and host-independent:

```kryndel
@test
fn test_answer() -> Void {
    println(42)
}
```

`kry test` discovers these zero-argument functions under `tests/**/*.kry` and
executes each file in an isolated VM. Assertions are available through
`assert(Bool)` and `assert_eq(Any, Any)`, with typed source wrappers in
`stdlib/testing/testing.kry`. Human results show every test; `kry test --format
json` emits a deterministic versioned report and returns one if any test fails.
`kry fmt` currently normalizes trailing horizontal whitespace and the final
newline without rewriting token spacing; `kry reproducible` compiles the
selected source twice and compares bytecode; `kry verify-bytecode` and
`kry verify-artifact` validate structural contracts. `kry host-report` emits
the offline inventory of every VM intrinsic and fails when dispatch, signature,
error metadata, fixture, or replacement information is missing. `kry doc`
emits deterministic source declarations, and `kry pack` writes a reproducible
`.krypkg` source archive with a SHA-256 checksum; neither command executes
source files.

The Python bootstrap owns the lexer, recursive-descent parser, checker,
module graph, compiler, VM, artifact container, CLI, local package resolver, and
the runtime implementation behind `stdlib/core/data.kry`.
Bytes execution, assertions, host-report, doc, pack, and the current test runner
are also bootstrap implementations behind visible contracts. Their language-level
signatures and deterministic fixtures are frozen, but no self-hosted
implementation is claimed. The controlled filesystem boundary is now also
available through `fs.read_bytes`, `fs.read_text`, `fs.write_bytes`, `fs.list_dir`, and `fs.stat`,
with executable wrappers in `stdlib/core/filesystem.kry`; it remains a temporary
VM capability rooted explicitly by the embedding. The complete implementation audit is in
[`docs/roadmap-status.md`](docs/roadmap-status.md).
The source language contracts, spans, diagnostic JSON, visibility rules,
module-resolution rules, bytecode JSON, qualified function names, KEXE checksum
rules, manifest subset, lockfile, semver, and package checksum algorithm are
language-independent and are future self-hosting boundaries.

## Route to self-hosting

The first self-hosted compiler must also reproduce module discovery,
visibility/export rules, qualified function linkage, diagnostics, and bytecode
serialization. Before rewriting those components in Kryndel, the project must
stabilize diagnostic codes/spans, AST meaning, nominal type rules, bytecode v1,
artifact checksums, manifest/lockfile formats, module graph fixtures, and
reproducibility tests. Python remains the bootstrap implementation until a
Kryndel compiler/runtime can build and run itself; Kryndel is not self-hosted
at this milestone.

## Repository layout

```text
kryndel/       lexer, AST, parser, checker, compiler, VM, artifacts, packages, CLI
examples/      runnable language examples
docs/          language, architecture, testing, roadmap, and versioned format contracts
tests/         standard-library-only regression suite
```

Run the suite from the repository root:

```bash
PYTHONPATH=. python3 -m py_compile kryndel/*.py tests/test_kryndel.py
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The test suite is the language contract. It covers existing structs and unit
enums, payload enums and match, diagnostics, malformed bytecode/runtime,
manifests, lockfiles, semver, local resolution, checksums, imports, CLI, KEXE,
data-core slices/builders/records, source manifest ranges, lockfile JSON,
normalized bytecode verification, determinism, and security boundaries. The
current checkout runs 103 Python unit tests; the
historical 78-test wording in older release notes is no longer accurate.
