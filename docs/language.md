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

## Imports

`import request` and `import request.http` are parsed. In a project, the first
component must appear in `[dependencies]`; otherwise the checker emits
`KRY5013`. This iteration does not yet expose imported symbols or implement a
complete module graph.

## Diagnostics

The compiler reports stable codes, severity, file, line/column span, notes,
help, and suggestions. Use `--format json` for deterministic tool output. The
full code ranges and package codes are listed in
[`architecture.md`](architecture.md).
