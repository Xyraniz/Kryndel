# Bootstrap contract

This document names the compiler stages and the proof required at each
boundary. The numbered `Stage 14` through `Stage 36` notes in
[`README.md`](README.md) describe incremental features; they are not the
bootstrap stage numbers below.

Every accepted stage must record: the source commit, exact rebuild command,
target triple, expected artifact names, SHA-256 hashes, and whether the command
invoked Go, C, an assembler, a linker, or the previous compiler. A stage is not
reproducible until a second clean build produces identical hashes.

| Bootstrap stage | Compiler and job | Expected output | Required independence proof | Current evidence |
| --- | --- | --- | --- | --- |
| 0 | Go reference compiler built from `cmd/kry` and `internal/kry`. | Host `kry` executable. | This is the trusted reference and may use Go. Record its executable hash and Go version. | Implemented; command is `go build -o kry ./cmd/kry`. No bootstrap hash lock is checked in. |
| 1 | Stage 0 checks the Kryndel frontend/backend, emits its KIR, and runs `kir_backend.kry` to produce a Linux amd64 source compiler. | `source-kir-compiler` ELF plus the exact KIR input. | After Stage 0 has produced and launched the ELF, compilation of a fixture must not start Go, C, an assembler, or a linker. | The first half of `TestStage36KryndelSecondCompilerBootstrap` covers this source-compiler subset on Linux amd64. The test skips executable checks on other hosts. |
| 2 | Stage 1 compiles the bundled `elf_backend.kry`, `dynamic_backend.kry`, and `source_kir_compiler.kry` sources into another compiler. | Second-level source compiler ELF and its source bundle. | Stage 1 itself must compile the bundle and fixture without launching Go, C, an assembler, a linker, or its earlier Stage 0 host. | The second half of `TestStage36KryndelSecondCompilerBootstrap` compiles and runs a fixture and checks an invalid-source diagnostic. This proves the tested subset, not complete language support. |
| 3 | Rebuild Stage 2 from the same checked-in Kryndel sources using the declared bootstrap input, then compare the rebuilt compiler. | Rebuilt Stage 2 ELF with byte-identical SHA-256. | The rebuild must succeed without Stage 0 and without Go, C, an assembler, a linker, or an earlier compiler. | Not implemented. There is no checked-in clean rebuild command or expected hash. |
| 4 | Compile the complete published language specification for every declared target. | Release compiler artifacts and the compatibility fixture results for every target. | The compiler must accept the full language and all its targets; every target build must honor the target's declared external-toolchain policy. | Not implemented. Modules, general heap values, linker/object-file support, Windows, and other documented unsupported features remain outstanding. |

## Reproducing the current Stage 1 and Stage 2 probe

On Linux amd64, the automated probe is:

```sh
go test ./internal/kry -run '^TestStage36KryndelSecondCompilerBootstrap$' -count=1 -timeout=20m -v
```

The host Go test emits KIR and runs `kir_backend.kry` to produce Stage 1. Stage
1 then compiles and runs a fixture, compiles the bundled frontend/backend into
Stage 2, and uses Stage 2 to compile and run the fixture again. The test does
not currently write a persistent manifest of the KIR, source bundle, Stage 1,
or Stage 2 hashes. Those hashes must be added before Stage 1 or Stage 2 is
called a reproducible bootstrap stage.

## Release gate

A future bootstrap lock file must bind each row to exact command arguments,
inputs, tool versions, target, output paths, and SHA-256 values. CI must rebuild
each claimed stage in a clean directory and fail if hashes differ or a
forbidden compiler process is launched after the stage boundary. Do not mark
Stage 3 or Stage 4 complete based only on the current Stage 36 probe.
