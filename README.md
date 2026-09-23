# Kryndel

[![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Kryndel is a small, statically checked language for developer tools and structured applications. The repository contains the compiler, checker, runtime, standard library wrappers, package tooling, and a self-contained command-line executable.

The implementation is written in Go and uses the Go standard library. Source goes through lexing, parsing, module resolution, static checking, validated IR, and runtime execution. Released binaries do not need Go or another host-language runtime.

## Quick start

A Go 1.22 or newer toolchain is needed when building from a checkout.

```bash
git clone https://github.com/Xyraniz/Kryndel.git
cd Kryndel
make

./tools/kry run examples/hello.kry
./tools/kry examples/hello.kry
./tools/kry check examples/control_flow.kry
./tools/kry build examples/hello.kry -o /tmp/hello.kexe
./tools/kry /tmp/hello.kexe
```

The direct file form is useful for desktop integrations. On Windows, the release installer associates `.kry` and `.kexe` with Kryndel for the current user. On Linux, `make install-association` installs a per-user desktop association.

## Commands

| Command | What it does |
| --- | --- |
| `kry check FILE` | Parses, resolves, and type-checks a source file or artifact without running user code. |
| `kry run FILE` | Checks and runs a `.kry` source file or `.kexe` artifact. |
| `kry FILE.kry` | Runs a source file directly. This is the form used by file associations and double-click. |
| `kry build FILE` | Creates a deterministic `KRYNATIVE4` bundle. Native PE and ELF output is available for supported targets. |
| `kry fmt FILE` | Formats valid source with stable whitespace and indentation. |
| `kry repl` | Starts the interactive evaluator. |
| `kry doctor` | Checks the executable, limits, standard library, and runtime capabilities. |
| `kry new PROJECT` / `kry init` | Creates a project layout and manifest. |
| `kry add` / `kry install` | Manages vendored package dependencies. |
| `kry test` | Runs the project entrypoint, defaulting to `main.kry`. |

Use `kry --help` for global limits, JSON diagnostics, restricted filesystem execution, emit options, registry commands, and package commands.

The default package registry is hosted in this repository, so a new project can install the published Discord package without extra setup:

```bash
mkdir mydiscordbot && cd mydiscordbot
kry install discord
```

The command creates a minimal manifest when the directory is new, writes the dependency to `kry.toml`, downloads and verifies the archive, copies it to `vendor/discord`, and records the exact URL and SHA-256 in `kry.lock`. It never overwrites an existing `main.kry`. Set `KRY_REGISTRY` only when using a private mirror or a local registry; the public default is versioned in this repository.

## Language

Kryndel uses braces for blocks, explicit types at function boundaries, and immutable bindings by default.

```kryndel
fn factorial(n: Int) -> Int {
    if n <= 1 {
        return 1
    }
    return n * factorial(n - 1)
}

let answer: Int = factorial(6)
println("factorial = " + str(answer))
```

The type system includes `Int`, `Float`, `Bool`, `String`, `Bytes`, arrays, maps, sets, structs, enums, `Option`, `Result`, channels, synchronized `Shared[T]` cells, `Actor[T]` mailboxes, threads, JSON, WebSockets, regexes, seeded random generators, SQLite databases, raw TCP/UDP sockets, and explicit FFI handles. It also supports deeply immutable compile-time `const`, private struct fields, signature-based overloads, and constrained generic functions (`Copy`, `Numeric`, and `Comparable`). Arithmetic is checked, conversions are explicit, and filesystem, network, process, graphics, camera, and concurrency operations stay behind typed builtins with bounded resources.

For concurrent workers, `TaskGroup` provides structured ownership, cancellation propagation, sibling cleanup, and deterministic group joins. Function overloads use statically checked multiple dispatch over all argument types; ambiguous calls are rejected at compile time.

Direct self-recursive calls in tail position use a runtime trampoline, allowing iterative algorithms written recursively without consuming one call-depth slot per iteration.

Kryndel has three build products: `kry run` interprets portable source; `kry build --format=kexe` writes a portable `KRYNATIVE4` bundle containing validated sources; and native formats emit machine code. `--format=elf` and `--format=exe` use the generated-C ahead-of-time backend and require a C compiler (`cc`/`gcc`/`clang` for Linux, `gcc`/MinGW for Windows); on Windows, a Linux target can use the configured WSL Ubuntu cross-compiler. `--format=elf-direct` emits machine code without C for its documented support subset. `--no-external-toolchain` rejects formats that require a compiler, and successful builds print the selected backend and toolchain dependency. `--format=c` emits the generated C source without compiling it. Host integrations that require Go runtime libraries or platform frameworks (HTTP/TLS, WebSockets, SQLite, FFI, regex handles, camera/display APIs, raw sockets, geolocation, process enumeration, and Windows APIs) are rejected by native backends with an explicit builtin diagnostic instead of being silently degraded; the interpreter remains the complete host-integration backend.

Project dependencies are managed with `kry install PACKAGE` and removed with `kry uninstall PACKAGE [PACKAGE ...]`. Uninstall updates `kry.toml`, prunes `vendor/`, and rewrites `kry.lock` while retaining transitive packages still required by another direct dependency. The global download cache is intentionally preserved.

The public registry includes expanded `async`, `crypto`, and `fs` libraries plus the new `json` library. These expose cancellation-aware sleep and structured groups, SHA-256/HMAC/random bytes, sandboxed text/byte filesystem operations, and safe JSON parse/stringify helpers.

## Cryptography and executable protection

The cryptography library covers hashing (`crypto_sha256`, `crypto_sha512`, `crypto_sha384`, `crypto_sha1`, `crypto_md5`), message authentication (`crypto_hmac_sha256`), authenticated encryption (`crypto_aes_gcm_encrypt`/`crypto_aes_gcm_decrypt`), key derivation (`crypto_pbkdf2_sha256`, `crypto_hkdf_sha256`), constant-time comparison (`crypto_constant_time_equal`), byte-wise `crypto_xor`, and `base64url_encode`/`base64url_decode`. Every primitive is implemented in both the interpreter and the native C backend, verified against published vectors (RFC 5869, RFC 6070, NIST GCM), and held to byte-for-byte parity across the interpreter, the Linux ELF, and the Windows PE.

Built artifacts can be protected. `kry build --encrypt` wraps an artifact in the authenticated `KRYSEAL1` container — AES-256-GCM under a PBKDF2-HMAC-SHA-256 key derived from a passphrase — and the plaintext artifact never touches disk. A wrong passphrase, a truncated file, or any tampering fails the GCM tag check and is reported as a categorized artifact diagnostic. Supply the secret with `--passphrase` or `--passphrase-file`; running a sealed `.kexe` interactively prompts for it. `kry build --obfuscate` masks string literals in the native binary so a `strings` scan does not reveal messages or secrets, without changing observable output.

```sh
kry build app.kry --format=elf --encrypt --passphrase-file secret.txt --obfuscate
kry run app.kexe --passphrase-file secret.txt
```

## Python-style ergonomics

Functions may declare default parameter values, so callers can omit trailing arguments:

```kryndel
fn greet(name: String, greeting: String = "Hello") -> String {
    return greeting + ", " + name
}
println(greet("Ada"))          // Hello, Ada
println(greet("Ada", "Hi"))    // Hi, Ada
```

The checker rejects a required parameter that follows a defaulted one and a default value of the wrong type. New helpers close common Python gaps: `string_slice` and `array_slice_range` provide negative-index slicing with clamped bounds, `string_format` substitutes `{}` placeholders with `{{`/`}}` escapes, and `array_indices`/`array_zip` support enumeration and pairing. All are available in the interpreter and the native backend with verified parity.

Functions are collected before the top-level program runs, so a function can be called before its declaration in the source file. The checker still validates the whole program before execution.

## Runtime polymorphism

Developer applications can create a dispatch slot for a family of interchangeable handlers and change the order at runtime. The API is deliberately narrow: a registered handler must be a top-level `fn(String) -> String`. This keeps the dynamic part easy to inspect and lets the normal runtime limits continue to apply.

```kryndel
fn json_handler(value: String) -> String {
    return "json:" + value
}

fn text_handler(value: String) -> String {
    return "text:" + value
}

let a: Result[Nil, String] = poly_register("render", "json_handler", 10)
let b: Result[Nil, String] = poly_register("render", "text_handler", 5)

match poly_dispatch("render", "hello") {
    ok(value) => { println(value) }
    err(message) => { println(message) }
}

let c: Result[Nil, String] = poly_reorder("render", "text_handler", "json_handler")
```

`poly_register` orders handlers by descending priority. `poly_reorder` moves one registered handler before another, and `poly_dispatch` invokes the first handler in the current order. Registration and dispatch return `Result` values so missing slots, duplicate handlers, incompatible functions, and handler failures are explicit.

Run the complete example with:

```bash
./tools/kry run examples/runtime_polymorphism.kry
```

The same API is available through `std/dispatch`, which keeps application code focused on the handler registry instead of builtin names:

```kryndel
import "std/dispatch"

let registered: Result[Nil, String] = register("render", "text_handler", 10)
println(call_or("render", "hello", "fallback"))
```

See `examples/dispatch_library.kry` for a complete module-based example.

## Modules and packages

Modules are resolved relative to the importing source file. The `.kry` extension is optional, paths cannot escape the module root, and only `pub` declarations are exported. Cycles, duplicate exports, unsafe paths, and invalid public types are rejected.

Projects use `kry.toml` and a lock file. Package archives are reproducible, dependency hashes are verified, and vendored sources go through the same parser and checker as local code.

## Runtime and safety

Kryndel uses a bounded runtime. Source size, artifact size, instruction count, wall-clock time, memory, output, network responses, process output, and collection sizes are limited. Threads and channels are shut down on normal exit and on failure. Process execution does not invoke a shell.

`check` runs before evaluation for `run`, `build`, and formatting. Diagnostics include stable categories and can be emitted as JSON with `--json`.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/kry` | CLI, REPL, formatter, and release entry point. |
| `cmd/kry-installer` | Windows installer with `.kry` and `.kexe` associations. |
| `internal/kry` | Lexer, parser, checker, IR, runtime, modules, artifacts, builtins, and package tooling. |
| `std/` | Typed standard-library wrappers. |
| `examples/` | Small programs covering the language and runtime features. |
| `tools/` | Repository launcher and desktop association installer. |
| `docs/` | Language, architecture, modules, diagnostics, testing, and platform notes. |

## Development

```bash
make test
make test-static
make test-race
make coverage
make check-docs
```

The test suite covers parsing, static errors, runtime behavior, modules, artifacts, concurrency cleanup, sandbox boundaries, diagnostics, native headers, package installation, and formatter stability.

## License

Kryndel is released under the [MIT License](LICENSE).
