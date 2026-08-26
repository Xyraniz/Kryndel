# Kryndel local autonomy audit

**Fecha de auditoría:** 26 de agosto de 2026

**Rama de trabajo:** `agent/python-independence`

**Identidad configurada para commits:** `Xyraniz <xyraniz@users.noreply.github.com>`

## Alcance y procedencia del checkout

El checkout se obtuvo como un snapshot de `main` mediante la API de GitHub
después de que dos intentos de `gh repo clone` por HTTPS terminaran con errores
TLS del entorno. Por esa razón, el checkout local conserva `origin` como URL
informativa, pero no tiene una referencia local `origin/main` ni el historial
completo del repositorio. La referencia remota observada por la API es
`f0cd4c454fde2afea7e2db040a1689e561db5679`. Esta limitación no se oculta ni se
interpreta como una divergencia de commits.

La línea base local del snapshot, antes de los cambios de esta rama, fue el
commit de importación `7adf883422b82cd6914dba46f7d33ac08d5ca5b8`. Los resultados
completos de las comprobaciones se guardaron fuera del repositorio en
`/home/ubuntu/kryndel_audit_baseline_20260826/`.

| Comprobación | Resultado medido |
| --- | --- |
| `git status --short --branch` | `## master` antes de crear la rama de trabajo |
| `git log -5 --oneline --decorate` | snapshot local con un commit de importación |
| `git rev-parse HEAD` | `7adf883422b82cd6914dba46f7d33ac08d5ca5b8` |
| `git rev-parse origin/main` | no disponible localmente; la referencia no existe |
| SHA de `main` obtenido de GitHub | `f0cd4c454fde2afea7e2db040a1689e561db5679` |
| `git diff --check` | correcto |
| `git ls-files` | 128 archivos rastreados en el snapshot |
| `python3 -m py_compile kryndel/*.py tests/test_kryndel.py` | correcto |
| `python3 -m unittest discover -s tests -v` | **117 pruebas, `OK`, 12.227 s** |
| Estado después de las pruebas | solo se generaron dos `__pycache__`; se eliminaron |

La suite Python es el oráculo diferencial de stage-0. No demuestra independencia
ni convierte los módulos `.kry` en componentes nativos.

## Cadena de transición

| Stage | Definición | Estado observado |
| --- | --- | --- |
| stage-0 | Bootstrap Python, VM, CLI y oráculo diferencial | Implementado |
| stage-1 | Seams fuente Kryndel y contratos versionados | Implementado solo en subsets delimitados |
| stage-2 | Runtime nativo que lee KEXE, verifica bytecode y ejecuta `main` | No implementado |
| stage-3 | Compiler, loader y CLI productiva nativos | No implementado |
| stage-4 | Bundle target-specific autocontenido y reproducible | No implementado |
| stage-5 | Dos reconstrucciones nativas equivalentes | No implementado |

Un módulo fuente interpretado por `kryndel/vm.py` se denomina **seam fuente bajo
bootstrap Python**. La etiqueta no se sustituye por `Kryndel-native` hasta que
la ruta normal deje de invocar la VM Python.

## Matriz de estados

La nueva auditoría ejecutable es `PYTHONPATH=. python3 -m kryndel autonomy-audit`.
Su matriz no es un porcentaje global; cada registro tiene evidencia, reemplazo y
un estado explícito.

| Estado | Componentes medidos |
| --- | ---: |
| `Kryndel-native` | 0 |
| `host capability nativa mínima` | 3 |
| `bootstrap Python` | 8 |
| `no implementado` | 3 |

Los tres checkpoints de host actualmente aislados son el formatter POSIX, el
seed ELF/KRYSEED1 y el checker de framing/checksum KEXE. El valor runtime,
lector bytecode/KEXE productivo, compiler, VM,
filesystem, package manager, CLI, documentación/packer siguen en bootstrap. El
bundle, self-hosting y backend UI permanecen no implementados.

La auditoría también enumera invocaciones textuales de `python -m kryndel` en
Markdown, Python, YAML y shell. Es una auditoría de rutas documentadas y no una
prueba completa contra ejecución indirecta.

## Cambios de este checkpoint

Los cambios funcionales quedaron en siete commits coherentes sobre
`agent/python-independence`; este documento de cierre se añade como el octavo:
`c6b2352` añade `kry autonomy-audit` y la matriz de
host; `6170a4d` añade `tools/kry-kexe-check`; `47e04d7` registra las primeras
mediciones; `7a1d8cf` hace branch-aware el análisis de pila; `eaef6ee` conecta
`verify_execution` al fixture runtime; `698ba59` añade
`tools/kry-bundle-check`, `bundle-audit-v1.json` y su job CI; y `680aaf6`
registra el estado final medido. El checkout final pasa 120 pruebas Python, los
checkers no-Python, la auditoría de candidato bundle, y sincroniza la
documentación afectada, conservando 117 como línea base.

## Límites verificados

La ruta normal todavía requiere Python para lexer, parser, checker, compiler,
VM, lectura/escritura de KEXE, resolución de paquetes, documentación, packer y
CLI. `tools/kry-format`, `tools/kry-seed-check` y
`tools/kry-native-run-check` son checkpoints estrechos de capacidades host; no
constituyen un bundle ni ejecutan programas Kryndel generales. No se afirma
self-hosting, autonomía, ejecución sin toolchains, ni ausencia de dependencias
externas en el producto final.

No se ejecutó `git push` ni se publicó nada en `main`. El último HEAD funcional
medido antes de este documento fue `680aaf655f87f6b8809ee4ab61afab9157b79ae9`,
precedido por `698ba59`, `eaef6ee`, `7a1d8cf`, `47e04d7`, `6170a4d`, y `c6b2352`;
la identidad de todos los commits es la indicada arriba.

## Siguiente checkpoint verificable

El siguiente trabajo debe ser pequeño y técnico: extender la lectura nativa de
artefactos desde el framing KEXE hacia el schema bytecode completo, congelar una
fixture válida y fixtures malformadas por opcode, y medirlo con el mismo entorno
sin afirmar todavía que existe un runtime nativo.
