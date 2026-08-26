# Kryndel formatter contract v1

`stdlib/core/format.kry` implements the conservative formatter contract used by
the bootstrap: it removes trailing spaces and tabs from every line, removes
trailing blank lines, preserves all other source text, and emits exactly one
final newline. The operation returns a nominal `FormatResult` with the formatted
text and a changed flag.

The source formatter is compared against those semantics for changed, unchanged,
and empty inputs and is idempotent. It executes through the Python VM and does
not yet provide a native file CLI; the bootstrap `format_file` implementation
remains the production path.
