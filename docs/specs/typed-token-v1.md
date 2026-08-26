# Kryndel typed-token contract v1

## Alcance

`stdlib/core/lexer.kry` conserva los campos existentes `kind`, `text` y `span`, y
agrega `literal`, un registro nominal `LiteralValue`. El registro evita que el
parser futuro tenga que reconstruir valores a partir del texto normalizado.

| Campo | Tipo | Semántica |
| --- | --- | --- |
| `category` | `String` | `int`, `float`, `bool`, `string`, `nil`, `text` o `none` |
| `int_value` | `Int` | Valor de tokens `INT`; cero para otras categorías |
| `float_value` | `Float` | Valor de tokens `FLOAT`; cero para otras categorías |
| `bool_value` | `Bool` | Valor de `TRUE`/`FALSE`; falso para otras categorías |
| `string_value` | `String` | Valor decodificado de `STRING` o texto normalizado de tokens `text`; vacío en las demás categorías |

Los identificadores, palabras clave, operadores y delimitadores usan la
categoría `text`. `TRUE` y `FALSE` usan valores booleanos reales, `NIL` se
representa con la categoría `nil`, y `EOF` con `none`. Las cadenas conservan el
valor decodificado de los escapes ya soportados por el seam. El fixture
`tests/fixtures/typed-token-v1.json` congela esos valores para una entrada
mixta de enteros, flotantes, booleanos, nil, string, operador y EOF.

## Límites

Este contrato sólo mejora la representación dentro del lexer fuente. El parser
fuente actual sigue consumiendo `kind`, `text` y `span`, y no afirma todavía
producir literales AST tipados. La lexer fuente continúa ejecutándose por la VM
Python; la lexer Python sigue siendo la referencia diferencial y dueña de la
ruta normal. No se modifican aún decimales científicos, sufijos numéricos,
interpolación, nuevos escapes, payloads de bytes ni serialización JSON/KEXE
nativa.
