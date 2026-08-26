# Kryndel direct backend seed contract v1

`stdlib/core/backend.kry` defines a deliberately narrow direct backend contract.
It accepts only a v1 `ModuleRecord` containing one empty `main` function and the
`x86_64-linux` target. It emits deterministic GNU assembler text for a Linux
`_start` entry that exits with status zero via the x86_64 `sys_exit` syscall.
Unsupported targets return `KRY8001`; non-seed modules return `KRY8002`.

The backend regression calls the source module twice and compares the emitted
assembly byte for byte. This is a bootstrap-executed seed for validating a
future direct backend boundary, not a complete native code generator. Function
calls, constants, arithmetic, control flow, data layout, object formats, and
linking remain unimplemented until the full compiler and native runtime
contracts are frozen.
