# Self-hosting stages

The bootstrap stage names and completion evidence are defined in the
[bootstrap contract](bootstrap-contract.md). The Stage 14–36 entries below are
feature milestones and must not be mistaken for complete bootstrap stages.

`elf_backend.kry` is the reusable lowering component of the first executable compiler written in Kryndel. `dynamic_backend.kry` adds the second lowering slice, and `kir_backend.kry` provides their command-line entrypoint. Together they consume the checked frontend's `kry-ir/v2` JSON and write a Linux x86-64 ELF64 file without invoking C, Go, or an external assembler.

`source_compiler.kry` is the next bootstrap stage. It contains a bounded but real source lexer and recursive-descent expression parser written in Kryndel itself. It handles comments, line separators, escaped strings, static `let`/`const` bindings, `print`/`println`, `str(...)`, parentheses, and the arithmetic precedence levels `* / %` above `+ -`. It evaluates that static subset and sends the resulting bytes through the same Kryndel ELF emitter.

`source_kir_compiler.kry` is the following frontend slice. It lexes and parses typed functions (including `pub fn`), parameters, `return`, mutable bindings, assignment, Boolean conditions, `if`/`else`, `while`, `for` over arrays, `break`/`continue`, struct declarations and literals, field access, multiline array literals and indexing, array concatenation, `len`, `array_push`, `array_get`, `array_concat`, `array_indices`, scalar conversion calls, generic `Option[...]`/`Result[...]` annotations (including nested array payloads), opaque `Json`/`Map[...]` ABI values, their constructors/predicates/unwrapping operations, and output calls. It serializes the typed subset to KIR JSON in Kryndel, validates it with `json_parse`, and invokes `dynamic_backend.kry`. It is tested against the Go direct backend as a byte-level oracle.

The KIR stage accepts:

- top-level `let`/`const` bindings whose values are statically displayable;
- integer literals and checked `Int` `+`, `-`, `*`, `/`, `%` expressions;
- string literals, static variable references, `str(...)`, and string concatenation;
- top-level `print(...)` and `println(...)` calls.

The dynamic KIR stage additionally accepts top-level and nested `let`/`const`, mutable Int/Bool/UInt slots, assignments, checked signed arithmetic, wrapping fixed-width unsigned arithmetic, comparisons, bitwise operations, `if`/`else`, `while`, `for` over `Array[T]`, `break`, `continue`, functions with up to six SysV AMD64 register parameters, scalar `Int`/`Bool`/`UInt` values, immutable `String` pointers, immutable `Array[T]` pointers with literal/index/`len`/`array_push`/`array_get`/`array_indices`/concatenation operations, boxed struct literals and field loads, immutable pointer-like `Option[T]` and `Result[T,E]` values with `some`/`none`/`ok`/`err`, predicates, `unwrap_or`, `result_unwrap`, and `result_error`, scalar/Array/struct/Option/Result function-local declarations and assignments, static display values, and dynamic `Bool`/`Int`/`UInt`/`String` values inside `print`/`println`. It emits stack loads/stores, decimal integer and Boolean conversion, relative branches, calls/returns, RIP-relative references to immutable string objects, overflow traps, Linux `mmap`-backed String, qword-array, boxed-struct, and tagged Option/Result allocation/copy runtimes, bounds checks, and Linux syscalls directly from Kryndel. Other dynamic String operations, Map/Set/resource values, unsupported array builtins, general heap values outside the documented slices, `match`, and non-Linux targets remain explicit rejection points.

Unsupported arbitrary calls, dynamic String operations other than `+`, collection and resource values outside the documented Array/struct slice, unsupported array builtins, and general heap values outside the documented slices are rejected with explicit errors. The dynamic KIR backend's ELF target remains Linux amd64.

The original source stage has the static output subset and performs its own lexical and syntactic validation instead of recognizing complete source lines by prefix. `source_kir_compiler.kry` owns the dynamic scalar source subset; both frontends reject unsupported constructs explicitly rather than guessing.

An end-to-end run from the repository root is:

```text
kry emit selfhost/fixtures/static_output.kry --target=linux-x64 --format=kry-ir -o input.kir
kry run selfhost/kir_backend.kry input.kir stage1

kry emit selfhost/fixtures/dynamic_output.kry --target=linux-x64 --format=kry-ir -o dynamic.kir
kry run selfhost/kir_backend.kry dynamic.kir dynamic-stage2

kry run selfhost/source_compiler.kry selfhost/fixtures/source_stage2.kry stage2
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_dynamic_stage3.kry stage3
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_scalar_functions_stage5.kry stage5
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_option_result_stage8.kry stage8
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/array_runtime_stage6.kry stage9
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_nested_generics_stage10.kry stage10
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_loop_control_stage11.kry stage11
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_public_function_stage12.kry stage12
kry run selfhost/source_kir_compiler.kry selfhost/fixtures/source_opaque_abi_stage13.kry stage13
kry emit selfhost/fixtures/option_result_runtime_stage7.kry --target=linux-x64 --format=kry-ir -o option-result.kir
kry run selfhost/kir_backend.kry option-result.kir option-result-stage7
```

The Go direct backend is kept as a byte-level oracle for these stages. Regression tests execute both Kryndel programs under the interpreter and require byte-identical ELF output before a change can pass. This is bootstrap progress, not yet a complete self-hosting compiler: the source frontend has a bounded same-directory function-module resolver, while full module/type parity, general heap values, linker/object-file support, and broad Windows target parity remain ahead of this subset.

Stage 14 extends the direct ELF oracle with boxed struct values (literals, field loads, function parameters and returns), scoped shadowing, `for` lowering over `Array[T]`, and `array_indices`. It is covered by an executable Linux regression fixture. Stage 17 mirrors this ABI in `dynamic_backend.kry`; the self-hosted emitter now produces byte-identical ELF and the Linux regression executes the generated `48\n` result.

Stage 15 adds the first direct-ELF host-runtime boundary needed by a bootstrap executable: `process_args`, Linux syscall-backed `fs_read_text`, `bytes(Array[Int])`, and syscall-backed `fs_write_bytes`. The regression exercises both a successful write and a rejected path, and verifies the resulting bytes.

Stage 16 mirrors that host boundary in `dynamic_backend.kry` itself. The Kryndel backend lowers the five runtimes directly to Linux x86-64 instructions, emits them in the same deterministic order as the Go oracle, and passes byte-for-byte ELF parity. On Linux amd64 the regression also executes the generated self-hosted ELF and verifies its arguments, file read, byte write, successful `Result`, and rejected path behavior. The bootstrap compiler still depends on the Go interpreter to run this Kryndel backend, but the emitted executable no longer depends on C, Go, libc, or an external assembler at runtime.

Stage 17 mirrors the Stage 14 aggregate and loop features in `dynamic_backend.kry`: struct field metadata is carried through the lowering environment, struct literals and field reads use the direct-backend boxed ABI, `for` lowers to checked indexed iteration, and `array_indices` returns a real heap array. The regression requires byte-for-byte parity with the Go oracle on Windows and Linux, and executes the generated ELF on Linux amd64.

Stage 18 extends `source_kir_compiler.kry` across that same aggregate boundary. The source frontend now registers struct definitions before parsing dependent functions, validates struct field declarations and literals, lowers chained field/index postfix expressions, preserves loop binding scope, and serializes struct metadata into KIR. Its regression feeds the Stage 14 source fixture through the Kryndel frontend and requires parity with the Go direct ELF oracle.

Stage 19 adds native direct-ELF lowering for `assert`, `assert_eq`, `contains`, `starts_with`, and `ends_with`. Assertions compare actual scalar values and branch to the checked trap path; string predicates scan the immutable `{length, bytes}` representation without libc. The source frontend recognizes these builtins so the next bootstrap probe reports the next unsupported operation precisely.

Stage 20 adds the direct-ELF `string_chars` runtime. It counts UTF-8 leading-byte sequences, allocates the result array through the existing checked `mmap` allocator, and creates each code-point string through the existing string allocator. The regression exercises ASCII plus multibyte `é` and `🙂` values and verifies their exact output on Linux amd64.

Stage 21 adds immutable direct-ELF maps backed by alternating key/value words. `map_get` and `map_contains_key` perform typed scalar or UTF-8 string lookup, while `map_insert` copies the map and replaces or appends without mutation. The regression covers replacement, missing-key fallback, and assertion paths.

Stage 22 adds checked direct-ELF `substring`. It interprets `start` and `length` as Unicode code-point indices, copies the selected UTF-8 byte range through the native string allocator, and returns an explicit `Result` error for invalid ranges.

Stage 23 adds direct-ELF and self-hosted dynamic-backend `str` lowering for dynamic `Int`, fixed-width `UInt`, `Bool`, and immutable `String` values. Signed and unsigned decimal conversion is performed in a stack buffer and copied into the native String ABI; the regression covers negative, zero, maximum `UInt8`, both Boolean values, multibyte UTF-8, byte-for-byte parity, and Linux execution.

Stage 24 adds direct-ELF and self-hosted dynamic-backend `int` lowering. `int(String)` accepts a complete ASCII decimal value with an optional leading sign, accumulates negative values so `-9223372036854775808` remains representable, and traps on empty, malformed, or overflowing input. `int(UIntN)` checks the signed `Int` range, while `int(Int)` and `int(Bool)` preserve their value. The regression covers both signed limits, signs, fixed-width unsigned values, Boolean values, exact ELF parity, Linux execution, and rejection of invalid input.

Stage 25 adds the first native JSON ABI used by the self-hosted KIR compiler. `Json` values are immutable `{length, bytes}` pointers, `json_parse` performs a bounded scanner and returns a typed `Result`, `json_kind` classifies validated values without allocation, and `json_object_get` scans object members with nested-container and quoted-string awareness before copying the selected value into a fresh JSON slice. The direct-ELF regression covers primitive kinds, nested arrays/objects, successful lookups, missing keys, malformed input, and exact Linux execution output. Array and scalar conversion accessors remain the next native lowering slice.

Stage 26 adds native `json_string`. It validates a JSON string node in two passes, allocates a fresh `String`, copies UTF-8 bytes without changing them, and decodes the eight single-byte JSON escapes (`\\"`, `\\\\`, `\\/`, `\\b`, `\\f`, `\\n`, `\\r`, `\\t`). Unsupported Unicode escapes (`\\uXXXX`) return an explicit error until the native UTF-8 encoder exists; they are never silently misdecoded. The direct-ELF regression covers ordinary text, supported escapes, non-string input, malformed text, and this explicit unsupported case.

Stage 27 adds native `json_int`. It parses a complete JSON integer token directly from the immutable `Json` text, accepts JSON whitespace around the value, accumulates negatively so both signed 64-bit limits remain exact, and returns typed errors for non-numbers, fractions, exponents, leading-zero forms, and overflow. The direct-ELF regression covers signs, limits, whitespace, and rejection cases.

Stage 28 fixes the native `u8_array` runtime's loop bound, which previously compared against an uninitialized callee-saved register and returned a correctly sized array filled with zero values. The regression round-trips `Bytes` through `u8_array`, and the bootstrap regression now compiles a Kryndel source program with the generated source compiler, validates the resulting ELF, and executes it to verify its static string data.

Stage 34 fixes KIR emission for unary Boolean negation: `!` is now serialized as `!` instead of the fallback operator text `?`. Its regression lowers a small KIR program through `kir_backend.kry`, checks byte parity with the Go direct backend, and executes the ELF on Linux amd64. KIR documents also have a separate `MaxJSONBytes` limit (64 MiB by default); this is distinct from the 16 MiB limit for ordinary Kryndel strings.

Stage 35 generates the first native source compiler from KIR emitted by the locked Stage 0 Go CLI. The CLI runs `kir_backend.kry` to produce a Linux amd64 ELF; that compiler then compiles and runs a fixture. This stage is exercised as the first half of the Stage 36 bootstrap regression. The current KIR is 74,426,272 bytes (about 71.0 MiB), 7,317,408 bytes above the ordinary 64 MiB CLI limit; the lock records that exact size and a bootstrap-only 134,217,728-byte (`128 MiB`) JSON limit, leaving 59,791,456 bytes of headroom. Every growth consumes the measured bootstrap budget and must be reviewed. Project-relative paths keep the artifact independent of its checkout location. `.gitattributes` keeps Kryndel sources in LF form across Windows/WSL and Linux checkouts.

Stage 36 verifies a second compiler level. The generated Stage 35 compiler uses its own module resolver to compile the checked-in graph `source_kir_compiler.kry` → `dynamic_backend.kry` → `elf_backend.kry` and `pe_backend.kry` into a second Linux amd64 compiler ELF. That compiler compiles and runs the fixture, rejects invalid source with the expected diagnostic, emits a Windows amd64 PE32+ executable that imports public struct and enum modules, passes and returns the struct through a function, and prints the imported enum using its source name. The CI bootstrap job uploads this exact Stage 2-produced PE, and the Windows job runs it and checks the output. This proves the tested frontend/backend module graph can rebuild without invoking the Go backend during second-level compilation and that its Windows PE output runs under the native loader. It does not complete all self-hosting goals: unsupported language features, full PE language parity, and linker/object-file support remain outstanding. Reproduce both levels on Linux amd64 with:

Stage 37 corrects module brace tracking when a string contains `{` or `}`, assigns internal KIR symbols per source module so private helpers and types with the same spelling remain separate, and exposes only each module's own declarations plus public declarations from direct imports. Public structs can be constructed, fields can be read subject to visibility, and simple fieldless enums can be declared, imported, passed, returned, and lowered as tagged integer values. The regression covers private helpers, imported public structs and enums, type-name collisions, private-type leaks, enum variants, import cycles, unsafe paths, and string-token handling. This remains direct-import resolution for source files in the same directory; nested module directories, aliases, manifests, and payload enums are unsupported.

```text
go test ./internal/kry -run '^TestStage36KryndelSecondCompilerBootstrap$' -count=1 -timeout=20m -v
```

The standalone Stage 1–3 bootstrap command is `./scripts/bootstrap-stage3.sh`.
It requires Linux x86-64 and the Go version in `selfhost/bootstrap.lock.json`.
The test builds and hash-checks the Stage 0 Go CLI, then uses that executable
to emit the source KIR and run `kir_backend.kry`. It checks SHA-256 values for
the KIR, module sources, fixture, and generated Stage 1, Stage 2, and Stage 3
compiler ELFs against that lock.
Stage 2 and Stage 3 must also be byte-identical. This remains a bounded subset
bootstrap rather than full self-hosting.

On non-Linux hosts, `TestStage36KryndelSecondCompilerBootstrap` skips before emitting KIR or validating an ELF. Cross-platform compile checks do not establish execution of this bootstrap; both levels must pass on Linux amd64 before this stage is considered complete.

## Windows PE output from the self-hosted source frontend

`source_kir_compiler.kry` can request the C-free Windows x86-64 PE32+ serializer for programs inside a deliberately bounded subset:

```text
kry run selfhost/source_kir_compiler.kry program.kry program.exe windows-amd64
```

The compiler frontend and dynamic backend are written in Kryndel. The bootstrap builds them into a standalone Linux amd64 compiler ELF; that ELF emits the Windows `.exe` without invoking C, MinGW, an assembler, or an external linker. The compiler itself is not yet a Windows executable, and the generated `.exe` supports only the subset below.

The PE subset includes native `process_args()`: it excludes the executable path, decodes Windows quoting through `CommandLineToArgvW`, converts UTF-16 arguments to UTF-8, and releases the parser's allocated argument block. The regression executes the PE on Windows with no user arguments and with empty, spaced, quoted, backslash, and non-ASCII arguments. The Stage 2 compiler remains a Linux ELF; its own filesystem host runtime is not available in PE output yet.

The PE subset accepts scalar `Int`, `Bool`, `String`, and `UInt8`/`UInt16`/`UInt32`/`UInt64` values, plus simple fieldless enums lowered to declaration-order integer tags and printed as `Enum::Variant`. A local struct can be constructed from a literal with scalar or fieldless-enum fields, and fields can be read from a local struct value. These structs can also be passed to and returned from Kryndel functions; internally each struct is a heap pointer passed in a 64-bit slot, not a C ABI aggregate value. `Array[T]` is supported as a local initialized by an array literal, by aliasing an existing local array, or by a function call; `T` must be one of those scalar types or a fieldless enum. Every literal element must match `T`; a zero-element literal needs an explicit context such as `let empty: Array[String] = []`. The supported array operations are `len(array)` and read-only indexing of a named local array, such as `array[index]`. Indexes are checked at runtime; negative indexes and indexes greater than or equal to the length trap with exit code 1. `Map[String, T]` is supported as a local initialized by a literal, by aliasing another local map, or by a function call; `T` must be one of those scalar types or a fieldless enum. Map literal keys must be unique String literals and all values must have the declared `T` type; lookups support `map_contains_key(map, string)` and `unwrap_or(map_get(map, string), fallback_t)` with a matching fallback. A zero-element map literal requires an explicit `Map[String, T]` context. Functions can accept and return these supported arrays and maps; the PE ABI passes them as 64-bit pointers. Their allocations use the Win32 process heap and remain live until process exit. Reassignment, array element mutation, array concatenation, map insertion/removal, other collection builtins, nested collection values, non-String map keys, and collection values other than scalars or fieldless enums remain unsupported. `str` converts runtime `Int`, `Bool`, and `UInt` values to strings using the Win32 process heap. Ordinary functions can pass scalar and pointer-sized values in the four Win64 argument registers and on the stack after the 32-byte shadow area; the native Windows regressions cover five through eight arguments and nested calls. Returns, locals, assignments, calls, `if`/`else`, `while`, `break`/`continue`, supported scalar arithmetic and comparisons, and `print`/`println` are supported. Source-module imports are resolved and flattened before PE serialization; these are distinct from PE DLL imports. The PE writer accepts imports by DLL and symbol name, while the current dynamic backend uses `GetStdHandle`, `WriteFile`, and `ExitProcess`, adding `GetProcessHeap` and `HeapAlloc` when its generated code needs allocation. The writer emits x64 unwind metadata for the entry and compiled source-function ranges and a `.reloc` directory with a `DIR64` entry while setting the PE dynamic-base flag.

The PE serializer still rejects maps outside the String-keyed scalar/fieldless-enum subset described above, arrays with nested or composite element types, array reassignment and element mutation, array builtins other than `len`, map insertion/removal, indexing expressions whose base is not a local array variable, structs with nested or collection fields, payload enums, generic/worker/unsafe/receiver functions, GUI targets, host filesystem builtins (`fs_read_text` and `fs_write_bytes`), and other unsupported builtins and expressions. The dynamic backend does not yet expose arbitrary source-level DLL calls, and the writer supports imports by name rather than ordinal or delay-loaded imports. Runtime helpers do not yet all have generated unwind entries; normal execution of the tested paths works, but exception unwinding through an unregistered helper is not guaranteed. The `.reloc` entry currently covers a writer-owned anchor rather than a registry of arbitrary absolute addresses; generated code uses relative references. A Windows regression forces a test copy of the image to load away from its preferred base and verifies that this single `DIR64` anchor is fixed up; it does not prove arbitrary relocation coverage or the usual randomized `DYNAMIC_BASE` path. Stage 2 remains a Linux amd64 ELF compiler, so this is not yet a native Windows Stage 2 → Stage 3 bootstrap. Unsupported programs fail with a `PE backend:` diagnostic instead of receiving a partially lowered executable. `internal/kry/selfhost_pe_test.go` and `internal/kry/selfhost_pe_rebase_windows_amd64_test.go` cover native execution on Windows, including source-to-PE compilation, imported enum use, local scalar-field structs and function passing/returns, scalar-element arrays, String-keyed scalar/enum map lookups, collection function arguments and returns, Windows `process_args()` with empty, spaced, quoted, backslash, and UTF-8 arguments, stack arguments, function calls, control flow, integer and string output, forced relocation of the writer-owned anchor, unsupported-feature rejection, array bounds traps, and a checked runtime trap.
