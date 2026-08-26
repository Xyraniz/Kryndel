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

`decode_bytecode(String)` añade un decoder de schema sobre el parser general.
Valida el formato `kryndel-bytecode`, la versión 1, la identidad del módulo, un
objeto de funciones, constantes escalares y el subset de instrucciones
`PUSH_CONST`, `PUSH_NIL`, `LOAD`, `STORE`, `JUMP`, `JUMP_IF_FALSE`, `POP` y
`RETURN`. Devuelve registros
nominales normalizados con los mismos campos que consume el verificador fuente;
la regresión pasa el resultado por `stdlib/core/bytecode.kry` y obtiene `Ok`.

`decode_bytecode_file(String)` compone `fs.read_text` con el decoder y se prueba
sobre `VirtualFileSystem`; un archivo ausente conserva el diagnóstico controlado
`KRY6302`. El decoder todavía no cubre todas las instrucciones, constantes
estructuradas, validaciones completas de aridad/entry o la construcción exacta
de todos los invariantes de `ModuleRecord`. Tampoco reemplaza el `json.loads`
Python de `kryndel/artifact.py`; funciona como una etapa fuente diferencial para
el subset congelado.

La implementación se ejecuta por la VM Python. Por tanto, este contrato es una
pieza diferencial de la futura cadena bootstrap y no evidencia de self-hosting o
de una CLI de artefactos sin Python.
