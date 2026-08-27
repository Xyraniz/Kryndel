# Kryndel architecture

Kryndel has two deliberately separated implementation routes. The historical
stage-0 compiler and VM are a Python standard-library bootstrap with explicit
language-independent contracts. The repository also contains an independent
native core in `native/kry.c` for the productive subset. The native core is a
real standalone execution path, while the broader bootstrap remains the
reference implementation until the full language and toolchain are migrated.

## Transition stages

| Stage | Ownership | Current evidence |
| --- | --- | --- |
| native-core | Native C11 lexer, parser, tree-walk runtime, and CLI for the productive subset | `native/kry.c`, `Makefile`, `tools/kry-native`, and `tests/native-core.sh`; independent of Python and Rust after build |
| stage-0 | Python bootstrap compiler, VM, CLI, and differential oracle | `kryndel/*.py`; reference route for the complete historical language |
| stage-1 | Kryndel source seams and frozen contracts | `stdlib/**/*.kry` plus versioned fixtures; source seams still run through stage-0 |
| stage-2 | Native artifact reader, verifier, runtime, and capability table for the full v1 toolchain | The native core exists for a source-artifact subset; historical KEXE reader and full capability table remain unimplemented natively |
| stage-3 | Native compiler, module loader, package manager, and productive CLI | Not implemented |
| stage-4 | Reproducible target-specific user bundle | Not implemented |
| stage-5 | Native compiler self-build and two equivalent clean rebuilds | Not implemented |

`kry autonomy-audit` is the executable inventory of this boundary. A source
module is **seam fuente bajo bootstrap Python** whenever its normal execution
enters `kryndel/vm.py`; the source language alone does not establish native
ownership.

## Pipeline

```text
Independent native route:
UTF-8 source -> C11 lexer -> native parser -> native tree-walk runtime -> stdout/native artifact

Historical full route:
UTF-8 SourceFile
    |
    v
Data core -----------------> bounded readers, slices, builders, nominal records
    |
    v
Lexer --------------------> tokens + KRY1xxx diagnostics
    |
    v
Recursive-descent parser --> dataclass AST + KRY2xxx diagnostics
    |
    v
Module graph loader -------> parsed modules + import edges
    |
    v
Static checker ------------> names/types/module interfaces + imports
                              KRY3xxx diagnostics and exhaustiveness
    |
    v
Bytecode compiler ---------> deterministic linked Module v1
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
`DiagnosticError.as_json()` emits sorted, stable JSON. Module diagnostics add
package and module notes without changing the existing source-span schema.

| Range | Meaning |
| --- | --- |
| KRY1000–1999 | lexer |
| KRY2000–2999 | parser/recovery |
| KRY3000–3999 | names, types, payloads, match |
| KRY5000–5999 | manifests, semver, packages, locks, checksums |
| KRY6000–6099 | runtime, malformed bytecode, artifacts |
| KRY6100–6199 | sequence layouts, indexing, and collection runtime errors |
| KRY6200–6299 | value conversion and UTF-8 boundary errors |
| KRY6300–6399 | IO, filesystem, program, and malformed-bytecode boundary errors |
| KRY6400–6499 | test assertion failures |

Existing codes remain unchanged. New payload/match codes include `KRY3043`–
`KRY3049`; imported-symbol codes are `KRY3050`–`KRY3052`; sequence semantic
codes are `KRY3053`–`KRY3056`; package/module
codes include `KRY5001`–`KRY5016`; test assertions use `KRY6401` and `KRY6402`.

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
`GET_FIELD`, `MAKE_ENUM`, `MATCH_ENUM`, `BIND_ENUM`, `MAKE_ARRAY`,
`MAKE_TUPLE`, and `INDEX`. The VM validates metadata shape, operand stack depth,
constant indices, jumps, function arity, enum payload arity, struct fields, and
sequence operands. Runtime failures are converted to `RuntimeKryndelError` with
function/line/call-stack context; CLI output never exposes a Python traceback.
Visible test assertions report `KRY6401` for a false condition and `KRY6402` for
an unequal pair. The complete v1 contract is in
[`specs/bytecode-v1.md`](specs/bytecode-v1.md).

KEXE wraps module JSON with a fixed magic, length, and SHA-256 payload digest.
It is a portable VM artifact, not a native Windows executable.

## Packages and modules

`packages.py` implements a strict manifest subset, semantic version requirements,
a local registry, path dependencies, dependency graph traversal,
cycle/incompatibility checks, checksum verification, deterministic JSON
lockfiles, and staged installation. The resolver sorts package names and lock
entries. Registry packages live under `.kryndel/registry/name/version`; installed
files live under `.kryndel/packages/name`.

`modules.py` resolves source modules after installation. The package root is
`src/lib.kry`; a child path accepts exactly one of `src/name.kry`,
`src/name/mod.kry`, or the compatibility form `src/name/lib.kry`. The same rule
is applied recursively for dotted imports. Existing candidates are checked
inside the installed package root, ambiguous candidates produce `KRY5015`, and
missing candidates produce `KRY5014`. Imports are visited in sorted path order;
cycles produce `KRY5016`. The resolver parses and checks source but never
executes package top-level code.

Declarations are private by default. The parser accepts only `pub fn`, `pub
struct`, and `pub enum` as visibility modifiers. The current linker exposes
public functions as qualified VM function names, such as
`request.http.client.get`; private functions remain callable only from their
own module. Aliases, reexports, imported nominal types, traits, and generics
remain outside this milestone.

No pip, Python package installation, database, login, remote service, or package
installation script is involved. Remote registry transport is deliberately not
implemented.


## Linked modules and Python boundary

Project-aware compilation loads the root source and every resolved module,
checks each module against its imported interface, and merges exported function
bytecode into one deterministic v1 `Module`. Dependency module functions are
namespaced; the project root keeps `main` as its entry point. Imported module
top-level statements are not implicitly executed.

## Python boundary and self-hosting

The full historical implementation still has Python ownership of the lexer,
parser, checker, compiler, VM, artifact container, package manager, and host
bridges. The native core owns its own lexer, parser, evaluator, console bridge,
value model, and native source artifact reader for the documented subset. The
source-level data core still executes through the Python VM and does not retire
Python ownership for the broader toolchain. `kry host-report`
emits the deterministic inventory and fails if VM dispatch names, visible
signatures, or error metadata diverge. The measured dependency inventory is in
[`host-dependency-inventory.md`](host-dependency-inventory.md). The
language-independent contracts are source spans, AST
semantics, visibility, module resolution, diagnostic JSON, qualified function
names, bytecode v1, KEXE checksums, manifest v1, lockfile ordering, semver, and
package checksum calculation. A future Kryndel lexer needs identifiers,
literals, comments, operators, and spans; a future parser needs declarations,
expressions, blocks, enums, payloads, match, imports, and recovery. The first
self-hosted milestone must reproduce module graphs, exported interfaces,
bytecode, and diagnostics from the same fixtures before the Python bootstrap is
retired. Kryndel is not self-hosted yet.
