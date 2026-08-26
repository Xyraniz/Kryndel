# Kryndel source JSON parser contract v1

## Alcance

`stdlib/core/json.kry` implementa un parser JSON fuente sobre `String` con
valores nominales `Null`, `Bool`, `Int`, `Float`, `String`, `Array` y
`Object`. Los objetos contienen un array ordenado de `JsonMember { key, value }`
en vez de un mapping del host. El parser consume espacios JSON, strings con los
escapes simples del contrato, números enteros/decimales/exponentes, arrays y
objetos anidados, y exige que no exista entrada posterior al valor raíz.

Los números se validan antes de llamar a `int` o `float`, por lo que signos
solos, exponentes incompletos, decimales sin dígitos y ceros iniciales inválidos
producen `KRY6304` en vez de propagar una excepción del host. Los escapes
Unicode `\\uXXXX` quedan fuera de este subset hasta congelar una conversión de
codepoints equivalente a la del runtime completo.

| Forma | Representación fuente |
| --- | --- |
| `null` | `JsonValue.Null` |
| `true` / `false` | `JsonValue.Bool(Bool)` |
| entero JSON | `JsonValue.Int(Int)` |
| decimal o exponente | `JsonValue.Float(Float)` |
| string | `JsonValue.String(String)` |
| array | `JsonValue.Array(Array<JsonValue>)` |
| object | `JsonValue.Object(Array<JsonMember>)` |
| input inválido | `JsonResult.Error("KRY6304 ...")` |

## Relación con KEXE

El parser se prueba con un documento que contiene los valores presentes en un
payload de bytecode. Todavía no convierte `JsonValue.Object` a los registros
`ModuleRecord`, no valida las claves requeridas del formato bytecode, no lee
archivos y no reemplaza el `json.loads` Python de `kryndel/artifact.py`. La
siguiente etapa puede añadir un decoder de schema sobre estos valores, separado
del parser JSON general, para construir funciones, constantes e instrucciones
que luego pasen por `stdlib/core/bytecode.kry`.

La implementación se ejecuta por la VM Python. Por tanto, este contrato es una
pieza diferencial de la futura cadena bootstrap y no evidencia de self-hosting o
de una CLI de artefactos sin Python.
