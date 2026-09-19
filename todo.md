# Kryndel — Native backend & missing features

## Goal
Make `kry build --format=exe/elf` produce **real, functional, runnable** executables
(PE for Windows, ELF for Linux) that execute the full checked language, not just
constant `print`/`println`. Fix known Python-style limitations. Test everything.

## Done
- [x] C-based AOT backend (codegen + embedded C runtime)
- [x] Functions, recursion, if/while/for, match, structs, enums, arrays, maps,
      sets, strings, bytes, Option/Result, `?`, defer, checked math
- [x] Pure builtins + fs/env/json/crypto/process in native runtime
- [x] Reject unsupported builtins with clear diagnostics
- [x] CLI `--format=c`, `--target`, executable bit on native output
- [x] ELF + PE parity vs interpreter (hello, fib, bytes, control_flow,
      typed_data, collections, module_demo, native_features)
- [x] Extended string/array/collection/math builtins (30) wired into codegen
- [x] Parity test for new builtins (ELF + PE via wine)

## Next
- [ ] Add regression test for the new builtins to the Go test suite
- [ ] Document the new builtins in docs/native.md
- [ ] Commit in small, time-separated, human-looking commits
