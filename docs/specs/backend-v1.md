# Kryndel direct backend seed contract v1

`stdlib/core/backend.kry` defines a deliberately narrow direct backend contract.
It accepts a v1 `ModuleRecord` and the `x86_64-linux` target. `emit` preserves
the empty-main exit-zero seed, while `emit_exit` accepts an explicit status in
`0..255` and emits the same direct x86_64 `sys_exit` sequence. `emit_program`
accepts one `main` with no parameters and one of two exact shapes: one scalar
constant with `PUSH_CONST 0` followed by `RETURN`, or two scalar constants with
the six-instruction `PUSH_CONST`/`JUMP_IF_FALSE`/`PUSH_CONST`/`JUMP`/
`PUSH_CONST`/`RETURN` control-flow seed. It parses the exit constant in source
and emits that bounded status. Unsupported targets return `KRY8001`; non-seed empty
modules return `KRY8002`; an out-of-range explicit status returns `KRY8003`;
unsupported program shapes return
`KRY8004`; invalid program constants return
`KRY8005`.

The backend regression calls the source module twice and compares the emitted
assembly byte for byte, checks statuses `0`, `7`, and `255` plus invalid bounds,
and passes JSON-decoded `PUSH_CONST`/`RETURN` and conditional-jump modules
through `emit_program`, checking `je .L4` and `jmp .L5` markers and including
`KRY8005` for status 256. `tools/kry-seed` exposes the same optional
status for a raw ELF seed. This is a bootstrap-executed seed for validating a
future direct backend boundary, not a complete native code generator. Function
calls, general constants, arithmetic, control flow, data layout, object formats,
linking, and compiler-to-backend integration remain unimplemented until the full
compiler and native runtime contracts are frozen.
