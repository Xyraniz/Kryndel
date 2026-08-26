# Kryndel seed-only offline verification contract v1

## Alcance

`tools/kry-seed-check` verifica únicamente la cadena mínima de `tools/kry-seed`. El
checker ejecuta el script con un `PATH` aislado que contiene sólo `sh`, `chmod`,
`mv`, `rm`, `od`, `tr`, `cmp` y las utilidades usadas por el propio checker; el
script seed no invoca Python, un ensamblador, un linker ni el CLI normal. La
salida se coloca en un directorio cuyo nombre contiene espacios, se genera dos
veces y se compara byte a byte.

Después valida la magia ELF64, los permisos ejecutables y la ejecución del
archivo con `HOME` vacío y un entorno mínimo. La expectativa del checker sigue
siendo un proceso Linux `x86_64` que termina con código cero y no escribe salida;
la interfaz independiente `tools/kry-seed OUTPUT [EXIT_STATUS]` también permite
probar estados explícitos de 0..255.

## Resultado

El contrato se limita a estos hechos observables:

| Propiedad | Garantía v1 |
| --- | --- |
| Entrada | Ningún archivo de programa; el seed representa un `main` vacío |
| Salida | ELF64 little-endian `x86_64` Linux raw, ejecutable; el checker verifica exit-0 y el generador acepta 0..255 |
| Reproducibilidad | Dos ejecuciones producen bytes idénticos |
| Aislamiento | `PATH` mínimo y `HOME` vacío durante generación y ejecución |
| Espacios | El artefacto se genera en una ruta con espacios |
| Dependencias excluidas | Python, `as`, `ld`, compilador C/C++, red y registro |
| No cubierto | Lexer, parser, checker, compiler, runtime, KEXE, package manager, SHA-256, linker y CLI completo |

## Interpretación

Este contrato demuestra un **seed nativo mínimo**, no un bundle autocontenido del
producto y no self-hosting. La ruta normal de Kryndel sigue usando el bootstrap
Python para lexer, parser, checker, compiler, VM, artefactos, resolución de
paquetes, checksums y CLI. El seed tampoco ejecuta los módulos `.kry` del
toolchain; éstos siguen siendo seams de compatibilidad ejecutados por la VM
Python.

El comando debe permanecer reproducible y offline. Una ampliación de alcance
requiere contratos separados para lectura binaria, representación de literales,
bytecode/KEXE, runtime nativo y resolución/instalación antes de poder retirar
cada dependencia correspondiente.
