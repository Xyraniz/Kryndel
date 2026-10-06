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

`build` accepts a source file, validates it, and writes a deterministic KRYNATIVE6 bundle with a fixed magic, format/compiler/language/target metadata, exact payload length, an ordered `<root>` plus imported source entries, package visibility scopes, a SHA-256 hash for every source, and canonical typed KIR with its own hash. The KIR uses the host-independent `portable/any` target, while native outputs keep their explicit OS and architecture. The reader rejects incompatible metadata, truncated or oversized fields, duplicate or unsafe paths, invalid hashes, trailing bytes, invalid KIR, and any embedded source that fails the ordinary lexer, parser, module, or checker pipeline. It regenerates KIR from the checked source graph using the embedded target and requires byte-for-byte agreement with the embedded IR, so the artifact cannot pair one checked representation with different source. Building the same source twice produces identical bytes. KRYNATIVE3 through KRYNATIVE5 artifacts remain readable; KRYNATIVE3 is interpreted as language version 1.0.0. `emit --format=kry-ir` exposes the checked program through deterministic KIR v5 JSON; the decoder accepts KIR v1 through v5. `--format=elf` uses the generated-C AOT backend, while `--format=elf-direct` lowers its supported KIR subset to Linux amd64 machine code and rejects unsupported constructs before emitting an executable.

## Intermediate representation status

`CompileMIR` converts the checker-annotated source tree into an opaque,
validated `ValidatedMIR` backed by flat typed node tables and checked indexes.
It checks that the supplied checker belongs to that tree, validates the KIR
structure and resource bounds, and snapshots source text and package
visibility as diagnostic sidecars. `DecodeMIR` parses and validates the wire
document once, then stores the same arena form. `EmitKIR` serializes from that
arena; source compilation does not encode and decode JSON to reach it.

All production run and native-build entrypoints begin from `ValidatedMIR`.
That value no longer retains the recursive wire document: its canonical state
is the typed arena, including explicit optional references and bounded child
ranges. Current Go lowerer implementations still request a complete typed
compatibility view from that arena; migrating their recursive walkers to
indexed access remains unfinished. This is a real intermediate step, not the
final single-representation boundary. The interpreter keeps its runtime state
in a persistent KIR executor, including across REPL snippets. Direct ELF and
PE reject constructs outside their supported subsets without an AST-derived
fallback.

`DecodeMIR` validates the portable KIR document without source-text and package
visibility sidecars, then flattens it into the same arena used by
`CompileMIR`. KIR carries source names and coordinates, so
runtime diagnostics retain file, line, and column; decoded documents do not
provide source excerpts. Artifact loading additionally checks the embedded
source graph and recompiles the MIR before accepting the bundle.
`ValidateASTLimits` only counts source-tree resource use before lowering; it is
not a second instruction representation.

KIR v5 retains checked types, resolved call targets, generic declarations and
instantiations, function values and captures, trait implementation targets,
source locations, and target metadata. Direct ELF's KIR slices cover scalar
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

The arena migration currently covers storage and construction in the Go
toolchain, not every consumer's traversal. The self-hosted compiler still keeps
serialized KIR JSON in `ValidatedKIR` and its dynamic backend lowers from that
document; the Go and Kry validators are not generated from a common schema.
The shared corpus remains a parity guard, not proof that the two compilers use
one in-memory representation. KIR v6 binding IDs and full source ranges, direct
indexed lowering in Go, and the self-hosted arena migration are still required.
