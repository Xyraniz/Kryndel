#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo "Stage 3 bootstrap requires Linux x86_64; use WSL2 or a Linux host." >&2
  exit 2
fi

seed_path="$(sed -n 's/.*"stage1_seed_path": "\([^"]*\)".*/\1/p' selfhost/bootstrap.lock.json)"
if [[ -z "$seed_path" || ! -x "$seed_path" ]]; then
  echo "The locked executable Stage 1 seed is missing: $seed_path" >&2
  exit 2
fi

locked_hash() {
  sed -n "s/.*\"$1\": \"\([0-9a-f]\{64\}\)\".*/\1/p" selfhost/bootstrap.lock.json
}

sha256() {
  case "$1" in
    *.kry) tr -d '\r' < "$1" | sha256sum | sed 's/ .*//' ;;
    *) sha256sum "$1" | sed 's/ .*//' ;;
  esac
}

verify_hash() {
  local key="$1"
  local path="$2"
  local expected actual
  expected="$(locked_hash "$key")"
  actual="$(sha256 "$path")"
  if [[ -z "$expected" || "$actual" != "$expected" ]]; then
    echo "SHA-256 mismatch for $key: expected $expected, got $actual" >&2
    exit 1
  fi
  printf '[bootstrap] %s %s\n' "$key" "$actual"
}

verify_hash stage1-source-kir-compiler.elf "$seed_path"
verify_hash source_kir_compiler.kry selfhost/source_kir_compiler.kry
verify_hash dynamic_backend.kry selfhost/dynamic_backend.kry
verify_hash validated_kir.kry selfhost/validated_kir.kry
verify_hash kir_arena.kry selfhost/kir_arena.kry
verify_hash kir_typed_arena.kry selfhost/kir_typed_arena.kry
verify_hash elf_backend.kry selfhost/elf_backend.kry
verify_hash native_image.kry selfhost/native_image.kry
verify_hash pe_backend.kry selfhost/pe_backend.kry
verify_hash bootstrap-fixture.kry selfhost/fixtures/bootstrap_hello_stage27.kry
verify_hash enum-match-fixture.kry selfhost/fixtures/source_enum_match_stage38.kry
verify_hash windows-ffi-propagation-fixture.kry selfhost/fixtures/windows_ffi_propagation_stage39.kry

tmp="$(mktemp -d /tmp/kryndel-bootstrap.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
fixture=selfhost/fixtures/bootstrap_hello_stage27.kry

run_fixture() {
  local compiler="$1"
  local label="$2"
  local output="$tmp/$label-fixture"
  "$compiler" "$fixture" "$output"
  chmod 700 "$output"
  local actual
  actual="$("$output")"
  if [[ "$actual" != "hello from bootstrap" ]]; then
    echo "$label fixture output mismatch: $actual" >&2
    exit 1
  fi
  printf '[bootstrap] %s fixture passed\n' "$label"
}

run_enum_match_fixture() {
  local compiler="$1"
  local label="$2"
  local fixture=selfhost/fixtures/source_enum_match_stage38.kry
  local output="$tmp/$label-enum-match-fixture"
  "$compiler" "$fixture" "$output"
  chmod 700 "$output"
  local actual="$tmp/$label-enum-match-stdout"
  local expected="$tmp/enum-match-expected"
  if ! "$output" > "$actual"; then
    echo "$label enum match fixture failed to execute" >&2
    exit 1
  fi
  printf 'red\nyellow\ngreen\nwildcard\n' > "$expected"
  if ! cmp -s "$expected" "$actual"; then
    echo "$label enum match fixture output did not match the expected bytes" >&2
    cat "$actual" >&2
    exit 1
  fi
  printf '[bootstrap] %s exhaustive enum match fixture passed\n' "$label"
}

run_fixture "$seed_path" stage1
run_enum_match_fixture "$seed_path" stage1

selfhost_pe_output="${KRY_STAGE38_SELFHOST_PE_OUTPUT:-}"
if [[ -n "$selfhost_pe_output" ]]; then
  mkdir -p "$(dirname "$selfhost_pe_output")"
  bash "$repo_root/scripts/build-selfhost-pe.sh" \
    "$repo_root/selfhost/fixtures/source_enum_match_stage38.kry" \
    "$selfhost_pe_output"
fi

selfhost_fibonacci_pe_output="${KRY_FIBONACCI_SELFHOST_PE_OUTPUT:-}"
if [[ -n "$selfhost_fibonacci_pe_output" ]]; then
  mkdir -p "$(dirname "$selfhost_fibonacci_pe_output")"
  bash "$repo_root/scripts/build-selfhost-pe.sh" \
    "$repo_root/examples/fibonacci.kry" \
    "$selfhost_fibonacci_pe_output"
fi

stage2="$tmp/stage2-source-kir-compiler"
"$seed_path" selfhost/source_kir_compiler.kry "$stage2"
chmod 700 "$stage2"
verify_hash stage2-source-kir-compiler.elf "$stage2"
run_fixture "$stage2" stage2
run_enum_match_fixture "$stage2" stage2

windows_pe_output="${KRY_STAGE36_WINDOWS_PE_OUTPUT:-}"
if [[ -n "$windows_pe_output" ]]; then
  windows_dir="$tmp/windows"
  mkdir -p "$windows_dir"
  cat > "$windows_dir/geometry.kry" <<'KRY'
pub struct Point { x: Int, y: Int }
pub fn translate(point: Point, delta: Int) -> Point {
    return Point { x: point.x + delta, y: point.y - delta }
}
KRY
  cat > "$windows_dir/modes.kry" <<'KRY'
pub enum Mode { Idle, Ready }
KRY
  cat > "$windows_dir/windows-program.kry" <<'KRY'
import "geometry"
import "modes"
fn main() -> Nil {
    let point = translate(Point { x: 40, y: 2 }, 2)
    println(mode_name(Mode::Ready))
    println(point.x + point.y)
}
fn mode_name(mode: Mode) -> String {
    match mode {
        Mode::Idle => { return "Mode::Idle" }
        Mode::Ready => { return "Mode::Ready" }
    }
}
KRY
  windows_pe="$windows_dir/windows-program.exe"
  "$stage2" "$windows_dir/windows-program.kry" "$windows_pe" windows-amd64
  mkdir -p "$(dirname "$windows_pe_output")"
  cp "$windows_pe" "$windows_pe_output"
  printf '[bootstrap] Stage 2 Windows amd64 PE written to %s\n' "$windows_pe_output"
fi

ffi_pe_output="${KRY_STAGE39_FFI_PE_OUTPUT:-}"
if [[ -n "$ffi_pe_output" ]]; then
  empty_tool_path="$tmp/empty-tool-path"
  mkdir -p "$empty_tool_path"
  ffi_pe="$tmp/windows-ffi-propagation.exe"
  PATH="$empty_tool_path" "$stage2" \
    "$repo_root/selfhost/fixtures/windows_ffi_propagation_stage39.kry" \
    "$ffi_pe" windows-amd64
  mkdir -p "$(dirname "$ffi_pe_output")"
  cp "$ffi_pe" "$ffi_pe_output"
  printf '[bootstrap] Stage 2 C-free FFI PE written with empty PATH to %s\n' "$ffi_pe_output"
fi

stage3="$tmp/stage3-source-kir-compiler"
"$stage2" selfhost/source_kir_compiler.kry "$stage3"
verify_hash stage3-source-kir-compiler.elf "$stage3"
if ! cmp -s "$stage2" "$stage3"; then
  echo "Stage 2 and Stage 3 compiler ELFs are not byte-identical." >&2
  exit 1
fi
chmod 700 "$stage3"
run_fixture "$stage3" stage3
run_enum_match_fixture "$stage3" stage3
printf '[bootstrap] Stage 2 and Stage 3 are byte-identical; no Go, C, assembler, or linker was invoked.\n'
