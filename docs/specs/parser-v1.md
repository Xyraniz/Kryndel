# Kryndel parser and AST contract v1

The bootstrap parser now exposes `kry parse source.kry`, which emits a deterministic parser contract containing the basename-only file label, ordered diagnostics, and a recursive nominal AST record rooted at `Program`. The AST preserves declaration/source order and full half-open spans. Nested `TypeName`, `StructFieldDecl`, `StructLiteral`, `LetStmt`, `ExprStmt`, and expression records retain their explicit record tags.

The optional `--fixture` argument compares the full JSON structure against a frozen oracle. `tests/fixtures/parser-input.kry` uses syntax already accepted by the compiler, and `tests/fixtures/parser-v1.json` freezes the resulting AST and empty diagnostic list. The command does not execute source code and does not use network or package resolution.

> This is a bootstrap parser/AST snapshot seam. It is not a Kryndel-native parser and cannot be used to claim self-hosting. A future native parser must reproduce the same structure and diagnostics before the Python parser is retired.
