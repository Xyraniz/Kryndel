# Kryndel ABI v1

Kryndel ABI v1 is the language-independent contract between a compiler and a
portable Kryndel runtime. The current implementation is the Python bootstrap;
this document does not claim a self-hosted compiler or native executable.

## Bytecode and calling convention

The bytecode container is deterministic UTF-8 JSON with `format:
kryndel-bytecode`, `version: 1`, a module name, an entry function, and a
function map. Each function records its arity, parameter names, constants, and
ordered instructions. Object keys are sorted during serialization while source
and declaration order is preserved in arrays.

A call evaluates arguments from left to right, places their values on the
operand stack, and executes a function identified by name and argument count.
The callee receives positional values bound to its parameter names. A return
places exactly one value on the stack; `nil` represents `Void` at the runtime
boundary. Runtime failures are reported as serializable Kryndel diagnostics by
the host CLI rather than as Python tracebacks.

## Linked modules

The root module entry is always `main`. An imported function uses its fully
qualified module name, such as `request.http.client.get`. That exact string is
stored in the `CALL` instruction and in the linked function map. Dependency
module top-level statements are not executed implicitly; only declarations
contribute linked bytecode in this milestone.

The module ABI currently supports exported functions with primitive parameter
and return types. Public structs and enums are recorded in the source-level
visibility model, but importing nominal types is intentionally deferred until
cross-module type identities and layouts are specified.

## Runtime layouts

Struct and enum layouts are already nominal and deterministic in bytecode v1.
A struct retains its type name and declaration-ordered field names. An enum
retains its type name, variant name, and ordered positional payload tuple. The
future array and string layouts are not frozen by this milestone because those
types are not yet implemented as Kryndel-native containers.

## Versioning and verification

A future runtime written in Kryndel must read this format without knowing
Python. It must reject unsupported versions, missing entry functions, malformed
instruction metadata, invalid constant indices, invalid jumps, and malformed
call metadata. `kry verify-bytecode` checks these structural invariants, while
`kry verify-artifact` first validates the KEXE header and SHA-256 payload before
checking the contained module.
