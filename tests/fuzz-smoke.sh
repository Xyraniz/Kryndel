#!/bin/sh
set -eu

BIN=${KRY_BIN:-./build/kry}
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Fixed byte patterns keep the smoke run deterministic while still covering
# invalid UTF-8, embedded NULs, truncated tokens, and artifact-like prefixes.
patterns='00 ff 00 01 02 03 04 05 0a 22 5c 7b 7d 5b 5d'
i=0
for pattern in $patterns; do
    i=$((i + 1))
    printf "\\$(printf '%03o' "0x$pattern")" > "$TMP/input-$i.kry" 2>/dev/null || true
    status=0
    timeout 2s "$BIN" check "$TMP/input-$i.kry" >/dev/null 2>&1 || status=$?
    case "$status" in
        0|1|2) ;;
        *) echo "fuzz-smoke: input $i exited with unexpected status $status" >&2; exit 1 ;;
    esac
done

# Exercise the formatter on malformed source as well; it must diagnose rather
# than rewrite or terminate abnormally.
printf '\377\000{unterminated\n' > "$TMP/format.kry"
status=0
timeout 2s "$BIN" fmt "$TMP/format.kry" >/dev/null 2>&1 || status=$?
case "$status" in
    0|1|2) ;;
    *) echo "fuzz-smoke: formatter exited with unexpected status $status" >&2; exit 1 ;;
esac

echo 'fuzz smoke: ok'
