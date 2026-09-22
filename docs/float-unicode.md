# Float, collections, and Unicode policy

## Float

`Float` is IEEE-754 binary64, but the language accepts only finite values. `NaN`, positive infinity, and negative infinity are rejected by literals, conversions, arithmetic, and builtins. Arithmetic that would produce a non-finite value is a runtime error.

Negative zero is representable and formats as `-0`; it compares equal to positive zero. Ordered comparisons use IEEE numeric ordering. Since non-finite values cannot exist, unordered `NaN` comparisons are not observable.

Float formatting is locale-independent and uses the shortest round-trippable decimal representation produced by the implementation. Decimal parsing accepts complete finite values only. Conversion from `Int` or `UInt` is explicit; it may round when the integer cannot be represented exactly by binary64, and conversion back to `Int` rejects values outside the signed range.

Subnormal finite values are valid when produced by conversion or parsing, including the smallest positive binary64 value. They must not be flushed to zero. The source literal grammar currently uses ordinary decimal literals; exponent-form values can be supplied to `float(String)`.

## Maps and sets

Maps and sets preserve insertion order. `map_keys`, `map_values`, `set_to_array`, display, and iteration return values in insertion order. Updating an existing map key does not move it. Removing and reinserting a key appends it at the new insertion point.

Map and set membership uses the language equality relation recursively. Keys must be comparable and must not contain external handles. Float keys are not part of the stable key domain; use an integer or a canonical string for numeric keys. Duplicate map keys are rejected; duplicate set values are ignored.

The implementation deliberately uses ordered persistent values instead of randomized hash-table iteration so output and compiler artifacts remain reproducible. This is a semantic guarantee, not an implementation accident.

## Unicode

Source strings are valid UTF-8. `len(String)`, indexing, `string_chars`, `string_slice`, and `substring` use Unicode code-point positions, not byte positions and not grapheme clusters. `Bytes` and `string_to_bytes` are the byte-oriented APIs.

A user-perceived grapheme such as `e\u0301`, `👩‍💻`, or `🇺🇳` may contain multiple code points and is therefore not treated as one character by the core string APIs. No Unicode normalization is performed: canonically equivalent strings remain distinct unless an application normalizes them explicitly.

Slicing and indexing never split the UTF-8 encoding of a code point. Invalid or out-of-range indices return the documented `Result`/`Option` error rather than producing malformed UTF-8. Case conversion follows the Unicode behavior of the standard library and may change the number of code points; it is not locale-sensitive. String comparison and equality compare valid UTF-8 code-point sequences by their binary UTF-8 representation, without normalization or locale collation.

The differential tests in `internal/kry/float_unicode_differential_test.go` cover combining marks, emoji joined by zero-width joiners, regional indicators, embedded NULs, nested values, and native/interpreter parity.
