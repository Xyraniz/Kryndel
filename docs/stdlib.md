# Standard library and builtins

Kryndel keeps its standard library small and explicit. The current release provides one Go builtin registry shared by the checker, runtime, CLI help, tests, and documentation. Builtins are ordinary calls and are checked before execution.

| Builtin | Registry signature | Behavior and failure rule |
| --- | --- | --- |
| `print` | `print(value: Display) -> Nil` | Writes one value without a newline; a stream failure is reported. |
| `println` | `println(value: Display) -> Nil` | Writes one value and a newline; a stream failure is reported. |
| `len` | `len(value: String\|Array[T]\|Bytes\|Map[K,V]\|Set[T]) -> Int` | Counts UTF-8 code points, elements, octets, map entries, or unique set elements. |
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
| `base64url_encode` | `base64url_encode(value: Bytes) -> String` | URL-safe base64 without padding, for tokens and filenames. |
| `base64url_decode` | `base64url_decode(value: String) -> Result[Bytes, String]` | Decode URL-safe base64; malformed input is rejected. |
| `crypto_sha512`, `crypto_sha384`, `crypto_sha1`, `crypto_md5` | `crypto_sha512(data: Bytes) -> Bytes` (and siblings) | Additional hash digests. SHA-1 and MD5 are provided for legacy compatibility only and must not be used for new security work. |
| `crypto_aes_gcm_encrypt` | `crypto_aes_gcm_encrypt(key: Bytes, nonce: Bytes, plaintext: Bytes) -> Result[Bytes, String]` | Authenticated AES-256-GCM encryption; the key must be 32 bytes and the nonce 12 bytes. The result is ciphertext followed by the 16-byte tag. |
| `crypto_aes_gcm_decrypt` | `crypto_aes_gcm_decrypt(key: Bytes, nonce: Bytes, ciphertext: Bytes) -> Result[Bytes, String]` | Authenticated decryption; a wrong key, wrong nonce, or any tampering fails the tag check and returns `err`. |
| `crypto_pbkdf2_sha256` | `crypto_pbkdf2_sha256(password: Bytes, salt: Bytes, iterations: Int, length: Int) -> Result[Bytes, String]` | Derive a key with PBKDF2-HMAC-SHA-256; iterations and length must be positive. |
| `crypto_hkdf_sha256` | `crypto_hkdf_sha256(ikm: Bytes, salt: Bytes, info: Bytes, length: Int) -> Result[Bytes, String]` | Expand key material with HKDF-SHA-256 (RFC 5869). |
| `crypto_constant_time_equal` | `crypto_constant_time_equal(left: Bytes, right: Bytes) -> Bool` | Compare two byte sequences without leaking timing; use it for MAC and token checks. |
| `crypto_xor` | `crypto_xor(left: Bytes, right: Bytes) -> Result[Bytes, String]` | Byte-wise XOR of two equal-length sequences; mismatched lengths are rejected. |
| `string_slice` | `string_slice(value: String, start: Int, end: Int) -> Result[String, String]` | Python-style code-point slice with negative indices and clamped bounds. |
| `array_slice_range` | `array_slice_range(value: Array[T], start: Int, end: Int) -> Array[T]` | Python-style array slice with negative indices and clamped bounds. |
| `string_format` | `string_format(template: String, args: Array[String]) -> String` | Substitute `{}` placeholders in order; `{{` and `}}` are literal braces and a missing argument leaves the placeholder visible. |
| `array_indices` | `array_indices(value: Array[T]) -> Array[Int]` | Return `0..len-1` for enumeration alongside `array_get`. |
| `array_zip` | `array_zip(left: Array[T], right: Array[T]) -> Array[Array[T]]` | Pair two arrays element-wise up to the shorter length. |
| `array_pop`, `array_get` | `Array[T] -> Option[T]` | Return `none` for out-of-range or empty access. |
| `array_concat`, `array_slice`, `array_reverse`, `array_contains`, `array_join` | Collection operations. | Require homogeneous element types and checked indexes. |
| `map_get`, `map_insert`, `map_keys` | `Map[K,V]` operations. | Maps preserve deterministic insertion order; insertion returns a new map and missing reads return `none`. |
| `set_contains`, `set_insert`, `set_len` | `Set[T]` operations. | Sets deduplicate by recursive value equality and return new values on insertion. |
| `json_parse` | `json_parse(value: String) -> Result[Json,String]` | Validates and canonicalizes JSON under the source-size limit. |
| `json_stringify` | `json_stringify(value: Json) -> String` | Returns the canonical validated JSON text. |
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
| `http_get`, `http_request` | Bounded HTTP/TLS operations. | Use a request timeout, cap response bytes, reject invalid UTF-8, and return non-2xx status as `Err`. |
| `http_request_auth` | Bearer-authenticated HTTP request. | Sends the token only in the Authorization header and never includes it in diagnostics. |
| `websocket_connect`, `websocket_send`, `websocket_receive`, `websocket_close` | RFC 6455 WebSocket lifecycle. | Require `ws`/`wss`, validate the handshake, mask client frames, bound payloads, and expose `WebSocket` as non-Copy. |
| `process_run` | `process_run(program: String, args: Array[String]) -> Result[Int,String]` | Starts a program directly without a shell and bounds combined output. |
| `win_registry_get`, `win_service_query`, `win_eventlog_write`, `win_raw_input`, `win_device_io_control` | Windows-only host operations. | Return an explicit unsupported-target failure on non-Windows; native adapters use Win32-compatible entry points. |
| `uuid_v4`, `uuid_v5`, `uuid_is_valid` | UUID generation and validation. | v4 uses the OS CSPRNG; v5 follows the RFC name-based SHA-1 format; malformed namespaces return `err`. |
| `platform_os`, `platform_arch`, `platform_runtime`, `platform_hostname` | Host identity and runtime metadata. | The OS, architecture, and runtime are read locally; hostname access is fallible and returns `Result`. |
| `dotenv_load` | `dotenv_load(path: String) -> Result[Map[String,String],String]` | Reads through the Kryndel sandbox, supports `export`, quoted values, comments, and `${NAME}` expansion, and never mutates the process environment. |
| `datetime_now`, `datetime_unix_ms` | Current UTC timestamp helpers. | `datetime_now` returns RFC 3339 with nanoseconds; Unix milliseconds are signed `Int` values. |
| `datetime_format`, `datetime_parse` | `Int` Unix milliseconds and Go time layouts. | Formatting and parsing are explicit and return `err` for invalid layouts or timestamps. |
| `random_new`, `random_int`, `random_float`, `random_choice` | Seeded `Random` handle operations. | Each handle is isolated and mutex-protected; integer ranges are inclusive, and empty choices return `none`. |
| `regex_compile`, `regex_is_match`, `regex_find`, `regex_find_all`, `regex_replace_all`, `regex_split` | RE2 regular-expression operations over UTF-8 strings. | Compilation errors are returned as `err`; the RE2 engine guarantees bounded, non-backtracking matching. |
| `sqlite_open`, `sqlite_exec`, `sqlite_query`, `sqlite_close` | Embedded SQLite database handles. | The pure-Go SQLite engine runs inside the interpreter; paths are sandboxed, `:memory:` is supported, query rows are bounded, and cells are returned as text. |
| `tcp_connect`, `tcp_send`, `tcp_receive`, `tcp_close` | Raw TCP client operations. | Connections use the runtime wall-clock deadline, reads are bounded, writes are completed or returned as errors, and handles must be closed. |
| `tcp_listen`, `tcp_accept`, `tcp_local_port`, `tcp_listener_close` | Raw TCP server operations. | Binding port `0` selects an ephemeral port; `tcp_local_port` makes it discoverable without exposing a native pointer. |
| `udp_bind`, `udp_send`, `udp_receive`, `udp_receive_from`, `udp_close` | Raw UDP datagram operations. | Datagram size and port values are checked; `udp_receive_from` returns canonical JSON with sender address, port, and base64 payload. |
| `ffi_library_open`, `ffi_symbol`, `ffi_library_close` | Dynamic-library and symbol handles. | Uses `purego` without CGO on Linux, macOS, FreeBSD, and Windows; loading and symbol errors return `err`, and libraries are explicitly unloadable. |
| `ffi_buffer_new`, `ffi_buffer_address`, `ffi_buffer_read`, `ffi_buffer_close` | NUL-terminated native-call buffers. | The returned address is an opaque token valid only as a `p` argument to `ffi_call`; Kryndel never exposes a raw pointer value to user code. |
| `ffi_call` | `ffi_call(symbol: FFISymbol, signature: String, args: Array[Int]) -> Result[Int,String]` | Supports integer/pointer C ABI signatures such as `i()`, `i(i)`, and `i(p)` with up to eight arguments. `f`/struct/variadic signatures are rejected; the caller must declare the real native signature. |

String-to-number conversion rejects whitespace-dependent partial parses and inputs such as `"12xyz"`. Float values and results must be finite. Integer arithmetic and `abs(Int minimum)` are checked. `Bytes` conversion never applies an implicit text encoding to arbitrary values.

The Go registry is the authoritative list. The CLI help renders its signatures and descriptions. Adding a builtin requires a registry entry, checker behavior, runtime behavior, documentation, and positive and negative tests. Package authors should use the typed wrappers in `std/env.kry`, `std/json.kry`, and `std/http.kry`; the official `packages/discord` package uses the same bounded primitives and never prints bot tokens.

## Cryptography

The crypto builtins are implemented twice — once in Go for the interpreter and once in the C runtime for the native backend — and the two implementations are held to byte-for-byte parity by the test suite. The primitives are checked against published vectors (RFC 5869 for HKDF, RFC 6070 for PBKDF2, and NIST GCM vectors for AES-256-GCM) so a regression is caught immediately.

The recommended building blocks are `crypto_sha256`/`crypto_sha512` for hashing, `crypto_hmac_sha256` for message authentication, `crypto_aes_gcm_encrypt`/`crypto_aes_gcm_decrypt` for authenticated encryption, `crypto_pbkdf2_sha256` for password-based key derivation, `crypto_hkdf_sha256` for key expansion, and `crypto_constant_time_equal` for comparing secrets. `crypto_random_bytes` draws from the operating system CSPRNG. SHA-1 and MD5 exist only so legacy formats can be read; they are not safe for new designs.

```kryndel
import "crypto"

let key: Bytes = crypto_random_bytes(32)?
let nonce: Bytes = crypto_random_bytes(12)?
let sealed: Bytes = crypto_aes_gcm_encrypt(key, nonce, string_to_bytes("secret"))?
let opened: Bytes = crypto_aes_gcm_decrypt(key, nonce, sealed)?
println(bytes_to_string(opened))
```

## Executable and artifact protection

`kry build --encrypt` wraps a built artifact in an authenticated, passphrase-protected container (the `KRYSEAL1` format). The inner artifact is encrypted with AES-256-GCM under a key derived from the passphrase with PBKDF2-HMAC-SHA-256, and the plaintext artifact never touches disk. Because GCM is authenticated, a wrong passphrase or any tampering is detected before a single byte of the inner artifact is trusted. Supply the passphrase with `--passphrase` or `--passphrase-file`; when a sealed `.kexe` is run interactively the CLI prompts for it.

`kry build --obfuscate` masks string literals in the generated native binary so secrets and messages are not visible to a simple `strings` scan. Obfuscation is a defence-in-depth measure, not a substitute for encryption: it raises the cost of casual inspection but does not make a binary tamper-proof.

```sh
kry build app.kry --format=elf --encrypt --passphrase-file secret.txt --obfuscate
kry run app.kexe --passphrase-file secret.txt
```

## Source wrappers

The `std/dispatch.kry` module provides named wrappers around the runtime polymorphism builtins. `register` adds a `String -> String` handler to a slot, `reorder` changes the order of two registered handlers, `call` returns the full `Result`, and `call_or` supplies a fallback string for an empty or failing slot. The module keeps application code independent from builtin names while preserving the same strict handler signature and runtime limits.

```kryndel
import "std/dispatch"

let registered: Result[Nil, String] = register("render", "text_handler", 10)
let output: String = call_or("render", "hello", "fallback")
println(output)
```
