# Kryndel testing guide

Tests use only Python's standard library and run without network, services,
locale assumptions, or a desktop session.

```bash
PYTHONPATH=. python3 -m py_compile kryndel/*.py tests/test_kryndel.py
PYTHONPATH=. python3 -m unittest discover -s tests -v
```

The pre-change checkout contained **117 tests**. This checkpoint adds one
regression, the KEXE checkpoint, the bundle-policy checkpoint, and the
runtime-builtins checkpoint add one each, so the post-change suite contains
**121 tests** and covers:

| Layer | Contract |
| --- | --- |
| Lexer/parser | comments, literals, spans, precedence, recovery, enum payload syntax, match syntax |
| Checker | nominal structs/enums, payload arity/types, bindings, imports, module interfaces, visibility, diagnostics, exhaustiveness |
| Compiler/VM | deterministic `MAKE_ENUM`, matching, extraction, equality, immutable `BytesValue`, strict UTF-8, octet validation, Bytes concatenation/indexing, assertions, malformed metadata, runtime errors |
| Packages and modules | manifest subset, semver, local/path registry, transitives, lock ordering, cycles, checksums, traversal, nested module resolution, ambiguity, exports, private symbols, deterministic linking |
| CLI/artifacts | human/JSON errors, exit codes, init/add/install/list/tree, build/run/inspect, KEXE checksums, `host-report`, `doc`, `pack`, and structured `kry test` results |
| Data core | bounded Unicode StringSlice and BytesSlice readers, explicit bounds errors, balanced StringBuilder, and nominal Span/Token/AST/diagnostic source records |
| Value layout | canonical `Value` discriminant order, nominal scalar/collection/toolchain records, immutable sequence payloads, and source-constructor regression under the bootstrap VM |

Failure tests assert stable codes and useful spans rather than entire prose
paragraphs. Happy-path tests run through `compile_source` or the project-aware
`compile_project` and `VM` so parser, checker, compiler, module linker, and
runtime mismatches are visible together. Module tests create only local path
packages, refresh their SHA-256 checksum, install offline, and assert concrete
qualified function names and output. Bytes tests execute the visible constructors,
strict decoder, octet indexing, range errors, and source-level stdlib wrappers;
they also compare deterministic value fixtures and compiled bytecode. Testing
assertions are available through `stdlib/testing/testing.kry`; failures use
`KRY6401` and `KRY6402`. `kry test --format json` runs each test in a fresh
compiled VM and reports deterministic per-test status without tracebacks.
Determinism tests compile identical source and resolve identical package/module
graphs twice. The value-layout test compiles `stdlib/core/value.kry` through the bootstrap
VM, validates the canonical fixture, exercises every frozen constructor, and
checks nested nominal records and enum payloads. It deliberately reports the
module as a source compatibility seam because the VM is still Python-owned. The
data-core test compiles `stdlib/core/data.kry` through the
bootstrap VM, checks Unicode codepoint indexing and octet indexing, exercises
invalid bounds, builds a string from chunks, and verifies declaration-ordered
nominal records against `tests/fixtures/data-core-v1.json`. Manifest tests
also compare source-level range results with the Python oracle and compare
canonical source lockfile JSON byte for byte with `Lockfile.dumps()`. The
source bytecode verifier test checks normalized v1 records, entry presence,
opcode/operand bounds, malformed metadata, declared-call arity, reachable
fallthrough without `RETURN`, and the explicit `verify_execution` operand-stack
gate without using host dictionaries. Structural schema fixtures remain allowed
to contain one metadata example for every opcode; they are not treated as
executable control-flow programs.
The full source-schema regression decodes one canonical JSON case for every v1
opcode, checks normalized metadata and typed scalar constants, passes the result
through the source verifier, and rejects malformed call, struct, enum, binding,
function, entry, opcode, and constant cases with `KRY6305`.

The source lexer test compares the published snapshot's token kinds, normalized
text, order, and spans, then exercises nested-comment, invalid-character, and
unterminated-string recovery. A typed-token fixture additionally checks tagged
integer, float, boolean, string, nil, text, and EOF payload fields. The source
parser test feeds those source
lexer tokens into `stdlib/core/parser.kry` and compares AST root kinds and
spans with `parser-v1.json`. A typed-AST fixture additionally verifies that the
literal payload remains attached after parsing. The source checker test then
validates the lexer-parser-checker pipeline, typed Int/Float/Bool/String/Void
assignments, primitive mismatch and unknown-name diagnostics,
and deterministic module resolution for valid, missing, duplicate, and cyclic
imports. The source compiler test lowers the source lexer/parser subset
into bytecode records and validates the emitted instruction sequence with the
source verifier. A typed-bytecode fixture additionally checks canonical
constant text, `PUSH_CONST` categories, `PUSH_NIL`, and decoding into nominal
runtime values, including unsupported-category `KRY7006`. The KEXE reader test
checks v1 magic, big-endian payload length, offsets, checksum extraction, and
malformed framing through `stdlib/core/artifact.kry`; it then passes the
extracted payload and canonical checksum text through the source SHA-256 verifier
and rejects a tampered payload. JSON/module decoding remains explicitly out of
scope. The SHA-256 source test runs
empty, single-block, and multi-block known vectors and verifies `KRY6205` on a
mismatch. The JSON source test parses nominal null, boolean, integer, float,
string, array, and object values, including escapes, and rejects malformed
subset input with `KRY6304`. The schema regression decodes a v1 JSON module
into nominal function/instruction records, passes it through the source verifier,
and rejects malformed format, version, function, and argument cases with
`KRY6305`. The same schema path is exercised through controlled
`VirtualFileSystem` input with `decode_bytecode_file`; a missing file preserves
`KRY6302`. The full schema test also re-encodes the normalized module and compares
canonical JSON byte for byte with the Python reference, including typed scalar
constants. The end-to-end KEXE source pipeline then reads a real Python-reference
artifact, rebuilds its framing byte for byte with `write_artifact_bytes`, verifies
its extracted digest, decodes its payload bytes, validates the normalized module,
and runs it through the source runtime to return `hello`. The source runtime
fixture `runtime-source-v1.json` additionally passes `verify_execution` and
executes arithmetic, typed values, loops, jumps, internal calls, builtin output,
enum matching/binding, collections,
struct fields, and unary operations through `stdlib/core/runtime.kry`; the
separate `runtime-builtins-v1.json` fixture covers `bytes`, immutable
`array_push`, `abs`, bounded `sqrt`, passing assertions, and
`KRY6202`/`KRY6401`/`KRY6402` failures. The
compiler subset executes end to end and checks the nominal completion value. The source backend test emits
the x86_64 Linux empty-main seed twice,
compares it byte for byte, executes the 132-byte direct ELF `Bytes` seed from a
path containing a space, checks exit statuses 0, 7, and 255, passes a decoded
`PUSH_CONST`/`RETURN` and fixed conditional-jump programs through the direct
backend, checks `je .L4` and `jmp .L5`, and rejects unsupported targets, shapes,
and out-of-range statuses. The seed CLI regression also
builds and executes a status-7 raw ELF.
The CLI integration
also builds and executes the raw ELF seed with only POSIX shell utilities and
without an installed Python, assembler, or linker. The seed-only offline checker
repeats that generation in a spaced directory with isolated `PATH`/`HOME`,
checks ELF magic and determinism, and executes the result; it does not verify a
Kryndel toolchain bundle. The native seed runtime checker uses a separate
`KRYSEED1` module in a spaced directory, runs the fixed ELF under an isolated
`PATH`/empty `HOME`, checks exact framing, reserved mode, nonzero payload length,
bounded stdout, first-byte exit status, malformed length, and missing-file errors,
and explicitly reports that it does not replace the compiler, full bytecode VM,
package manager, or normal Python CLI. `tools/kry-kexe-check` is tested separately
against a Python-generated KEXE differential artifact under `PATH`/`HOME` isolation;
it verifies only v1 framing, payload length, digest, and symlink rejection, and
must not be described as a native module reader or runtime. The bundle policy
regression uses `bundle-audit-v1.json` and a temporary directory with a clean
candidate, forbidden Python/toolchain content, and a symlink; it runs
`tools/kry-bundle-check` with `PATH`/`HOME` isolation and does not imply that a
real product bundle exists.
`kry autonomy-audit` emits a deterministic `kryndel-autonomy-audit` report. It
lists the normal `python3 -m kryndel` route, the bootstrap modules, every
textual Python invocation found in supported repository files, and a four-state
matrix distinguishing `Kryndel-native`, `host capability nativa mínima`,
`bootstrap Python`, and `no implementado`. The report is evidence for migration
planning; it does not turn a source seam into a native component.

The formatter CLI test exercises check/rewrite modes
and empty-file handling without Python. The source formatter test also checks
trailing-horizontal-whitespace removal, blank-line trimming, final newline
canonicalization, and idempotence.

Temporary package registries are created under temporary directories. Tests
never modify a user's package registry or install Python dependencies. The
module graph tests additionally assert `KRY5013` through `KRY5016` and
`KRY3050` through `KRY3052`, including package/module context for import errors.

## Tests written in Kryndel

A project may place source tests below `tests/` and mark zero-argument functions
with `@test`:

```kryndel
@test
fn test_answer() -> Void {
    let answer: Int = 40 + 2
    println(answer)
}
```

`kry test` discovers files in deterministic path order and functions in source
order, then executes each function through the VM. The discovery format and
result names are language-independent even though the initial runner is still
implemented by the Python bootstrap. The runner isolates each source file,
continues through known compile/runtime failures, returns exit code 1 if any
case fails, and can emit a versioned JSON result document. Unknown host errors
are not converted into success and remain visible to the caller.
