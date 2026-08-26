# Kryndel source KEXE framing reader v1

## Formato observado

El artefacto `.kexe` de Kryndel comienza con un encabezado de 44 bytes en
big-endian: ocho bytes de magia `KRYNEXE\x01`, un entero sin signo de 32 bits
con la longitud del payload y 32 bytes reservados para el digest SHA-256. El
payload comienza en el offset 44 y debe terminar exactamente en el tamaño total
del archivo.

`stdlib/core/artifact.kry` implementa `read_file(String) -> ReadResult` sobre la
capacidad `fs.read_bytes` y `read_header(Bytes) -> ReadResult`. Valida
la longitud mínima, la magia y la igualdad entre `44 + payload_length` y el
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

## Límites

Este es un lector/escritor de framing conectado al comparador SHA-256 fuente, no un
verificador completo. El campo de 32 bytes se extrae y se canonicaliza para
`stdlib/core/sha256.kry`; la regresión valida el payload `abc` y rechaza una
mutación con `KRY6205`. Todavía no se decodifica el JSON del payload, no se
construye un `ModuleRecord` completo y no se ejecutan bytes KEXE. La lexer,
parser, checker, compiler, VM, CLI y la ruta normal de artefactos siguen siendo
implementaciones del bootstrap Python.

El fixture `tests/fixtures/kexe-reader-v1.json` y su regresión fijan la magia,
la longitud big-endian, los offsets, la extracción del digest, el payload, la
lectura desde `VirtualFileSystem` con rutas con espacios, el error de archivo
faltante y los rechazos de framing. La siguiente ampliación separable es decodificar el JSON fuente del payload y
construir un `ModuleRecord` validable; sólo después de esa etapa puede plantearse
verificar y ejecutar un KEXE completo sin el bootstrap.
