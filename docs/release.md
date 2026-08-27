# Build and release guide

Kryndel can be built from source with any C11 compiler that supports the standard library, POSIX file APIs, and POSIX pthreads used by the native runtime. The tested release platforms are Linux and macOS:

```bash
make
./tools/kry version
```

`CC` may select a compiler explicitly. The launcher checks `CC`, then `cc`, then `gcc`, then `clang`; when none is available it exits with `69` and explains how to install or select a compiler. The launcher cache records the compiler path, flags, host system, architecture, source checksum, and compiler version, so a stale native binary is rebuilt automatically. The native executable links the system math library for numeric builtins and pthreads for channels and workers.

A reproducible verification build uses the following targets:

```bash
make clean
make test
make test-sanitized
make test-static
make check-docs
```

Artifacts are created only by `kry build`. The `KRYNATIVE2` container records the format version, compiler version, target, a deterministic source-entry order, exact source bytes, and SHA-256 hashes. Imported source modules are embedded, while timestamps, host paths, random identifiers, and generated binaries are excluded from the content. Writes are atomic and the decoder validates metadata, lengths, paths, hashes, duplicates, and trailing bytes before execution. Release automation should build on each supported platform and publish only binaries that actually completed that platform's build.

The repository CI uses GCC and Clang static checks, sanitizer tests, documentation audits, and the no-Python-bootstrap policy. A platform without a particular sanitizer may skip only that component with an explicit CI message; normal sanitizer jobs must not disable leak detection.
