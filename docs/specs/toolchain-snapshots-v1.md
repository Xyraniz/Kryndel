# Kryndel module graph and compiler snapshots v1

The bootstrap now exposes deterministic reports for the next toolchain seam. `kry graph project/src/main.kry` loads the project module graph without executing source or contacting a registry, then emits path-independent module IDs, package names, relative source paths, sorted imports, public symbols, and exported function type records. `kry compiler-report source.kry` emits the linked bytecode v1 JSON under a compiler contract wrapper.

Compiler snapshots normalize the source label to a basename before compilation, so absolute checkout paths cannot enter the report. Graph snapshots derive paths relative to each package root and sort records. The fixtures `graph-v1.json` and `compiler-v1.json` verify that repeated reports and different source-label locations produce equal output.

> These reports freeze bootstrap behavior for differential development. The graph, checker, and compiler implementations remain Python-owned; no self-hosting or external-runtime independence is claimed.

The next replacement step is a Kryndel-native checker/compiler that consumes the lexer and parser records, reproduces these interfaces and bytecode bytes, and then runs the same verifier and runtime acceptance matrix.
