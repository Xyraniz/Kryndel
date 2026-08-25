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
`MAKE_ENUM`, `MATCH_ENUM`, and `BIND_ENUM`.

`MAKE_ENUM` metadata is `{ "type": name, "variant": name }` for unit
variants, or adds `arity` for positional payloads. Payload values are consumed
in declaration order and retained in the runtime's nominal `EnumValue`.
`MATCH_ENUM` metadata always contains `type`, `variant`, and `arity` and leaves
a boolean. `BIND_ENUM` contains a source local, an ordered list of internal
binding slots, and the arity. A runtime must reject malformed metadata and
stack underflow instead of exposing host exceptions.

Version 1 is portable VM bytecode, not native machine code. A self-hosted
compiler must emit this contract byte-for-byte for reproducible builds or
explicitly select a future format version.
