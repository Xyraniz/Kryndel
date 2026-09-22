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

The native backend accepts `windows-x64` and `windows-arm64` as PE header targets; the PE32+ artifact contains a real minimal entry point and an `ExitProcess` import. `kry inspect` validates the `MZ`/`PE\0\0` signatures, architecture, and section count without executing the file. Generating a complete Kryndel executable with every feature still requires broader IR lowering and native runtime linking; until then, the `KRYNATIVE3` bundle remains the complete portable path.
