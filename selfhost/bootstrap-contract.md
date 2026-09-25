# Bootstrap contract

This document names the compiler stages and the proof required at each
boundary. The numbered `Stage 14` through `Stage 37` notes in
[`README.md`](README.md) describe incremental features; they are not the
bootstrap stage numbers below.

Every accepted stage must record: the source revision anchor, exact rebuild
command, target triple, expected artifact names, SHA-256 hashes, and whether the
command invoked Go, C, an assembler, a linker, or the previous compiler. The
revision anchor must be an ancestor of the checkout; the lock's source hashes
bind the exact current input bytes without requiring a commit to contain its
own hash. A stage is not reproducible until a second clean build produces
identical hashes.

| Bootstrap stage | Compiler and job | Expected output | Required independence proof | Current evidence |
| --- | --- | --- | --- | --- |
| 0 | Go reference compiler built from `cmd/kry` and `internal/kry`. | Linux amd64 host `kry` executable. | This is the trusted reference and may use Go. Record its exact build command, executable hash, and Go version. | Pinned in schema 3 of `selfhost/bootstrap.lock.json`: Go 1.27.1, `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o <tmp>/kry ./cmd/kry`, and the executable SHA-256. Stage 36 builds that executable, verifies the hash, then uses its CLI to emit KIR and run `kir_backend.kry` with an explicit 128 MiB JSON limit; ordinary CLI commands retain their 64 MiB default. Input hashes bind the exact source files, while `source_revision` is a history anchor required to precede the checkout. |
| 1 | Stage 0 checks the Kryndel frontend/backend, emits its KIR, and runs `kir_backend.kry` to produce a Linux amd64 source compiler. | `source-kir-compiler` ELF plus the exact KIR input. | After Stage 0 has produced and launched the ELF, compilation of a fixture must not start Go, C, an assembler, or a linker. | The first half of `TestStage36KryndelSecondCompilerBootstrap` covers this source-compiler subset on Linux amd64. The test skips executable checks on other hosts. |
| 2 | Stage 1 uses the source compiler's import resolver on the checked-in `source_kir_compiler.kry` → `dynamic_backend.kry` → `elf_backend.kry`/`pe_backend.kry` module graph. | Second-level source compiler ELF and its source modules. | Stage 1 itself must compile the module graph and fixture without launching Go, C, an assembler, a linker, or its earlier Stage 0 host. | The second half of `TestStage36KryndelSecondCompilerBootstrap` compiles and runs a fixture and checks an invalid-source diagnostic. Stage 37 tests nested imports, direct-import visibility, and module-qualified private function symbols. The CI bootstrap job also exports a PE from this Stage 2 compiler for execution in the Windows job. This proves the tested subset, not complete language support. |
| 3 | Rebuild the second-level source compiler from the same checked-in module graph using that compiler itself. | Rebuilt compiler ELF with byte-identical SHA-256. | The rebuild must succeed without Stage 0 and without Go, C, an assembler, a linker, or an earlier compiler. | `scripts/bootstrap-stage3.sh` runs the clean Stage 1–3 probe. `selfhost/bootstrap.lock.json` pins the Linux amd64 target, Go 1.27.1 host, Stage 0 executable, source revision, KIR, module sources, fixture, and Stage 1/2/3 artifact hashes. The test requires each hash to match and requires Stage 2/3 byte identity. This claim remains limited to the tested compiler subset. |
| 4 | Compile the complete published language specification for every declared target. | Release compiler artifacts and the compatibility fixture results for every target. | The compiler must accept the full language and all its targets; every target build must honor the target's declared external-toolchain policy. | Not implemented. General heap values, linker/object-file support, full Windows language parity, and other documented unsupported features remain outstanding. |

## Reproducing the current Stage 1–3 probe

On Linux amd64 with the locked Go version, run from the repository root:

```bash
./scripts/bootstrap-stage3.sh
```

The test builds the locked Go 1.27.1 Stage 0 executable, verifies its hash, and
uses its CLI to emit KIR and run `kir_backend.kry` with the bootstrap-specific
128 MiB JSON limit to produce Stage 1 in a
fresh temporary directory. Stage 1 compiles and runs a fixture, compiles the
checked-in source compiler module graph into Stage 2, and Stage 2 compiles and
runs the same fixture. Stage 2 then rebuilds itself from the same module graph
to produce Stage 3.
The test checks every current input and compiler artifact against the
checked-in lock; it also requires the source revision anchor to precede the
checkout, then compares the complete Stage 2 and Stage 3 ELF byte streams. Stage 1
produces Stage 2 and Stage 2 produces Stage 3 directly. Go is only the Stage 0
host used to create and verify the earlier stages. The lock is specific to this
Linux amd64 bootstrap slice, not a claim of full-language or cross-target
parity.

## Release gate

A future bootstrap lock file must bind each row to exact command arguments,
inputs, tool versions, target, output paths, and SHA-256 values. CI must rebuild
each claimed stage in a clean directory and fail if hashes differ or a
forbidden compiler process is launched after the stage boundary. Do not mark
Stage 3 or Stage 4 complete based only on the current Stage 36 probe.
