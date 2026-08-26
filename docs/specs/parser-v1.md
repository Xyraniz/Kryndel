# Kryndel parser and AST contract v1

The bootstrap parser now exposes `kry parse source.kry`, which emits a deterministic parser contract containing the basename-only file label, ordered diagnostics, and a recursive nominal AST record rooted at `Program`. The AST preserves declaration/source order and full half-open spans. Nested `TypeName`, `StructFieldDecl`, `StructLiteral`, `LetStmt`, `ExprStmt`, and expression records retain their explicit record tags.

The optional `--fixture` argument compares the full JSON structure against a frozen oracle. `tests/fixtures/parser-input.kry` uses syntax already accepted by the compiler, and `tests/fixtures/parser-v1.json` freezes the resulting AST and empty diagnostic list. The command does not execute source code and does not use network or package resolution.

> This is a bootstrap parser/AST snapshot seam. It is not a Kryndel-native parser and cannot be used to claim self-hosting. A future native parser must reproduce the same structure and diagnostics before the Python parser is retired.

## Source-level parser milestone

`stdlib/core/parser.kry` consumes normalized `Token` records from
`stdlib/core/lexer.kry` and produces nominal `ParseResult`, `AstNode`, `Span`,
and `Diagnostic` values. The first subset parses struct declarations, typed
`let` statements, integer and string literals, member access, calls with up to
two arguments, and struct literals. It preserves source order and half-open
spans, and returns `KRY2001` diagnostics for recoverable subset failures. Literal
nodes now retain the lexer `LiteralValue` tag and payload fields in an
additional field after the existing `children` field.

The source parser is executed through the bootstrap VM and is compared against
`parser-v1.json` for root AST record kinds and spans, while
`typed-ast-v1.json` freezes integer, float, boolean, nil, and string payload
propagation. Full expression precedence,
functions, enums, match, imports, attributes, and complete diagnostic recovery
remain to be migrated. The bootstrap parser remains the typed AST oracle, so
this milestone does not claim a native parser or self-hosting.
