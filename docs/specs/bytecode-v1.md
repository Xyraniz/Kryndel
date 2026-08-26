# Kryndel bytecode format v1

This is the stable bootstrap contract between the Python compiler and a future
runtime written in Kryndel. It is a deterministic UTF-8 JSON document:

```json
{
  "format": "kryndel-bytecode",
  "version": 1,
  "name": "source.kry",
  "entry": "main",
  "functions": {}
}
```

Object keys are serialized in sorted order. Function and instruction arrays
retain source/declaration order. A function contains `name`, `arity`,
`parameters`, `constants`, and `instructions`; an instruction contains `op`,
`arg`, and `line`.

The v1 instruction set includes `PUSH_CONST`, `PUSH_NIL`, `LOAD`, `STORE`,
`STORE_RESULT`, `POP`, `DUP`, `CALL`, `BINARY`, `UNARY`, `JUMP`,
`JUMP_IF_FALSE`, `JUMP_IF_TRUE`, `RETURN`, `MAKE_STRUCT`, `GET_FIELD`,
`MAKE_ENUM`, `MATCH_ENUM`, `BIND_ENUM`, `MAKE_ARRAY`, `MAKE_TUPLE`, and
`INDEX`.

`MAKE_ARRAY` and `MAKE_TUPLE` take a non-negative integer arity and consume
that many values in source order. They produce immutable nominal runtime
values. `INDEX` consumes a sequence and an `Int`; it produces one element and
fails with KRY6102 (non-Int index), KRY6103 (wrong sequence kind), or KRY6104
(out of bounds). Arrays concatenate with `BINARY +` when their element types
are compatible.

`MAKE_ENUM` metadata is `{ "type": name, "variant": name }` for unit
variants, or adds `arity` for positional payloads. Payload values are consumed
in declaration order and retained in the runtime's nominal `EnumValue`.
`MATCH_ENUM` metadata always contains `type`, `variant`, and `arity` and leaves
a boolean. `BIND_ENUM` contains a source local, an ordered list of internal
binding slots, and the arity. A runtime must reject malformed metadata and
stack underflow instead of exposing host exceptions.

## Linked project modules

Project-aware compilation keeps the root entry name `main`. Functions imported
from dependency modules are stored with their dotted module path, for example
`request.http.client.get`. A `CALL` instruction uses that exact qualified name.
The function-name string is therefore part of the module ABI for linked builds;
function and instruction arrays remain deterministic, and module function maps
are serialized with sorted object keys. Imported module top-level statements are
not executed implicitly; only linked function declarations contribute bytecode.

Version 1 is portable VM bytecode, not native machine code. A self-hosted
compiler must emit this contract byte-for-byte for reproducible builds or
explicitly select a future format version. Qualified names do not make a `.kexe`
file a native executable.

## Source-level compiler milestone

`stdlib/core/compiler.kry` lowers the tested parser subset into normalized
bytecode records: literals become `PUSH_CONST`, names become `LOAD`, members
become `GET_FIELD`, calls become `CALL`, struct literals become `MAKE_STRUCT`,
lets become `STORE`, expression statements become `POP`, and a deterministic
`RETURN` terminates `main`. The emitted module is accepted by the source
structural verifier in `stdlib/core/bytecode.kry`.

The source compiler currently stores the canonical textual spelling in its
normalized String constant array and uses `PUSH_CONST.text` as a narrow category
(`int`, `float`, `bool`, `string`, or legacy empty/text metadata). `nil` uses
`PUSH_NIL`. `typed-bytecode-v1.json` freezes this representation and the source
runtime decodes it into nominal `Int`, `Float`, `Bool`, `String`, and `Nil` values.
The source compiler still emits only a single `main` function. Full functions,
control flow, arithmetic, enums, imports, native constant-pool serialization,
module linking, and native bytecode serialization remain Python-owned.
