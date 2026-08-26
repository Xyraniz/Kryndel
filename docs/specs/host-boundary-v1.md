# Kryndel host boundary v1

This contract defines the temporary filesystem capability used by the bootstrap while a Kryndel-native implementation is being built. The language-facing boundary accepts only relative POSIX paths and bytes; host paths and arbitrary host objects do not cross into semantic values.

## Path policy

A path must be relative, must not contain a drive prefix, backslash, or a component equal to `..`, and is normalized without locale or current-working-directory dependence. The root is represented by `.`. An absolute path or a path that escapes the root produces `KRY6303`; malformed separators or invalid input produce `KRY6304`.

The rooted adapter resolves each component beneath one explicit root and rejects symlinks before reading, writing, statting, or listing. It never follows an entry outside the root. Missing files and directories produce `KRY6302`; non-regular entries, failed IO, and invalid write targets produce `KRY6301`.

## Operations

The v1 surface consists of `read_bytes(path)`, `read_text(path)`, `write_bytes(path, value)`, `list_dir(path)`, and `stat(path)`. The executable source wrappers in `stdlib/core/filesystem.kry` expose these operations as `String -> Bytes`, `String -> String`, `String, Bytes -> Void`, `String -> Array`, and `String -> FileMetadata`. Directory listings are sorted by portable relative path. `FileMetadata` is a nominal value with declaration-ordered fields `path: String`, `kind: String`, and `size: Int`; timestamps, permissions, absolute paths, and host-specific inode data are excluded.

`VirtualFileSystem` implements the same operations in memory and is the primary deterministic test seam. `RootedFileSystem` is the small temporary host adapter. The VM receives one explicit `FileSystem` capability; it never derives a root from the current working directory and it refuses filesystem calls when no capability is configured. Both adapters return immutable byte results and serializable `DiagnosticError` values rather than Python tracebacks. The bootstrap maps those diagnostics to runtime messages retaining `KRY6301`–`KRY6304` and does not expose host paths as semantic values.

> This boundary is an executable bootstrap contract. It does not claim that the language parser, package manager, or runtime has been rewritten in Kryndel.

## Acceptance evidence

The regression suite checks sorted listings, byte and strict UTF-8 text round trips, nominal metadata fields, missing paths, malformed separators, absolute and traversal rejection, and symlink rejection for both adapters and the source-level API. The next integration step is to make the manifest reader consume this interface without changing manifest v1 diagnostics or lockfile bytes.
