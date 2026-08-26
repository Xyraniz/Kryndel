# Kryndel language reference

This document describes behavior implemented and tested by Kryndel 0.1.0.

## Enums with positional payloads

Enum declarations are nominal. Variants may be unit variants or have ordered
payload types:

```kryndel
enum MaybeInt { None Some(Int) }
enum Message { Quit Move(Int, Int) Text(String) }
```

Values use `Enum.Variant` for unit variants and
`Enum.Variant(expression, expression)` for payload variants. Payloads may be
primitive types, structs, enums, or nested combinations. The checker validates
the variant name, arity, and each payload type. Values print deterministically:
`MaybeInt.None`, `MaybeInt.Some(42)`, `Message.Move(10, 20)`, and
`Message.Text("hello")`.

Enum equality and inequality require the same nominal enum type and compare all
payloads structurally. Ordering is rejected.

## Match

The first matching form is:

```kryndel
match value {
    MaybeInt.None => println("none")
    MaybeInt.Some(number) => {
        println(number)
    }
}
```

Patterns are unit/positional enum variants or `_`. Bindings are local to their
branch. A payload pattern must have exactly one binding per payload; `_` may be
used as an ignored binding. Arms are ordered and deterministic. Every enum
variant must be covered, or a `_` arm must be present. Duplicate variants,
duplicate wildcards, wrong enum patterns, missing variants, and wrong binding
counts are diagnostics, not runtime fallbacks.

Arbitrary patterns, struct destructuring, guards, OR patterns, macros,
generics, ownership, borrowing, and lifetimes are not part of this version.

## Existing core language

Kryndel also supports explicit `Int`, `Float`, `Bool`, `String`, `UiNode`, and
`Void` types; nominal structs; immutable bindings by default; `let mut`;
functions with typed parameters/returns; `if`, `while`, `break`, `continue`,
returns, calls, field access, arithmetic, comparison, and short-circuit boolean
operators. Struct construction requires each declared field once and stores
fields in declaration order.

## Imports, modules, and exports

An import contains a package name followed by zero or more dotted module names:

```kryndel
import request
import request.http
import request.http.client
```

The package root is `src/lib.kry`. A child module resolves to exactly one of
`src/name.kry`, `src/name/mod.kry`, or the compatibility form
`src/name/lib.kry`; the rule is applied recursively to dotted paths. The
resolver rejects missing modules with `KRY5014`, ambiguous candidates with
`KRY5015`, and circular import graphs with `KRY5016`. The imported package must
be declared in `[dependencies]` and installed; otherwise `KRY5013` or
`KRY5014` is reported. Resolution is deterministic and package source is never
executed during discovery.

Declarations are private by default. The only supported export modifier is
`pub` before a function, struct, or enum:

```kryndel
pub fn get(value: Int) -> Int {
    return value + 1
}
```

The current project linker exposes public functions through their qualified
module path, for example `request.http.client.get(41)`. A private function is
valid inside its defining module but produces `KRY3051` when called from an
importer. A missing exported function produces `KRY3050`. Local declarations
cannot collide with imported package roots (`KRY3052`). Aliases, reexports,
imported nominal struct/enum types, traits, and generics are not implemented in
this milestone.

## Arrays, tuples, Option, Result, and runtime errors

Array literals (`[1, 2]`), tuple literals (`("name", 7)`), length,
concatenation, and safe indexing are implemented without generics:

```kryndel
let numbers: Array = [1, 2] + [3]
let pair: Tuple = ("answer", 42)
println(numbers[2])
println(pair[0])
```

Arrays are homogeneous and tuples are fixed-width. Indexes must be `Int` and
out-of-range access is a runtime error. `array_push(Array, Any) -> Array` returns
a new immutable array and leaves its input unchanged; incompatible collection
values produce `KRY6203`. The source wrapper is `stdlib/collections/sequences.kry`.
`Option`/`Maybe` and `Result`/`Error` are ordinary enums in the initial stdlib
contract; generic payloads are still not implemented.
The executable `stdlib/core/option.kry` module exports
`none() -> Option`, `some(value: Int) -> Option`, `is_some(value: Option) -> Bool`,
`is_none(value: Option) -> Bool`, and total `unwrap_or(value: Option, fallback: Int) -> Int`
(and its `get_or` alias). The executable `stdlib/core/result.kry` module exports
`ok(value: Int) -> Result`, `error(message: String) -> Result`,
`is_ok(value: Result) -> Bool`, `is_error(value: Result) -> Bool`, and total
`unwrap_or(value: Result, fallback: Int) -> Int` (and its `get_or` alias).
These functions are ordinary Kryndel declarations compiled to the existing
bytecode; they do not add hidden Python builtins. The fallback accessors are
deliberately total, so an absent `Option` or failed `Result` returns the caller's
fallback. A partial `unwrap` operation and its value-absent error remain outside
this non-generic milestone until a serializable program-error contract is frozen.

`Bytes` is an immutable nominal sequence of octets. The constructor
`bytes(Array)` requires every element to be an `Int` in `0..255`; it rejects
other values with `KRY6202`. `string_to_bytes(String)` returns canonical UTF-8,
while `bytes_to_string(Bytes)` validates and decodes octets strictly. No
normalization, locale conversion, or replacement character is used. `len` on
`Bytes` counts octets, `Bytes[index]` returns an `Int`, and `Bytes + Bytes`
concatenates octets. The first invalid sequence raises `KRY6201` and reports its
zero-based byte offset and expected sequence length. The source-level wrappers
are in `stdlib/string/utf8.kry` and `stdlib/collections/bytes.kry`.

Runtime errors for the existing bytecode operations are represented by `RuntimeKryndelError`; malformed bytecode, stack underflow, invalid calls, division by zero, invalid sequence indexes, invalid octets, invalid UTF-8, and unsupported sequence values are diagnosed without exposing Python tracebacks.

## Controlled filesystem API

The temporary source-level filesystem API is exposed through the `fs` capability. A VM embedding must provide one explicit project root; paths are relative POSIX paths and cannot contain a drive prefix, backslash, repeated separators, `..`, or symlinks. The API is available through the executable wrappers in `stdlib/core/filesystem.kry`:

```kryndel
fn read_bytes(path: String) -> Bytes
fn read_text(path: String) -> String
fn write_bytes(path: String, value: Bytes) -> Void
fn list_dir(path: String) -> Array
fn stat(path: String) -> FileMetadata
```

`FileMetadata` is a nominal record with `path: String`, `kind: String` (`file` or `directory`), and `size: Int`. Directory results are deterministic and sorted by portable relative path. Missing entries produce `KRY6302`, root escapes and symlinks produce `KRY6303`, malformed paths or invalid UTF-8 values produce `KRY6304`, and unconfigured or failed host IO produces `KRY6301`. The API is a bootstrap boundary: the current VM adapter is temporary and does not make the compiler or runtime self-hosted.


Testing exposes `assert(value: Bool) -> Void` and
`assert_eq(left: Any, right: Any) -> Void`. A false condition produces `KRY6401`
and unequal values produce `KRY6402`; the typed source wrappers are in
`stdlib/testing/testing.kry`. `kry test --format json` returns a versioned,
deterministic list of passed and failed tests and exits with status 1 when any
case fails.

The complete value, UTF-8, error, frame, call, and host-boundary contract is
versioned in [`specs/value-runtime-v1.md`](specs/value-runtime-v1.md).


## Diagnostics

The compiler reports stable codes, severity, file, line/column span, notes,
help, and suggestions. Use `--format json` for deterministic tool output. The
full code ranges and package codes are listed in
[`architecture.md`](architecture.md).
