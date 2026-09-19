# Self-hosting stages

`elf_backend.kry` is the reusable lowering component of the first executable compiler written in Kryndel. `dynamic_backend.kry` adds the second lowering slice, and `kir_backend.kry` provides their command-line entrypoint. Together they consume the checked frontend's `kry-ir/v1` JSON and write a Linux x86-64 ELF64 file without invoking C, Go, or an external assembler.

`source_compiler.kry` is the next bootstrap stage. It contains a bounded but real source lexer and recursive-descent expression parser written in Kryndel itself. It handles comments, line separators, escaped strings, static `let`/`const` bindings, `print`/`println`, `str(...)`, parentheses, and the arithmetic precedence levels `* / %` above `+ -`. It evaluates that static subset and sends the resulting bytes through the same Kryndel ELF emitter.

`source_kir_compiler.kry` is the following frontend slice. It lexes and parses typed scalar functions, parameters, `return`, mutable bindings, assignment, Boolean conditions, `if`/`else`, `while`, `break`/`continue`, arithmetic and bitwise precedence, comparisons, logical operators, array literals and indexing, array concatenation, `len`, `array_push`, `array_get`, `array_concat`, scalar conversion calls, generic `Option[...]`/`Result[...]` annotations (including nested array payloads), their constructors/predicates/unwrapping operations, and output calls. It serializes the typed subset to KIR JSON in Kryndel, validates it with `json_parse`, and invokes `dynamic_backend.kry`. It is tested against the Go direct backend as a byte-level oracle.

The KIR stage accepts:

- top-level `let`/`const` bindings whose values are statically displayable;
- integer literals and checked `Int` `+`, `-`, `*`, `/`, `%` expressions;
- string literals, static variable references, `str(...)`, and string concatenation;
- top-level `print(...)` and `println(...)` calls.

The dynamic KIR stage additionally accepts top-level and nested `let`/`const`, mutable Int/Bool/UInt slots, assignments, checked signed arithmetic, wrapping fixed-width unsigned arithmetic, comparisons, bitwise operations, `if`/`else`, `while`, `break`, `continue`, functions with up to six SysV AMD64 register parameters, scalar `Int`/`Bool`/`UInt` values, immutable `String` pointers, immutable `Array[T]` pointers with literal/index/`len`/`array_push`/`array_get`/concatenation operations, immutable pointer-like `Option[T]` and `Result[T,E]` values with `some`/`none`/`ok`/`err`, predicates, `unwrap_or`, `result_unwrap`, and `result_error`, scalar/Array/Option/Result function-local declarations and assignments, static display values, and dynamic `Bool`/`Int`/`UInt`/`String` values inside `print`/`println`. It emits stack loads/stores, decimal integer and Boolean conversion, relative branches, calls/returns, RIP-relative references to immutable string objects, overflow traps, Linux `mmap`-backed String, qword-array, and tagged Option/Result allocation/copy runtimes, bounds checks, and Linux syscalls directly from Kryndel. Other dynamic String operations, Map/Set/resource values, unsupported array builtins, non-array heap values, `for`, `match`, and non-Linux targets remain explicit rejection points.

Unsupported arbitrary calls, dynamic String operations other than `+`, collection and resource values outside the documented Array slice, unsupported array builtins, non-array heap values, and non-Linux targets are rejected with explicit errors. This restriction is intentional while the lowering is being expanded.

The original source stage has the static output subset and performs its own lexical and syntactic validation instead of recognizing complete source lines by prefix. `source_kir_compiler.kry` owns the dynamic scalar source subset; both frontends reject unsupported constructs explicitly rather than guessing.

An end-to-end run from the repository root is:

```text
kry emit selfhost/fixtures/static_output.kry --target=linux-x64 --format=kry-ir -o input.kir
kry run selfhost/kir_backend.kry input.kir stage1

kry emit selfhost/fixtures/dynamic_output.kry --target=linux-x64 --format=kry-ir -o dynamic.kir
kry run selfhost/kir_backend.kry dynamic.kir dynamic-stage2

kry run selfhost/source_compiler.kry selfhost/fixtures/source_stage2.kry stage2
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_dynamic_stage3.kry stage3
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_scalar_functions_stage5.kry stage5
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_option_result_stage8.kry stage8
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/array_runtime_stage6.kry stage9
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_nested_generics_stage10.kry stage10
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_loop_control_stage11.kry stage11
kry emit selfhost/fixtures/option_result_runtime_stage7.kry --target=linux-x64 --format=kry-ir -o option-result.kir
kry run selfhost/kir_backend.kry option-result.kir option-result-stage7
```

The Go direct backend is kept as a byte-level oracle for these stages. Regression tests execute both Kryndel programs under the interpreter and require byte-identical ELF output before a change can pass. This is bootstrap progress, not yet a complete self-hosting compiler: functions, modules/import resolution, general heap values, linker/object-file support, and Windows target remain ahead of this subset.
