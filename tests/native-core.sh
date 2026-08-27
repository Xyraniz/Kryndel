#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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
actual=$(PATH="/usr/bin:/bin" "$root/tools/kry-native" run "$work/core.kry")
test "$actual" = "$expected"
PATH="/usr/bin:/bin" "$root/tools/kry-native" check "$work/core.kry"
PATH="/usr/bin:/bin" "$root/tools/kry-native" build "$work/core.kry" -o "$work/core.kexe"
PATH="/usr/bin:/bin" "$root/tools/kry-native" build "$work/core.kry" -o "$work/core-second.kexe"
cmp -s "$work/core.kexe" "$work/core-second.kexe"
artifact_actual=$(PATH="/usr/bin:/bin" "$root/tools/kry-native" run "$work/core.kexe")
test "$artifact_actual" = "$expected"
cp "$work/core.kexe" "$work/core-trailing.kexe"
printf x >> "$work/core-trailing.kexe"
if PATH="/usr/bin:/bin" "$root/tools/kry-native" run "$work/core-trailing.kexe" >/dev/null 2>"$work/artifact-error.txt"; then
    printf '%s\n' 'native core accepted a malformed artifact' >&2
    exit 1
fi
grep -q 'malformed native artifact' "$work/artifact-error.txt"

cat > "$work/bad.kry" <<'KRY'
println(missing)
KRY
if PATH="/usr/bin:/bin" "$root/tools/kry-native" run "$work/bad.kry" 2> "$work/error.txt"; then
    printf '%s\n' 'native core accepted an unknown variable' >&2
    exit 1
fi
grep -q 'unknown variable' "$work/error.txt"
! grep -q 'Traceback' "$work/error.txt"

printf '%s\n' 'native core integration: ok'
