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
out-of-range access is a runtime error. `Option`/`Maybe` and `Result`/`Error`
are ordinary enums in the initial stdlib contract; generic payloads are still
not implemented. Runtime errors for the existing bytecode operations are
represented by `RuntimeKryndelError`; malformed bytecode, stack underflow,
invalid calls, division by zero, invalid sequence indexes, and unsupported
sequence values are diagnosed without exposing Python tracebacks.

## Diagnostics

The compiler reports stable codes, severity, file, line/column span, notes,
help, and suggestions. Use `--format json` for deterministic tool output. The
full code ranges and package codes are listed in
[`architecture.md`](architecture.md).
