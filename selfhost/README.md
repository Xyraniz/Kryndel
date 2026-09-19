# Self-hosting stages

`elf_backend.kry` is the reusable lowering component of the first executable compiler written in Kryndel. `dynamic_backend.kry` adds the second lowering slice, and `kir_backend.kry` provides their command-line entrypoint. Together they consume the checked frontend's `kry-ir/v1` JSON and write a Linux x86-64 ELF64 file without invoking C, Go, or an external assembler.

`source_compiler.kry` is the next bootstrap stage. It contains a bounded but real source lexer and recursive-descent expression parser written in Kryndel itself. It handles comments, line separators, escaped strings, static `let`/`const` bindings, `print`/`println`, `str(...)`, parentheses, and the arithmetic precedence levels `* / %` above `+ -`. It evaluates that static subset and sends the resulting bytes through the same Kryndel ELF emitter.

The KIR stage accepts:

- top-level `let`/`const` bindings whose values are statically displayable;
- integer literals and checked `Int` `+`, `-`, `*`, `/`, `%` expressions;
- string literals, static variable references, `str(...)`, and string concatenation;
- top-level `print(...)` and `println(...)` calls.

The dynamic KIR stage additionally accepts top-level and nested `let`/`const`, mutable Int/Bool slots, assignments, checked signed arithmetic, comparisons, bitwise operations, `if`/`else`, `while`, `break`, `continue`, and static display values inside `print`/`println`. It emits stack loads/stores, relative branches, RIP-relative data references, overflow traps, and Linux syscalls directly from Kryndel. Dynamic integer formatting, functions, `for`, `match`, heap values, and non-Linux targets remain explicit rejection points.

Dynamic bindings, arbitrary calls, control flow, functions, and non-Linux targets are rejected with explicit errors. This restriction is intentional while the lowering is being expanded.

The source stage has the same output subset but performs its own lexical and syntactic validation instead of recognizing complete source lines by prefix. It still rejects mutable bindings, control flow, arbitrary calls, floating-point literals, and unsupported operators explicitly.

An end-to-end run from the repository root is:

```text
kry emit selfhost/fixtures/static_output.kry --target=linux-x64 --format=kry-ir -o input.kir
kry run selfhost/kir_backend.kry input.kir stage1

kry emit selfhost/fixtures/dynamic_output.kry --target=linux-x64 --format=kry-ir -o dynamic.kir
kry run selfhost/kir_backend.kry dynamic.kir dynamic-stage2

kry run selfhost/source_compiler.kry selfhost/fixtures/source_stage2.kry stage2
```

The Go direct backend is kept as a byte-level oracle for these stages. Regression tests execute both Kryndel programs under the interpreter and require byte-identical ELF output before a change can pass. This is bootstrap progress, not yet a complete self-hosting compiler: the general frontend, dynamic integer formatting, function lowering, linker/object-file support, and Windows target remain ahead of this subset.
