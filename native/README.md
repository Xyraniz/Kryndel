# Native implementation layout

Kryndel intentionally keeps one C11 translation unit in `native/kry.c` so the checker and runtime cannot drift into behaviorally different implementations. The file is organized into explicit native component sections: allocator and sources, diagnostics, lexer, type model, AST and parser, checker, value and scope runtime, builtin registry and execution, module loader, artifact reader and writer, formatter, REPL, doctor, and CLI.

This layout is a conscious trade-off for a small language distribution: the build has one semantic implementation and one linker target, while the section boundaries keep ownership and review responsibilities clear. If the implementation grows beyond this scale, the sections may be moved into `lexer.c/.h`, `parser.c/.h`, `types.c/.h`, `checker.c/.h`, `runtime.c/.h`, `builtins.c/.h`, `diagnostics.c/.h`, `artifact.c/.h`, and `cli.c/.h` without changing the public language contract. Such a split must preserve the same checker, runtime, builtin registry, and test path.

The source is compiled directly by `Makefile` and by `tools/kry`; no generated source, host-language bootstrap, or parallel interpreter participates in execution.
