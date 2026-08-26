# Kryndel value and runtime contract v1

This document freezes the language-independent value, error, frame, call, and
host-boundary rules for the 0.1 bootstrap. It is a compatibility contract for
the future Kryndel compiler and runtime; it does not claim that the current
Python bootstrap implements every future value.

## Versioning and representation

The contract version is `1`. A value is a nominal Kryndel value, never a host
dictionary or an arbitrary host object. Host implementations may use private
storage internally, but the observable tag, fields, ordering, equality, and
serialization below are stable.

The portable representation used by fixtures is deterministic JSON with UTF-8
encoding, sorted object keys, no insignificant trailing whitespace, and arrays
preserving declaration/source order. Runtime values are not serialized into
bytecode constants unless the bytecode specification explicitly permits that
constant kind.

| Value | Runtime layout | Equality | Mutable? |
| --- | --- | --- | --- |
| `Int` | signed integer value | numeric | no |
| `Float` | IEEE-754 value; finite values in v1 | numeric after language rules | no |
| `Bool` | `true` or `false` | Boolean | no |
| `String` | valid UTF-8 scalar sequence | codepoint sequence | no |
| `Bytes` | nominal `Bytes(items: ordered octets)` | byte sequence | no |
| `FileMetadata` | nominal record `(path: String, kind: String, size: Int)` | fieldwise | no |
| `Array` | nominal `Array(items: ordered values)` | elementwise; homogeneous | no |
| `Tuple` | nominal `Tuple(items: ordered values)` | elementwise; fixed width | no |
| `Option` | nominal enum `None` or `Some(Int)` | nominal and payload-structural | no |
| `Result` | nominal enum `Ok(Int)` or `Error(String)` | nominal and payload-structural | no |
| `Void` | no source payload; runtime `nil` | only equal to `nil` | no |
| `nil` | the single absence/Void sentinel | only equal to itself | no |

`Option` and `Result` remain non-generic in v1. Generic payloads require a new
type-identity and layout specification and are deliberately deferred.

## Strings, bytes, and indexing

Source text and `String` values are UTF-8. A decoder must reject overlong
encodings, surrogate code points, truncated sequences, invalid continuation
bytes, and code points above `U+10FFFF`. The error is `KRY6201` and includes
the zero-based byte offset and offending sequence length.

`String` length counts Unicode code points, not bytes. `String[index]` indexes
code points and returns a one-codepoint `String`. `Bytes[index]` indexes octets
and returns an `Int` in `0..255`. Both indexes are non-negative `Int` values;
the wrong index type is `KRY6102` and an out-of-range index is `KRY6104`.

`String` concatenation is codepoint-preserving. `Bytes` concatenation is
octet-preserving. Conversion from `Bytes` to `String` validates all bytes and
returns `KRY6201` on failure; conversion from `String` to `Bytes` returns its
canonical UTF-8 encoding. No normalization, locale conversion, or implicit
lossy replacement is performed.

The current bootstrap implements `String`, arrays, tuples, and the nominal
`BytesValue`. The visible constructors are `bytes(Array)` for validated octets
and `string_to_bytes(String)` for canonical UTF-8. `bytes_to_string(Bytes)` is a
strict decoder; all invalid sequences produce `KRY6201` with the first byte
offset and expected sequence length. The implementation stores octets as an
immutable ordered sequence and does not expose host byte buffers as semantic
values.

## Errors

Runtime errors are serializable records with `code`, `message`, `function`,
`line`, and an ordered `call_stack`. Human formatting is a presentation layer;
Python exceptions and tracebacks are not part of the interface.

| Code | Meaning | Required boundary |
| --- | --- | --- |
| `KRY6102` | index is not `Int` | sequence indexing |
| `KRY6103` | value is not indexable | indexing |
| `KRY6104` | index is outside bounds | indexing |
| `KRY6105` | value is not length-bearing | `len` |
| `KRY6201` | invalid UTF-8 | source/bytes-to-string decoding |
| `KRY6202` | conversion is not representable | explicit value conversion |
| `KRY6203` | incompatible collection operation | collection operations |
| `KRY6204` | value is absent | explicit partial accessor only |
| `KRY6301` | IO failure | host IO boundary |
| `KRY6302` | file does not exist | filesystem boundary |
| `KRY6303` | path escapes project root | filesystem boundary |
| `KRY6304` | malformed program input | source/manifest readers |
| `KRY6305` | malformed bytecode | bytecode reader/verifier |
| `KRY6401` | assertion condition is false | test assertion boundary |
| `KRY6402` | assertion values are unequal | assertion boundary |
| `KRY7006` | unsupported typed constant category | source runtime constant decoder |

`Option`/`Result` fallback operations are total and must not produce
`KRY6204`: `unwrap_or` returns its fallback for `None`, and `get_or` returns
its fallback for `Error`. The current non-generic APIs remain source-defined in
`stdlib/core`.

## Frames and calls

Each call creates a frame containing the qualified function name, arity,
parameter bindings, instruction pointer, operand stack, and caller link. A
callee receives arguments left-to-right. A normal return pushes exactly one
value; `Void` pushes `nil`. A malformed call, unknown function, stack underflow,
or invalid return is `KRY6305` when caused by bytecode metadata, otherwise a
specific runtime code from the table above.

Call stacks preserve caller-to-callee order. They contain qualified language
function names only and never host filenames, absolute paths, or Python
implementation frames. Verification must reject a bytecode version other than
`1` before execution.

## Host boundary

The temporary host boundary is limited to memory allocation/storage, output and
input IO, monotonic clock, controlled filesystem access, process exit status,
and reading the bytecode container. Each operation must be reached through a
Kryndel-visible signature and return either a nominal value or a serializable
error. No host dictionary is a semantic Kryndel value. Filesystem metadata is
returned as the nominal `FileMetadata` record defined above, never as a host
mapping.

The dependency inventory in `docs/host-dependency-inventory.md` records the
current implementation path and replacement plan. The bootstrap remains the
implementation reference until differential fixtures pass for a Kryndel
implementation. Bytes behavior is covered by `tests/fixtures/bytes-v1.json` and
its deterministic runtime tests. Assertion behavior is covered by
`tests/fixtures/stdlib-testing-v1.json`; `assert` and `assert_eq` are visible
bootstrap boundaries until a Kryndel-native test runner replaces them.

## Source-level runtime milestone

`stdlib/core/runtime.kry` executes the normalized bytecode subset emitted by
`stdlib/core/compiler.kry`. It provides nominal `Value` and `RuntimeResult`
records, a stack and local-binding model, tagged constants, `LOAD`/`STORE`,
struct construction and field access, builtin `print`/`println` calls, `POP`,
and `RETURN`. A `PUSH_CONST` category decodes into `Int`, `Float`, `Bool`, or
`String`; `PUSH_NIL` produces `Nil`. The public `decode_constant` seam also
rejects unsupported categories with `KRY7006`. It reports stable `KRY7001`–`KRY7005`
failures for malformed constants, stack underflow, unknown callables, unsupported
opcodes, and a missing entry.

An end-to-end regression compiles the parser fixture and executes it through the
source runtime. The typed-constant fixture also freezes the compiler metadata
and nominal decoded values. This module is still interpreted by the Python VM; it does not
read KEXE bytes, implement the complete opcode set, provide host IO, or establish
an independent Kryndel runtime.
