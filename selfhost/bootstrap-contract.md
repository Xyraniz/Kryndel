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
| 0 | Go reference compiler built from `cmd/kry` and `internal/kry`. | Linux amd64 host `kry` executable. | This is the trusted reference used only to regenerate the checked-in Stage 1 seed. Record its exact build command, executable hash, and Go version. | Pinned in schema 3 of `selfhost/bootstrap.lock.json`: Go 1.27.1, `CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o <tmp>/kry ./cmd/kry`, and the executable SHA-256. The provenance test builds that executable, verifies its hash, emits KIR, and runs `kir_backend.kry` with an explicit 128 MiB JSON limit; ordinary CLI commands retain their 64 MiB default. Input hashes bind the exact source files, while `source_revision` is a history anchor required to precede the checkout. |
| 1 | Checked-in Stage 1 compiler, reproducible from Stage 0. | `source-kir-compiler-stage1.elf` Linux amd64 executable plus the exact KIR input used to create it. | The normal Stage 1–3 bootstrap starts from this Kryndel compiler seed and does not require Go. Regenerating and validating the seed may use Stage 0. | `selfhost/bootstrap/linux-amd64/source-kir-compiler-stage1.elf` is hash-locked in `bootstrap.lock.json`. `TestStage36KryndelSecondCompilerBootstrap` rebuilds Stage 1 from Stage 0; the standalone script executes the checked-in seed directly. |
| 2 | Stage 1 uses the source compiler's import resolver on the checked-in `source_kir_compiler.kry` → `dynamic_backend.kry` → `elf_backend.kry`/`pe_backend.kry` module graph. | Second-level source compiler ELF and its source modules. | Stage 1 itself must compile the module graph and fixture without launching Go, C, an assembler, a linker, or its earlier Stage 0 host. | The second half of `TestStage36KryndelSecondCompilerBootstrap` compiles and runs a fixture and checks an invalid-source diagnostic. Stage 37 tests nested imports, direct-import visibility, and module-qualified private function symbols. The CI bootstrap job also exports a PE from this Stage 2 compiler for execution in the Windows job. This proves the tested subset, not complete language support. |
| 3 | Rebuild the second-level source compiler from the same checked-in module graph using that compiler itself. | Rebuilt compiler ELF with byte-identical SHA-256. | The rebuild must succeed without Stage 0 and without Go, C, an assembler, a linker, or an earlier compiler. | `scripts/bootstrap-stage3.sh` starts from the locked Stage 1 ELF seed, builds Stage 2 and Stage 3, runs fixtures, verifies locked hashes, and requires Stage 2/3 byte identity. The separate Stage 0 provenance test proves the checked-in seed is reproducible. This claim remains limited to the tested compiler subset. |
| 4 | Compile the complete published language specification for every declared target. | Release compiler artifacts and the compatibility fixture results for every target. | The compiler must accept the full language and all its targets; every target build must honor the target's declared external-toolchain policy. | Not implemented. General heap values, linker/object-file support, full Windows language parity, and other documented unsupported features remain outstanding. |

## Reproducing the current Stage 1–3 probe

On Linux amd64 with Bash and coreutils, run from the repository root:

```bash
./scripts/bootstrap-stage3.sh
```

The standalone script verifies the checked-in Stage 1 ELF seed, runs a fixture
with it, builds Stage 2 from the checked-in Kryndel module graph, and has Stage 2
rebuild itself as Stage 3. It verifies the seed, source, and compiler hashes and
requires byte-identical Stage 2 and Stage 3 ELFs. This path does not invoke Go,
C, an assembler, or a linker.

To regenerate the seed and prove it matches the checked-in executable, use the
locked Go 1.27.1 reference build:

```bash
go test ./internal/kry -run '^TestStage36KryndelSecondCompilerBootstrap$' -count=1 -timeout=20m -v
```

That provenance check creates Stage 0, emits KIR, generates Stage 1, and then
continues through Stage 2 and Stage 3. The normal bootstrap does not need this
Go-based seed regeneration. Both paths cover the bounded Linux amd64 compiler
subset; neither claims full-language or cross-target parity.

## Release gate

A future bootstrap lock file must bind each row to exact command arguments,
inputs, tool versions, target, output paths, and SHA-256 values. CI must rebuild
each claimed stage in a clean directory and fail if hashes differ or a
forbidden compiler process is launched after the stage boundary. Do not mark
Stage 3 or Stage 4 complete based only on the current Stage 36 probe.
