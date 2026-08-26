# Data core v1

The data core is the first toolchain-oriented source contract in Kryndel. It is
implemented in `stdlib/core/data.kry` and currently executes through the Python
bootstrap VM. The contract is a compatibility boundary for the future native
compiler and runtime; it is not a claim of self-hosting.

## Bounded readers

`StringSlice` stores a source `String`, an inclusive `start` codepoint offset,
and an exclusive `end` codepoint offset. `BytesSlice` stores a source `Bytes`, an
inclusive `start` octet offset, and an exclusive `end` octet offset. Construction
accepts only `0 <= start <= end <= len(source)`. Invalid construction returns the
corresponding `*SliceResult.Error` with `KRY6202` in its message rather than
creating a malformed value.

`string_slice_length` and `bytes_slice_length` return `end - start`. The indexed
readers reject negative or out-of-range indexes with `*ReadResult.Error`
containing `KRY6104`. A valid String read returns one Unicode codepoint as a
`String`; a valid Bytes read returns an `Int` octet in `0..255`. The source value
is immutable and remains available through the slice, so repeated reads are
deterministic and do not mutate the cursor or source.

## String builder

`StringBuilder` contains an immutable `Array` of String chunks. Appending returns
a new builder and leaves its input unchanged. Finishing an empty builder returns
`""`. Non-empty chunks are combined with a divide-and-conquer recursion tree,
which avoids the quadratic left-fold assembly pattern used by the earlier
manifest helper. Chunk order is preserved exactly and no normalization or locale
conversion occurs.

## Nominal records

The source module defines the following declaration-ordered records:

| Record | Fields in layout order |
| --- | --- |
| `SpanRecord` | `start: Int`, `end: Int`, `line: Int`, `column: Int` |
| `TokenRecord` | `kind: String`, `text: String`, `span: SpanRecord` |
| `AstRecord` | `kind: String`, `text: String`, `span: SpanRecord`, `children: Array` |
| `DiagnosticRecord` | `severity: String`, `code: String`, `message: String`, `span: SpanRecord`, `notes: Array`, `help: String` |

These are nominal source values, not Python dictionaries. The current fixture
freezes field order and the regression suite checks that nested records retain
identity and declaration order. `TokenRecord.text` is the normalized textual
payload for this first seam; the future lexer must specify typed literal payloads
before replacing the bootstrap `Token.value` representation.

## Evidence and ownership

`tests/fixtures/data-core-v1.json` freezes the source API, record layouts, valid
cases, and `KRY6104`/`KRY6202` error coverage. `tests/test_kryndel.py` executes
the module with Unicode strings, arbitrary octets, invalid bounds, repeated
builder appends, and nested records. The Python VM still owns primitive String,
Bytes, Array, indexing, arithmetic, and enum execution. Therefore the measured
Python implementation share remains 100% for this boundary until a native value
runtime reproduces the fixture.
