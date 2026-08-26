# Kryndel lexer contract v1

The bootstrap lexer now has an observable deterministic snapshot boundary. `kry lex source.kry` emits `contract`, `version`, a basename-only file label, ordered nominal `Token` records, and ordered serializable diagnostics. `--fixture` compares the emitted structure with a frozen oracle and fails on the first whole-snapshot difference.

Token values preserve runtime types: Boolean keywords carry `true` or `false`, integers and floats remain numeric, strings remain decoded UTF-8, and punctuation retains its source spelling. Spans are half-open byte offsets with one-based line and column fields. Comments and whitespace do not emit tokens; the EOF token is always present.

`tests/fixtures/lexer-input.kry` and `tests/fixtures/lexer-v1.json` cover comments, declarations, type names, Unicode strings, arithmetic, calls, and EOF. The test suite checks repeated snapshots, canonical JSON, and the CLI fixture comparison.

> The current snapshot compares the Python bootstrap lexer against a frozen fixture. It is not evidence that a Kryndel-native lexer exists; a future native implementation must reproduce the same record tree before the bootstrap lexer is retired.

## Source-level lexer milestone

`stdlib/core/lexer.kry` now implements a deterministic source lexer over String
codepoints. It recognizes the current keywords, identifiers, integer and float
lexemes, strings and escapes, nested block comments, line comments, one- and
two-character operators, delimiters, and EOF. It returns nominal `LexerResult`,
`Token`, `Span`, and `Diagnostic` values and recovers after unexpected
characters, unknown escapes, unterminated strings, and unterminated comments.

The source implementation is compared against `lexer-v1.json` for token kind,
normalized text, order, and spans. Its `Token.text` field intentionally stores
lexeme text rather than the typed `Token.value` payload used by the bootstrap;
typed literal records must be frozen before the bootstrap lexer can be retired.
The source module executes through the Python VM and is not a native compiler
component yet.
