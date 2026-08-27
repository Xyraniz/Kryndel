# Kryndel language reference

Kryndel uses braces for blocks and infix expressions. The syntax is intentionally small, but the checker assigns a concrete type to every expression before execution. Type annotations after `:` and `->` are semantic declarations, not comments.

## Declarations and control flow

```kryndel
let greeting: String = "hello"
let mut total: Int = 0

fn sum_to(limit: Int) -> Int {
    let mut value: Int = 0
    let mut index: Int = 0
    while index <= limit {
        value = value + index
        index = index + 1
    }
    return value
}

if sum_to(4) == 10 {
    println(greeting)
} else {
    println("error")
}
```

`let` creates an immutable binding. `let mut` is required before a binding can be assigned. A child scope may shadow a parent name, and assignment updates only the nearest binding; an outer immutable binding can never be changed through a nested scope. Branch and loop bindings are local to their block. Function calls use local lexical scopes and may be recursive.

Supported statements are `let`, `let mut`, expression statements, assignment, `fn`, `if`, `else`, `while`, `return`, `break`, `continue`, `import`, `struct`, `enum`, and `match`. A function parameter must have an explicit type, and a non-`Nil` function must return its declared type on every checked path.

## Types and expressions

| Type | Examples | Rules |
| --- | --- | --- |
| `Int` | `0`, `-7` | Signed 64-bit integer with checked arithmetic. |
| `Float` | `3.14` | Finite IEEE floating-point value; conversions are explicit. |
| `Bool` | `true`, `false` | Required by `if`, `while`, `!`, `&&`, `||`, and `assert`. |
| `String` | `"Kryndel"` | Valid UTF-8 text; `+`, `len`, and code-point indexing. |
| `Bytes` | `bytes([65, 66])` | Opaque byte sequence; `+`, `len`, indexing, and UTF-8 conversion. |
| `Array[T]` | `[1, 2, 3]` | Homogeneous, indexable collection. Bare `Array` is a compatibility annotation inferred from its initializer. |
| `Nil` | `nil` | Explicit empty value. |
| `Option[T]` | `some(7)`, `none()` | Explicit presence or absence. |
| `Result[T, E]` | `ok(7)`, `err("bad")` | Explicit success or error value. |
| `Channel[T]` | `thread_channel()` | Bounded synchronized channel for copy-safe values. |
| `Thread[T]` | `thread_spawn("worker")` | OS-backed worker handle whose result type is `T`. |
| `Struct` | `Point{ x: 1, y: 2 }` | Named fields checked against a declaration. |
| `Enum` | `Color::Red` | Tagged variant checked against a declaration. |

Numeric operators require matching numeric types. `Int + Float` is rejected; use `float(integer)` explicitly. `+` also concatenates two strings, two arrays with compatible element types, or two byte sequences. Remainder is defined only for `Int`. Equality requires the same type. There is no implicit truthiness: `if 1`, `if "text"`, and `while [1]` are type errors. `bool(value)` is the explicit conversion when a program needs a convenient predicate.

Operator precedence, from low to high, is `||`, `&&`, equality, ordered comparison, addition/subtraction, and multiplication/division/remainder. Parentheses group expressions. `&&` and `||` are short-circuiting, and both operands must be `Bool`.

Strings use `//` line comments and nested `/* ... */` block comments. String escapes are `\\`, `\"`, `\n`, `\r`, `\t`, and `\xNN`. Source files and decoded strings must be valid UTF-8.

## Functions and return checking

Function parameters and return types are explicit:

```kryndel
fn repeat(value: String, count: Int) -> String {
    let mut result: String = ""
    let mut index: Int = 0
    while index < count {
        result = result + value
        index = index + 1
    }
    return result
}
```

The checker reports unknown functions, wrong arity, wrong argument types, unresolved return types, and return mismatches before evaluating the program. Functions do not close over mutable runtime state and are called by value.

## Structs, enums, and matching

Structs declare named, typed fields. Enums declare a finite set of variants. `match` checks enum variants and requires either every variant or a `_` wildcard. `Option[T]` requires both `some(name)` and `none` unless `_` is present; `Result[T, E]` requires both `ok(name)` and `err(name)`. Duplicate alternatives are rejected:

```kryndel
enum Color { Red, Blue }
let color: Color = Color::Red
match color {
    Color::Red => { println("red") }
    Color::Blue => { println("blue") }
}
```

`Option[T]` patterns use `some(name)` and `none`; `Result[T, E]` patterns use `ok(name)` and `err(name)`. Pattern bindings are immutable and local to the arm. A `nil` pattern is an alias for an empty `Option`, not a `Result` alternative.

## Threads and channels

The stable concurrency API uses seven explicit builtins: `thread_channel`, `thread_spawn`, `thread_send`, `thread_receive`, `thread_receive_timeout`, `thread_join`, and `thread_close`. A channel is a bounded single-slot synchronization object. Worker functions are named, take no arguments, and return a declared type. `thread_send` accepts only Copy values: primitives, strings, bytes, enums, and recursively Copy arrays, options, and results. Structs, channel handles, and thread handles are not transferable values.

Workers receive only global channel handles through a private runtime scope; ordinary global values and mutable application data are not exposed to a worker. Channel operations are synchronized with a mutex and condition variables. Closing a channel wakes blocked senders and receivers; `thread_receive_timeout` provides a bounded millisecond deadline; the runtime closes channels and joins outstanding workers during program shutdown. Worker failures are propagated by `thread_join` and by shutdown when no earlier error exists.

## Modules

`import "path/to/module"` resolves a source file relative to the importing file. The `.kry` extension is optional. Absolute paths, `..` traversal, artifacts, missing files, duplicate exports, and cyclic imports are rejected. Only top-level declarations marked `pub` are exported. See [the module guide](modules.md) for the complete resolution contract.

## Static checking

`kry check file.kry` lexes, parses, resolves imports, and type-checks the complete program without executing user code. `kry run` and `kry build` perform the same validation before evaluation or artifact creation. Unknown type names, invalid annotations, immutable assignments, bad conditions, unsafe operators, invalid indexing expressions, and builtin signature errors therefore fail before user output can occur.
