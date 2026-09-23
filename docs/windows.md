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

The CLI recognizes `windows-x64` and `windows-arm64` target aliases, but the C AOT backend currently produces PE output only for Windows amd64 and requires a MinGW-capable `gcc`. Windows arm64 PE code generation is not implemented. `kry inspect` validates the `MZ`/`PE\0\0` signatures, architecture, and section count without executing the file. A generated executable still supports only the documented native backend subset; until broader IR lowering and native runtime linking are complete, the `KRYNATIVE4` bundle and interpreter remain the portable path.
