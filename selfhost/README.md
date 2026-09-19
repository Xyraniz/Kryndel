# Self-hosting stages

`elf_backend.kry` is the first executable compiler component written in Kryndel. It consumes the checked frontend's `kry-ir/v1` JSON and writes a Linux x86-64 ELF64 file without invoking C, Go, or an external assembler.

The current stage accepts:

- top-level `let`/`const` bindings whose values are statically displayable;
- integer literals and checked `Int` `+`, `-`, `*`, `/`, `%` expressions;
- string literals, static variable references, `str(...)`, and string concatenation;
- top-level `print(...)` and `println(...)` calls.

Dynamic bindings, arbitrary calls, control flow, functions, and non-Linux targets are rejected with explicit errors. This restriction is intentional while the lowering is being expanded.

An end-to-end run from the repository root is:

```text
kry emit selfhost/fixtures/static_output.kry --format=kry-ir -o input.kir
kry run selfhost/elf_backend.kry input.kir stage1
```

The Go direct backend is kept as a byte-level oracle for this stage. The regression test executes the Kryndel backend under the interpreter and requires byte-identical ELF output before a change can pass.
