# Kryndel

Kryndel es un lenguaje pequeño y expresivo para programación estructurada. Esta distribución tiene una única implementación ejecutable: `native/kry.c`. El núcleo incluye lexer, parser, evaluador, funciones, control de flujo, valores dinámicos, arrays, texto UTF-8, bytes, aserciones y artefactos reproducibles, sin necesitar un intérprete escrito en otro lenguaje.

> La ruta normal del proyecto es `tools/kry`. El repositorio no contiene una carpeta de bootstrap ni archivos fuente `.py`.

## Inicio rápido

Se necesita un compilador C11 únicamente para construir el ejecutable nativo. Después de la primera construcción, los comandos se ejecutan directamente mediante `build/kry`.

```bash
make
./tools/kry run examples/hello.kry
./tools/kry run examples/fibonacci.kry
./tools/kry check examples/control_flow.kry
./tools/kry build examples/hello.kry -o /tmp/hello.kexe
./tools/kry run /tmp/hello.kexe
```

La CLI también acepta un archivo como abreviatura de `run` y ofrece `version` y `--help`.

| Comando | Función |
| --- | --- |
| `kry check archivo.kry` | Lexa y analiza el archivo sin ejecutar el programa. |
| `kry run archivo.kry` | Ejecuta una fuente Kryndel o un artefacto `.kexe`. |
| `kry build archivo.kry [-o salida.kexe]` | Valida y empaqueta la fuente en un contenedor determinista. |
| `kry version` | Muestra la versión del lenguaje y del ejecutable. |
| `kry --help` | Muestra la ayuda de la CLI. |

## Lenguaje disponible

La implementación ofrece declaraciones `let`, parámetros y funciones `fn`, valores `Int`, `Float`, `Bool`, `String`, `Bytes`, `Array` y `nil`, expresiones aritméticas y booleanas, concatenación de strings y arrays, indexación, `if`/`else`, `while`, `return`, `break`, `continue`, comentarios de línea y bloque, además de llamadas a funciones definidas por el usuario.

```kryndel
fn factorial(n: Int) -> Int {
    if n <= 1 {
        return 1
    }
    return n * factorial(n - 1)
}

let answer: Int = factorial(6)
println("factorial = " + str(answer))
assert(answer == 720)
```

Los builtins mínimos forman la biblioteca incorporada: `print`, `println`, `len`, `bytes`, `string_to_bytes`, `bytes_to_string`, `array_push`, `int`, `float`, `str`, `assert` y `assert_eq`. Los strings se validan como UTF-8 cuando una operación depende de caracteres y la indexación devuelve un code point como string.

## Diseño de ejecución

El comando `check` ejecuta el mismo lexer y parser que `run`, por lo que un archivo que pasa la comprobación tiene una ruta de ejecución idéntica. `build` conserva la fuente validada en un formato binario con cabecera `KRYNATIVE1`, tamaño little-endian de 64 bits y payload exacto. `run` valida la cabecera, el tamaño y vuelve a analizar el payload mediante el mismo núcleo; no hay código generado por un segundo lenguaje ni runtime oculto.

La implementación es deliberadamente pequeña y tree-walk. Esto mantiene el comportamiento fácil de auditar y permite que el lenguaje sea utilizable hoy. El siguiente crecimiento natural es añadir módulos y tipos nominales dentro de este mismo núcleo, sin reintroducir un bootstrap externo.

## Estructura

| Ruta | Propósito |
| --- | --- |
| `native/kry.c` | Lexer, parser, runtime, builtins, CLI y formato de artefacto. |
| `tools/kry` | Lanzador que construye `build/kry` cuando hace falta. |
| `examples/` | Programas Kryndel ejecutables. |
| `tests/` | Pruebas de integración del ejecutable nativo. |
| `docs/` | Contratos de lenguaje, arquitectura y pruebas. |

## Verificación

```bash
make test
```

La suite comprueba construcción con advertencias estrictas, ejecución de ejemplos, control de flujo, UTF-8, conversiones a bytes, errores de nombres desconocidos, empaquetado determinista y lectura de artefactos. Para retirar archivos temporales se usa `make clean`.
