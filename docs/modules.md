# Modules

A module is a `.kry` source file imported by a root program or another module. The syntax is:

```kryndel
import "lib/math"
```

The `.kry` suffix is optional.

> [!NOTE]
> Import paths resolve relative to the file containing the import, not the
> process working directory.

The resolver canonicalizes the candidate path, requires it to remain under
the root program's directory, and rejects absolute paths and any path
component named `..`. This prevents path traversal and keeps resolution
deterministic.

Only top-level declarations marked `pub` are exported. For standalone source modules, private functions, structs, and enums remain available only inside their defining file. Files under the same directory with a `kry.toml` manifest share package-private access, including private struct fields; this lets a package split its implementation across files without making helpers public. A caller outside that package still needs `pub` declarations. Imported public declarations are merged into the importing program's single checked namespace; duplicate names are rejected. Enum variants use the explicit `Type::Variant` form. There is no implicit wildcard namespace or host-language fallback.

Each module is parsed once per canonical path. A module stack detects cycles and reports the path involved rather than recursing indefinitely. Missing files, malformed source, malformed UTF-8, unsafe paths, artifact imports, duplicate exports, and type errors retain the source file and location that caused the failure.

The current standard-library namespace is intentionally small and implemented by the Go builtin registry. Repository modules may be organized beside the root file, and their public APIs are ordinary checked Kryndel declarations. A future package manifest may extend the search root only after its format and safety rules are validated; the current implementation does not pretend to support an unvalidated package manager.
