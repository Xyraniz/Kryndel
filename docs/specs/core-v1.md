# Kryndel core contract v1

This document freezes the bootstrap audit surface for the portable value and runtime fixtures. It is an executable compatibility check, not a claim that Kryndel is self-hosted.

## Canonical representation

The command `kry core-report` reads the four repository fixtures that define the current core surface: value/runtime, Bytes, testing assertions, and the host-boundary inventory. Each file must be UTF-8 JSON whose bytes are exactly the repository canonical form: sorted object keys, source-order arrays, two-space indentation, no insignificant trailing data, and one final newline.

The report emits the relative fixture path, byte length, SHA-256 checksum, and version. Fixture paths are sorted lexicographically. The report itself is deterministic and contains no timestamps, absolute paths, locale-dependent values, or network data.

## Required fixture invariants

The value/runtime fixture must declare version `1`, contain valid and invalid cases, and cover the currently frozen bootstrap errors `KRY6102`, `KRY6104`, `KRY6201`, `KRY6202`, `KRY6301`, `KRY6303`, and `KRY6305`. The Bytes fixture must expose construction and operation records; the testing fixture must point at `stdlib/testing/testing.kry`; and the host-boundary fixture must expose intrinsic and layer records.

> `kry core-report` validates the current Python bootstrap reference. It does not move implementation ownership from Python to Kryndel and must not be used as evidence of self-hosting.

## Compatibility rule

A future Kryndel-native reader may reproduce this report byte for byte. Until that differential check exists, the Python command remains the reference implementation and the roadmap must continue to identify the compiler, runtime, filesystem, and tooling layers as host-temporary.
