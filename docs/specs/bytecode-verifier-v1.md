# Kryndel bytecode verifier v1

The structural verifier now shares the frozen opcode set with the bytecode model and rejects instructions outside that set before VM execution. It validates the version, entry function, function key/name identity, exact arity/parameter count, non-empty unique parameter names, source-line bounds, constant indices, jump targets, call metadata, sequence arities, and nominal metadata for structs and enums.

`MAKE_STRUCT` requires a string type and a unique ordered string field list. `MAKE_ENUM` accepts a string type and variant plus an optional non-negative payload arity. `MATCH_ENUM` requires the full type/variant/arity record. `BIND_ENUM` requires a source name, unique binding names, and an arity equal to the number of bindings. String-bearing opcodes reject non-string or empty arguments, and no-argument opcodes reject hidden metadata.

The source verifier also exposes a bounded branch-aware operand-stack
analysis through `verify_execution`. Every opcode declares a required depth and
a net delta; the verifier propagates `(program counter, depth)` states through
conditional and unconditional jumps, rejecting underflow, negative depth,
and any reachable program-counter equal to the instruction count before
execution. When a `CALL` target is declared in
the module, its arity must match; documented builtins remain valid targets.
Unknown targets are left to the runtime's stable unknown-function diagnostic.
This analysis is a source-level safety gate for the normalized record shape.
The state worklist is
bounded at 1024 states to avoid unbounded analysis of malformed loops. It does
not replace the Python production verifier.

The negative fixture `tests/fixtures/bytecode-verifier-v1.json` covers unknown opcodes, parameter metadata, struct metadata, and enum binding metadata. A compiled valid module is verified in the same test. This is still the Python bootstrap verifier; it is not evidence of a Kryndel-native runtime.

## Source-level verifier milestone

`stdlib/core/bytecode.kry` defines nominal `InstructionRecord`,
`FunctionRecord`, and `ModuleRecord` values and a `verify` operation that checks
version and header fields, entry presence, function arity/parameter metadata,
known opcodes, source-line bounds, constant indices, jump targets, call
metadata, sequence arities, and struct/enum/binding metadata. It returns a
serializable `VerifyResult` and uses `KRY6305` for malformed records.

The source verifier is exercised against valid and malformed normalized records
in `bytecode-native-verifier-v1.json`, including stack underflow, reachable
fallthrough without `RETURN`, and call-arity failures. The Python bootstrap still parses the
JSON bytecode container and remains the production verifier. A native reader,
SHA-256 implementation, KEXE parser, and differential byte-for-byte artifact
check are still required before Python can leave the normal path.
