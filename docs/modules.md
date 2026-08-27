# Modules

A module is a `.kry` source file imported by a root program or another module. The syntax is:

```kryndel
import "lib/math"
```

The `.kry` suffix is optional. Resolution is relative to the directory containing the importing file, not the process working directory. The resolver canonicalizes the candidate path, requires it to remain under the root program's directory, and rejects absolute paths and any path component named `..`. This prevents path traversal and keeps resolution deterministic.

Only top-level declarations marked `pub` are exported. Private functions, structs, and enums remain available only inside their defining module. Imported public declarations are merged into the importing program's single checked namespace; duplicate names are rejected. There is no implicit wildcard namespace or host-language fallback.

Each module is parsed once per canonical path. A module stack detects cycles and reports the path involved rather than recursing indefinitely. Missing files, malformed source, malformed UTF-8, unsafe paths, artifact imports, duplicate exports, and type errors retain the source file and location that caused the failure.

The current standard-library namespace is intentionally small and native. Repository modules may be organized beside the root file, and their public APIs are ordinary checked Kryndel declarations. A future package manifest may extend the search root only after its format and safety rules are validated; the current implementation does not pretend to support an unvalidated package manager.
