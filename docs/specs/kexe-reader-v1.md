# Kryndel source KEXE framing reader v1

## Formato observado

El artefacto `.kexe` de Kryndel comienza con un encabezado de 44 bytes en
big-endian: ocho bytes de magia `KRYNEXE\x01`, un entero sin signo de 32 bits
con la longitud del payload y 32 bytes reservados para el digest SHA-256. El
payload comienza en el offset 44 y debe terminar exactamente en el tamaño total
del archivo.

`stdlib/core/artifact.kry` implementa `read_file(String) -> ReadResult` sobre la
capacidad `fs.read_bytes` y `read_header(Bytes) -> ReadResult`. Valida la
longitud mínima, la magia y la igualdad entre `44 + payload_length` y el
tamaño del buffer. Si la estructura es válida, devuelve un `Header` nominal con
la versión, offsets, longitud, bytes del checksum, su representación hexadecimal
minúscula de 64 caracteres y bytes del payload. Los bytes se copian a valores
`Bytes` nominales; no se devuelven mappings del host. `write_artifact_bytes`
construye la operación inversa sobre `Bytes`, conserva la longitud big-endian y
rechaza checksums que no midan exactamente 32 bytes.

| Caso | Resultado v1 |
| --- | --- |
| Encabezado menor de 44 bytes | `Error("KRY6305 ...")` |
| Magia distinta | `Error("KRY6305 ...")` |
| Longitud que no cubre exactamente el payload | `Error("KRY6305 ...")` |
| Encabezado válido | `Ok(Header)` con offsets 44 y `44 + length` |

## Cargador de artefactos del bootstrap

La ruta Python `kryndel.artifact.read_artifact` conserva el framing anterior y
agrega una segunda frontera verificable. El path debe referir a un archivo
regular que no sea symlink; el tamaño del payload está limitado a 16 MiB antes
de reservar o procesar el contenido; la lectura detecta cambios de tamaño; el
checksum se comprueba antes de interpretar el payload; y el JSON se decodifica
sin claves duplicadas, números no finitos ni campos extra en los registros de
módulo, función o instrucción. La escritura aplica el mismo límite y rechaza
salidas symlink.

| Código | Frontera | Resultado |
| --- | --- | --- |
| `KRY6301` | I/O/path | el artefacto no es un archivo regular, no puede inspeccionarse o no puede leerse |
| `KRY6205` | checksum | el digest del payload no coincide |
| `KRY6305` | framing/schema | magic, longitud, UTF-8, JSON, versión, metadata o módulo son inválidos |

La CLI `verify-artifact`, `inspect` y `run` ejecuta el verificador estructural y
el gate de stack después de leer el artefacto y antes de entregar el módulo a la
VM. `--format json` conserva el código, mensaje y archivo en el sobre de
 diagnóstico existente.

## Límites

Este es un lector/escritor de framing conectado al comparador SHA-256 fuente, no
un verificador nativo completo. El utility `tools/kry-kexe-check` añade un
checkpoint de host capability nativa mínima: lee el mismo encabezado con
utilities POSIX, comprueba magic, longitud, checksum y rechazo de symlinks sin
invocar Python. No decodifica JSON y no es todavía el lector del runtime
productivo; sus utilities host no forman parte del bundle final.

El campo de 32 bytes se extrae y se canonicaliza para
`stdlib/core/sha256.kry`; la regresión valida el payload `abc` y rechaza una
mutación con `KRY6205`. La implementación Python ahora decodifica y valida el
módulo contenido, pero todavía no existe un ejecutable Kryndel-native que lea
KEXE, ejecute la VM o posea las capabilities host. La lexer, parser, checker,
compiler, VM, CLI y la ruta normal de artefactos siguen siendo implementaciones
del bootstrap Python.

El fixture `tests/fixtures/kexe-reader-v1.json` y su regresión fijan la magia,
la longitud big-endian, los offsets, la extracción del digest, el payload, la
lectura desde `VirtualFileSystem` con rutas con espacios, el error de archivo
faltante y los rechazos de framing. `tests/fixtures/verification-boundary-v1.json`
fija además los límites del preflight Python. La siguiente ampliación separable
sigue siendo reemplazar esta lectura y validación por un lector nativo que
construya valores Kryndel sin entrar en `kryndel/vm.py`.
