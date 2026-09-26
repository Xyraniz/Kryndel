# Packages, registries, and reproducibility

Kryndel uses `kry.toml` as the project manifest and `kry.lock` as the resolution result. Every manifest must declare a `kryndel` compiler requirement. New manifests record `language_version = "1.0.0"` under `[package]`; manifests without that key use 1.0.0 for compatibility. The current compiler version is `1.3.0`. Project requirements are checked before `check`, `run`, `build`, and `emit`; package requirements are checked before a package is cached or copied into `vendor`.

Version requirements accept exact three-part numeric versions, `^`, `~`, `>=`, and `*` (an optional leading `v` is accepted). Partial versions, wildcards inside a version, prerelease suffixes, combined operators, and malformed constraints are rejected. Candidates are sorted semantically and dependencies are processed in stable order. The installer verifies SHA-256, archive coordinates, the required `kry.toml` and `main.kry`, and dependency metadata before extraction and vendoring.

| Command | Result | Guarantee |
| --- | --- | --- |
| `kry new app` | Creates a project with `kry.toml` and `main.kry`. | Does not introduce an external runtime. |
| `kry add discord ^2.3.0` | Declares the current Discord bot API library release in `[dependencies]`. | The manifest is serialized deterministically; `kry install` still checks compiler compatibility. |
| `kry add discord-self ^1.2.0` | Adds the separate user-account package and its `discord` transport dependency. | It is opt-in and automating ordinary user accounts is prohibited by Discord. |
| `kry install` | Resolves, downloads, verifies, extracts, and writes `kry.lock`. | Hashes and URLs remain recorded. |
| `kry update` | Repeats resolution from the configured registry. | The installation can be audited from the lockfile. |
| `kry uninstall name [name ...]` | Removes direct dependencies and prunes `vendor/`. | Reachable transitive dependencies are preserved and the global cache is not deleted. |
| `kry package` | Creates a reproducible `tar.gz`. | Omits `kry.lock`, fixes entry times, and sorts paths. |
| `kry publish` | Sends the archive to `PUT /publish/<name>/<version>`. | The registry recomputes SHA-256 and updates its index. |
| `kry cache clean` | Removes cached indexes and archives. | The project and lockfile are untouched. |

> [!CAUTION]
> `kry update` can change resolved package versions. Review the resulting
> `kry.lock` before sharing or releasing a project; it records the exact archive
> URLs and SHA-256 hashes.

By default, the client uses Kryndel's static public registry at `https://raw.githubusercontent.com/Xyraniz/Kryndel/main/registry`. Its index and archives are versioned repository files. The current official releases `async` 1.2.1, `crypto` 1.3.1, `discord` 2.3.0, `discord-self` 1.2.0, `fs` 1.2.1, and `json` 1.1.1 all support compiler 1.3.0 and declare `kryndel = ">=1.3.0"`. `KRY_REGISTRY` selects a mirror, private registry, or local registry; `kry registry serve ROOT --addr 127.0.0.1:8765` serves the minimum layout `ROOT/index/<name>.json` and `ROOT/packages/<archive>.tar.gz`. Since publishing has no authentication, the server refuses non-loopback bind addresses. Package-manager requests block cross-origin redirects. `KRY_CACHE` changes the cache location, and `KRY_OFFLINE=1` restricts the client to already stored indexes and archives.

The `discord` index contains only package `2.3.0`; historical Discord archives have been removed. `discord-self` 1.2.0 is a separate package and depends on `discord ^2.3.0`.

> The installer rejects absolute paths, alternate separators that escape the root, `..`, symbolic links, directories in the archive, duplicate entries, and files larger than the per-file limit. Cryptographic verification happens before extraction.

Packages are imported with `import "name"` and resolved from `vendor/name/main.kry`. Files under the same manifest directory share package-private access. Only declarations marked `pub` cross the package boundary; a private helper in an imported package module is not part of the caller's API. Official libraries include `discord`, `async`, `crypto`, `fs`, and `json`; these expose structured concurrency, standard cryptography, sandboxed filesystem operations, and safe JSON over typed runtime primitives.

See [Discord package usage](discord.md) for webhook URL parsing, execution, message management, and the current API boundaries.
