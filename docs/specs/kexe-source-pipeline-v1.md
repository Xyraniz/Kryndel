# Kryndel source KEXE pipeline contract v1

## Alcance

La regresión `test_source_kexe_pipeline_decodes_verifies_and_runs` construye un
KEXE real con el serializador Python de referencia, después entrega sus bytes a
la cadena fuente por etapas independientes: `core/artifact.read_header`,
`core/sha256.verify`, `core/json.decode_bytecode_bytes`,
`core/bytecode.verify` y `core/runtime.run`. El artefacto contiene un módulo v1
con una función `main` que carga la constante textual `hello` y retorna.

| Etapa | Garantía observada |
| --- | --- |
| Framing | Se valida `KRYNEXE\\x01`, longitud big-endian y offsets 44/`44 + length` |
| Checksum | El digest extraído se canonicaliza y valida contra el payload |
| JSON | El payload UTF-8 se convierte en valores fuente y registros nominales |
| Schema | El subset `PUSH_CONST`/`RETURN` es aceptado por el verificador fuente |
| Runtime | El módulo decodificado retorna un `Value` fuente de tipo `String` con `hello` |
| Reproducibilidad | El fixture congela la serialización, longitud 460 y digest SHA-256 |

## Límites

La construcción inicial del archivo usa `write_artifact` y `Module.dumps()` de
Python únicamente como oráculo diferencial de la regresión; la lectura,
hashing, decodificación, verificación y ejecución bajo prueba son llamadas a
módulos Kryndel fuente ejecutados por la VM Python. La cadena no reemplaza el
CLI `kry run`, no cubre todas las instrucciones ni invariantes de bytecode, no
carga un KEXE directamente desde archivo dentro de una única función fuente y
no genera un binario nativo desde el módulo completo.

Por tanto, el resultado es una **pipeline fuente verificable para un subset**, no
self-hosting ni independencia operativa de Python. El siguiente trabajo debe
ampliar el schema y el lector binario gradualmente, y sólo después comparar la
ruta completa contra `read_artifact` y `VM` sin cambiar la matriz de dependencia
sin evidencia equivalente.
