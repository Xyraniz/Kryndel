# Kryndel manifest reader boundary v1

The strict manifest subset now has a pure text parser and a filesystem adapter. `parse_manifest_text` owns the manifest grammar and stable `KRY5001`, `KRY5002`, `KRY5003`, `KRY5010`, and `KRY5012` diagnostics. `read_manifest_from_filesystem` obtains UTF-8 bytes only through the `FileSystem` protocol and maps invalid UTF-8 to `KRY6304`.

The existing `read_manifest` API remains compatible for project callers. It creates a `RootedFileSystem` at the manifest parent, reads only the basename, and restores the physical manifest path on the returned `Manifest` so path-dependency resolution and deterministic lockfiles retain their previous behavior.

The `VirtualFileSystem` path is the differential seam. `tests/fixtures/manifest-reader-v1.json` freezes one valid `[package]` plus `[dependencies]` input and its structured result. Negative coverage checks malformed UTF-8 and the existing unsupported-section diagnostics.

> This increment makes manifest IO explicit and testable; it does not make the parser Kryndel-native or remove Python from package resolution.
