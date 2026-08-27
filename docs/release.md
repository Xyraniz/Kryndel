# Build and release guide

Kryndel can be built from source with any C11 compiler that supports the standard library and the POSIX file APIs used by the launcher:

```bash
make
./tools/kry version
```

`CC` may select a compiler explicitly. The launcher checks `CC`, then `cc`, then `gcc`; when none is available it exits with `69` and explains how to install or select a compiler. The native executable links the system math library for `sqrt`.

A reproducible verification build uses the following targets:

```bash
make clean
make test
make test-sanitized
make test-static
make check-docs
```

Artifacts are created only by `kry build`. The `KRYNATIVE1` container has a fixed ASCII header, an unsigned little-endian payload length, and the exact source bytes. No timestamp, host path, random identifier, or generated binary is inserted. Release automation should build on each supported platform and publish only binaries that actually completed that platform's build.

The repository CI uses GCC and Clang static checks, sanitizer tests, documentation audits, and the no-Python-bootstrap policy. A platform without a particular sanitizer may skip only that component with an explicit CI message; normal sanitizer jobs must not disable leak detection.
