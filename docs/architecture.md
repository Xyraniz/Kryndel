# Arquitectura nativa

Kryndel se distribuye como un ejecutable nativo pequeño. La cadena de ejecución es única y directa:

```text
fuente UTF-8
    -> lexer
    -> parser y AST en memoria
    -> evaluador tree-walk
    -> salida, errores o artefacto KRYNATIVE1
```

El lexer convierte caracteres en tokens con línea y columna. El parser construye expresiones, declaraciones, funciones y bloques. El evaluador resuelve nombres por entorno léxico, crea un entorno por llamada de función y aplica los operadores y builtins. `check` recorre las mismas dos primeras etapas sin ejecutar efectos; `run` añade la evaluación.

| Componente | Responsabilidad | Dependencias de ejecución |
| --- | --- | --- |
| Lexer | Comentarios, identificadores, literales, operadores y posiciones. | C estándar y memoria del proceso. |
| Parser | Funciones, bindings, expresiones, bloques y control de flujo. | AST interno del ejecutable. |
| Runtime | Valores, entornos, llamadas, recursión, arrays, bytes y UTF-8. | C estándar y stdout/stderr. |
| Artefactos | Cabecera, longitud, payload y validación exacta. | `stdio` para lectura y escritura. |
| CLI | `check`, `run`, `build`, `version` y ayuda. | Sistema de archivos para las rutas explícitas. |

No existe una segunda implementación que el ejecutable invoque como fallback. Los errores de léxico, sintaxis y ejecución se producen desde el mismo binario y conservan el archivo, la línea y la columna disponibles.

## Memoria y semántica

Los valores son inmutables al evaluarse. Una asignación reemplaza el binding del entorno más cercano que ya contiene el nombre; `let mut` documenta la intención mutable de la fuente, mientras que el núcleo actual no realiza todavía un chequeo estático de mutabilidad. Arrays y bytes se copian al construir resultados de concatenación o `array_push`, lo que evita aliasing observable entre esas operaciones.

Las llamadas de función son por valor y se ejecutan en un entorno hijo. Las funciones no son valores de primera clase todavía: una llamada debe usar un nombre sencillo que corresponda a un builtin o a una función declarada en el programa. El control de flujo usa `return`, `break` y `continue`; `return` fuera de una función y el control de bucle fuera de un `while` son errores explícitos.

## Artefactos

`build` acepta una fuente Kryndel, la valida y escribe:

| Campo | Tamaño | Contenido |
| --- | ---: | --- |
| Magic | 11 bytes | ASCII `KRYNATIVE1\n`. |
| Longitud | 8 bytes | Entero little-endian sin signo del payload. |
| Payload | variable | La fuente Kryndel exacta, sin transformación. |

El lector sólo acepta un tamaño que coincida exactamente con el archivo. El artefacto no contiene instrucciones de otro compilador y `run` vuelve a pasar el payload por el lexer y parser nativos.
