# Paquetes, registry y reproducibilidad

Kryndel usa `kry.toml` como manifiesto de proyecto y `kry.lock` como resultado de resolución. La resolución acepta versiones exactas, `^`, `~`, `>=` y `*`; ordena candidatos semánticamente y procesa dependencias en orden estable. El instalador descarga archivos `tar.gz`, valida SHA-256 antes de extraerlos y copia únicamente entradas regulares bajo `vendor/<nombre>`.

| Comando | Resultado | Garantía |
| --- | --- | --- |
| `kry new app` | Crea un proyecto con `kry.toml` y `main.kry`. | No introduce un runtime externo. |
| `kry add discord ^1.0.0` | Actualiza `[dependencies]`. | El manifiesto se serializa determinísticamente. |
| `kry install` | Resuelve, descarga, verifica, extrae y escribe `kry.lock`. | Los hashes y URLs quedan registrados. |
| `kry update` | Repite la resolución desde el registry configurado. | La instalación puede auditarse desde el lockfile. |
| `kry uninstall nombre [nombre ...]` | Quita dependencias directas y poda `vendor/`. | Conserva dependencias transitivas todavía alcanzables y no borra la caché global. |
| `kry package` | Crea un `tar.gz` reproducible. | Omite `kry.lock`, fija tiempos de entrada y ordena rutas. |
| `kry publish` | Envía el archivo al endpoint `PUT /publish/<name>/<version>`. | El registry recalcula el SHA-256 y actualiza su índice. |
| `kry cache clean` | Elimina la caché de índices y archivos. | No toca el proyecto ni el lockfile. |

El cliente usa por defecto el registry público estático de Kryndel en `https://raw.githubusercontent.com/Xyraniz/Kryndel/main/registry`. Su índice y sus archives son archivos versionados en el repositorio, así que `kry install discord` funciona sin configuración adicional. `KRY_REGISTRY` permite usar un mirror, un registry privado o el registry local; `kry registry serve ROOT --addr 127.0.0.1:8765` sirve la estructura mínima `ROOT/index/<name>.json` y `ROOT/packages/<archive>.tar.gz`. `KRY_CACHE` cambia la caché y `KRY_OFFLINE=1` obliga a utilizar únicamente índices y archivos ya almacenados.

> El instalador rechaza rutas absolutas, separadores alternativos que escapen de la raíz, `..`, enlaces simbólicos, directorios en el archivo, entradas duplicadas y archivos mayores que el límite de archivo. La verificación criptográfica ocurre antes de la extracción.

Los paquetes se importan con `import "nombre"` y se resuelven desde `vendor/nombre/main.kry`. Solamente las declaraciones `pub` cruzan la frontera del paquete. Las librerías oficiales incluyen `discord`, `async`, `crypto`, `fs` y `json`; estas últimas exponen concurrencia estructurada, criptografía estándar, operaciones filesystem sandboxed y JSON seguro sobre las primitivas tipadas del runtime.
