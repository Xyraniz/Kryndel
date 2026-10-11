# KIR v6

KIR (`kry-ir`) is the stable interchange format between the checked Kryndel frontend and compiler backends. `kry emit file.kry --format=kry-ir` writes one canonical UTF-8 JSON document followed by a newline.

KIR v6 is the canonical interchange tree. Go `ValidatedMIR` stores a flat,
typed arena built from the checked frontend or from one validated decode of
this wire format, with immutable source-text and visibility sidecars for
diagnostics. `EmitKIR` serializes from the arena. The interpreter follows arena
indexes through shallow scalar adapters and preflights its supported subset.
C AOT, all PE paths, and both static and dynamic ELF lowering also traverse
arena indexes directly. No native backend materializes a recursive typed
compatibility view after validation.

The checked source AST is consumed by `CompileMIR` only to populate the
validated arena; runtime execution and native lowerers receive that arena
through `ValidatedMIR`. The Engine interpreter keeps globals, closures, and
runtime lifecycle state in a persistent KIR executor. Its evaluator resolves
child references through the typed arena and leaves recursive fields empty on
its scalar adapters. C AOT and direct PE follow arena indexes directly and
materialize only scalar node rows while emitting code.
Direct ELF lowers its bounded native subset from the validated arena and rejects
unsupported forms during typed preflight, before generating output. `DecodeMIR` can be executed
by the interpreter or consumed by native lowerers without source or visibility
sidecars. KIR retains source names and coordinates for diagnostics, but decoded
KIR cannot provide source excerpts. The direct ELF KIR subset lowers to x86-64
instructions that calculate values and run branches and loops inside the
generated executable. Its core scalar slice covers `Int`, `Bool`, `String`,
and `Nil` bindings, assignments, integer arithmetic and comparisons, Boolean logic,
String equality, `if`/`while`, and scalar `print`/`println`. A wider direct KIR
value slice also supports bounded non-capturing function calls, immutable
arrays, structs, and tagged `Option`/`Result` values, with tested control flow
and runtime operations. Its runtime enforces
instruction, wall-time, and output limits and writes source-mapped runtime
diagnostics to stderr before exiting with status 1. The scalar subset accepts
KIR v3 through v6 with top-level statements or one plain zero-argument
`main() -> Nil`; the wider subset has its own checked KIR shape and runtime
limits. Float, closures, and other unimplemented KIR forms are rejected by the
direct KIR lowerers. The builder targets Linux amd64 and caps traversal at
20,000 KIR nodes and language-local stack storage at 1 MiB; ordinary decoder
and artifact limits also apply.

The Go KIR executor backs the production interpreter and also provides bounded
direct KIR execution for differential tests and native subset validation; it
does not precompute native executable output. Supported host builtins use the
shared runtime implementation with the same sandbox and limits as source
execution. Filesystem calls remain confined to the restricted root, SQLite
remains unavailable there, and open SQLite handles are closed at execution
end with the same leak diagnostic as the interpreter. Builtin support is
checked explicitly; an unknown builtin cannot inherit support from its effect
category alone. `DecodeKIR` checks the wire structure and resource bounds,
while executor validation additionally checks supported types and resolved
lexical bindings. Neither replaces the source checker or proves semantics for
arbitrary KIR. The self-hosted KIR backend accepts a narrower language subset
and rejects unsupported constructs.
See the [language specification](language-spec.md) for source semantics and
the [architecture status](architecture.md#intermediate-representation-status)
for the shared MIR boundary and target-specific support limits.

The top-level contract is:

```json
{
  "format": "kry-ir",
  "version": 6,
  "language_version": "1.0.0",
  "module": "...",
  "source": "...",
  "target": { "os": "linux", "arch": "amd64", "gui": false },
  "imports": [],
  "import_records": [],
  "sources": [],
  "structs": [],
  "enums": [],
  "traits": [],
  "trait_impls": [],
  "functions": [],
  "statements": []
}
```

KIR v6 is intentionally typed and lossless for the checked frontend representation:

- Every expression contains `kind`, source coordinates, and its resolved `type`.
- Binary and unary nodes contain the canonical operator spelling (`<<`, `&`, `|`, and so on).
- Calls contain `call_target`; builtins additionally contain their stable registry `builtin_id`.
- An indirect call stores its typed callable expression in `callee`; its argument count and argument/result types are checked against that function signature.
- A lambda expression stores a function body, signature, and ordered lexical `captures`. Each capture records its name, checked type, mutability, and declaration source coordinate. Nested closures propagate outer captures they need to construct later.
- Resolved parameter, local, loop, pattern, and variable bindings include a canonical binding ID, declaration source coordinate, UTF-8 byte span, checked type, and mutability, so shadowed names retain distinct identities. The validator checks that lambda captures match their uses and rejects missing, unused, mismatched, or locally declared captures.
- A function reference used as a value has a signature type and a signature-qualified function target, so overload selection remains fixed by the checker. For functions without a receiver, the decoder verifies the referenced signature, including the substituted signature and constraints for instantiated generic function values.
- User calls to an overloaded name carry a deterministic signature-qualified target (`function:<name>@<sha256>`), derived from the module, receiver, name, and parameter types. A plain name remains the target when it identifies exactly one declaration. The decoder rejects a plain call target when that name is overloaded, so consumers can follow the checker's selection without resolving overloads again.
- Function parameters, defaults, declarations, control-flow bodies, match patterns, and source names are preserved.
- Generic function and struct declarations retain their type parameters and constraints. Calls and struct expressions retain their checked generic type arguments and instantiated struct type; the decoder validates their declared arity and type syntax, then checks struct constraints recursively in signatures, fields, expressions, calls, and constructions.
- Direct calls validate receiver instantiations, generic constraints, and argument/result types after substituting receiver and function type arguments.
- Struct constructors check type arguments against declared constraints and literal values against named field types after substitution.
- Struct field access checks that the base is a declared struct, the field exists, and the checked result type matches the field after generic substitution.
- Map literals check their declared `Map[K, V]` type, supported key type, and every key/value against `K`/`V`.
- Array and set literals check that each element matches the declared element type and that set elements do not contain function values.
- Index expressions check the base collection, index type, and result type for strings, bytes, arrays, and maps.
- Propagation expressions check that `?` runs inside a compatible Option/Result-returning function and returns the operand payload type.
- Trait declarations record their required methods and exact signatures. Trait implementations record one concrete target and the function identity for each implementation method. A generic call through a trait bound carries `trait_name` and the symbolic `trait:Trait::method` call target; the decoder checks the trait, signature, implementation metadata, and function reference structurally.
- Source-bearing nodes and declarations retain a half-open UTF-8 byte span in addition to source name, line, and column, including trait implementation functions, so KIR consumers can reproduce precise source frames.
- Constant-folded expressions contain a `const` value, including the width and value of `UInt8/16/32/64`.

For a program loaded from files, `module`, `source`, declaration modules, source
coordinates, and `sources` use slash-separated paths relative to the resolved
project root. `imports` preserves the root file's declared import paths. The
emitter also uses these logical module names when hashing overloaded call
targets. Identical source trees therefore produce identical KIR after being
moved to another checkout directory; absolute host paths are not embedded.
KIR generated for a portable KRYNATIVE6 bundle uses the reserved `portable/any`
target with `gui` disabled, so the checked representation is independent of the
host that creates or runs the artifact. Native targets remain tied to their
explicit OS and architecture.

The machine-readable structural schema is [docs/kir-v6.schema.json](kir-v6.schema.json); cross-field invariants remain validator responsibilities. The encoder uses ordered structs and source order, not Go maps, so identical checked input and target produce byte-identical output. `DecodeKIR` rejects malformed JSON, trailing data, incomplete or unsupported targets, unknown fields, unsupported versions, and documents over the configured byte limit. It also checks declaration names and references, required expression types, operator and node kinds, call targets, function signatures and source locations, capture records, generic arity and type syntax (including substituted call and function-value signatures and constraints), trait signatures and implementation references, struct field names, value types, and field access types after generic substitution, array/set element and map key/value types, index base/index/result types, propagation operand/result context, statement and match shape, exhaustiveness for Bool, Nil, Option, Result, and enum matches, and recursive node, nesting, and list limits. These checks enforce KIR's typed structural contract; they do not rerun source overload resolution or prove that a backend implements a node's semantics. KIR v1 is accepted and assigned language version 1.0.0 because v1 predates that field; v2 remains readable for programs without v3 function-value nodes, v3 remains readable without v4 generic-instantiation metadata, and v4–v5 remain readable without v6 source spans, binding IDs, and import records. Older inputs receive reconstructed binding IDs and keep unknown byte spans. A backend must explicitly opt into a future KIR version before consuming a changed schema.

The interpreter exposes JSON as validated values with a sorted, limit-bounded object-key accessor and typed object, array, string, integer, unsigned-integer, float, boolean, and null accessors. A compiler written in Kryndel can therefore decode this interchange document without a Go helper. After validation, executable consumers follow the typed arena rather than the recursive JSON value. LLVM output is not advertised yet: the former placeholder emitted a constant-returning function and has been removed rather than treated as a compiler backend.

The self-hosted compiler components are `selfhost/source_kir_compiler.kry`, `selfhost/elf_backend.kry`, and `selfhost/dynamic_backend.kry`. The source frontend has a bounded parser and module resolver; function values, closures, and other unimplemented language features remain outside its supported subset. It appends checked declarations, functions, bindings, expressions, statements, patterns, values, spans, imports, and child references directly to schema-shaped typed tables. `compile_path` validates that arena through `validate_kir_typed_arena` and passes the resulting `ValidatedKIR` to the backend without serializing or parsing JSON. `--emit-kir` serializes the validated arena at the output boundary. Portable KIR input receives a wire-shape check and one conversion to the same typed arena before that validator runs. The self-hosted validator checks instantiated generic struct field types and constraints, function signatures, supported builtin contracts, references, lexical bindings, mutability, and v6 metadata. ELF and dynamic ELF/PE lowerers consume indexed typed views; `ValidatedKIR` does not retain the recursive JSON document. Go and Kry use separate row implementations, so the shared schema and corpus guard the portable wire contract and representative validation parity, not identical in-memory layouts or complete language parity. Backends reject unsupported operations and targets explicitly.

The verified Linux amd64 ELF runtime subset includes stack-backed Int/Bool/UInt values, static String and immutable qword-backed Array objects, tagged pointer-like Option/Result values, assignments, checked signed and wrapping unsigned arithmetic, comparisons, `if`/`else`, `while`, loop control, calls using up to six SysV AMD64 register parameters, scalar and pointer returns in `RAX`, bounds checks, and direct Linux `mmap` allocation. Tested JSON operations cover bounded parsing and kind checks, object/array access, and string/bool/signed/unsigned accessors. Tested `Map[String, Int]` operations cover initialization, lookup, membership, insertion, and removal. Go-direct parity and executable selfhost tests cover representative option/result, source-array, loop-control, imports, builtins, and opaque-ABI paths. Broader Map key/value types, untested JSON accessors, dynamic String operations other than concatenation, Set/resource values, unsupported array builtins, general heap values, and non-Linux targets remain explicit rejection points.
