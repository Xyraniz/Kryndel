# Capacidades Windows

Kryndel conserva una frontera explícita entre el lenguaje portable y las APIs específicas de Windows. El módulo interno `windows_api.go` expone operaciones de registro, consulta de servicios, escritura de Event Log, lectura Raw Input y `DeviceIoControl`; en Windows los adaptadores llaman a herramientas o entry points Win32 reales, y en otros sistemas devuelven un error de plataforma en lugar de fingir compatibilidad.

| Capacidad | Adaptador Windows | Política fuera de Windows |
| --- | --- | --- |
| Registro | `reg.exe query` con ruta y valor separados. | Error explícito de plataforma. |
| Servicios | `sc.exe query`. | Error explícito de plataforma. |
| Event Log | `eventcreate.exe` en el canal Application. | Error explícito de plataforma. |
| Raw Input | Frontera reservada para un host con message pump GUI. | Error explícito de plataforma. |
| `DeviceIoControl` | `CreateFileW`, `DeviceIoControl` y `CloseHandle` con buffers acotados. | Error explícito de plataforma. |

La ejecución de procesos en el lenguaje utiliza `exec.CommandContext` sin shell; esto evita que una cadena de entrada se interprete como comandos compuestos. Los adaptadores Windows también limitan la salida capturada. Las operaciones privilegiadas no se elevan automáticamente y los fallos de permisos se propagan como errores verificables.

El backend nativo acepta `windows-x64` y `windows-arm64` como targets de cabecera PE; el artefacto PE32+ contiene una entrada mínima real y una importación `ExitProcess`. `kry inspect` valida las firmas `MZ`/`PE\0\0`, arquitectura y número de secciones sin ejecutar el archivo. La generación de un ejecutable Kryndel completo con todas las funciones aún debe ampliar el lowering de IR y enlazar el runtime nativo; hasta entonces el bundle `KRYNATIVE3` sigue siendo la ruta portable completa.
