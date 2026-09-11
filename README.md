# Kryndel

Kryndel es un lenguaje pequeño para programación estructurada. El repositorio contiene una implementación única en Go que analiza el código, resuelve módulos, comprueba tipos, valida una representación intermedia y ejecuta el resultado.

## Primeros pasos

La compilación requiere Go 1.22 o posterior. El ejecutable no necesita Go para ejecutarse después de construirlo.

```bash
make
./tools/kry run examples/hello.kry
./tools/kry check examples/control_flow.kry
./tools/kry build examples/hello.kry -o /tmp/hello.kexe
./tools/kry run /tmp/hello.kexe
```

`check` no ejecuta el programa. `run` comprueba y ejecuta una fuente o un artefacto. `build` empaqueta el grafo de módulos de forma determinista y `emit` puede producir IR compatible con LLVM. `inspect` examina cabeceras PE o ELF sin arrancar el binario.

## Lenguaje y runtime

Las variables son inmutables por defecto y `let mut` permite reasignarlas. Las funciones declaran parámetros y retornos; el checker valida expresiones, módulos, colecciones, `Option`, `Result`, canales, threads y límites de recursos antes de ejecutar. La biblioteca estándar incluye filesystem tipado, JSON validado, HTTP/TLS acotado, WebSockets, cancelación cooperativa y lanzamiento de procesos sin shell.

Los módulos relativos y los paquetes vendorizados se resuelven de manera determinista. Las operaciones de red, filesystem, procesos, colecciones y buffers tienen límites configurables. `unsafe` marca de forma explícita el punto que queda fuera de las garantías normales del checker.

## Desarrollo

Los ejemplos están en `examples/`, la documentación detallada en `docs/` y las pruebas en la configuración del proyecto. `./tools/kry doctor` comprueba el ejecutable, la biblioteca estándar y los contratos registrados. El código y la licencia se encuentran en el propio repositorio.
