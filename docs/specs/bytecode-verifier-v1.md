# Kryndel bytecode verifier v1

The verifier is a **bootstrap safety gate**, not a claim of native execution. The Python implementation validates the normalized bytecode model before any VM execution, while `stdlib/core/bytecode.kry` remains the source compatibility seam used for differential tests.

## Structural contract

The structural gate requires a non-empty v1 module name and entry, a non-empty function table, exact function key/name identity, non-negative arity, unique parameter names, a scalar constant table, valid source-line integers, known opcodes, and exact metadata shapes. `PUSH_CONST` indices, jump targets, sequence arities, call records, and nominal struct/enum/binding metadata are checked before a module reaches the VM. JSON decoders reject extra record fields, duplicate object keys, and non-finite numeric constants rather than silently normalizing them.

`MAKE_STRUCT` requires a non-empty string type and a unique ordered string field list. `MAKE_ENUM` accepts a string type and variant plus an optional non-negative payload arity. `MATCH_ENUM` requires the full type/variant/arity record. `BIND_ENUM` requires a source name, an arity equal to the binding count, and unique non-wildcard binding names. String-bearing opcodes reject non-string or empty arguments, and no-argument opcodes reject hidden metadata.

## Executable stack gate

`verify_execution` first runs the structural gate and then propagates reachable `(program counter, operand-stack depth)` states through every function. The fixed v1 stack effects are as follows: pushes add one value; `STORE`, `POP`, and conditional jumps consume one; `STORE_RESULT`, `MATCH_ENUM`, `GET_FIELD`, and `UNARY` preserve depth; `DUP` adds one; constructors replace their arity inputs with one value; `INDEX` and `BINARY` replace two inputs with one; `CALL` replaces its argument count with one; and `RETURN` consumes one value. `BIND_ENUM` and unconditional `JUMP` do not change the operand stack.

The analysis rejects underflow, negative depth, reachable fallthrough at the instruction-count boundary, incompatible stack depths at a control-flow join, and a stack above the 4096-value safety limit. Its worklist is bounded at 1024 distinct states so malformed loops cannot force unbounded analysis. Internal call targets and documented builtins must exist with matching arity; unknown callable names are rejected before runtime. This check preserves the source-level `verify_execution` rules while adding an equivalent Python-side preflight for production bootstrap paths.

## Diagnostic codes

| Code | Boundary | Meaning |
|---|---|---|
| `KRY7001` | module header | module version, name, entry, or function table is malformed |
| `KRY7002` | entry | declared entry function is absent |
| `KRY7003` | function identity | function table key and record name do not match |
| `KRY7004` | function metadata | arity, parameters, or scalar constant table is malformed |
| `KRY7005` | instruction | opcode, source line, or instruction table is malformed |
| `KRY7006` | instruction metadata | an opcode has an invalid argument shape or range |
| `KRY7007` | callable | an internal/builtin callable is unknown or has the wrong arity |
| `KRY7008` | executable stack | reachable stack flow is unsafe or exceeds verifier limits |

The code range is intentionally separate from runtime value and filesystem errors. CLI JSON output includes these codes through the existing structured diagnostic envelope.

## Decoding boundary

`Instruction.from_dict`, `BytecodeFunction.from_dict`, and `Module.from_dict` enforce the same exact field and scalar rules for direct callers. `Module.load` uses the strict JSON decoder. Artifact loading applies the same decoder after KEXE framing and checksum validation. Canonical serialization remains deterministic and byte-compatible for valid modules.

The negative fixture `tests/fixtures/bytecode-verifier-v1.json` retains its original structural cases, and `tests/fixtures/verification-boundary-v1.json` freezes the new code range and limits. The corresponding regressions cover duplicate keys, non-finite numbers, typed metadata, function identity, stack underflow, fallthrough, incompatible joins, unknown callables, and CLI JSON errors.

This remains **bootstrap Python**. It does not replace the native reader, native value runtime, native compiler, or native CLI, and it does not make a `.kexe` artifact a native executable.

## Source-level verifier milestone

`stdlib/core/bytecode.kry` defines nominal `InstructionRecord`, `FunctionRecord`, and `ModuleRecord` values and a serializable `VerifyResult` operation. Its source verifier remains an important differential oracle for the normalized schema, but the normal implementation path still enters `kryndel/vm.py`. A Kryndel-native reader, SHA-256 implementation, KEXE parser, runtime, and compiler are still required before Python can leave the normal path.
