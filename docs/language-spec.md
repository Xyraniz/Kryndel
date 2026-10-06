# Kryndel language specification

This document defines source-language behavior. It is normative for the current
Kryndel dialect. Implementation descriptions in other documents do not change
these rules. A backend that cannot preserve a rule must reject the program
before producing an executable.

The CLI checks a complete source graph before `run` executes user statements or
`build` writes an artifact. Lexing, parsing, import resolution, and type errors
therefore happen before source-level effects.

## Program and evaluation order

Types and function signatures are collected before function bodies and
top-level statements are checked. A function can be called before its
declaration. Runtime top-level statements execute in source order; top-level
bindings become available after their initializer completes. Type and function
declarations do not execute as statements. If a program has no top-level
statements, the runtime calls `main` when it is defined.

Within a statement, initializers and conditions are evaluated before their
binding or branch body. Expressions evaluate left to right:

- A binary expression evaluates its left operand before its right operand.
- `&&` and `||` evaluate the right operand only when the left value does not
  decide the result.
- Array items, map entries, struct field values, call arguments, and default
  arguments evaluate in written or declaration order. For a map entry, the key
  evaluates before its value.
- A method receiver evaluates before its explicit arguments. Omitted trailing
  function arguments use their declared defaults after all explicit arguments
  have evaluated.
- An indirect call evaluates its callee before its explicit arguments.
- Indexing evaluates the collection before the index.

An evaluation error stops the current expression and propagates to the enclosing
statement. Later operands, arguments, entries, or statements are not evaluated.

## Names, bindings, and scopes

`let` binds an immutable value. `let mut` permits assignment to that binding;
assignment changes the nearest binding with that name. `const` is immutable and
requires a compile-time initializer whose complete value type is const-safe.

Each block creates a lexical scope. A declaration may shadow a name in a parent
scope but may not redeclare a name in the same scope. Lookup and assignment
search from the innermost scope outward. Branches, loop bodies, match arms, and
function calls do not leak their local bindings after they exit.

Values are passed by value. Strings, bytes, arrays, maps, sets, options, results,
structs, and enums are immutable after construction; operations that appear to
update collections return new values. A closure retains the lexical bindings it
uses. Capturing a `let` binding preserves its immutable value; capturing a
`let mut` binding shares that binding, so later assignments are visible to every
closure that captured it. A captured binding remains alive while a closure that
uses it remains reachable. Mutable sharing outside closures occurs only through
explicit handle APIs such as `Shared[T]`, channels, actors, and runtime resource
handles. Handle types are excluded from the structural `Copy` constraint.

## Functions, calls, and control flow

Function declarations are statically resolved, and a function can also be used
as a value. A function type is written `fn(T1, T2) -> R`; for example,
`fn(Int, String) -> Bool`. A lambda uses the same signature and a block body:
`fn(value: Int) -> Int { return value + 1 }`. Lambda parameters require explicit
types and cannot declare default values. An omitted lambda return type means
`Nil`.

Functions can be passed as arguments, returned, stored in bindings, and called
through any expression with a function type. Function references to overloaded
declarations require an expected function type that selects exactly one
overload. A generic function reference also needs enough expected type
information to resolve every generic parameter. Calls through a function value
require every declared argument; default arguments are not part of a function
type. Functions and closures are not comparable and cannot be `const` or satisfy
the structural `Copy` constraint, so they cannot cross thread or actor message
boundaries.

The interpreter supports function values and closures. Current C AOT, direct ELF,
and direct PE backends reject a program that uses function types, function
references, or lambdas with a backend-specific diagnostic before emission.

Parameters have explicit types; generic type parameters use the declared `Copy`,
`Integer`, `Numeric`, or `Comparable` constraints, or one user-defined trait
constraint. Overloads are selected from the complete argument type tuple. Zero
matches and multiple equally specific matches are type errors.

Method calls use the receiver's checked type and the same overload rules for
their explicit arguments. A private method is visible only within its declaring
module. The checker-selected declaration is retained in KIR and is the target
the interpreter invokes; runtime dispatch does not select a method again by
name.

Traits define named method signatures for static dispatch:

```kryndel
trait Render {
    fn render() -> String
}

struct North { label: String }
struct South { label: String }

impl Render for North {
    fn render() -> String { return "north:" + self.label }
}
impl Render for South {
    fn render() -> String { return "south:" + self.label }
}

fn render[T: Render](value: T) -> String { return value.render() }
```

The receiver is implicit in a trait method signature and is named `self` in an
implementation. Each implementation must provide every declared method exactly
once with the same explicit parameters and return type. A concrete type may
implement a trait only once. Generic struct instances can be implementation
targets when fully specified, such as `impl Render for Box[Int]`; generic
functions with a `T: Render` bound are checked against the declared signatures
and resolved to the concrete implementation after `T` is inferred. The
interpreter uses that checked target, and C AOT specializes generic calls and
emits direct calls to the implementation.

This static-dispatch slice supports non-generic traits and methods and one trait
bound per generic parameter. Generic traits or methods, default method bodies,
associated types or constants, generic and blanket implementations, multiple
bounds, `dyn Trait`, and implementations for non-struct types are rejected with
diagnostics. Traits are not runtime values.

Generic calls use structural type inference with the following rules:

| Parameter or result type | Inference rule |
| --- | --- |
| `T` | Infer `T` from the corresponding argument and check its declared constraint. |
| `Array[T]`, `Option[T]`, `Result[T, E]`, `Map[K, V]`, and other supported generic containers | Match the container constructor, then recursively infer each type argument. |
| Repeated `T` in multiple parameters | Every occurrence must resolve to the same type. |
| Generic result with an unresolved parameter | The expected result type may infer remaining parameters; otherwise the call has no matching overload. |
| Competing overloads | Apply constraints and argument inference to each candidate; exactly one candidate must match. |

User-defined structs may declare constrained type parameters. A use must
provide exactly one argument for each parameter, and each argument must satisfy
the declaration's constraint. `Box[T]` field types are substituted for a
concrete `Box[Int]` instance; struct literals spell the instance explicitly as
`Box[Int]{value: 1}`. An `impl Box[T]` block makes methods available to every
matching instance. Methods may add their own constrained type parameters, and
receiver and method parameters are inferred together from the receiver,
arguments, and expected result. Generic enums and generic associated constants
are not supported.

Type nesting is bounded by the configured `MaxTypeDepth` resource limit.
Overload candidate sets are bounded by `MaxOverloadsPerName` (256 by default)
so a call cannot trigger an unbounded overload search.
`Integer` accepts `Int` and all `UInt` widths, and enables `%` in generic
bodies. `Numeric` accepts `Integer` types and `Float`, and enables `+`, `-`,
`*`, `/`, and ordered comparisons. Generic unary negation remains unavailable
because a `Numeric` or `Integer` parameter may be an unsigned type. Concrete
`Float` remainder is rejected by the checker.
Generic parameters may infer a function type when a function accepts or returns
one, subject to their declared constraints. In a generic higher-order call, an
explicitly typed lambda can infer its parameter types even when the callback is
listed before the value that supplies the same type parameter. Overloaded or
generic named function references are resolved after the other arguments and
expected return type have supplied enough information. Generic methods use the
same inference rules as generic functions.

Generic enums, generic associated constants, and explicit monomorphization
controls are not implemented. Generic user-defined structs and their methods
are supported with checked type substitution and C AOT monomorphization for
concrete uses.

Explicit call arguments evaluate left to right. Omitted trailing defaults are
evaluated in declaration order in the callee's lexical environment,
with `self` and earlier parameters available. A named function does not see its
caller's local bindings; a closure sees only the lexical bindings it captured.
`return` exits the current function. `break` and `continue` affect the innermost
loop. `match` evaluates its scrutinee once, then selects the first matching arm; the checker rejects
non-exhaustive alternatives for enums, `Option`, and `Result`, as well as
duplicate alternatives.

`kry check FILE` reports source warnings for constant Boolean conditions,
constant-true loops without a `break`, repeated match arms, wildcard arms that
hide remaining enum/option/result cases, and statements after an unconditional
exit. Warnings have stable `KRYW001`–`KRYW004` codes and do not change execution.
`KRYW001` covers redundant match arms, `KRYW002` constant conditions,
`KRYW003` non-terminating loops, and `KRYW004` unreachable statements or arms.
`kry check -Werror FILE` reports all enabled diagnostics as errors and exits
with status 1 when any warning is present. `-Werror=KRYW002,KRYW004` promotes
only the selected rules, and `-Wno=KRYW003` disables selected rules. Unknown
warning codes are usage errors.

`defer` records its body for the end of the current lexical scope. Deferred
bodies execute in reverse registration order on normal exit, `return`, `break`,
`continue`, or a runtime error. Every deferred body runs even when an earlier
deferred body fails. The first pending runtime error is preserved; if execution
was otherwise successful, the first deferred-body error becomes the result.
Top-level deferred bodies run when top-level execution finishes.

## Numeric and comparison behavior

`Int` is signed 64-bit with checked addition, subtraction, multiplication,
division, remainder, negation, and absolute value. Overflow and division or
remainder by zero are runtime errors. `UInt8`, `UInt16`, `UInt32`, and `UInt64`
arithmetic wraps modulo its declared width. Shifts require a nonnegative `Int`
count smaller than the left operand's width.

`Float` is finite IEEE-754 binary64. NaN and infinities are rejected. Arithmetic
that produces a non-finite value fails. Positive and negative zero compare equal;
formatting preserves negative zero. Conversion from an integer to `Float`
rounds to the nearest representable binary64 value and may lose precision.
Conversion from `Float` to `Int` truncates toward zero and accepts exactly the
half-open interval `[-2^63, 2^63)`; other values fail. Parsing a `Float` from a
string requires the entire string to be a finite decimal number.

Equality is type-exact and structural for arrays, maps, sets, options, results,
and structs. Float equality follows finite IEEE equality, including equality of
the two signed zeroes. String equality compares UTF-8 contents without
normalization. Handle equality compares handle identity. Function values and
composite values containing functions cannot be compared. Map and set equality
also compares iteration order.

## Collections and keys

Arrays, maps, and sets are immutable ordered values. Arrays retain element order.
Maps and sets retain insertion order, and iteration and conversion to arrays use
that order. `map_insert` replaces an existing value without moving its key. A
key removed and inserted again is appended at the end. `set_insert` ignores an
existing equal value and preserves the first occurrence.

A map literal with duplicate keys is a runtime error. This differs from
`map_insert`, which replaces an existing value. Map keys are restricted to
`Int`, `UInt`, `Bool`, or `String`; `Float` is not a key type. Equality, not hash
bucket order, determines key lookup. The implementation has no observable hash
iteration order.

## Strings, bytes, and aliasing

Source and `String` values are valid UTF-8. String length, indexing, character
iteration, and substring offsets count Unicode code points, not bytes or
grapheme clusters. Strings are not normalized. `Bytes` is an opaque sequence of
octets; conversion between strings and bytes validates UTF-8 at the string
boundary.

Collection updates and ordinary assignment do not mutate a value through an
alias. Copies can share immutable storage. Handle APIs are the explicit
exception: copying a non-`Copy` handle is rejected by generic `Copy` constraints
and cross-thread transfer checks, but the language does not yet have a general
borrow checker or lifetime syntax. APIs that take resource handles must state
whether they borrow or close them. SQLite, TCP, UDP, WebSocket, FFI library, and
FFI buffer handles report use-after-close through their normal failure channel;
an explicit second close reports a runtime diagnostic. At the end of a top-level
invocation or worker, any still-open tracked handle produces a `resource`
diagnostic at its acquisition site and the runtime closes it during cleanup.
Channels and task-group shutdown remain idempotent synchronization operations,
not owned external resources.

## Concurrency

Worker functions have declared return types and are registered by name. A
`thread_spawn` worker name must be a string literal naming a zero-argument
function. Channels, actors, shared cells, and task groups are explicit
synchronization boundaries. Values sent through channels or actors must satisfy
the recursive `Copy` rule. A worker receives only the channel and shared handles
allowed by the checker; ordinary mutable global bindings are not visible to it.

Channel close wakes blocked senders and receivers. The interpreter cancels
workers, closes channels, and joins outstanding workers during invocation
shutdown. Runtime polymorphism uses a per-invocation dispatch table with a fixed
`String -> String` handler signature; it is not a general function-value ABI.
Literal handler names passed to `poly_register` and `poly_reorder` are checked
early and must resolve to one unambiguous top-level `fn(String) -> String`.
String expressions may also supply names dynamically; the runtime validates
their signatures and registration state and reports failures as recoverable
`Result` values.
Native backends support only the concurrency features listed in their support
contracts and must reject unsupported calls.

## Errors and process results

Static errors, runtime diagnostics, and recoverable `Result[T, E]` values are
distinct. A runtime diagnostic terminates the current invocation after deferred
cleanup. `?` propagates a failed `Option` or `Result` only when the containing
function returns the same fallible kind. For `Result`, the error type must be
compatible with the containing function's error type; the successful value may
have a different type and is checked where the `?` expression is used. For
`Option`, any optional value may propagate into an `Option`-returning function.
Host operations return `Result` where the builtin contract declares it; the
caller may handle those failures.

CLI diagnostics expose a stable category and code (`KRY001` through `KRY008`),
severity, source, line, column, and a human-readable message. They do not
currently carry a structured cause chain. Interpreter diagnostics include
ordered call-site frames for user functions; native backend traps do not yet
provide equivalent frames, and tail-call optimization can omit repeated tail
frames. Most recoverable standard-library errors use `String` payloads, so their
wording is not a stable machine-readable API. A standard structured error value
and uniform cross-thread serialization are not yet implemented.

## Language-version and compatibility policy

The language version is independent of the compiler's release number and uses
`MAJOR.MINOR.PATCH`:

- A patch release fixes an implementation defect to match this specification
  and makes documentation clarifications without changing specified behavior.
- A minor release adds syntax, types, or library operations while preserving
  existing programs' meaning.
- A major release may remove deprecated features or change syntax, typing, or
  runtime semantics incompatibly.

Before removing a feature, the compiler must warn that it is deprecated for at
least one published minor release and one year, whichever is longer. The
manifest, KIR, and portable artifact carry the selected language version.
Unknown major versions must fail with an explicit compatibility diagnostic;
the compiler must not reinterpret them as the current dialect. Each published
language version must retain source, stdout, exit-status, and diagnostic fixtures
under `tests/compat/<version>/`.

The current compiler supports language version 1.0.0. New manifests, KIR v6,
and KRYNATIVE6 artifacts record it explicitly. For compatibility, manifests
without the field, KIR v1 through v5 documents, and KRYNATIVE3 through KRYNATIVE5 artifacts
are interpreted as 1.0.0. Unknown or malformed versions are rejected instead
of being silently treated as the current dialect.
