# Referencia del lenguaje

Kryndel usa una sintaxis de bloques con llaves y expresiones infijas. Los tipos después de `:` y `->` son aceptados como anotaciones de documentación; el runtime actual conserva los valores dinámicos y valida las operaciones al ejecutarse.

## Declaraciones y control

```kryndel
let greeting: String = "hola"
let mut total: Int = 0

fn sum_to(limit: Int) -> Int {
    let mut value: Int = 0
    let mut index: Int = 0
    while index <= limit {
        value = value + index
        index = index + 1
    }
    return value
}

if sum_to(4) == 10 {
    println(greeting)
} else {
    println("error")
}
```

Se admiten `let`, `let mut`, `fn`, `if`, `else`, `while`, `return`, `break` y `continue`. Las funciones pueden llamarse recursivamente. Una declaración de función puede estar precedida por `pub`; la visibilidad no cambia todavía en el runtime nativo, pero la forma se conserva para evolución futura.

## Valores y expresiones

| Categoría | Ejemplos | Operaciones principales |
| --- | --- | --- |
| Entero | `0`, `-7` | `+`, `-`, `*`, `/`, `%`, comparaciones. |
| Flotante | `3.14` | `+`, `-`, `*`, `/`, comparaciones. |
| Booleano | `true`, `false` | `!`, `&&`, `||`, igualdad. |
| String | `"Kryndel"` | Concatenación, `len`, indexación UTF-8. |
| Array | `[1, 2, 3]` | Concatenación, `len`, indexación, `array_push`. |
| Bytes | `bytes([65, 66])` | Concatenación, `len`, indexación y conversión UTF-8. |
| Nil | `nil` | Igualdad y representación. |

La precedencia, de menor a mayor, es `||`, `&&`, igualdad, comparación, suma/resta y multiplicación/división/módulo. Los paréntesis permiten agrupar expresiones. Los comentarios pueden ser `//` hasta el final de línea o `/* ... */` y admiten anidamiento.

## Builtins

| Nombre | Firma conceptual | Resultado |
| --- | --- | --- |
| `print` / `println` | `(Any)` | Escribe un valor, con o sin salto de línea. |
| `len` | `(String \| Array \| Bytes)` | Cantidad de code points, elementos u octetos. |
| `bytes` | `(Array)` | Bytes inmutables a partir de enteros `0..255`. |
| `string_to_bytes` | `(String)` | Codificación UTF-8 estricta. |
| `bytes_to_string` | `(Bytes)` | Decodificación UTF-8 estricta. |
| `array_push` | `(Array, Any)` | Nuevo array con un elemento añadido. |
| `int` / `float` / `str` | `(Any)` | Conversión explícita compatible. |
| `assert` | `(Bool)` | Falla si la condición es falsa. |
| `assert_eq` | `(Any, Any)` | Falla si los valores no son iguales. |

Los errores tienen formato `kry: archivo:línea:columna: ...` y terminan con código distinto de cero. El ejecutable nunca continúa después del primer error de léxico, parseo o runtime.

## Límites conscientes

El núcleo actual no incluye imports, módulos, closures, funciones como valores, structs, enums, pattern matching, generics, macros, ownership ni un checker estático completo. La sintaxis no soportada se rechaza en lugar de interpretarse de manera ambigua. Estos límites son explícitos para que la superficie implementada sea honesta y verificable.
