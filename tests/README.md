# Kryndel test suite

La cobertura funcional y de seguridad vive en `internal/kry/toolchain_test.go`, una suite nativa de Go que prueba lexer, parser, checker, control de flujo, Copy recursivo, módulos, sandbox, artefactos, formatter, REPL/runtime y límites. Esto evita depender de Bash, GNU coreutils, Python, C o un intérprete externo para validar el proyecto.

Los comandos de verificación son:

```text
make test          # build, ejemplos y pruebas Go
make test-static   # gofmt, go vet y pruebas limpias
make test-race     # detector de races de Go
make fuzz-smoke    # corpus determinista acotado
make coverage      # perfil de cobertura
make release       # binarios cruzados y SHA256SUMS
```

Las pruebas de proceso siguen siendo deliberadamente pequeñas y se ejecutan desde la CLI sólo cuando prueban una frontera pública que no puede observarse de forma más precisa dentro del paquete. Los errores deben ser deterministas, acotados y acompañados por una aserción negativa; un timeout es un fallo, no un resultado esperado.
