# Standard library and builtins

Kryndel keeps its standard library small and explicit. The current release provides one Go builtin registry shared by the checker, runtime, CLI help, tests, and documentation. Builtins are ordinary calls and are checked before execution.

| Builtin | Registry signature | Behavior and failure rule |
| --- | --- | --- |
| `print` | `print(value: Display) -> Nil` | Writes one value without a newline; a stream failure is reported. |
| `println` | `println(value: Display) -> Nil` | Writes one value and a newline; a stream failure is reported. |
| `len` | `len(value: String\|Array[T]\|Bytes) -> Int` | Counts UTF-8 code points, elements, or octets; unsupported values are rejected. |
| `bytes` | `bytes(value: Array[Int]) -> Bytes` | Converts integers in `0..255`; out-of-range elements are rejected. |
| `string_to_bytes` | `string_to_bytes(value: String) -> Bytes` | Preserves valid UTF-8 bytes; invalid UTF-8 is rejected. |
| `bytes_to_string` | `bytes_to_string(value: Bytes) -> String` | Decodes only valid UTF-8; malformed bytes are rejected. |
| `array_push` | `array_push(array: Array[T], value: T) -> Array[T]` | Returns a new array; mismatched elements and allocation overflow are rejected. |
| `int` | `int(value: Int\|Float\|Bool\|String) -> Int` | Performs explicit conversion; incomplete, malformed, non-finite, and out-of-range values are rejected. |
| `float` | `float(value: Int\|Float\|String) -> Float` | Performs explicit finite conversion; incomplete, malformed, and non-finite values are rejected. |
| `str` | `str(value: Display) -> String` | Produces deterministic display text. |
| `bool` | `bool(value: Display) -> Bool` | Performs explicit conversion using documented scalar and collection rules. |
| `assert` | `assert(condition: Bool) -> Nil` | A false condition raises a runtime assertion error. |
| `assert_eq` | `assert_eq(left: T, right: T) -> Nil` | Unequal values raise a runtime assertion error. |
| `abs` | `abs(value: Int\|Float) -> Int\|Float` | Computes checked absolute value; Int minimum and non-finite Float values are rejected. |
| `sqrt` | `sqrt(value: Int\|Float) -> Float` | Computes a finite square root; negative and non-finite values are rejected. |
| `min`, `max` | `min/max(left: Int\|Float, right: Int\|Float) -> Int\|Float` | Require matching numeric operands. |
| `floor`, `ceil`, `round` | `floor/ceil/round(value: Float) -> Int` | Convert finite rounded results only when representable as Int. |
| `pow`, `log`, `sin`, `cos` | `pow/log/sin/cos(value: Float, ...) -> Float` | Require finite Float results. |
| `is_nan`, `is_finite` | `is_nan/is_finite(value: Float) -> Bool` | Inspect floating-point classification. |
| `some` | `some(value: T) -> Option[T]` | Constructs a present option. |
| `none` | `none() -> Option[T]` | Constructs an empty option and requires an `Option[T]` context. |
| `ok` | `ok(value: T) -> Result[T, E]` | Constructs a success result; the counterpart error type comes from context or `Nil`. |
| `err` | `err(value: E) -> Result[T, E]` | Constructs a failure result; the counterpart success type comes from context or `Nil`. |
| `is_some`, `is_none` | `is_some/is_none(value: Option[T]) -> Bool` | Inspect an option without unwrapping it. |
| `is_ok`, `is_err` | `is_ok/is_err(value: Result[T, E]) -> Bool` | Inspect result status without consuming its payload. |
| `unwrap_or` | `unwrap_or(option: Option[T], fallback: T) -> T` | Returns the value or a type-matching fallback. |
| `substring` | `substring(text: String, start: Int, length: Int) -> Result[String, String]` | Uses UTF-8 code-point indexes and reports range errors. |
| `contains`, `starts_with`, `ends_with`, `trim`, `split`, `replace`, `codepoints` | Text operations with explicit String signatures. | Preserve UTF-8 validation and deterministic output. |
| `byte_at` | `byte_at(text: String, index: Int) -> Result[Int, String]` | Reads a byte with a checked index. |
| `hex_encode`, `base64_encode` | `Bytes -> String` | Encode raw bytes deterministically. |
| `hex_decode`, `base64_decode` | `String -> Result[Bytes, String]` | Reject malformed encodings. |
| `array_pop`, `array_get` | `Array[T] -> Option[T]` | Return `none` for out-of-range or empty access. |
| `array_concat`, `array_slice`, `array_reverse`, `array_contains`, `array_join` | Collection operations. | Require homogeneous element types and checked indexes. |
| `thread_channel` | `thread_channel() -> Channel[T]` | Creates an unbounded queue by default; a `Channel[T]` context is required. |
| `thread_channel_with_capacity` | `thread_channel_with_capacity(capacity: Int) -> Channel[T]` | Creates a bounded queue with positive capacity. |
| `thread_spawn` | `thread_spawn(name: String) -> Thread[T]` | Starts a zero-argument named worker; unknown workers and startup failures are rejected. |
| `thread_send` | `thread_send(channel: Channel[T], value: T) -> Nil` | Sends a Copy value while synchronized; closed channels and non-Copy values are rejected. |
| `thread_try_send` | `thread_try_send(channel: Channel[T], value: T) -> Result[Nil, String]` | Returns `full` or `closed` instead of blocking. |
| `thread_send_timeout` | `thread_send_timeout(channel: Channel[T], value: T, milliseconds: Int) -> Result[Nil, String]` | Uses a bounded send deadline. |
| `thread_receive` | `thread_receive(channel: Channel[T]) -> T` | Receives a value while synchronized; a closed empty channel is rejected. |
| `thread_receive_timeout` | `thread_receive_timeout(channel: Channel[T], milliseconds: Int) -> T` | Receives with a bounded deadline; negative durations and elapsed deadlines are rejected. |
| `thread_try_receive` | `thread_try_receive(channel: Channel[T]) -> Result[T, String]` | Returns `empty` or `closed` instead of blocking. |
| `thread_join` | `thread_join(thread: Thread[T]) -> T` | Joins a worker, returns its result, and propagates worker failures. |
| `thread_join_timeout` | `thread_join_timeout(thread: Thread[T], milliseconds: Int) -> Result[T, String]` | Returns the worker result or `timeout` without detaching it. |
| `thread_cancel` | `thread_cancel(thread: Thread[T]) -> Nil` | Requests cooperative cancellation and wakes channel waits. |
| `thread_close` | `thread_close(channel: Channel[T]) -> Nil` | Closes a channel and wakes blocked operations; repeated close is harmless. |
| `fs_read_bytes`, `fs_write_bytes`, `fs_exists` | Typed byte-file and existence operations. | Reject NUL paths and return I/O failures as `Result`. |

String-to-number conversion rejects whitespace-dependent partial parses and inputs such as `"12xyz"`. Float values and results must be finite. Integer arithmetic and `abs(Int minimum)` are checked. `Bytes` conversion never applies an implicit text encoding to arbitrary values.

The Go registry is the authoritative list. The CLI help renders its signatures and descriptions. Adding a builtin requires a registry entry, checker behavior, runtime behavior, documentation, and positive and negative tests. Directory, process, network, terminal, and FFI modules are outside the stable source API and are described as future design work in `docs/design.md`.
