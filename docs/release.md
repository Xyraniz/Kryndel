# Build and release guide

Kryndel can be built from source with Go 1.27.1 or newer and the modules listed in `go.mod`. The supported release targets are Linux amd64/arm64, macOS amd64/arm64, and Windows amd64; distributed binaries are built with `CGO_ENABLED=0` and include their Go dependencies, so they require no external runtime:

```bash
make
./tools/kry version
```

The launcher does not detect or invoke C, Python, Rust, or Node.js. When `build/kry` is missing it exits with `69` and instructs the developer to run `make build`. Numeric operations, channels, workers, filesystem checks, and serialization use Go code; host integrations also use the Go modules declared in `go.mod`.

A reproducible verification build uses the following targets:

```bash
make clean
make test
make test-static
make test-race
make coverage
make check-docs
make release
```

Artifacts are created only by `kry build`. The `KRYNATIVE5` container records the format version, compiler version, language version, target, package visibility scopes, a deterministic source-entry order, exact source bytes, and SHA-256 hashes. The decoder also accepts KRYNATIVE4 and KRYNATIVE3 as language version 1.0.0 where applicable. Imported source modules are embedded, while timestamps, host paths, random identifiers, and generated binaries are excluded from the content. Writes are atomic and the decoder validates metadata, lengths, paths, hashes, duplicates, and trailing bytes before execution. Release automation should build on each supported platform and publish only binaries that actually completed that platform's build.

The repository CI uses `go vet`, race detection, fuzz smoke tests, coverage, documentation audits, cross-compilation, reproducible artifact comparisons, and a clean-environment execution job. Go's race detector is the applicable concurrency sanitizer for this implementation; no C/ASan/UBSan job is required because production C is absent. For each release target, the GitHub release workflow creates an SPDX 2.3 SBOM from the built release executables with Syft, validates that it contains Go module records, records target/compiler identity, and writes `SHA256SUMS` after all assets and metadata exist. The checksum file covers each `kry-*` binary, installer when present, SBOM, and provenance file; it does not hash itself.

## Windows installer and Discord scope

Releases also publish `kry-installer-windows-amd64.exe`. It downloads the Kryndel CLI from the GitHub release, installs it under the current user's configuration directory, and persists that directory in the user's `PATH` without requiring administrator rights. The Discord package supports bot accounts and moderation endpoints such as ban, unban, and kick. Selfbots are intentionally unsupported because automating normal user accounts violates Discord's platform rules.
