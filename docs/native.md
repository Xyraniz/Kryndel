# Implementación nativa

`native/kry.c` es el núcleo completo de la distribución actual. Se compila como un ejecutable C11 llamado `build/kry`; `tools/kry` sólo automatiza esa construcción bajo demanda y reenvía todos los argumentos.

```bash
make
./tools/kry --version
./tools/kry check examples/control_flow.kry
./tools/kry run examples/control_flow.kry
```

El binario no carga módulos de otro lenguaje ni necesita una instalación de Python, Rust, Node o un runtime equivalente para ejecutar programas Kryndel. La única herramienta de compilación externa es un compilador C11 en el momento de construir el binario.

## Contrato de comandos

| Entrada | Comportamiento | Código de éxito |
| --- | --- | ---: |
| `check fuente.kry` | Lee, lexifica y parsea la fuente. | `0` |
| `run fuente.kry` | Repite la validación y ejecuta el programa. | `0` |
| `run archivo.kexe` | Valida el contenedor y ejecuta su payload. | `0` |
| `build fuente.kry` | Valida y escribe un `.kexe`. | `0` |
| `version` | Imprime la versión. | `0` |
| Uso inválido | Imprime ayuda. | `2` |
| Error de fuente o runtime | Imprime diagnóstico. | `1` |

## Formato KRYNATIVE1

El formato es intencionalmente simple y determinista:

```text
11 bytes:  KRYNATIVE1\n
8 bytes:   longitud little-endian del payload
N bytes:   fuente Kryndel exacta
```

La longitud debe coincidir con el tamaño restante. Un archivo que contiene bytes extra, una cabecera incompleta o una longitud inconsistente se rechaza antes de la ejecución. Construir dos veces la misma fuente produce el mismo archivo byte por byte.

## Portabilidad

El código usa tipos de ancho explícito para el formato, la biblioteca estándar C para I/O y memoria, y no genera ensamblador ni enlaza objetos durante `run`. La interfaz de `tools/kry` detecta `CC` o un compilador C disponible; si no encuentra uno, devuelve `69` con una instrucción clara en vez de intentar una ruta alternativa.
