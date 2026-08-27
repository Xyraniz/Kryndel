# Registro de cambios

## 1.0.0 — Núcleo nativo

Kryndel ahora se ejecuta mediante una única CLI nativa en `tools/kry`, respaldada por `native/kry.c`. Se retiró la implementación de bootstrap, la carpeta de módulos del intérprete, la metadata de paquetes y los fixtures históricos que dependían de esa ruta.

El núcleo soporta lexer, parser, funciones recursivas, bindings, expresiones numéricas y booleanas, strings UTF-8, arrays, bytes, control de flujo, aserciones, diagnósticos con posiciones, comprobación sin ejecución y artefactos deterministas `KRYNATIVE1`. La suite de integración usa el mismo ejecutable que se entrega a los usuarios.
