# Self-hosting stages

`elf_backend.kry` is the reusable lowering component of the first executable compiler written in Kryndel. `dynamic_backend.kry` adds the second lowering slice, and `kir_backend.kry` provides their command-line entrypoint. Together they consume the checked frontend's `kry-ir/v1` JSON and write a Linux x86-64 ELF64 file without invoking C, Go, or an external assembler.

`source_compiler.kry` is the next bootstrap stage. It contains a bounded but real source lexer and recursive-descent expression parser written in Kryndel itself. It handles comments, line separators, escaped strings, static `let`/`const` bindings, `print`/`println`, `str(...)`, parentheses, and the arithmetic precedence levels `* / %` above `+ -`. It evaluates that static subset and sends the resulting bytes through the same Kryndel ELF emitter.

`source_kir_compiler.kry` is the following frontend slice. It lexes and parses typed functions (including `pub fn`), parameters, `return`, mutable bindings, assignment, Boolean conditions, `if`/`else`, `while`, `for` over arrays, `break`/`continue`, struct declarations and literals, field access, multiline array literals and indexing, array concatenation, `len`, `array_push`, `array_get`, `array_concat`, `array_indices`, scalar conversion calls, generic `Option[...]`/`Result[...]` annotations (including nested array payloads), opaque `Json`/`Map[...]` ABI values, their constructors/predicates/unwrapping operations, and output calls. It serializes the typed subset to KIR JSON in Kryndel, validates it with `json_parse`, and invokes `dynamic_backend.kry`. It is tested against the Go direct backend as a byte-level oracle.

The KIR stage accepts:

- top-level `let`/`const` bindings whose values are statically displayable;
- integer literals and checked `Int` `+`, `-`, `*`, `/`, `%` expressions;
- string literals, static variable references, `str(...)`, and string concatenation;
- top-level `print(...)` and `println(...)` calls.

The dynamic KIR stage additionally accepts top-level and nested `let`/`const`, mutable Int/Bool/UInt slots, assignments, checked signed arithmetic, wrapping fixed-width unsigned arithmetic, comparisons, bitwise operations, `if`/`else`, `while`, `for` over `Array[T]`, `break`, `continue`, functions with up to six SysV AMD64 register parameters, scalar `Int`/`Bool`/`UInt` values, immutable `String` pointers, immutable `Array[T]` pointers with literal/index/`len`/`array_push`/`array_get`/`array_indices`/concatenation operations, boxed struct literals and field loads, immutable pointer-like `Option[T]` and `Result[T,E]` values with `some`/`none`/`ok`/`err`, predicates, `unwrap_or`, `result_unwrap`, and `result_error`, scalar/Array/struct/Option/Result function-local declarations and assignments, static display values, and dynamic `Bool`/`Int`/`UInt`/`String` values inside `print`/`println`. It emits stack loads/stores, decimal integer and Boolean conversion, relative branches, calls/returns, RIP-relative references to immutable string objects, overflow traps, Linux `mmap`-backed String, qword-array, boxed-struct, and tagged Option/Result allocation/copy runtimes, bounds checks, and Linux syscalls directly from Kryndel. Other dynamic String operations, Map/Set/resource values, unsupported array builtins, general heap values outside the documented slices, `match`, and non-Linux targets remain explicit rejection points.

Unsupported arbitrary calls, dynamic String operations other than `+`, collection and resource values outside the documented Array/struct slice, unsupported array builtins, general heap values outside the documented slices, and non-Linux targets are rejected with explicit errors. This restriction is intentional while the lowering is being expanded.

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
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_public_function_stage12.kry stage12
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_opaque_abi_stage13.kry stage13
kry emit selfhost/fixtures/option_result_runtime_stage7.kry --target=linux-x64 --format=kry-ir -o option-result.kir
kry run selfhost/kir_backend.kry option-result.kir option-result-stage7
```

The Go direct backend is kept as a byte-level oracle for these stages. Regression tests execute both Kryndel programs under the interpreter and require byte-identical ELF output before a change can pass. This is bootstrap progress, not yet a complete self-hosting compiler: modules/import resolution, general heap values, linker/object-file support, and Windows target remain ahead of this subset.

Stage 14 extends the direct ELF oracle with boxed struct values (literals, field loads, function parameters and returns), scoped shadowing, `for` lowering over `Array[T]`, and `array_indices`. It is covered by an executable Linux regression fixture. Stage 17 mirrors this ABI in `dynamic_backend.kry`; the self-hosted emitter now produces byte-identical ELF and the Linux regression executes the generated `48\n` result.

Stage 15 adds the first direct-ELF host-runtime boundary needed by a bootstrap executable: `process_args`, Linux syscall-backed `fs_read_text`, `bytes(Array[Int])`, and syscall-backed `fs_write_bytes`. The regression exercises both a successful write and a rejected path, and verifies the resulting bytes.

Stage 16 mirrors that host boundary in `dynamic_backend.kry` itself. The Kryndel backend lowers the five runtimes directly to Linux x86-64 instructions, emits them in the same deterministic order as the Go oracle, and passes byte-for-byte ELF parity. On Linux amd64 the regression also executes the generated self-hosted ELF and verifies its arguments, file read, byte write, successful `Result`, and rejected path behavior. The bootstrap compiler still depends on the Go interpreter to run this Kryndel backend, but the emitted executable no longer depends on C, Go, libc, or an external assembler at runtime.

Stage 17 mirrors the Stage 14 aggregate and loop features in `dynamic_backend.kry`: struct field metadata is carried through the lowering environment, struct literals and field reads use the direct-backend boxed ABI, `for` lowers to checked indexed iteration, and `array_indices` returns a real heap array. The regression requires byte-for-byte parity with the Go oracle on Windows and Linux, and executes the generated ELF on Linux amd64.

Stage 18 extends `source_kir_compiler.kry` across that same aggregate boundary. The source frontend now registers struct definitions before parsing dependent functions, validates struct field declarations and literals, lowers chained field/index postfix expressions, preserves loop binding scope, and serializes struct metadata into KIR. Its regression feeds the Stage 14 source fixture through the Kryndel frontend and requires parity with the Go direct ELF oracle.

Stage 19 adds native direct-ELF lowering for `assert`, `assert_eq`, `contains`, `starts_with`, and `ends_with`. Assertions compare actual scalar values and branch to the checked trap path; string predicates scan the immutable `{length, bytes}` representation without libc. The source frontend recognizes these builtins so the next bootstrap probe reports the next unsupported operation precisely.

Stage 20 adds the direct-ELF `string_chars` runtime. It counts UTF-8 leading-byte sequences, allocates the result array through the existing checked `mmap` allocator, and creates each code-point string through the existing string allocator. The regression exercises ASCII plus multibyte `é` and `🙂` values and verifies their exact output on Linux amd64.

Stage 21 adds immutable direct-ELF maps backed by alternating key/value words. `map_get` and `map_contains_key` perform typed scalar or UTF-8 string lookup, while `map_insert` copies the map and replaces or appends without mutation. The regression covers replacement, missing-key fallback, and assertion paths.

Stage 22 adds checked direct-ELF `substring`. It interprets `start` and `length` as Unicode code-point indices, copies the selected UTF-8 byte range through the native string allocator, and returns an explicit `Result` error for invalid ranges.

Stage 23 adds direct-ELF and self-hosted dynamic-backend `str` lowering for dynamic `Int`, fixed-width `UInt`, `Bool`, and immutable `String` values. Signed and unsigned decimal conversion is performed in a stack buffer and copied into the native String ABI; the regression covers negative, zero, maximum `UInt8`, both Boolean values, multibyte UTF-8, byte-for-byte parity, and Linux execution.

Stage 24 adds direct-ELF and self-hosted dynamic-backend `int` lowering. `int(String)` accepts a complete ASCII decimal value with an optional leading sign, accumulates negative values so `-9223372036854775808` remains representable, and traps on empty, malformed, or overflowing input. `int(UIntN)` checks the signed `Int` range, while `int(Int)` and `int(Bool)` preserve their value. The regression covers both signed limits, signs, fixed-width unsigned values, Boolean values, exact ELF parity, Linux execution, and rejection of invalid input.

Stage 25 adds the first native JSON ABI used by the self-hosted KIR compiler. `Json` values are immutable `{length, bytes}` pointers, `json_parse` performs a bounded scanner and returns a typed `Result`, `json_kind` classifies validated values without allocation, and `json_object_get` scans object members with nested-container and quoted-string awareness before copying the selected value into a fresh JSON slice. The direct-ELF regression covers primitive kinds, nested arrays/objects, successful lookups, missing keys, malformed input, and exact Linux execution output. Array and scalar conversion accessors remain the next native lowering slice.
