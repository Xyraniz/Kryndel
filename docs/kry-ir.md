# KIR v2

KIR (`kry-ir`) is the stable interchange format between the checked Kryndel frontend and compiler backends. `kry emit file.kry --format=kry-ir` writes one canonical UTF-8 JSON document followed by a newline.

KIR v2 is currently a typed serialization of the checked AST, not a shared
execution IR. The Go interpreter, C generator, and direct ELF backend still
consume the AST and checker state through separate paths. The self-hosted KIR
backend accepts a narrower language subset and rejects unsupported constructs.
See the [language specification](language-spec.md) for source semantics and
the [architecture status](architecture.md#intermediate-representation-status)
for the planned common intermediate representation.

The top-level contract is:

```json
{
  "format": "kry-ir",
  "version": 2,
  "language_version": "1.0.0",
  "module": "...",
  "source": "...",
  "target": { "os": "linux", "arch": "amd64", "gui": false },
  "imports": [],
  "sources": [],
  "structs": [],
  "enums": [],
  "functions": [],
  "statements": []
}
```

KIR v2 is intentionally typed and lossless for the checked frontend representation:

- Every expression contains `kind`, source coordinates, and its resolved `type`.
- Binary and unary nodes contain the canonical operator spelling (`<<`, `&`, `|`, and so on).
- Calls contain `call_target`; builtins additionally contain their stable registry `builtin_id`.
- User calls to an overloaded name carry a deterministic signature-qualified target (`function:<name>@<sha256>`), derived from the module, receiver, name, and parameter types. A plain name remains the target when it identifies exactly one declaration. The decoder rejects a plain call target when that name is overloaded, so consumers can follow the checker's selection without resolving overloads again.
- Function parameters, defaults, declarations, control-flow bodies, match patterns, and source names are preserved.
- Constant-folded expressions contain a `const` value, including the width and value of `UInt8/16/32/64`.

The encoder uses ordered structs and source order, not Go maps, so identical checked input and target produce byte-identical output. `DecodeKIR` rejects malformed JSON, trailing data, incomplete or unsupported targets, unknown fields, unsupported versions, and documents over the configured byte limit. It also checks declaration names and references, required expression types, operator and node kinds, call targets, struct and map shape, statement and match shape, and recursive node, nesting, and list limits. These checks enforce KIR's structural contract; they do not rerun source overload resolution or prove that a backend implements a node's semantics. KIR v1 is accepted and assigned language version 1.0.0 because v1 predates that field. A backend must explicitly opt into a future KIR version before consuming a changed schema.

The interpreter exposes JSON as a validated value plus typed field, array, string, integer, unsigned-integer, float, boolean, and null accessors. A compiler written in Kryndel can therefore traverse this document without a Go helper. LLVM output is not advertised yet: the former placeholder emitted a constant-returning function and has been removed rather than treated as a compiler backend.

The repository's self-hosted slices are `selfhost/elf_backend.kry`, `selfhost/dynamic_backend.kry`, and `selfhost/source_kir_compiler.kry`. The latter lexes and parses the scalar function/source subset, including `pub fn`, `break`/`continue`, multiline array literals/indexing and generic `Option[...]`/`Result[...]` annotations with nested payloads, plus opaque `Json`/`Map[...]` ABI values and their checked constructor, predicate, unwrap, and error-projection calls. It serializes that subset to KIR v2 JSON and feeds the dynamic backend. The dynamic backend consumes the KIR v2 subset, lowers stack-backed Int/Bool/UInt state, static String objects represented by pointers, immutable qword-backed Array objects, tagged pointer-like Option/Result objects, assignments, checked signed and wrapping unsigned arithmetic, comparisons, `if`/`else`, `while`, loop control, function calls using up to six SysV AMD64 registers and pointer/scalar returns in `RAX`, scalar/Array/Option/Result/Json/Map function-local declarations, static and dynamic Boolean/integer/String output, dynamic String concatenation and Array/Option/Result allocation through direct Linux `mmap` runtimes, Array bounds checks, `array_get` Option construction, relative x86-64 branches, RIP-relative data references, and ELF64 headers using only Kryndel arrays and `UInt16/32/64`. `array_set` applies checked immutable patches after emission. The Go direct backend is the byte-level oracle for these slices; regression tests require byte-identical output, including the Option/Result, source-array, loop-control, and opaque-ABI stages. JSON/Map operations are still an explicit next runtime boundary: parser recognition and opaque ABI transport do not claim native object manipulation yet. Other dynamic String operations, Set/resource values, unsupported array builtins, non-array heap values, and non-Linux targets are rejected explicitly.
