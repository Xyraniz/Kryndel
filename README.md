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

Supported commands are `init`, `add`, `remove`, `install`, `update`, `list`,
`tree`, `check`, `build`, `run`, and `inspect`:

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

`import request` and `import request.http` are parsed as package imports. The
top-level package must be declared in the project manifest. Exported symbols
and a complete module graph are intentionally a later milestone.

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
    -> checker -> names, nominal types, payloads, imports, exhaustiveness
    -> compiler -> deterministic bytecode Module
    -> VM/artifact -> checked execution or portable serialization
```

The Python bootstrap owns the lexer, recursive-descent parser, checker,
compiler, VM, artifact container, CLI, and local package resolver. The source
language contracts, spans, diagnostic JSON, bytecode JSON, KEXE checksum rules,
manifest subset, lockfile, semver, and package checksum algorithm are language
independent and are the future self-hosting boundaries.

## Route to self-hosting

The first self-hosted compiler only needs the current lexer subset (UTF-8
characters, identifiers, literals, comments, operators, and spans), the
recursive-descent parser subset (declarations, expressions, blocks, enums,
payloads, and match), and deterministic JSON/bytecode serialization. Before
rewriting those components in Kryndel, the project must stabilize diagnostic
codes/spans, AST meaning, nominal type rules, bytecode v1, artifact checksums,
manifest/lockfile formats, and reproducibility tests. Python remains the
bootstrap implementation until a Kryndel compiler/runtime can build and run
itself.

## Repository layout

```text
kryndel/       lexer, AST, parser, checker, compiler, VM, artifacts, packages, CLI
examples/      runnable language examples
docs/          language, architecture, testing, and versioned format contracts
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
determinism, and security boundaries.
