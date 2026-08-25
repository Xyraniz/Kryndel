# Kryndel Language Reference

## Unit enums

Enums currently contain unit variants only:

```kryndel
enum Color { Red Green Blue }
let color: Color = Color.Green
println(color) // Color.Green
```

`Color.Green` is a nominal `Color` value. Only `==` and `!=` are supported between values of the same enum; ordering is rejected. Payloads and `match` do not exist yet.

This document describes the language implemented by version 0.1.0. It is intentionally narrower than the long-term vision; a rule belongs here only when the compiler and tests enforce it.

## Source files

Kryndel source files use the `.kry` extension. Source text is UTF-8. Whitespace separates tokens but does not otherwise affect meaning. Statements may end at a newline or with `;`; braces still delimit blocks.

Comments use either `//` to the end of a line or `/* ... */`. Block comments may be nested.

## Names and declarations

Identifiers contain letters, digits, and underscores, but cannot begin with a digit. A local binding is immutable unless it uses `mut`:

```kryndel
let name: String = "Ada"
let mut retries: Int = 0
retries = retries + 1
```

A declaration in the same lexical scope cannot reuse an existing name. A nested block may shadow an outer name. The compiler gives each local binding an internal slot, so shadowing does not overwrite the outer runtime slot.

## Types

The initial type set is:

| Type | Values |
| --- | --- |
| `Int` | Signed integer values represented by the host runtime. |
| `Float` | IEEE-style floating-point values provided by the host runtime. |
| `Bool` | `true` or `false`. |
| `String` | Immutable text values. |
| `UiNode` | A node in the declarative UI tree. |
| `StructName` | A nominal user-defined struct value with declaration-ordered fields. |
| `Void` | The absence of a useful value. |

The type checker also uses internal `Any` and `Unknown` types. `Any` is reserved for intentionally open standard-library entry points such as `print`. `Unknown` is an error-recovery type and is never a valid user annotation.

The accepted explicit type spellings are the built-in names `Int`, `Float`, `Bool`, `String`, `UiNode`, and `Void`, plus the names of structs declared in the same source file.

## Structs

A struct declaration introduces a nominal type with declaration-ordered fields. Field declarations use one field per line or whitespace-separated field declaration inside braces:

```kryndel
struct Point {
    x: Int
    y: Int
}
```

A constructor names the struct and provides each field exactly once. Constructor field order is independent of source order, but bytecode and runtime storage use declaration order:

```kryndel
let point: Point = Point { y: 4, x: 3 }
println(point.x)
println(point.y)
```

The checker registers all struct declarations before checking expressions, so a struct may be used before its declaration in the same file. Struct names are unique, field names are unique within a declaration, and every field type must be known. Constructors must provide all declared fields, must not provide unknown or duplicate fields, and each value must be compatible with its field type. Accessing a field that does not exist is a compile-time error with the field name span highlighted. Struct types are not represented as `Any`.

Struct values are immutable in this version. Assignment to `point.x` is rejected because field mutability has not yet been specified; only variable bindings may be reassigned with `let mut`.

## Functions

Functions require parameter types and a return type:

```kryndel
fn add(left: Int, right: Int) -> Int {
    return left + right
}
```

The entry function is synthesized as `main` from top-level statements. A user-defined function named `main` is reserved by the compiler and should not be declared.

Functions may call themselves recursively. Parameters are immutable bindings. A non-`Void` function must contain a return statement on every obvious control-flow branch recognized by the current checker.

## Expressions

Expressions support literals, names, struct constructors, field access, calls, member-qualified calls, unary operators, binary operators, and assignment.

```kryndel
let result: Float = (2 + 3) / 2
let ready: Bool = result >= 2.5 and true
let message: String = "value=" + str(result)
```

Operator precedence, from strongest to weakest, is:

1. Calls, member access, and parenthesized expressions.
2. Unary `-`, `!`, and `not`.
3. `*`, `/`, and `%`.
4. `+` and `-`.
5. `<`, `<=`, `>`, and `>=`.
6. `==` and `!=`.
7. `and` and `&&`.
8. `or` and `||`.
9. Assignment `=`.

The logical operators short-circuit. Assignment is an expression and returns the assigned value, but its target must be a mutable local binding.

## Numeric rules

Arithmetic requires numeric operands. `Int` with `Int` produces `Int` except for `/`, which produces `Float`. If either operand is `Float`, the result is `Float`. An `Int` may be promoted to `Float` when a `Float` is expected.

String concatenation uses `+` only when both operands are `String`. Use `str(value)` for explicit conversion.

## Control flow

Kryndel supports `if`, `else`, `while`, `break`, `continue`, and `return`:

```kryndel
let mut value: Int = 0
while value < 10 {
    value = value + 1
    if value == 4 {
        continue
    }
    if value == 8 {
        break
    }
}
```

Conditions must have type `Bool`. `break` and `continue` are valid only inside a `while` loop.

## Standard library

The bundled runtime provides these functions:

| Function | Signature | Behavior |
| --- | --- | --- |
| `print` | `print(Any...) -> Void` | Writes values separated by spaces. |
| `println` | `println(Any...) -> Void` | Writes values separated by spaces and a newline. |
| `str` | `str(Any) -> String` | Converts a runtime value to Kryndel text. |
| `int` | `int(Any) -> Int` | Converts a value to an integer using host conversion rules. |
| `float` | `float(Any) -> Float` | Converts a value to a floating-point value. |
| `len` | `len(String) -> Int` | Returns the number of host string characters. |
| `abs` | `abs(Int) -> Int` | Returns the absolute integer value. |
| `sqrt` | `sqrt(Float) -> Float` | Returns a square root. |
| `clock` | `clock() -> Float` | Returns a monotonic host-clock reading. |

## Declarative UI API

The current UI layer is a deterministic tree runtime, not an operating-system window backend:

| Function | Signature | Behavior |
| --- | --- | --- |
| `ui.window` | `ui.window(String, Int, Int) -> UiNode` | Creates a window node. |
| `ui.vbox` | `ui.vbox(UiNode) -> UiNode` | Adds a vertical container. |
| `ui.hbox` | `ui.hbox(UiNode) -> UiNode` | Adds a horizontal container. |
| `ui.label` | `ui.label(UiNode, String) -> UiNode` | Adds a label. |
| `ui.button` | `ui.button(UiNode, String) -> UiNode` | Adds a button. |
| `ui.set_text` | `ui.set_text(UiNode, String) -> Void` | Changes node text. |
| `ui.on_click` | `ui.on_click(UiNode, Any) -> Void` | Attaches callback metadata. |
| `ui.show` | `ui.show(UiNode) -> Void` | Renders the tree to the runtime output. |
| `ui.run` | `ui.run() -> Void` | Reserved event-loop boundary; currently returns immediately. |

## Error codes

Compiler errors use stable `KRY` codes. The exact wording may evolve, but a code identifies the category:

| Range | Phase |
| --- | --- |
| `KRY1000`-`KRY1999` | Lexing. |
| `KRY2000`-`KRY2999` | Parsing. |
| `KRY3000`-`KRY3999` | Name and type checking. |

Diagnostics always include a filename, line, column, severity, highlighted source range, and a message. A correction hint is added when the compiler can offer one without guessing intent.

## Deliberate omissions

Version 0.1.0 does not implement enumerations, generics, traits, closures, modules, ownership, borrowing, lifetimes, exceptions, asynchronous tasks, or operating-system GUI windows. These are design work, not syntax placeholders, and will be added only with an accompanying semantic specification and regression tests.
