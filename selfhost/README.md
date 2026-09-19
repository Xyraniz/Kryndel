# Self-hosting stages

`elf_backend.kry` is the reusable lowering component of the first executable compiler written in Kryndel. `dynamic_backend.kry` adds the second lowering slice, and `kir_backend.kry` provides their command-line entrypoint. Together they consume the checked frontend's `kry-ir/v1` JSON and write a Linux x86-64 ELF64 file without invoking C, Go, or an external assembler.

`source_compiler.kry` is the next bootstrap stage. It contains a bounded but real source lexer and recursive-descent expression parser written in Kryndel itself. It handles comments, line separators, escaped strings, static `let`/`const` bindings, `print`/`println`, `str(...)`, parentheses, and the arithmetic precedence levels `* / %` above `+ -`. It evaluates that static subset and sends the resulting bytes through the same Kryndel ELF emitter.

`source_kir_compiler.kry` is the following frontend slice. It lexes and parses mutable bindings, assignment, Boolean conditions, `if`/`else`, `while`, comparisons, logical operators, and output calls, serializes the typed subset to KIR JSON in Kryndel, validates it with `json_parse`, and invokes `dynamic_backend.kry`. It is tested against the Go direct backend as a byte-level oracle.

The KIR stage accepts:

- top-level `let`/`const` bindings whose values are statically displayable;
- integer literals and checked `Int` `+`, `-`, `*`, `/`, `%` expressions;
- string literals, static variable references, `str(...)`, and string concatenation;
- top-level `print(...)` and `println(...)` calls.

The dynamic KIR stage additionally accepts top-level and nested `let`/`const`, mutable Int/Bool/UInt slots, assignments, checked signed arithmetic, wrapping fixed-width unsigned arithmetic, comparisons, bitwise operations, `if`/`else`, `while`, `break`, `continue`, scalar functions with up to six SysV AMD64 register parameters and scalar `Int`/`Bool`/`UInt` returns, static display values, and dynamic `Int`/`UInt` values inside `print`/`println`. It emits stack loads/stores, decimal integer conversion, relative branches, calls/returns, RIP-relative data references, overflow traps, and Linux syscalls directly from Kryndel. String/collection/resource parameters and returns, function-local declarations, `for`, `match`, heap values, and non-Linux targets remain explicit rejection points.

Unsupported arbitrary calls, non-scalar function parameters/return values/locals, heap values, and non-Linux targets are rejected with explicit errors. This restriction is intentional while the lowering is being expanded.

The original source stage has the static output subset and performs its own lexical and syntactic validation instead of recognizing complete source lines by prefix. `source_kir_compiler.kry` owns the dynamic source subset; both frontends reject unsupported constructs explicitly rather than guessing.

An end-to-end run from the repository root is:

```text
kry emit selfhost/fixtures/static_output.kry --target=linux-x64 --format=kry-ir -o input.kir
kry run selfhost/kir_backend.kry input.kir stage1

kry emit selfhost/fixtures/dynamic_output.kry --target=linux-x64 --format=kry-ir -o dynamic.kir
kry run selfhost/kir_backend.kry dynamic.kir dynamic-stage2

kry run selfhost/source_compiler.kry selfhost/fixtures/source_stage2.kry stage2
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_dynamic_stage3.kry stage3
```

The Go direct backend is kept as a byte-level oracle for these stages. Regression tests execute both Kryndel programs under the interpreter and require byte-identical ELF output before a change can pass. This is bootstrap progress, not yet a complete self-hosting compiler: functions, modules/import resolution, general heap values, linker/object-file support, and Windows target remain ahead of this subset.
