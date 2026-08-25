# Kryndel manifest and lockfile format v1

The bootstrap accepts a deliberate subset of TOML. It accepts only the
`[package]` and `[dependencies]` tables, blank/comment lines, and plain
`key = "value"` assignments. The package table must contain exactly:

```toml
[package]
name = "demo"
version = "0.1.0"
edition = "2026"

[dependencies]
request = "1.0.0"
```

Package names are ASCII identifiers without path separators. Versions are
semantic `MAJOR.MINOR.PATCH`. Requirements additionally support `^1.2`,
`~1.2`, and `>=1.0.0,<2.0.0`. A local dependency is represented internally as
`path:../package`; it must remain inside the project workspace.

`kry.lock` is deterministic JSON with `format = kryndel-lock`, version `1`,
and a sorted `packages` array. Every entry records `name`, `version`,
`checksum`, `source`, and sorted direct dependency names. Checksums are SHA-256
over sorted relative file names and file bytes, excluding the `checksum` file.

The only implemented registry is the offline local tree
`.kryndel/registry/<name>/<version>/`. A registry package must contain a valid
manifest and a matching `checksum` file. Installation copies validated files to
`.kryndel/packages/<name>` using a temporary staging directory. Package files
are never executed, and symlinks/path traversal outside the package are
rejected. Remote registries are intentionally not claimed by v1.

## Source package archive

`kry pack` produces a `.krypkg` ZIP archive containing `kry.toml` and the
sorted regular files below `src/`. It never executes source code, includes no
absolute paths, excludes generated artifacts and symlinks, and sets every ZIP
entry timestamp to `1980-01-01T00:00:00Z`. The archive order is lexical and its
SHA-256 is printed as the observable package checksum. Packing identical source
and manifest bytes twice must produce identical archive bytes and checksum.
The archive is a source distribution for the current bootstrap; it is not a
native executable and does not claim an independent runtime.
