# Kryndel nominal value layout v1

This specification freezes the first source-level representation boundary for the future native runtime. It is an executable compatibility seam, not a claim that the current bootstrap has stopped depending on Python.

## Contract

`stdlib/core/value.kry` defines the nominal `Value` sum type and the records carried by its variants. The discriminants are declaration ordered and stable:

| Discriminant | Payload |
| ---: | --- |
| `Nil` | none |
| `Int` | `IntValue(value: Int)` |
| `Float` | `FloatValue(value: Float)` |
| `Bool` | `BoolValue(value: Bool)` |
| `String` | `StringValue(value: String)` |
| `Bytes` | `BytesValue(items: Bytes)` |
| `Array` | `ArrayValue(items: Array)` |
| `Tuple` | `TupleValue(items: Array)` |
| `Struct` | `StructValue(type_name: String, fields: Array)` |
| `Enum` | `EnumValue(type_name: String, variant_name: String, payloads: Array)` |
| `Option` | `OptionValue.None` or `OptionValue.Some(Int)` |
| `Result` | `ResultValue.Ok(Int)` or `ResultValue.Error(String)` |
| `Function` | `FunctionValue(name, arity, parameters, constants, instructions)` |
| `Module` | `ModuleValue(name, entry, version, functions)` |
| `Instruction` | `InstructionValue(op, line, text, text2, number, names)` |
| `Diagnostic` | `DiagnosticValue(severity, code, message, span, notes, help, suggestion)` |
| `FileMetadata` | `FileMetadataValue(path, kind, size)` |

The fields in every record preserve the order declared in the source module. Sequence fields are immutable language-level `Array` or `Bytes` values; they are not host mappings. `Option` and `Result` remain intentionally non-generic in v1 and retain the existing payload contract.

## Constructors and fixture

The source module provides constructors for every `Value` variant, the `SpanValue` record, and the fixed layout version. `tests/fixtures/value-layout-v1.json` freezes the record fields, variant order, and sequence-field names. The core contract validator rejects missing, reordered, or altered layouts.

The regression suite executes the source module through the bootstrap VM and checks nominal type names, variant names, payload record fields, immutable sequence values, nested `Option`/`Result`, module/toolchain records, diagnostics, and file metadata. The fixture is canonical UTF-8 JSON with sorted object keys and a final newline.

## Ownership boundary

The module is currently interpreted by the Python VM. Its verified status is therefore **source layout compatibility under bootstrap Python**, not Kryndel-native runtime ownership. The next dependent work is to make the bytecode schema and KEXE loader consume these nominal records, then reproduce the same behavior from the native seed before retiring the bootstrap path.
