# Pruebas

La suite se ejecuta desde la raíz del repositorio y no requiere un entorno de paquetes. `make test` compila el núcleo con advertencias estrictas y ejecuta la integración bajo una ruta mínima.

```bash
make test
```

La prueba principal cubre una muestra vertical del lenguaje: funciones recursivas, `if`, `while`, bindings mutables, operadores, arrays, strings Unicode, bytes, aserciones, diagnóstico de variables desconocidas, construcción determinista y ejecución de artefactos.

| Caso | Verificación |
| --- | --- |
| `examples/hello.kry` | Salida de texto y ejecución básica. |
| `examples/fibonacci.kry` | Recursión y retorno de enteros. |
| `examples/bytes.kry` | UTF-8, bytes e indexación. |
| `examples/control_flow.kry` | Condicionales, bucles, `break`, `continue` y arrays. |
| `tests/native-core.sh` | CLI, artefactos, determinismo y errores. |

Los tests usan `PATH` y `HOME` controlados cuando verifican el launcher. No se acepta silenciosamente una ruta alternativa: si no existe un compilador C11 durante la construcción, la operación falla con un mensaje explícito.

Para validar manualmente un programa:

```bash
./tools/kry check mi_programa.kry
./tools/kry run mi_programa.kry
./tools/kry build mi_programa.kry -o mi_programa.kexe
./tools/kry run mi_programa.kexe
```

Un cambio de sintaxis debe incluir un ejemplo positivo y, cuando corresponda, un archivo que produzca un error estable. Un cambio en el formato de artefactos debe probar al menos una copia idéntica y una alteración de longitud o payload.
