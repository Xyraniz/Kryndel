# Native implementation layout

Kryndel keeps one semantic C11 translation unit so the checker and runtime cannot drift into behaviorally different implementations. The main `native/kry.c` file is organized into explicit sections for allocator and sources, diagnostics, lexer, type model, AST and parser, checker, value and scope runtime, builtin registry and execution, module loader, formatter, REPL, doctor, and CLI. The artifact reader/writer is extracted into `native/kry_artifacts.inc`, included exactly once at the artifact boundary and compiled as part of the same native path.

This layout is a conscious modularization boundary for a small language distribution: each component has one owner and the build has one linker target, while the include boundary makes artifact parsing and writing independently reviewable without duplicating semantic definitions. The shared invariants are deterministic serialization, strict validation, arena ownership, and the single checker/runtime pipeline. Future extraction into `.c/.h` pairs must preserve those same contracts and the existing test path.

The source and included native components are compiled directly by `Makefile` and by `tools/kry`; no generated source, host-language bootstrap, or parallel interpreter participates in execution. The launcher cache hashes every file in `native/` so component changes cannot leave a stale binary.
