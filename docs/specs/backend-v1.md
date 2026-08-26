# Kryndel direct backend seed contract v1

`stdlib/core/backend.kry` defines a deliberately narrow direct backend contract.
It accepts only a v1 `ModuleRecord` containing one empty `main` function and the
`x86_64-linux` target. `emit` emits deterministic GNU assembler text for a
Linux `_start` entry that exits with status zero, while `emit_exit` accepts an
explicit status in `0..255` and emits the same direct x86_64 `sys_exit` sequence.
Unsupported targets return `KRY8001`; non-seed modules return `KRY8002`; an
out-of-range status returns `KRY8003`.

The backend regression calls the source module twice and compares the emitted
assembly byte for byte, then checks statuses `0`, `7`, and `255` plus invalid
bounds. `tools/kry-seed` exposes the same optional status for a raw ELF seed.
This is a bootstrap-executed seed for validating a future direct backend
boundary, not a complete native code generator. Function calls, constants,
arithmetic, control flow, data layout, object formats, linking, and compiler to
backend integration remain unimplemented until the full compiler and native
runtime contracts are frozen.
