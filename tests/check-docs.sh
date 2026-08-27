#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"

if git ls-files '*.py' | grep -q .; then
    echo 'documentation check: tracked Python sources are forbidden' >&2
    exit 1
fi
if git ls-files | grep -v '^tests/check-docs.sh$' | xargs -r grep -nE '[áéíóúñÁÉÍÓÚÑ¿¡]'; then
    echo 'documentation check: non-English Spanish prose marker found' >&2
    exit 1
fi
if git ls-files | grep -v '^tests/check-docs.sh$' | xargs -r grep -nE 'TODO|FIXME|implement later|not yet|only documentation|dynamic runtime'; then
    echo 'documentation check: stale implementation claim found' >&2
    exit 1
fi
for required in README.md CHANGELOG.md CONTRIBUTING.md docs/language.md docs/architecture.md docs/native.md docs/testing.md docs/modules.md docs/types.md docs/diagnostics.md docs/stdlib.md docs/release.md native/README.md; do
    test -s "$required"
done
printf '%s\n' 'documentation check: ok'
