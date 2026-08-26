# Kryndel direct backend seed contract v1

`stdlib/core/backend.kry` defines a deliberately narrow direct backend contract.
It accepts a v1 `ModuleRecord` and the `x86_64-linux` target. `emit` preserves
the empty-main exit-zero seed, while `emit_exit` accepts an explicit status in
`0..255` and emits the same direct x86_64 `sys_exit` sequence. `emit_program`
accepts one `main` with no parameters and one of two exact shapes: one scalar
constant with `PUSH_CONST 0` followed by `RETURN`, or two scalar constants with
the six-instruction `PUSH_CONST`/`JUMP_IF_FALSE`/`PUSH_CONST`/`JUMP`/
`PUSH_CONST`/`RETURN` control-flow seed. It also exposes `emit_elf_program` for
one `PUSH_CONST`/`PUSH_CONST`/`BINARY(+)`/`RETURN` program with non-negative
integer constants whose sum is at most 255; this path constructs an ELF64 image
and x86_64 machine-code bytes directly, without assembler or linker.
Unsupported targets return `KRY8001`; non-seed empty
modules return `KRY8002`; an out-of-range explicit status returns `KRY8003`;
unsupported program shapes return
`KRY8004`; invalid program constants return
`KRY8005`; the bounded direct arithmetic path returns `KRY8006` for any other
shape and `KRY8007` for a negative or overflowing sum. `emit_elf_exit` additionally returns the exact 132-byte ELF64 seed as
nominal `Bytes` for the empty-main shape, patches the exit status directly in the
machine-code image, and rejects unsupported targets, non-seed modules, and status
values outside `0..255` with the corresponding backend errors.

The backend regression calls the source module twice and compares the emitted
assembly byte for byte, executes the direct `Bytes` ELF from a path containing a
space, checks statuses `0`, `7`, and `255` plus invalid bounds,
and passes JSON-decoded `PUSH_CONST`/`RETURN` and conditional-jump modules
through `emit_program`, checking `je .L4` and `jmp .L5` markers and including
`KRY8005` for status 256. The direct arithmetic fixture also executes the
ELF bytes from a path with spaces, checks exit status 42, and rejects an
overflowing sum with `KRY8007`. `tools/kry-seed` exposes the same optional status
for a raw ELF seed. This remains a bootstrap-executed bounded backend checkpoint,
not a complete native code generator: function calls, general constants,
subtraction/comparison, general control flow, data layout, object formats,
linking, and compiler-to-backend integration remain unimplemented until the full
compiler and native runtime contracts are frozen.
