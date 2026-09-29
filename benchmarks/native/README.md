# Native execution benchmarks

Run the same process-level corpus against the interpreter, available C AOT and direct backends, and Rust:

```powershell
go test ./internal/kry -run '^$' -bench '^BenchmarkNativeBackendCorpus$' -benchmem -count=5 -v
```

`scalar_loop` measures a loop with a non-inlined function call. `array_sum` repeatedly reduces a 16-element `Array[Int]`. The harness builds artifacts before timing, runs each program once to verify identical output, then times process startup and execution. The direct backends are included only when the current host can execute them and the program fits their accepted subset. Rust requires `rustc`; Go, Rust, target, and CPU are printed with the results.

These results measure command-level behavior. `BenchmarkInterpreterPhases` separately measures interpreter lexing, parsing, checking, and runtime work; do not compare those phase timings directly with this process-level benchmark.
