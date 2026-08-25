# Kryndel bytecode verifier v1

The structural verifier now shares the frozen opcode set with the bytecode model and rejects instructions outside that set before VM execution. It validates the version, entry function, function key/name identity, exact arity/parameter count, non-empty unique parameter names, source-line bounds, constant indices, jump targets, call metadata, sequence arities, and nominal metadata for structs and enums.

`MAKE_STRUCT` requires a string type and a unique ordered string field list. `MAKE_ENUM` accepts a string type and variant plus an optional non-negative payload arity. `MATCH_ENUM` requires the full type/variant/arity record. `BIND_ENUM` requires a source name, unique binding names, and an arity equal to the number of bindings. String-bearing opcodes reject non-string or empty arguments, and no-argument opcodes reject hidden metadata.

The negative fixture `tests/fixtures/bytecode-verifier-v1.json` covers unknown opcodes, parameter metadata, struct metadata, and enum binding metadata. A compiled valid module is verified in the same test. This is still the Python bootstrap verifier; it is not evidence of a Kryndel-native runtime.
