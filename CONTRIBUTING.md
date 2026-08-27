# Contribuir a Kryndel

Kryndel tiene una implementación nativa deliberadamente pequeña. Una modificación de sintaxis debe considerar lexer, parser, runtime, ejemplos, diagnósticos y documentación. Mantén la cadena de ejecución dentro de `native/kry.c` o añade código Kryndel que pueda ser ejecutado por la CLI existente; no agregues un intérprete paralelo ni una dependencia de runtime en otro lenguaje.

## Desarrollo local

Se requiere un compilador C11 para construir el ejecutable:

```bash
make test
```

La CLI se puede probar directamente:

```bash
./tools/kry check examples/hello.kry
./tools/kry run examples/fibonacci.kry
./tools/kry build examples/hello.kry -o /tmp/hello.kexe
./tools/kry run /tmp/hello.kexe
```

## Cambios de lenguaje

Implementa una rebanada vertical coherente. El lexer debe producir posiciones estables, el parser debe rechazar sintaxis incompleta y el runtime debe emitir errores deterministas. Todo comportamiento nuevo necesita un ejemplo positivo en `examples/` y una regresión en `tests/native-core.sh` cuando afecte a la CLI o al formato de artefactos.

Conserva la determinación: no introduzcas fechas, identificadores aleatorios, rutas absolutas ni salida dependiente de la locale. No incluyas binarios, `build/` ni artefactos `.kexe` en los commits.

## Revisión

Antes de abrir un cambio, ejecuta `make test`, revisa `git diff --check` y confirma que `find . -type f -name '*.py'` no devuelve resultados. La documentación debe describir el comportamiento realmente implementado, incluyendo límites conocidos.

## Licencia

Las contribuciones se distribuyen bajo la licencia MIT del repositorio.
