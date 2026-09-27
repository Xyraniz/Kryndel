#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: scripts/build-selfhost-pe.sh SOURCE.kry [OUTPUT.exe] [--gui]" >&2
  exit 2
}

if [[ $# -lt 1 || $# -gt 3 ]]; then usage; fi

source="$1"
output=""
target="windows-amd64"
if [[ $# -ge 2 ]]; then
  if [[ "$2" == "--gui" ]]; then
    if [[ $# -ne 2 ]]; then usage; fi
    target="windows-amd64-gui"
  else
    output="$2"
  fi
fi
if [[ $# -eq 3 ]]; then
  if [[ "$3" != "--gui" ]]; then usage; fi
  target="windows-amd64-gui"
fi

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo "the self-hosted PE compiler runs on Linux x86_64; use WSL2 or a Linux host" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
compiler="$repo_root/selfhost/bootstrap/linux-amd64/source-kir-compiler-stage1.elf"
lock="$repo_root/selfhost/bootstrap.lock.json"
if [[ ! -f "$source" ]]; then
  echo "Kryndel source file not found: $source" >&2
  exit 2
fi
if [[ -z "$output" ]]; then
  if [[ "$source" == *.kry ]]; then
    output="${source%.kry}.exe"
  else
    output="$source.exe"
  fi
fi
if [[ ! -x "$compiler" ]]; then
  echo "locked Stage 1 compiler is missing or not executable: $compiler" >&2
  exit 2
fi
expected_hash="$(sed -n 's/.*"stage1-source-kir-compiler.elf": "\([0-9a-f]\{64\}\)".*/\1/p' "$lock")"
actual_hash="$(sha256sum "$compiler" | sed 's/ .*//')"
if [[ -z "$expected_hash" || "$actual_hash" != "$expected_hash" ]]; then
  echo "locked Stage 1 compiler hash mismatch: expected $expected_hash, got $actual_hash" >&2
  exit 1
fi

"$compiler" "$source" "$output" "$target"
if [[ ! -s "$output" ]]; then
  echo "self-hosted compiler did not write a Windows executable: $output" >&2
  exit 1
fi
printf '[selfhost] wrote Windows amd64 PE: %s\n' "$output"
