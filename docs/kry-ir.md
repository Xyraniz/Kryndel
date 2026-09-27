# KIR v5

KIR (`kry-ir`) is the stable interchange format between the checked Kryndel frontend and compiler backends. `kry emit file.kry --format=kry-ir` writes one canonical UTF-8 JSON document followed by a newline.

KIR v5 is a typed serialization of the checked AST, but is not yet the shared
execution IR for the whole compiler. `Engine.RunPathWithArgs` executes the
validated scalar KIR subset; other valid programs fall back to the checked AST
only when the subset validator returns its explicit unsupported sentinel. KIR
runtime diagnostics and malformed KIR do not trigger a retry, and output from
the supported subset is written before its diagnostic. `DebugPath` remains on
the AST route for breakpoints and local-variable inspection. The C generator
still consumes AST and checker state. The direct ELF backend now decodes KIR and
lowers an explicitly bounded subset to x86-64 instructions that calculate
values and run branches and loops inside the generated executable. That slice
covers scalar `Int`, `Bool`, `String`, and `Nil` bindings, assignments, integer
arithmetic and comparisons, Boolean logic, String equality, `if`/`while`, and
`print`/`println` of `Int`, `Bool`, or `String`. Its runtime enforces
instruction, wall-time, and output limits and writes source-mapped runtime
diagnostics to stderr before exiting with status 1. It accepts KIR v3 through v5 programs
with top-level statements or one plain zero-argument `main() -> Nil` function;
imports, user functions, Float, `str`, and dynamic String concatenation are
outside this direct KIR lowering. Other constructs may still use the existing
direct backend when that backend supports their semantics. This machine-code
lowerer targets Linux amd64, accepts at most 20,000 KIR nodes, and reserves at
most 1 MiB for language-local stack slots; ordinary decoder and artifact limits
also apply.

The separate Go KIR executor runs the Engine's bounded scalar slice and serves
as a semantic oracle for the differential tests and the native subset
validator; it does not precompute the native executable's output. `DecodeKIR` checks the wire structure and
resource bounds, while subset validation additionally checks scalar types and
resolved lexical bindings. Neither replaces the source checker or proves
semantics for arbitrary KIR. The self-hosted KIR backend accepts a narrower
language subset and rejects unsupported constructs.
The self-hosted KIR backend accepts a narrower language subset and rejects
unsupported constructs.
See the [language specification](language-spec.md) for source semantics and
the [architecture status](architecture.md#intermediate-representation-status)
for the planned common intermediate representation.

The top-level contract is:

```json
{
  "format": "kry-ir",
  "version": 5,
  "language_version": "1.0.0",
  "module": "...",
  "source": "...",
  "target": { "os": "linux", "arch": "amd64", "gui": false },
  "imports": [],
  "sources": [],
  "structs": [],
  "enums": [],
  "traits": [],
  "trait_impls": [],
  "functions": [],
  "statements": []
}
```

KIR v5 is intentionally typed and lossless for the checked frontend representation:

- Every expression contains `kind`, source coordinates, and its resolved `type`.
- Binary and unary nodes contain the canonical operator spelling (`<<`, `&`, `|`, and so on).
- Calls contain `call_target`; builtins additionally contain their stable registry `builtin_id`.
- An indirect call stores its typed callable expression in `callee`; its argument count and argument/result types are checked against that function signature.
- A lambda expression stores a function body, signature, and ordered lexical `captures`. Each capture records its name, checked type, mutability, and declaration source coordinate. Nested closures propagate outer captures they need to construct later.
- Resolved parameter, local, loop, pattern, and variable bindings include the declaration source coordinate, type, and mutability, so shadowed names retain distinct identities. The validator checks that lambda captures match their uses and rejects missing, unused, mismatched, or locally declared captures.
- A function reference used as a value has a signature type and a signature-qualified function target, so overload selection remains fixed by the checker.
- User calls to an overloaded name carry a deterministic signature-qualified target (`function:<name>@<sha256>`), derived from the module, receiver, name, and parameter types. A plain name remains the target when it identifies exactly one declaration. The decoder rejects a plain call target when that name is overloaded, so consumers can follow the checker's selection without resolving overloads again.
- Function parameters, defaults, declarations, control-flow bodies, match patterns, and source names are preserved.
- Generic function and struct declarations retain their type parameters and constraints. Calls and struct expressions retain their checked generic type arguments and instantiated struct type; the decoder validates their declared arity and type syntax.
- Trait declarations record their required methods and exact signatures. Trait implementations record one concrete target and the function identity for each implementation method. A generic call through a trait bound carries `trait_name` and the symbolic `trait:Trait::method` call target; the decoder checks the trait, signature, implementation metadata, and function reference structurally.
- Function declarations retain their source name, line, and column, including trait implementation functions, so KIR consumers can reproduce the function's source frame.
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

The encoder uses ordered structs and source order, not Go maps, so identical checked input and target produce byte-identical output. `DecodeKIR` rejects malformed JSON, trailing data, incomplete or unsupported targets, unknown fields, unsupported versions, and documents over the configured byte limit. It also checks declaration names and references, required expression types, operator and node kinds, call targets, function signatures and source locations, capture records, generic arity and type syntax, trait signatures and implementation references, struct and map shape, statement and match shape, and recursive node, nesting, and list limits. These checks enforce KIR's structural contract; they do not rerun source overload resolution or prove that a backend implements a node's semantics. KIR v1 is accepted and assigned language version 1.0.0 because v1 predates that field; v2 remains readable for programs without v3 function-value nodes, v3 remains readable without v4 generic-instantiation metadata, and v4 remains readable without v5 trait metadata and function locations. A backend must explicitly opt into a future KIR version before consuming a changed schema.

The interpreter exposes JSON as a validated value plus typed field, array, string, integer, unsigned-integer, float, boolean, and null accessors. A compiler written in Kryndel can therefore traverse this document without a Go helper. LLVM output is not advertised yet: the former placeholder emitted a constant-returning function and has been removed rather than treated as a compiler backend.

The repository's self-hosted slices are `selfhost/elf_backend.kry`, `selfhost/dynamic_backend.kry`, and `selfhost/source_kir_compiler.kry`. The latter lexes and parses the scalar function/source subset, including `pub fn`, `break`/`continue`, multiline array literals/indexing and generic `Option[...]`/`Result[...]` annotations with nested payloads, plus opaque `Json`/`Map[...]` ABI values and their checked constructor, predicate, unwrap, and error-projection calls. It still emits the narrower KIR v2 subset, which the Go decoder reads for backward compatibility; function values and closures are outside that self-hosted slice. The dynamic backend consumes the KIR v2 subset, lowers stack-backed Int/Bool/UInt state, static String objects represented by pointers, immutable qword-backed Array objects, tagged pointer-like Option/Result objects, assignments, checked signed and wrapping unsigned arithmetic, comparisons, `if`/`else`, `while`, loop control, function calls using up to six SysV AMD64 registers and pointer/scalar returns in `RAX`, scalar/Array/Option/Result/Json/Map function-local declarations, static and dynamic Boolean/integer/String output, dynamic String concatenation and Array/Option/Result allocation through direct Linux `mmap` runtimes, Array bounds checks, `array_get` Option construction, relative x86-64 branches, RIP-relative data references, and ELF64 headers using only Kryndel arrays and `UInt16/32/64`. `array_set` applies checked immutable patches after emission. Tested Linux amd64 runtime slices cover bounded JSON parsing and kind checks, object lookup, array length/index access, and string/bool/signed/unsigned scalar accessors, plus `Map[String, Int]` initialization, lookup, membership, insertion, and removal. The Go direct backend is the byte-level oracle for the Option/Result, source-array, loop-control, and opaque-ABI parity stages; JSON and Map runtime fixtures separately execute generated ELF files. Broader Map key/value types and untested JSON accessors remain outside the verified runtime boundary. Other dynamic String operations, Set/resource values, unsupported array builtins, non-array heap values, and non-Linux targets are rejected explicitly.
