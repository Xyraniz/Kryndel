# Kryndel nominal records v1

The bootstrap exposes a deterministic wire representation for the records that a future Kryndel-native toolchain must be able to produce: `Token`, AST nodes, and source spans. The representation is a JSON-shaped boundary value; it is not a claim that Python dictionaries are semantic Kryndel values.

Each dataclass record is encoded with a `record` tag containing its nominal type name. Its declared fields follow in their source/declaration order, while map keys at the outer serialization boundary are sorted. Lists preserve source order. `Span` records contain `start`, `end`, `line`, and `column`. Non-finite floats and arbitrary host objects are rejected rather than silently stringified.

`Token.as_dict()`, `Node.as_dict()`, `token_records()`, and `ast_record()` are executable bootstrap APIs. The sample `tests/fixtures/records-v1.json` freezes a `LET` token and its span. The regression suite checks repeated serialization byte-for-byte and confirms that a parsed `Program` retains node names and spans.

> This is a serialization seam for differential development. It does not make the lexer or parser Kryndel-native and does not retire the Python bootstrap.

A future implementation may reproduce this record tree without importing Python implementation classes. Until then, the record fixture is evidence for compatibility only.
