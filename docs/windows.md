# Windows capabilities

Kryndel keeps an explicit boundary between the portable language and Windows-specific APIs. The internal `windows_api.go` module exposes registry operations, service queries, Event Log writes, Raw Input reads, and `DeviceIoControl`; on Windows the adapters call real tools or Win32 entry points, while other systems return a platform error instead of pretending compatibility.

| Capability | Windows adapter | Policy outside Windows |
| --- | --- | --- |
| Registry | `reg.exe query` with path and value separated. | Explicit platform error. |
| Services | `sc.exe query`. | Explicit platform error. |
| Event Log | `eventcreate.exe` on the Application channel. | Explicit platform error. |
| Raw Input | Boundary reserved for a host with a GUI message pump. | Explicit platform error. |
| `DeviceIoControl` | `CreateFileW`, `DeviceIoControl`, and `CloseHandle` with bounded buffers. | Explicit platform error. |

Process execution in the language uses `exec.CommandContext` without a shell; this prevents an input string from being interpreted as compound commands. Windows adapters also bound captured output. Privileged operations are never elevated automatically, and permission failures propagate as verifiable errors.

The CLI recognizes `windows-x64` and `windows-arm64` target aliases. The
`exe`/`pe` C AOT formats produce Windows amd64 PE through a MinGW-capable
`gcc`; `pe-direct` writes PE32+ itself without a C compiler, assembler, or
linker for a bounded scalar subset on Windows amd64: `Int`, `UInt`, `Bool`, and
`String` values; functions with up to four register arguments; `if`, `while`,
`break`, `continue`; and `print`/`println`. It emits PE imports and Win64 unwind
records directly. Arrays, maps, floats, runtime helpers, and arguments beyond
the four register slots fail with a diagnostic. Windows arm64 PE code generation
is not implemented. `kry inspect` recognizes the
`MZ`/`PE\0\0` signatures and reports architecture and section count without
executing the file; it is not a complete PE validator. Broader IR lowering
and a native runtime are still required for the full language. The
`KRYNATIVE4` bundle and interpreter cover the portable language features.

The [Windows x64 ABI contract](windows-abi.md) records the register, stack,
return, unwind, and import rules a direct PE backend must follow. CI runs the
Go test suite on a native `windows-latest` runner as well as cross-compiling
the CLI. A portable regression checks that `windows-x64` KIR retains checked
types and argument positions. Direct PE tests parse the image with Go's
independent PE reader and launch both static and dynamic `.exe` files on
Windows. The dynamic regression covers nested calls with four integer
arguments, mutable strings, signed number output, and `if`/`while` control
flow. Floating-point, aggregate, stack-passed, and host API calls remain outside
the direct backend's supported subset.
