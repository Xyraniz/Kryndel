#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo "Stage 3 bootstrap requires Linux x86_64; use WSL2 or a Linux host." >&2
  exit 2
fi

go_version="$(go version | awk '{print $3}')"
locked_go_version="$(sed -n 's/.*"host_go": "\([^"]*\)".*/\1/p' selfhost/bootstrap.lock.json)"
if [[ "$go_version" != "$locked_go_version" ]]; then
  echo "Stage 3 bootstrap lock requires $locked_go_version; found $go_version." >&2
  exit 2
fi

go test ./internal/kry -run '^TestStage36KryndelSecondCompilerBootstrap$' -count=1 -timeout=20m -v
