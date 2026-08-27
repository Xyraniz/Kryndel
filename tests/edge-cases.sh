#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bin=${KRY_BIN:-"$root/tools/kry"}
run() { "$bin" "$@"; }
expect_check_failure() { if run check "$1" >"$work/out" 2>"$work/error"; then echo "expected check failure: $1" >&2; exit 1; fi; }
expect_run_failure() { if run run "$1" >"$work/out" 2>"$work/error"; then echo "expected run failure: $1" >&2; exit 1; fi; }

cat > "$work/literal-overflow.kry" <<'KRY'
let x: Int = 9223372036854775808
KRY
expect_check_failure "$work/literal-overflow.kry"
grep -q 'outside the supported Int range' "$work/error"

cat > "$work/underflow.kry" <<'KRY'
println(-9223372036854775807 - 2)
KRY
expect_run_failure "$work/underflow.kry"
grep -q 'overflow' "$work/error"

cat > "$work/division-zero.kry" <<'KRY'
println(1 / 0)
KRY
expect_run_failure "$work/division-zero.kry"
grep -q 'division by zero' "$work/error"

cat > "$work/remainder-zero.kry" <<'KRY'
println(1 % 0)
KRY
expect_run_failure "$work/remainder-zero.kry"
grep -q 'remainder by zero' "$work/error"

cat > "$work/minimum.kry" <<'KRY'
let minimum: Int = -9223372036854775807 - 1
println(-minimum)
KRY
expect_run_failure "$work/minimum.kry"
grep -q 'negation overflow' "$work/error"

cat > "$work/abs-minimum.kry" <<'KRY'
let minimum: Int = -9223372036854775807 - 1
println(abs(minimum))
KRY
expect_run_failure "$work/abs-minimum.kry"
grep -q 'minimum Int' "$work/error"

cat > "$work/bad-conversion.kry" <<'KRY'
println(int("12xyz"))
KRY
expect_run_failure "$work/bad-conversion.kry"
grep -q 'complete decimal String' "$work/error"

cat > "$work/bad-array.kry" <<'KRY'
let values = [1, true]
KRY
expect_check_failure "$work/bad-array.kry"
grep -q 'homogeneous' "$work/error"

cat > "$work/bad-builtin.kry" <<'KRY'
println()
KRY
expect_check_failure "$work/bad-builtin.kry"
grep -q 'expects 1 argument' "$work/error"

cat > "$work/bad-return.kry" <<'KRY'
fn wrong() -> Int {
    return "no"
}
KRY
expect_check_failure "$work/bad-return.kry"
grep -q 'function must return Int' "$work/error"

cat > "$work/branch-immutable.kry" <<'KRY'
let fixed: Int = 1
if true {
    fixed = 2
}
KRY
expect_check_failure "$work/branch-immutable.kry"
grep -q 'immutable binding' "$work/error"

cat > "$work/missing-module.kry" <<'KRY'
import "missing"
KRY
expect_check_failure "$work/missing-module.kry"
grep -q 'cannot resolve module' "$work/error"

cat > "$work/short-circuit-ok.kry" <<'KRY'
assert(true || (1 / 0 == 0))
let skipped: Bool = false && (1 / 0 == 0)
KRY
run run "$work/short-circuit-ok.kry"

printf '%s\n' 'edge cases: ok'
