# Kryndel checker and module-resolution contract v1

`stdlib/core/checker.kry` defines a source-level semantic boundary over the
nominal AST records emitted by `stdlib/core/parser.kry`. Its `check` operation
tracks struct names and local bindings, detects duplicate declarations and
fields, validates the tested primitive and struct-literal type relationships,
and reports unknown names and types with stable `KRY3008`, `KRY3023`, `KRY3024`,
and `KRY3003` codes.

The same module defines nominal `ModuleRecord` and `ResolveResult` values. Its
`resolve` operation performs deterministic dependency-first traversal and
rejects duplicate module names with `KRY5015`, missing modules with `KRY5014`,
and cycles with `KRY5016`.

The implementation is exercised through a lexer-parser-checker pipeline and
module-graph regression test. It executes through the Python VM and is a
compatibility seam for the tested subset; complete type identity, overload and
call checking, imports, visibility, full AST forms, and native module loading
remain Python-owned.
