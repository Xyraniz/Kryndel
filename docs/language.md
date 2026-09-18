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

`let` creates an immutable binding. `let mut` is required before a binding can be assigned. `const` creates a compile-time immutable binding; its initializer must contain only literal data and checked operators, so it cannot hide I/O or scheduling effects. A child scope may shadow a parent name, and assignment updates only the nearest binding; an outer immutable binding can never be changed through a nested scope. Branch and loop bindings are local to their block. Function calls use local lexical scopes and may be recursive.

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

Functions may be overloaded by parameter signature. Calls are resolved statically and an ambiguous or unmatched call is rejected before execution. Generic functions use readable type parameters with constraints:

```kryndel
fn identity[T: Copy](value: T) -> T { return value }
fn choose(value: Int) -> String { return "int" }
fn choose(value: String) -> String { return "text" }
```

The built-in constraints are `Copy`, `Numeric`, and `Comparable`. `Copy` is structural and excludes channels, threads, actors, sockets, and other owned handles.

Overload resolution is multiple dispatch over the complete argument tuple, not just the function name or first argument. Every visible candidate is checked against the static argument types; exactly one candidate must match. If two concrete or generic candidates match equally, compilation fails with an ambiguity diagnostic instead of depending on declaration order.

Struct fields are public by default inside a public API, but `private field: T` makes access and construction outside the declaring module a static error. This is enforced by the checker rather than by naming convention.

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

## Threads, actors, and async effects

The stable concurrency API uses seven explicit builtins: `thread_channel`, `thread_spawn`, `thread_send`, `thread_receive`, `thread_receive_timeout`, `thread_join`, and `thread_close`. A channel is a bounded single-slot synchronization object. Worker functions are named, take no arguments, and return a declared type. `thread_send` accepts only Copy values: primitives, strings, bytes, enums, and recursively Copy arrays, options, and results. Structs, channel handles, and thread handles are not transferable values.

Workers receive only global channel handles through a private runtime scope; ordinary global values and mutable application data are not exposed to a worker. Channel operations are synchronized with a mutex and condition variables. Closing a channel wakes blocked senders and receivers; `thread_receive_timeout` provides a bounded millisecond deadline; the runtime closes channels and joins outstanding workers during program shutdown. Worker failures are propagated by `thread_join` and by shutdown when no earlier error exists.

`Actor[T]` is an isolated, bounded mailbox for recursively Copy messages. `actor_channel` creates one, `actor_send` blocks with cancellation, and `actor_try_receive`, `actor_receive_timeout`, and `actor_close` provide non-panicking mailbox operations. `await` and `await_timeout` are explicit effect boundaries over `Thread[T]`; `yield_now` and `sleep_ms` cooperate with the runtime context instead of busy-waiting. There is no hidden scheduler or shared mutable memory in these APIs.

`TaskGroup` provides structured concurrency for named zero-argument workers. `task_spawn(group, "worker")` registers each child with its group, `task_group_wait` joins every child, and the first worker failure cancels and waits for its siblings before returning the failure. `task_group_cancel` cancels all children and makes the group wait result an error. Child handles remain joinable after group completion, but the group owns their lifetime and no child is silently detached.

For intentionally shared state, `Shared[T]` is the only global mutable memory handle available to workers. `shared_new` creates a cell, `shared_read` takes a cloned snapshot under a read lock, `shared_write` replaces the value under an exclusive lock, and `shared_swap` performs an atomic replacement and returns the previous snapshot. `T` must be recursively `Copy`; callers cannot obtain a raw pointer or mutate the cell without one of these synchronized operations. This gives shared memory a controlled ownership boundary instead of exposing ordinary global bindings to workers.

`const` is deeply immutable, not merely an immutable name. Its initializer must be compile-time evaluable and its complete type graph may contain only primitive values, enums, immutable collections, and structs recursively composed of those values. Channels, threads, actors, `Shared[T]`, sockets, and other runtime handles are rejected even when hidden inside a struct or collection. Runtime bindings are also non-mutable, so there is no assignment path that can alter a const value after initialization.

## Modules

`import "path/to/module"` resolves a source file relative to the importing file. The `.kry` extension is optional. Absolute paths, `..` traversal, artifacts, missing files, duplicate exports, and cyclic imports are rejected. Only top-level declarations marked `pub` are exported. See [the module guide](modules.md) for the complete resolution contract.

## Runtime polymorphism

Developer applications can use a bounded runtime dispatch table when an integration needs to reorder implementations without recompiling. A handler must be a top-level function with the exact signature `fn(String) -> String`; runtime registration rejects incompatible names. `poly_register(slot, handler, priority)` inserts a handler in descending priority order, `poly_reorder(slot, handler, before)` moves an existing handler, and `poly_dispatch(slot, input)` invokes the first handler in the current order and returns a `Result[String, String]`. The table belongs to one runtime invocation, is not global mutable host state, and remains subject to call-depth, instruction, memory, and wall-clock limits.

```kryndel
fn json_handler(value: String) -> String { return "json:" + value }
fn text_handler(value: String) -> String { return "text:" + value }

let a: Result[Nil, String] = poly_register("render", "json_handler", 10)
let b: Result[Nil, String] = poly_register("render", "text_handler", 5)
match poly_dispatch("render", "hello") {
    ok(value) => { println(value) }
    err(message) => { println(message) }
} // json:hello
let c: Result[Nil, String] = poly_reorder("render", "text_handler", "json_handler")
match poly_dispatch("render", "hello") {
    ok(value) => { println(value) }
    err(message) => { println(message) }
} // text:hello
```

## Static checking

`kry check file.kry` lexes, parses, resolves imports, and type-checks the complete program without executing user code. `kry run` and `kry build` perform the same validation before evaluation or artifact creation. Unknown type names, invalid annotations, immutable assignments, bad conditions, unsafe operators, invalid indexing expressions, and builtin signature errors therefore fail before user output can occur.
