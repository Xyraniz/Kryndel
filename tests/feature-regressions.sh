#!/bin/sh
set -eu

BIN=${KRY_BIN:-./build/kry}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/transfer.kry" <<'KRY'
let strings: Channel[String] = thread_channel()
let nested: Channel[Array[Array[Int]]] = thread_channel()
fn worker() -> Nil {
    thread_send(strings, "worker")
    thread_send(nested, [[1, 2], []])
}
let t: Thread[Nil] = thread_spawn("worker")
thread_join(t)
println(thread_receive(strings))
println(thread_receive(nested))
KRY
[ "$("$BIN" run "$TMP/transfer.kry")" = "worker
[[1, 2], []]" ]
cat > "$TMP/transfer-all.kry" <<'KRY'
let text: Channel[String] = thread_channel()
let bytes_value: Channel[Bytes] = thread_channel()
let option_value: Channel[Option[Int]] = thread_channel()
let result_value: Channel[Result[Int, String]] = thread_channel()
fn transfer_worker() -> Nil {
    thread_send(text, bytes_to_string(bytes([65, 0, 66])))
    thread_send(bytes_value, bytes([0, 1, 255]))
    thread_send(option_value, some(7))
    thread_send(result_value, err("bad"))
}
let t: Thread[Nil] = thread_spawn("transfer_worker")
thread_join(t)
println(len(string_to_bytes(thread_receive(text))))
println(len(thread_receive(bytes_value)))
println(thread_receive(option_value))
println(thread_receive(result_value))
KRY
[ "$("$BIN" run "$TMP/transfer-all.kry")" = "3
3
some(7)
err(bad)" ]

cat > "$TMP/numeric.kry" <<'KRY'
println(9223372036854775807 > 9223372036854775806)
println(-9223372036854775807 < -9223372036854775806)
println(9007199254740993 > 9007199254740992)
KRY
[ "$("$BIN" run "$TMP/numeric.kry")" = "true
true
true" ]

cat > "$TMP/bare-array.kry" <<'KRY'
let values: Array = [1, 2]
println(array_concat(values, [3]))
KRY
[ "$("$BIN" run "$TMP/bare-array.kry")" = "[1, 2, 3]" ]

cat > "$TMP/channel.kry" <<'KRY'
let c: Channel[Int] = thread_channel_with_capacity(1)
println(thread_try_send(c, 1))
println(thread_try_send(c, 2))
println(thread_try_receive(c))
KRY
[ "$("$BIN" run "$TMP/channel.kry")" = "ok(nil)
err(full)
ok(1)" ]

cat > "$TMP/module.kry" <<'KRY'
pub fn add(a: Int, b: Int) -> Int { return a + b }
KRY
cat > "$TMP/main.kry" <<'KRY'
import "module"
println(add(2, 3))
KRY
"$BIN" build "$TMP/main.kry" -o "$TMP/main.kexe" >/dev/null
cp "$TMP/main.kexe" "$TMP/first.kexe"
"$BIN" build "$TMP/main.kry" -o "$TMP/main.kexe" >/dev/null
cmp "$TMP/first.kexe" "$TMP/main.kexe"
rm "$TMP/module.kry"
[ "$("$BIN" run "$TMP/main.kexe")" = "5" ]
cat > "$TMP/private.kry" <<'KRY'
fn helper(x: Int) -> Int { return x * 2 }
pub fn api(x: Int) -> Int { return helper(x) }
KRY
cat > "$TMP/private-main.kry" <<'KRY'
import "private"
println(api(4))
KRY
[ "$("$BIN" run "$TMP/private-main.kry")" = "8" ]
printf 'let x: Int = 7\nx\nlet y: Int = x + 5\ny\n:quit\n' | "$BIN" repl > "$TMP/repl.out"
[ "$(cat "$TMP/repl.out")" = "7
12" ]
{ printf 'let long: String = "'; awk 'BEGIN { for (i = 0; i < 5000; i++) printf "a" }'; printf '"\nlen(long)\n:quit\n'; } | "$BIN" repl > "$TMP/long-repl.out"
grep -q '^5000$' "$TMP/long-repl.out"

printf 'let x: Int = true\n' > "$TMP/bad.kry"
if "$BIN" --json check "$TMP/bad.kry" > "$TMP/diagnostic.json" 2>/dev/null; then
    echo 'expected --json check to fail' >&2
    exit 1
fi
grep -q '"code":"KRY003"' "$TMP/diagnostic.json"
grep -q '"category":"type-mismatch"' "$TMP/diagnostic.json"

printf 'println(1)\n' > "$TMP/too-large.kry"
if "$BIN" --max-source 1 check "$TMP/too-large.kry" > "$TMP/limit.out" 2>&1; then
    echo 'expected source size limit to fail' >&2
    exit 1
fi
grep -q 'configured input size limit' "$TMP/limit.out"
cat > "$TMP/restricted.kry" <<'KRY'
let result: Result[String, String] = fs_read_text("../etc/passwd")
match result { ok(value) => { println("unexpected") } err(problem) => { println(problem) } }
KRY
[ "$("$BIN" --restricted "$TMP" run "$TMP/restricted.kry")" = 'path denied by sandbox' ]
ln -s /etc/passwd "$TMP/outside" 2>/dev/null || true
cat > "$TMP/symlink.kry" <<'KRY'
let result: Result[String, String] = fs_read_text("outside")
match result { ok(value) => { println("unexpected") } err(problem) => { println(problem) } }
KRY
if [ -L "$TMP/outside" ]; then
    [ "$("$BIN" --restricted "$TMP" run "$TMP/symlink.kry")" = 'path denied by sandbox' ]
fi

cat > "$TMP/worker-safe.kry" <<'KRY'
let global: Int = 7
fn helper() -> Nil { println(global) }
fn worker() -> Nil { helper() }
let t: Thread[Nil] = thread_spawn("worker")
thread_join(t)
KRY
if "$BIN" check "$TMP/worker-safe.kry" >/dev/null 2>&1; then
    echo 'expected transitive worker global access to fail' >&2
    exit 1
fi

cat > "$TMP/forward.kry" <<'KRY'
fn before() -> Int { return later }
let later: Int = 1
println(before())
KRY
if "$BIN" check "$TMP/forward.kry" >/dev/null 2>&1; then
    echo 'expected forward global reference to fail' >&2
    exit 1
fi

cat > "$TMP/match.kry" <<'KRY'
let flag: Bool = true
match flag { true => { println(1) } false => { println(0) } }
match nil { nil => { println(2) } }
KRY
[ "$("$BIN" run "$TMP/match.kry")" = '1
2' ]

cat > "$TMP/bad-match.kry" <<'KRY'
match true { true => { println(1) } }
KRY
if "$BIN" check "$TMP/bad-match.kry" >/dev/null 2>&1; then
    echo 'expected non-exhaustive Bool match to fail' >&2
    exit 1
fi

cp "$TMP/first.kexe" "$TMP/corrupt.kexe"
printf X | dd of="$TMP/corrupt.kexe" bs=1 seek=48 conv=notrunc status=none
if "$BIN" run "$TMP/corrupt.kexe" >/dev/null 2>&1; then
    echo 'expected corrupted artifact to fail' >&2
    exit 1
fi
cp "$TMP/first.kexe" "$TMP/bad-version.kexe"
printf '\001' | dd of="$TMP/bad-version.kexe" bs=1 seek=11 conv=notrunc status=none
if "$BIN" run "$TMP/bad-version.kexe" >/dev/null 2>&1; then
    echo 'expected artifact version mismatch to fail' >&2
    exit 1
fi

printf 'let x: Int = 1   \nprintln(x)\n' > "$TMP/format.kry"
"$BIN" fmt -w "$TMP/format.kry"
cp "$TMP/format.kry" "$TMP/format.once"
"$BIN" fmt -w "$TMP/format.kry"
cmp "$TMP/format.once" "$TMP/format.kry"

echo 'feature regressions: ok'
