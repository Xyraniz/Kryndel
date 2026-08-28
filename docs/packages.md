# Paquetes, registry y reproducibilidad

Kryndel usa `kry.toml` como manifiesto de proyecto y `kry.lock` como resultado de resolución. La resolución acepta versiones exactas, `^`, `~`, `>=` y `*`; ordena candidatos semánticamente y procesa dependencias en orden estable. El instalador descarga archivos `tar.gz`, valida SHA-256 antes de extraerlos y copia únicamente entradas regulares bajo `vendor/<nombre>`.

| Comando | Resultado | Garantía |
| --- | --- | --- |
| `kry new app` | Crea un proyecto con `kry.toml` y `main.kry`. | No introduce un runtime externo. |
| `kry add discord ^1.0.0` | Actualiza `[dependencies]`. | El manifiesto se serializa determinísticamente. |
| `kry install` | Resuelve, descarga, verifica, extrae y escribe `kry.lock`. | Los hashes y URLs quedan registrados. |
| `kry update` | Repite la resolución desde el registry configurado. | La instalación puede auditarse desde el lockfile. |
| `kry package` | Crea un `tar.gz` reproducible. | Omite `kry.lock`, fija tiempos de entrada y ordena rutas. |
| `kry publish` | Envía el archivo al endpoint `PUT /publish/<name>/<version>`. | El registry recalcula el SHA-256 y actualiza su índice. |
| `kry cache clean` | Elimina la caché de índices y archivos. | No toca el proyecto ni el lockfile. |

El registry local se sirve con `kry registry serve ROOT --addr 127.0.0.1:8765`. Su estructura mínima es `ROOT/index/<name>.json` y `ROOT/packages/<archive>.tar.gz`. Los índices contienen el nombre, las versiones, la URL del archivo, su SHA-256 y las dependencias transitivas. `KRY_REGISTRY` cambia el registry, `KRY_CACHE` cambia la caché y `KRY_OFFLINE=1` obliga a utilizar únicamente índices y archivos ya almacenados.

> El instalador rechaza rutas absolutas, separadores alternativos que escapen de la raíz, `..`, enlaces simbólicos, directorios en el archivo, entradas duplicadas y archivos mayores que el límite de archivo. La verificación criptográfica ocurre antes de la extracción.

Los paquetes se importan con `import "nombre"` y se resuelven desde `vendor/nombre/main.kry`. Solamente las declaraciones `pub` cruzan la frontera del paquete. El paquete oficial `packages/discord` ofrece la primera integración externa: su API es Kryndel puro y delega HTTPS y RFC 6455 en primitivas tipadas del runtime.
