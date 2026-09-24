# Packages, registries, and reproducibility

Kryndel uses `kry.toml` as the project manifest and `kry.lock` as the resolution result. New manifests record `language_version = "1.0.0"` under `[package]`; manifests without that key use 1.0.0 for compatibility. Resolution accepts exact versions, `^`, `~`, `>=`, and `*`; candidates are sorted semantically and dependencies are processed in stable order. The installer downloads `tar.gz` archives, validates SHA-256 before extraction, and copies only regular entries under `vendor/<name>`.

| Command | Result | Guarantee |
| --- | --- | --- |
| `kry new app` | Creates a project with `kry.toml` and `main.kry`. | Does not introduce an external runtime. |
| `kry add discord ^2.2.0` | Adds the current Discord API library release to `[dependencies]`. | The manifest is serialized deterministically. |
| `kry install` | Resolves, downloads, verifies, extracts, and writes `kry.lock`. | Hashes and URLs remain recorded. |
| `kry update` | Repeats resolution from the configured registry. | The installation can be audited from the lockfile. |
| `kry uninstall name [name ...]` | Removes direct dependencies and prunes `vendor/`. | Reachable transitive dependencies are preserved and the global cache is not deleted. |
| `kry package` | Creates a reproducible `tar.gz`. | Omits `kry.lock`, fixes entry times, and sorts paths. |
| `kry publish` | Sends the archive to `PUT /publish/<name>/<version>`. | The registry recomputes SHA-256 and updates its index. |
| `kry cache clean` | Removes cached indexes and archives. | The project and lockfile are untouched. |

By default, the client uses Kryndel's static public registry at `https://raw.githubusercontent.com/Xyraniz/Kryndel/main/registry`. Its index and archives are versioned repository files, so `kry install discord` works without additional configuration. `KRY_REGISTRY` selects a mirror, private registry, or local registry; `kry registry serve ROOT --addr 127.0.0.1:8765` serves the minimum layout `ROOT/index/<name>.json` and `ROOT/packages/<archive>.tar.gz`. `KRY_CACHE` changes the cache location, and `KRY_OFFLINE=1` restricts the client to already stored indexes and archives.

The public Discord index currently contains only package `2.2.0`. Historical Discord archives have been removed; exact older pins and constraints that exclude 2.2.0 must be updated.

> The installer rejects absolute paths, alternate separators that escape the root, `..`, symbolic links, directories in the archive, duplicate entries, and files larger than the per-file limit. Cryptographic verification happens before extraction.

Packages are imported with `import "name"` and resolved from `vendor/name/main.kry`. Files under the same manifest directory share package-private access. Only declarations marked `pub` cross the package boundary; a private helper in an imported package module is not part of the caller's API. Official libraries include `discord`, `async`, `crypto`, `fs`, and `json`; these expose structured concurrency, standard cryptography, sandboxed filesystem operations, and safe JSON over typed runtime primitives.

See [Discord package usage](discord.md) for webhook URL parsing, execution, message management, and the current API boundaries.
