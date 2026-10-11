# Native architecture

Kryndel is distributed as one native executable. The source pipeline is deliberately direct:

```text
UTF-8 source
    -> lexer
    -> parser and AST
    -> module resolver
    -> static type checker annotating the AST
    -> validated in-memory MIR/KIR
    -> interpreter or target backend
    -> bounded runtime or emitted artifact
    -> output, diagnostics, or versioned KRYNATIVE6 bundle
```

`check`, `run`, and `build` share the lexer, parser, module resolver, and checker. `check` stops before evaluation; `build` stops before evaluation and stores the exact validated root and imported sources; `run` evaluates the checked program or embedded artifact graph. The formatter also parses and checks before producing output, so invalid source is never silently rewritten.

| Component | Responsibility | Runtime dependency |
| --- | --- | --- |
| Lexer | UTF-8 validation, comments, identifiers, literals, operators, positions, and token limits. | Go standard library only. |
| Parser | Expressions, bindings, functions, modules, data declarations, blocks, and patterns. | Native AST. |
| Type checker | Type inference, annotations, complete-argument multiple dispatch, constrained function and user-struct generics, static trait bounds and method resolution, receiver/method substitutions, privacy, operators, mutability, returns, conditions, exhaustiveness, and constant folding. | Native type model and source locations. |
| Module resolver | Relative source lookup, public exports, cycle detection, and traversal rejection. | Explicit filesystem paths only. |
| Runtime | Evaluates validated KIR directly with lexical scopes, first-class function values and closures, mutable capture cells, indirect calls, tail-call trampolines, recursion, collections, UTF-8, options, results, control flow, channels, synchronized `Shared[T]` cells, actor mailboxes, structured `TaskGroup` workers, safepoints, and budgets. | Go standard library, `internal/platform` for camera/display capture, and modules listed in `go.mod` for host integrations. |
| Generated C runtime | Supplies ordered C source fragments for value formatting, display, arithmetic, equality, builtins, collections, and host integrations. | Independent leaf package `internal/cruntime`; Go standard library only. |
| Platform integrations | Bounded camera frame capture and display enumeration/screenshot encoding behind byte-oriented APIs. | Target-specific camera implementations, screenshot/webcam libraries, or FFmpeg on Windows camera capture. |
| Artifact reader/writer | Versioned KRYNATIVE6 metadata, package visibility scopes, deterministic source bundle, checked KIR payload, SHA-256 hashes, atomic writes, and strict replay validation; reads KRYNATIVE3 through KRYNATIVE5. | Go binary/file APIs. |
| CLI and language server | `check`, `run`, `build`, `fmt`, `lsp`, `repl`, `doctor`, `version`, and help. | Explicit command-line and filesystem inputs; LSP messages over standard input/output. |

## Ownership and values

All compiler and runtime allocations are tracked by a per-invocation arena. The arena is released after successful checking, runtime failure, parser failure, module failure, malformed artifact input, and every other ordinary command path. The launcher and CLI keep only file buffers that they explicitly free after the invocation.

Kryndel values are immutable after construction except for the explicit `Shared[T]` cell API. Bindings carry a mutability bit, and assignments are permitted only for bindings declared with `let mut`. Arrays, bytes, and strings are not mutated in place; concatenation and `array_push` create new storage, while function calls pass value representations that are safe to share because collection elements cannot be assigned. Struct fields, enum tags, option contents, and result contents are also immutable. A `const` binding additionally requires a recursively const-safe type graph, so mutable runtime handles cannot be hidden inside it.

Lexical scopes are represented by parent-linked environments. A declaration is local to the current scope and may shadow a parent name. Lookup and assignment walk from the innermost scope outward; an assignment to an outer immutable binding is rejected by both checker and runtime.

## Concurrency and cleanup

`Channel[T]` is a FIFO queue backed by Go synchronization primitives. Send and receive operations use predicate-based waits, try/timed variants return typed status values, and close wakes both wait sets. `Thread[T]` owns a managed worker and a result channel. Worker functions are named zero-argument functions, execute in a channel-only worker-safe scope, and report their first diagnostic through `thread_join`; worker safety is propagated through the reachable call graph. The parent runtime requests cooperative cancellation, closes channels, and joins outstanding workers on every ordinary exit path, including an earlier runtime failure. Worker-local arenas release their allocations after execution; channel synchronization primitives are destroyed after all workers have joined.

## Numeric behavior

`Int` is a signed 64-bit value. Addition, subtraction, multiplication, unary negation, division, remainder, and `abs` use explicit boundary checks. `UInt8`, `UInt16`, `UInt32`, and `UInt64` are separate fixed-width values: arithmetic and bitwise operations mask results to the declared width, and no implicit signed/unsigned conversion occurs. Division and remainder by zero, `Int` minimum negation, `Int` minimum absolute value, and the `Int` minimum divided by `-1` are deterministic errors. Unsigned shifts reject negative or out-of-width counts. Float literals and results must be finite; float division by positive or negative zero is rejected. Allocation byte counts are checked before multiplication or addition.

## Artifact format

`build` accepts a source file, validates it, and writes a deterministic KRYNATIVE6 bundle with a fixed magic, format/compiler/language/target metadata, exact payload length, an ordered `<root>` plus imported source entries, package visibility scopes, a SHA-256 hash for every source, and canonical typed KIR with its own hash. The KIR uses the host-independent `portable/any` target, while native outputs keep their explicit OS and architecture. The reader rejects incompatible metadata, truncated or oversized fields, duplicate or unsafe paths, invalid hashes, trailing bytes, invalid KIR, and any embedded source that fails the ordinary lexer, parser, module, or checker pipeline. It regenerates KIR from the checked source graph using the embedded target and requires byte-for-byte agreement with the embedded IR, so the artifact cannot pair one checked representation with different source. Building the same source twice produces identical bytes. KRYNATIVE3 through KRYNATIVE5 artifacts remain readable; KRYNATIVE3 is interpreted as language version 1.0.0. `emit --format=kry-ir` exposes the checked program through deterministic KIR v6 JSON; the decoder accepts KIR v1 through v6. `--format=elf` uses the generated-C AOT backend, while `--format=elf-direct` lowers its supported KIR subset to Linux amd64 machine code and rejects unsupported constructs before emitting an executable.

## Intermediate representation status

`CompileMIR` lowers the checker-annotated source tree straight into an opaque
`ValidatedMIR` backed by flat typed node tables and checked indexes. It checks
that the supplied checker belongs to that tree, requires successful semantic
checking, validates source resource bounds and target metadata, validates all
arena references, and snapshots source text and package visibility as
diagnostic sidecars. It does not build a recursive KIR tree or encode and
decode JSON. `DecodeMIR` parses and validates the portable wire document once,
then stores the same arena form. `EmitKIR` serializes from that arena.

All production run and native-build entrypoints begin from `ValidatedMIR`.
That value no longer retains the recursive wire document: its canonical state
is the typed arena, including explicit optional references and bounded child
ranges. C AOT, the interpreter, direct ELF, and direct PE now follow those
indexes and use shallow scalar row adapters with recursive edges left empty.
The interpreter resolves expression, statement, function, parameter, and match
references through the arena and preflights its supported subset before
execution. Dynamic ELF validates and lowers its supported subset through the
same typed arena; it does not construct a recursive compatibility view. The
interpreter keeps its runtime state in a persistent KIR executor, including
across REPL snippets. Native backends reject constructs outside their
supported subsets without an AST-derived fallback.

`DecodeMIR` validates the portable KIR document without source-text and package
visibility sidecars, then flattens it into the same arena used by
`CompileMIR`. KIR carries source names and coordinates, so
runtime diagnostics retain file, line, and column; decoded documents do not
provide source excerpts. Artifact loading additionally checks the embedded
source graph and recompiles the MIR before accepting the bundle.
`ValidateASTLimits` only counts source-tree resource use before lowering; it is
not a second instruction representation.

KIR v6 retains checked types, resolved call targets, generic declarations and
instantiations, function values and captures, trait implementation targets,
canonical binding IDs, UTF-8 byte source spans, and target metadata. Versions
1–5 remain readable; older documents receive reconstructed binding IDs and
keep line/column locations without invented byte spans. Direct ELF's KIR slices cover scalar
bindings, arithmetic and comparisons, Boolean logic, String equality,
assignments, branches and loops, plus bounded function, immutable array,
struct, and `Option`/`Result` paths. Direct PE also lowers its supported subset
from KIR. The direct machine backends target Linux amd64 and Windows amd64,
respectively, and do not imply support for every valid KIR node.

The source checker remains the semantic authority for source programs. KIR
decoding validates the wire contract and checked invariants such as operator
types, assignment mutability, control-flow bindings, call metadata, and match
exhaustiveness; it does not independently rerun source overload resolution.
Target feature checks now walk validated KIR against the selected target
before output bytes are emitted.
Backend support limits and KIR version history are tracked in the
[KIR reference](kry-ir.md).

The Go interpreter and native lowerers consume the indexed arena described
above. The selfhost source parser accumulates schema-shaped typed rows and
references directly in `KIRTypedArena`, including declarations, functions,
bindings, expressions, statements, patterns, values, spans, imports, and child
ranges. `build_source_arena` combines imported modules into the same arena;
`compile_path` passes it through `validate_kir_typed_arena` before calling
`compile_validated_kir`. It does not serialize or parse KIR JSON. The optional
`--emit-kir` path serializes the validated arena at the output boundary.

Portable JSON enters selfhost through a separate decoder boundary. Its wire
shape is checked, it is converted to the typed arena, and the same arena
validator checks references, node shapes, checked types, lexical bindings,
mutability, call targets, and v6 metadata before returning `ValidatedKIR`.
That value retains the typed arena only. The ELF and dynamic ELF/PE lowerers
consume indexed `KIRTypedNodeView` values, whose child access follows table
references; they do not retain a recursive JSON document. Go and Kry use
separately implemented row types. The shared schema and corpus guard the wire
contract and representative validator parity, rather than identical in-memory
layouts or complete language support in the selfhost frontend.

The production boundaries are:

| Path | Arena construction or validation | Consumer |
| --- | --- | --- |
| Go checked source | `ir.go`: `CompileMIR`; `kir_arena_builder.go` | Opaque `ValidatedMIR` |
| Go portable KIR | `kir.go`: `DecodeMIR` | The same `ValidatedMIR` arena |
| Go interpreter | `engine.go`, `kir_exec.go`, `kir_exec_arena.go` | Indexed expression, statement, function, and binding rows |
| C AOT | `codegen.go`: `generateCFromValidatedKIR`; `codegen_arena.go` | Typed rows and child ranges |
| Direct ELF | `machine.go`: `buildDirectELFFromMIR`; `kir_machine_static_arena.go`, `kir_machine_direct_arena.go` | The validated arena for static and dynamic subsets |
| Direct PE | `machine_pe.go`, `kir_machine_pe.go`: `lowerDirectPEKIR` | The validated arena for static and dynamic subsets |
| Selfhost checked source | `source_kir_compiler.kry`: `build_source_arena`; `validated_kir.kry`: `validate_kir_typed_arena` | `ValidatedKIR` |
| Legacy selfhost source CLI | `source_compiler.kry`: delegates to `source_kir_compiler.kry`'s `compile_path` | The same validated source arena and backend |
| Selfhost portable KIR | `kir_backend.kry`; `validated_kir.kry`: `validate_kir_document` | The same `ValidatedKIR` arena |
| Selfhost ELF and dynamic ELF/PE | `elf_backend.kry`: `compile_kir`; `dynamic_backend.kry`: `compile_validated_kir` | Indexed typed views and explicit capability rejection |

The self-hosted dynamic backend has an explicit `LoweredNativeImage` contract
between lowering and serialization. It carries code, data, target, imports,
and typed relocation/function-range rows; `native_image.kry` selects the ELF
or PE writer without inspecting KIR. Lowering no longer traverses recursive
JSON. `pe_backend.kry` serializes the already lowered native image. The
standalone `kir_arena.kry` generic JSON utility is not a production lowering
input. Structural regressions enforce these boundaries; bootstrap provenance
and target runtime parity must also be rerun for changed compiler sources.
