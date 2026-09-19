# KIR v1

KIR (`kry-ir`) is the stable interchange format between the checked Kryndel frontend and compiler backends. `kry emit file.kry --format=kry-ir` writes one canonical UTF-8 JSON document followed by a newline.

The top-level contract is:

```json
{
  "format": "kry-ir",
  "version": 1,
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

KIR v1 is intentionally typed and lossless for the checked frontend representation:

- Every expression contains `kind`, source coordinates, and its resolved `type`.
- Binary and unary nodes contain the canonical operator spelling (`<<`, `&`, `|`, and so on).
- Calls contain `call_target`; builtins additionally contain their stable registry `builtin_id`.
- Function parameters, defaults, declarations, control-flow bodies, match patterns, and source names are preserved.
- Constant-folded expressions contain a `const` value, including the width and value of `UInt8/16/32/64`.

The encoder uses ordered structs and source order, not Go maps, so identical checked input and target produce byte-identical output. `DecodeKIR` rejects malformed JSON, trailing data, incomplete targets, unknown fields, unsupported versions, and oversized documents. A backend must explicitly opt into a future KIR version before consuming a changed schema.

The interpreter exposes JSON as a validated value plus typed field, array, string, integer, unsigned-integer, float, boolean, and null accessors. A compiler written in Kryndel can therefore traverse this document without a Go helper. LLVM output is not advertised yet: the former placeholder emitted a constant-returning function and has been removed rather than treated as a compiler backend.

The repository's self-hosted slices are `selfhost/elf_backend.kry`, `selfhost/dynamic_backend.kry`, and `selfhost/source_kir_compiler.kry`. The latter lexes and parses the scalar function/source subset, serializes it to KIR JSON, and feeds the dynamic backend. The dynamic backend reads KIR v1, lowers stack-backed Int/Bool/UInt state, assignments, checked signed and wrapping unsigned arithmetic, comparisons, `if`/`else`, `while`, loop control, scalar function calls using up to six SysV AMD64 registers and scalar returns in `RAX`, scalar function-local declarations, static and dynamic integer output, relative x86-64 branches, RIP-relative data references, and ELF64 headers using only Kryndel arrays and `UInt16/32/64`. `array_set` applies checked immutable patches after emission. The Go direct backend is the byte-level oracle for all three slices; regression tests require byte-identical output. Non-scalar parameters/returns/locals, heap values, and non-Linux targets are rejected explicitly.
