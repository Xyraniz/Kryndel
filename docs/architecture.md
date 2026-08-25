# Kryndel Architecture

## Unit enums

The parser produces `EnumDecl`, `EnumVariantDecl`, and `EnumValue`. The checker registers enums before expressions, so a declaration may be used earlier in the same file. `EnumType` is nominal. The compiler emits deterministic `MAKE_ENUM` metadata such as `{ "type": "Color", "variant": "Green" }`; the VM creates `EnumValue(type_name, variant_name)` and prints it as `Color.Green`. Variants have no payload and `match` is not implemented.

Kryndel is organized as a sequence of explicit compiler phases. Each phase owns one kind of knowledge and communicates through a small data structure. The implementation currently targets a bundled stack virtual machine; the phase boundaries are designed to support a future native backend without changing the source language.

## Pipeline

```text
UTF-8 source
    |
    v
Lexer ----------------------> tokens + lexical diagnostics
    |
    v
Recursive-descent parser ----> AST + parse diagnostics
    |
    v
Static checker --------------> name/type diagnostics
    |
    v
Bytecode compiler -----------> Module
    |
    +-------------> JSON dump or KEXE artifact
    |
    v
Stack VM --------------------> runtime values and output
```

The compiler never executes user code. The VM is the only component that invokes runtime functions or performs side effects.

## Source and spans

`SourceFile` stores the original text and computes half-open spans. A span records byte offsets as well as one-based line and column values. Every token and AST node keeps a span so later diagnostics can highlight the construct that caused a failure.

The current diagnostic renderer is intentionally terminal-friendly. It prints a filename, one-based location, severity, stable code, source line, caret underline, and optional help text. The data model is independent of the renderer, which leaves room for JSON or language-server diagnostics later.

## Front end

The lexer is hand-written. It recognizes identifiers, reserved words, integer and floating-point literals, strings with a small explicit escape set, comments, punctuation, and operators. Nested block comments are supported. Unknown characters and incomplete strings are reported without allowing a malformed token to silently enter the parser.

The parser is a recursive-descent implementation. Each precedence level is represented by a method, which makes associativity and precedence visible in the source. The parser can recover at statement boundaries so multiple syntax errors can be displayed in one compilation.

The AST is a collection of dataclasses rather than an untyped dictionary tree. This keeps compiler code explicit and makes tests able to assert on the shape of a program without parsing implementation details.

## Type checker

The type checker performs declaration registration before checking executable items. It first registers struct names and their declaration-ordered fields, then registers function signatures so functions can call declarations that appear later in the file, and finally checks statements and expressions against lexical scopes. This permits a struct to be used before its declaration in the same source file without adding modules prematurely.

Bindings are immutable unless declared with `let mut`. A scope stack tracks shadowing and rejects duplicate names in one scope. Built-in functions are represented by the same `FunctionType` structure as user functions. This makes calls to `ui.window` and ordinary calls share one checking path. Structs use nominal `StructType` values with field metadata; constructors are checked for complete, unique, known, and type-compatible fields, while field access resolves to the declared field type.

The checker deliberately uses `Unknown` for error recovery. Once an unknown name or type has been reported, subsequent checks can continue and report independent failures without cascading into dozens of meaningless messages.

## Bytecode

The bytecode VM is stack-based. Instructions are small records with an operation, an optional argument, and a source line. A function owns its constants and instructions. The module owns named functions and an entry name.

Important instructions include:

| Instruction | Purpose |
| --- | --- |
| `PUSH_CONST` | Push a function constant. |
| `MAKE_STRUCT` | Consume field values in declaration order and create a typed struct value from stable metadata. |
| `GET_FIELD` | Consume a struct value and push a named field after runtime validation. |
| `PUSH_NIL` | Push the `nil` value. |
| `LOAD` | Load a local slot. |
| `STORE` | Store and consume a value. |
| `STORE_RESULT` | Store and leave the value on the stack. |
| `CALL` | Invoke a named function or built-in. |
| `BINARY` | Apply a checked binary operator. |
| `UNARY` | Apply a checked unary operator. |
| `JUMP` | Unconditional control-flow transfer. |
| `JUMP_IF_FALSE` | Conditional transfer after consuming a condition. |
| `JUMP_IF_TRUE` | Conditional transfer after consuming a condition. |
| `RETURN` | Return the top stack value. |

Compiler scopes map source names to internal slot names. A binding such as `value` becomes a unique internal key, which prevents a nested shadowing declaration from overwriting an outer runtime value.

Logical expressions use `DUP` and conditional jumps so the right-hand side is skipped when the left-hand side decides the result. Loops use patchable jump lists for exits and continues.

## Runtime

Each function call receives a fresh local dictionary and operand stack. The VM checks function arity, stack depth, local loads, instruction names, and artifact structure. `StructValue` retains the nominal type name and an ordered tuple of `(field_name, value)` pairs; it is not a generic dictionary. Runtime failures include the current function, source line when available, and the call stack, including explicit errors for malformed `MAKE_STRUCT` metadata and invalid `GET_FIELD` operands.

The standard library is intentionally small. Printing, conversions, string length, numeric helpers, a monotonic clock, and the deterministic UI tree are implemented directly in `vm.py`. No dynamic import is performed from Kryndel source.

## UI boundary

The current UI implementation is a platform-neutral tree. `UINode` stores a kind, properties, children, and optional callback metadata. Its renderer is deterministic, which makes it useful in tests and documentation.

This boundary is intentional. A real desktop backend requires decisions about window ownership, event dispatch, layout measurement, painting, threads, resource loading, accessibility, and platform distribution. Those decisions should be specified before an operating-system backend is connected to the language.

## KEXE format

The KEXE format is a portable Kryndel package, not a PE or ELF executable. Its fixed header contains:

```text
magic      8 bytes   KRYNEXE version marker
length     4 bytes   big-endian payload length
checksum  32 bytes   SHA-256 of the payload
payload     N bytes  deterministic module JSON
```

The loader validates all three header properties before parsing the payload. This gives the project a reproducible distribution boundary today and leaves room for a future native payload format.

## Extension points

The compiler can gain a second backend beside the bytecode compiler. A typed intermediate representation should be introduced before adding native code generation, because an IR can make ownership, calling convention, layout, and platform assumptions explicit. The existing `Module` contract should remain the stable artifact-level interface while backend-specific payloads evolve.
