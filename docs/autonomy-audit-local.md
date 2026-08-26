# Auditoría local de autonomía de Kryndel

**Fecha de auditoría:** 25 de agosto de 2026

**Rama:** `main`

**Identidad de commit:** `Xyraniz <xyraniz@users.noreply.github.com>`

## Estado de publicación

El árbol de trabajo está limpio. La rama local contiene **20 commits por delante**
de `origin/main` y no contiene commits por detrás. No se ejecutó `git push` en
este tramo de trabajo. El último commit remoto permanece en
`5179a5c4cffc05aa7e037919d460e8690c46c03f`; el commit local actual es
`9e905de8a389f171f007df9262831dd54c51fd39`.

| Propiedad | Resultado |
| --- | --- |
| Rama | `main` |
| Estado | limpio |
| Divergencia | `origin/main...HEAD = 0 20` |
| Último remoto | `5179a5c4cffc05aa7e037919d460e8690c46c03f` |
| HEAD local | `9e905de8a389f171f007df9262831dd54c51fd39` |
| Publicación | pendiente de autorización explícita del usuario |
| Artefactos prohibidos en el repositorio | ninguno encontrado: no hay `.pyc`, `.kexe` ni `.krypkg` |

Los veinte commits locales, en orden, son:

| Commit | Tema |
| --- | --- |
| `c5c58b0` | runtime fuente del subset |
| `431e8f4` | contrato seed del backend directo |
| `226107a` | CLI seed ELF sin Python |
| `2d4df3d` | contrato formatter fuente |
| `0ae609d` | CLI formatter sin Python |
| `cdd0b19` | verificación offline del seed |
| `a0ede72` | payload tipado de tokens |
| `10536a5` | propagación de literales al AST |
| `0b4dd7c` | asignaciones de literales tipadas |
| `2bfed7d` | constantes tipadas fuente |
| `adb373b` | lector de framing KEXE fuente |
| `be4fe6c` | SHA-256 fuente |
| `9b4ed0d` | estados de salida 0..255 en seed |
| `fefab89` | checksum KEXE conectado a SHA-256 |
| `5612a02` | parser JSON fuente |
| `9f968e8` | decoder de schema bytecode fuente |
| `653ce1d` | lowering de retorno constante |
| `051674a` | seed de control condicional acotado |
| `db94fe2` | lector de payload mediante capacidad controlada |
| `9e905de` | pipeline KEXE fuente completo para subset |

## Evidencia de pruebas

La validación final ejecutó compilación sintáctica de los módulos Python, las
regresiones focales de JSON, KEXE, SHA-256, backend, filesystem y verificador,
y la suite completa. El resultado medido fue:

| Validación | Resultado |
| --- | --- |
| `git diff --check` | correcto |
| `python3 -m py_compile kryndel/*.py tests/test_kryndel.py` | correcto |
| Regresiones focales finales | 4 pruebas, `OK` |
| Suite completa | **113 pruebas en 11.202 s, `OK`** |
| Limpieza de `__pycache__` | realizada |

La regresión `kexe-source-pipeline-v1` usa el serializador Python de referencia
sólo para construir un artefacto diferencial. La cadena bajo prueba realiza, en
orden, framing KEXE fuente, extracción y comparación SHA-256 fuente, conversión
de bytes a JSON fuente, construcción del subset bytecode, verificación fuente y
ejecución del módulo por el runtime fuente bajo la VM Python.

## Capacidad incorporada

| Área | Implementación actual | Límite que permanece |
| --- | --- | --- |
| Datos, spans y registros | módulos fuente con contratos y fixtures | el runtime de valores sigue en la VM Python |
| Lexer, parser y checker | seams fuente para un subset, con literales tipados | la ruta productiva y la paridad completa siguen en Python |
| Compiler y runtime | constantes tipadas, `PUSH_NIL` y subset ejecutable | faltan todos los opcodes, serialización y runtime nativo |
| KEXE y checksum | framing, SHA-256 y pipeline de payload para un subset | faltan schema completo, CLI productivo y reemplazo de `read_artifact` |
| JSON | valores nominales, decoder bytecode acotado y lectura controlada | faltan todas las invariantes de schema y módulo |
| Backend directo | empty-main, retorno constante y un seed condicional fijo x86_64 Linux | faltan lowering general, datos, llamadas, object/linker e integración productiva |
| Utilidades sin Python | `tools/kry-seed`, `tools/kry-format`, `tools/kry-seed-check` | son utilidades acotadas; no forman un bundle de toolchain |

> **Conclusión:** Kryndel aún no es self-hosted ni autónomo de Python.

La ruta normal continúa dependiendo del bootstrap Python para lexer, parser,
checker, compiler, VM, lectura y escritura de artefactos, resolución de paquetes,
checksums productivos y CLI. Los módulos `.kry` añadidos son implementaciones
fuente reales y verificables de contratos delimitados, pero se ejecutan por la
VM Python. El seed ELF sin Python sólo representa un `main` vacío o un estado de
salida fijo; no ejecuta el compilador ni el runtime de Kryndel.

## Próximos cuellos de botella técnicos

El siguiente trabajo realista es ampliar el decoder y el verificador bytecode
por familias de instrucciones, añadir invariantes de `entry`, aridad,
constantes y llamadas, y mantener fixtures diferenciales contra Python. Después
se puede conectar el lector de archivos KEXE a una API fuente controlada y
comparar más módulos. Sólo cuando lexer, parser, checker, compiler, runtime,
lector binario, schema, checksum, CLI y backend tengan sustitutos nativos
completos y ejecutables será válido reducir la matriz de dependencia Python.

Este informe es local y auditable. No implica publicación remota ni autoriza
ningún push posterior.
