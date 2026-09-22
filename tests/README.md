# Kryndel test suite

Functional and security coverage lives in `internal/kry/toolchain_test.go`, a native Go suite covering the lexer, parser, checker, control flow, recursive `Copy`, modules, sandbox, artifacts, formatter, REPL/runtime, and limits. This avoids relying on Bash, GNU coreutils, Python, C, or an external interpreter to validate the project.

Verification commands are:

```text
make test          # build, examples, and Go tests
make test-static   # gofmt, go vet, and clean tests
make test-race     # Go race detector
make fuzz-smoke    # bounded deterministic corpus
make coverage      # coverage profile
make benchmark     # Go benchmarks
make verify-fast   # fast complete verification matrix
make verify-full   # fast checks plus native, parity, race, coverage, and benchmarks
make verify        # alias for verify-full
make release       # cross-compiled binaries and SHA256SUMS
```

Process tests remain deliberately small and run from the CLI only when they exercise a public boundary that cannot be observed more precisely inside the package. Errors must be deterministic, bounded, and accompanied by a negative assertion; a timeout is a failure, not an expected result.
