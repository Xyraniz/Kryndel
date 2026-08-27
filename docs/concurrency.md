# Concurrency guide

Kryndel exposes a deliberately small managed concurrency surface implemented with Go synchronization primitives. Programs create FIFO `Channel[T]` queues with configurable capacity, start a named zero-argument worker as `Thread[T]`, exchange recursively Copy values, and join or close explicitly.

```kryndel
let channel: Channel[Int] = thread_channel()

fn producer() -> Nil {
    thread_send(channel, 42)
}

let worker: Thread[Nil] = thread_spawn("producer")
let value: Int = thread_receive_timeout(channel, 1000)
thread_join(worker)
println(value)
```

The channel state is protected by Go mutex and condition-wait primitives. A sender waits while a bounded queue is full, a receiver waits while it is empty, and `thread_close` broadcasts to both wait sets. `thread_try_send` and `thread_try_receive` return typed `Result` values for `full`, `empty`, or `closed`; timed send, timed receive, and timed join use non-negative millisecond deadlines and return `timeout` without detaching or closing the channel. Receiving from a closed empty channel and sending to a closed channel are deterministic runtime errors for the legacy blocking APIs.

Worker names are string literals resolved by the checker. The target must be a zero-argument function with a declared return type. A worker receives a private scope containing only global `Channel[T]` handles; ordinary global values, mutable application state, channel handles, and thread handles are not transferable. `thread_send` accepts primitives, strings, bytes, enums, and recursively Copy arrays, options, and results.

`thread_join` waits for the managed Go worker and re-raises its first runtime diagnostic. `thread_join_timeout` reports a typed timeout while leaving the worker joinable; `thread_cancel` sets a cooperative context token and wakes channel waits. If main execution fails before an explicit join, the runtime requests cancellation, closes channels, wakes blocked workers, joins every outstanding worker within the shutdown budget, and preserves the earlier diagnostic unless the worker is the first failure. A worker that does not observe cancellation remains joinable and is never detached. This shutdown rule also applies to normal completion and is covered by the Go race-detector target.

The current model intentionally excludes detached threads, nested worker creation, arbitrary shared mutable state, atomics, async tasks, process handles, sockets, and FFI pointers. Those features require separate ownership, cancellation, and portability contracts before they can enter the stable source API.
