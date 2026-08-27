#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bin=${KRY_BIN:-"$root/tools/kry"}
run() { "$bin" "$@"; }

cat > "$work/core.kry" <<'KRY'
fn factorial(n: Int) -> Int {
    if n <= 1 {
        return 1
    }
    return n * factorial(n - 1)
}

let mut total: Int = 0
let mut i: Int = 0
while i < 5 {
    total = total + i
    i = i + 1
}

assert_eq(factorial(6), 720)
assert(total == 10)
println(factorial(6))
println(total)
println(array_push([1, 2], 3))
println(len(string_to_bytes("A😀")))
println(len("A😀"))
println("A😀"[1])
println(bytes_to_string(bytes([75, 114, 121])))
KRY
expected='720
10
[1, 2, 3]
5
2
😀
Kry'
test "$(PATH="/usr/bin:/bin" run version)" = 'Kryndel 1.2.0'
test "$(PATH="/usr/bin:/bin" run run "$work/core.kry")" = "$expected"
test -z "$(PATH="/usr/bin:/bin" run check "$work/core.kry")"
PATH="/usr/bin:/bin" run build "$work/core.kry" -o "$work/core.kexe"
PATH="/usr/bin:/bin" run build "$work/core.kry" -o "$work/core-second.kexe"
cmp -s "$work/core.kexe" "$work/core-second.kexe"
test "$(PATH="/usr/bin:/bin" run run "$work/core.kexe")" = "$expected"
cp "$work/core.kexe" "$work/core-trailing.kexe"
printf x >> "$work/core-trailing.kexe"
if PATH="/usr/bin:/bin" run run "$work/core-trailing.kexe" >/dev/null 2>"$work/artifact-error.txt"; then exit 1; fi
grep -q 'malformed native artifact' "$work/artifact-error.txt"
dd if="$work/core.kexe" of="$work/truncated.kexe" bs=1 count=10 status=none
if run run "$work/truncated.kexe" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q 'malformed native artifact: truncated header' "$work/error"
cp "$work/core.kexe" "$work/bad-length.kexe"
printf '\000' | dd of="$work/bad-length.kexe" bs=1 seek=41 conv=notrunc status=none
if run run "$work/bad-length.kexe" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q 'length' "$work/error"

cat > "$work/immutable.kry" <<'KRY'
let fixed: Int = 1
fixed = 2
KRY
if run check "$work/immutable.kry" >"$work/out" 2>"$work/error"; then exit 1; fi
grep -q "immutable binding 'fixed'" "$work/error"
grep -q '^error\[type-mismatch\]:' "$work/error"
grep -q '\^' "$work/error"

cat > "$work/bad-condition.kry" <<'KRY'
if 1 { println("bad") }
KRY
if run check "$work/bad-condition.kry" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q 'condition must be Bool' "$work/error"

cat > "$work/unknown-type.kry" <<'KRY'
let value: DefinitelyNotARealType = 7
KRY
if run check "$work/unknown-type.kry" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q 'DefinitelyNotARealType' "$work/error"

cat > "$work/overflow.kry" <<'KRY'
println(9223372036854775807 + 1)
KRY
if run run "$work/overflow.kry" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q 'overflow' "$work/error"

cat > "$work/unknown-function.kry" <<'KRY'
missing(1)
KRY
if run check "$work/unknown-function.kry" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q "unknown function 'missing'" "$work/error"

cat > "$work/short-circuit.kry" <<'KRY'
assert(false && (1 / 0 == 0))
KRY
if run run "$work/short-circuit.kry" >/dev/null 2>"$work/error"; then exit 1; fi
grep -q 'assertion failed' "$work/error"
! grep -q 'division by zero' "$work/error"

cat > "$work/lib.kry" <<'KRY'
pub fn add(a: Int, b: Int) -> Int {
    return a + b
}
KRY
cat > "$work/module-main.kry" <<'KRY'
import "lib"
println(add(2, 3))
KRY
test "$(run run "$work/module-main.kry")" = 5

cat > "$work/enum.kry" <<'KRY'
enum Color { Red, Blue }
let color: Color = Color::Red
match color {
    Color::Red => { println("red") }
    Color::Blue => { println("blue") }
}
KRY
test "$(run run "$work/enum.kry")" = red

cat > "$work/builtins.kry" <<'KRY'
print("prefix")
println(float(3))
println(int(3.9))
println(str(7))
println(bool(1))
println(abs(-7))
println(sqrt(9))
println(some(1))
let missing: Option[Int] = none()
println(missing)
println(ok(1))
println(err("bad"))
KRY
test "$(run run "$work/builtins.kry")" = 'prefix3
3
7
true
7
3
some(1)
none
ok(1)
err(bad)'

cat > "$work/system.kry" <<'KRY'
let content: Result[String, String] = fs_read_text("examples/hello.kry")
match content {
    ok(text) => { assert(len(text) > 0) }
    err(problem) => { assert(false) }
}
let written: Result[Nil, String] = fs_write_text("build/kry-test-stdlib.txt", "Kryndel")
match written {
    ok(value) => { assert(value == nil) }
    err(problem) => { assert(false) }
}
let environment: Option[String] = env_get("PATH")
match environment {
    some(value) => { assert(len(value) > 0) }
    none => { assert(false) }
}
KRY
run run "$work/system.kry"
test "$(cat build/kry-test-stdlib.txt)" = Kryndel
rm -f build/kry-test-stdlib.txt

cat > "$work/threads.kry" <<'KRY'
let channel: Channel[Int] = thread_channel()
fn worker() -> Nil {
    thread_send(channel, 42)
}
let worker_thread: Thread[Nil] = thread_spawn("worker")
let received: Int = thread_receive_timeout(channel, 1000)
thread_join(worker_thread)
println(received)
KRY
test "$(run run "$work/threads.kry")" = 42

cat > "$work/worker-failure.kry" <<'KRY'
fn failing() -> Nil {
    println(1 / 0)
}
let worker_thread: Thread[Nil] = thread_spawn("failing")
thread_join(worker_thread)
KRY
if run run "$work/worker-failure.kry" >"$work/out" 2>"$work/error"; then exit 1; fi
grep -q 'division by zero' "$work/error"

cat > "$work/closed-channel.kry" <<'KRY'
let channel: Channel[Int] = thread_channel()
thread_close(channel)
thread_close(channel)
thread_receive_timeout(channel, 1000)
KRY
if run run "$work/closed-channel.kry" >"$work/out" 2>"$work/error"; then exit 1; fi
grep -q 'closed channel' "$work/error"

printf 'let x: Int = 1   \nprintln(x)\n' > "$work/fmt.kry"
test "$(run fmt "$work/fmt.kry")" = 'let x: Int = 1
println(x)'
if run fmt --check "$work/fmt.kry" >/dev/null 2>"$work/error"; then
    echo 'fmt --check unexpectedly accepted unformatted source' >&2
    exit 1
fi
run fmt -w "$work/fmt.kry"
run fmt --check "$work/fmt.kry"
test "$(printf '1 + 2\n:quit\n' | run repl)" = 3
run doctor >"$work/doctor.txt"
grep -q 'doctor: ready' "$work/doctor.txt"
rm -f "$root/build/kry"
launcher_status=0
CC=definitely-not-a-compiler "$root/tools/kry" version >"$work/launcher-out" 2>"$work/launcher-error" || launcher_status=$?
test "$launcher_status" -eq 69
grep -q 'no usable C11 compiler' "$work/launcher-error"
"$root/tools/kry" version >/dev/null

printf '%s\n' 'native integration: ok'
