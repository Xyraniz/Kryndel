# Kryndel architecture

Kryndel is a Python standard-library bootstrap with explicit boundaries. The
boundaries are contracts for a future independent compiler/runtime; they are
not a claim that the current implementation is self-hosted.

## Pipeline

```text
UTF-8 SourceFile
    |
    v
Lexer --------------------> tokens + KRY1xxx diagnostics
    |
    v
Recursive-descent parser --> dataclass AST + KRY2xxx diagnostics
    |
    v
Static checker -----------> names/types/imports + KRY3xxx diagnostics
    |                         match exhaustiveness
    v
Bytecode compiler ---------> deterministic Module v1
    |                 \\
    v                  v
   VM                KEXE artifact
```

The compiler never executes source code. The VM is the only phase allowed to
perform runtime effects. The package resolver is a separate project-level
phase and never executes package files.

## Diagnostics

`Diagnostic` stores severity, code, primary span, secondary labelled spans,
message, notes, help, and a suggestion. Human output renders source context;
`DiagnosticError.as_json()` emits sorted, stable JSON.

| Range | Meaning |
| --- | --- |
| KRY1000–1999 | lexer |
| KRY2000–2999 | parser/recovery |
| KRY3000–3999 | names, types, payloads, match |
| KRY5000–5999 | manifests, semver, packages, locks, checksums |
| KRY6000–6099 | runtime, malformed bytecode, artifacts |

Existing codes remain unchanged. New payload/match codes include `KRY3043`–
`KRY3049`; package codes include `KRY5001`–`KRY5013`.

## Enums and match

The AST keeps variant payload type names and enum value payload expressions.
`EnumType` is nominal and stores declaration-ordered `(variant, payload types)`
metadata. Registration occurs before executable checking, so enums and payload
types can be used before their declarations. The compiler emits payload values
then `MAKE_ENUM`; the VM retains a nominal `EnumValue(type, variant, payloads)`.
Dataclass equality gives structural equality of payloads while type and variant
remain nominal. Ordering is rejected by the checker.

`match` evaluates its value once into an internal local. Each ordered variant
arm emits `MATCH_ENUM`, a conditional jump, and (for payload arms) `BIND_ENUM`.
Bindings live in a compiler scope that ends with the arm. The checker rejects
wrong enum names, duplicate variants, duplicate wildcards, wrong binding arity,
and missing variants unless `_` covers the remainder.

## Bytecode and runtime safety

Bytecode v1 is deterministic JSON. Important operations are `MAKE_STRUCT`,
`GET_FIELD`, `MAKE_ENUM`, `MATCH_ENUM`, and `BIND_ENUM`. The VM validates
metadata shape, operand stack depth, constant indices, jumps, function arity,
enum payload arity, and struct fields. Runtime failures are converted to
`RuntimeKryndelError` with function/line/call-stack context; CLI output never
exposes a Python traceback. The complete v1 contract is in
[`specs/bytecode-v1.md`](specs/bytecode-v1.md).

KEXE wraps module JSON with a fixed magic, length, and SHA-256 payload digest.
It is a portable VM artifact, not a native Windows executable.

## Packages

`packages.py` implements a strict manifest subset, semantic version
requirements, a local registry, path dependencies, dependency graph traversal,
cycle/incompatibility checks, checksum verification, deterministic JSON
lockfiles, and staged installation. The resolver sorts package names and lock
entries. Registry packages live under `.kryndel/registry/name/version`; installed
files live under `.kryndel/packages/name`.

No pip, Python package installation, database, login, remote service, or package
installation script is involved. Remote registry transport is deliberately not
implemented. Imports currently validate that the top-level package is declared;
exported module symbols and ambiguity resolution remain future work.

## Python boundary and self-hosting

Currently Python owns all implementation code and the host filesystem/clock/
stdout bridges. The language-independent contracts are source spans, AST
semantics, diagnostic JSON, bytecode v1, KEXE checksums, manifest v1, lockfile
ordering, semver, and package checksum calculation. A future Kryndel lexer
needs identifiers, literals, comments, operators, and spans; a future parser
needs declarations, expressions, blocks, enums, payloads, match, and recovery.
The first self-hosted milestone must reproduce bytecode and diagnostics from
the same fixtures before the Python bootstrap is retired.
