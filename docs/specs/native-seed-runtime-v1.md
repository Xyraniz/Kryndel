# Native seed runtime v1

## Propósito

`tools/kry-native-run` is a transitional no-Python launcher for a deliberately small
native runtime. It materializes a fixed x86_64 Linux ELF image from octal bytes and
executes that image directly. The runtime is intentionally smaller than the Kryndel
VM: its purpose is to prove an observable seed that can read one checked module,
write its payload to stdout, and return a deterministic status without a dynamic
loader or language runtime.

> This contract is **not** the Kryndel compiler, complete runtime, `.kexe` loader,
> normal CLI, package manager, or final bundle. The launcher still uses POSIX shell
> utilities to materialize the fixed ELF image; the generated ELF itself has no
> dynamic-loader dependency.

## KRYSEED1 framing

A module is a byte sequence with the following layout. All integer fields use
little-endian byte order.

| Offset | Size | Field | Requirement |
| --- | ---: | --- | --- |
| 0 | 8 | Magic | ASCII `KRYSEED` followed by version byte `0x01` |
| 8 | 4 | Payload length | Unsigned 32-bit length, including no header bytes |
| 12 | 1 | Mode | Reserved and currently required to be `0x00` |
| 13 | `payload_length` | Payload | At least one byte; maximum file size is 8192 bytes |

The native runtime opens the path supplied as its first argument, reads the entire
bounded module, validates the magic and exact total length, writes the payload bytes
to file descriptor 1 without transformation, and exits with the unsigned value of
the first payload byte. This intentionally small observable behavior is sufficient
to test file input, stdout, status propagation, malformed framing, and a real native
process boundary.

## Errors and limits

| Condition | Exit status |
| --- | ---: |
| Missing or unopenable module | 66 |
| Wrong magic, short read, or length mismatch | 65 |
| Valid module | First payload byte |

The current image reads at most 8192 bytes and does not parse JSON, SHA-256, KEXE,
bytecode, strings, or Kryndel source. The shell launcher reports usage errors as 64
and missing paths as 66 before invoking the native image.

## Acceptance

`tools/kry-native-run-check` runs with an isolated `PATH` and empty `HOME`. It uses
a spaced input directory and verifies valid output/status, malformed length, and a
missing module. The check does not call Python, GCC, LLVM, an assembler, a linker,
or a dynamic runtime. It must remain described as a **native seed runtime checkpoint**
until a Kryndel-generated runtime can replace the launcher and execute the full
bytecode/KEXE contract.
