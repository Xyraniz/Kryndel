from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any, Iterable


CORE_CONTRACT = "kryndel-core"
CORE_VERSION = 1

_REQUIRED_FIXTURES = (
    "tests/fixtures/value-runtime-v1.json",
    "tests/fixtures/bytes-v1.json",
    "tests/fixtures/stdlib-testing-v1.json",
    "tests/fixtures/host-boundary-v1.json",
    "tests/fixtures/filesystem-v1.json",
    "tests/fixtures/collections-v1.json",
    "tests/fixtures/data-core-v1.json",
)
_REQUIRED_RUNTIME_ERRORS = {
    "KRY6102",
    "KRY6104",
    "KRY6201",
    "KRY6202",
    "KRY6301",
    "KRY6303",
    "KRY6305",
}


def canonical_json(value: Any) -> str:
    """Serialize a contract value with the repository's stable JSON rules."""
    return json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n"


def _fixture_path(root: Path, relative: str) -> Path:
    path = root / relative
    if not path.is_file():
        raise ValueError(f"required core-v1 fixture is missing: {relative}")
    return path


def _read_canonical(path: Path) -> tuple[Any, bytes]:
    raw = path.read_bytes()
    try:
        decoded = raw.decode("utf-8")
        value = json.loads(decoded)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError(f"fixture is not canonical JSON: {path}") from exc
    expected = canonical_json(value).encode("utf-8")
    if raw != expected:
        raise ValueError(f"fixture is not canonical JSON: {path}")
    return value, raw


def _validate_fixture_shape(relative: str, value: Any) -> None:
    if not isinstance(value, dict):
        raise ValueError(f"fixture root must be an object: {relative}")
    version = value.get("version")
    if version != CORE_VERSION:
        raise ValueError(f"fixture {relative} has unsupported version {version!r}")
    contract = value.get("contract")
    if not isinstance(contract, str) or not contract:
        raise ValueError(f"fixture {relative} has no contract name")
    if relative.endswith("value-runtime-v1.json"):
        invalid = value.get("invalid")
        valid = value.get("valid")
        if not isinstance(invalid, list) or not isinstance(valid, list):
            raise ValueError("value-runtime-v1 fixture must contain valid and invalid cases")
        errors = {case.get("error") for case in invalid if isinstance(case, dict)}
        missing = sorted(_REQUIRED_RUNTIME_ERRORS - errors)
        if missing:
            raise ValueError(f"value-runtime-v1 fixture is missing errors: {', '.join(missing)}")
    elif relative.endswith("bytes-v1.json"):
        if value.get("contract") != "kryndel-bytes":
            raise ValueError("bytes fixture has an unexpected contract")
        if not isinstance(value.get("construction"), dict) or not isinstance(value.get("operations"), dict):
            raise ValueError("bytes fixture must contain construction and operations")
    elif relative.endswith("stdlib-testing-v1.json"):
        if value.get("source") != "stdlib/testing/testing.kry":
            raise ValueError("stdlib testing fixture points at an unexpected source")
    elif relative.endswith("host-boundary-v1.json"):
        if value.get("contract") != "kryndel-host-boundary":
            raise ValueError("host-boundary fixture has an unexpected contract")
        if not isinstance(value.get("intrinsics"), list) or not isinstance(value.get("layers"), dict):
            raise ValueError("host-boundary fixture must contain intrinsics and layers")
    elif relative.endswith("filesystem-v1.json"):
        if value.get("contract") != "kryndel-filesystem":
            raise ValueError("filesystem fixture has an unexpected contract")
        if not isinstance(value.get("api"), dict) or set(value["api"]) != {
            "fs.list_dir",
            "fs.read_bytes",
            "fs.read_text",
            "fs.stat",
            "fs.write_bytes",
        }:
            raise ValueError("filesystem fixture must declare all filesystem intrinsics")
        if not isinstance(value.get("metadata"), dict) or value["metadata"].get("type") != "FileMetadata":
            raise ValueError("filesystem fixture must declare FileMetadata")
        fields = value["metadata"].get("fields")
        if fields != [
            {"name": "path", "type": "String"},
            {"name": "kind", "type": "String"},
            {"name": "size", "type": "Int"},
        ]:
            raise ValueError("filesystem metadata fields are not canonical")
    elif relative.endswith("collections-v1.json"):
        if value.get("contract") != "kryndel-collections":
            raise ValueError("collections fixture has an unexpected contract")
        if value.get("api") != {"array_push": "array_push(Array, Any) -> Array"}:
            raise ValueError("collections fixture must declare array_push")
        if not isinstance(value.get("operations"), dict) or set(value["operations"]) != {
            "append_int",
            "append_string",
        }:
            raise ValueError("collections fixture must contain canonical append operations")
    elif relative.endswith("data-core-v1.json"):
        if value.get("contract") != "kryndel-data-core":
            raise ValueError("data-core fixture has an unexpected contract")
        if value.get("source") != "stdlib/core/data.kry":
            raise ValueError("data-core fixture points at an unexpected source")
        operations = value.get("operations")
        if not isinstance(operations, dict) or set(operations) != {
            "bytes_slice",
            "bytes_slice_get",
            "bytes_slice_length",
            "bytes_slice_to_bytes",
            "string_builder_append",
            "string_builder_finish",
            "string_builder_new",
            "string_slice",
            "string_slice_get",
            "string_slice_length",
            "string_slice_to_string",
        }:
            raise ValueError("data-core fixture must declare the complete source API")
        records = value.get("records")
        if records != {
            "AstRecord": ["kind", "text", "span", "children"],
            "DiagnosticRecord": ["severity", "code", "message", "span", "notes", "help"],
            "SpanRecord": ["start", "end", "line", "column"],
            "TokenRecord": ["kind", "text", "span"],
        }:
            raise ValueError("data-core record layouts are not canonical")
        invalid = value.get("invalid")
        if not isinstance(invalid, list) or {case.get("error") for case in invalid if isinstance(case, dict)} != {
            "KRY6104",
            "KRY6202",
        }:
            raise ValueError("data-core fixture must cover bounded-reader errors")
        if not isinstance(value.get("valid"), list) or not value["valid"]:
            raise ValueError("data-core fixture must contain valid cases")


def core_contract_report(root: str | Path) -> dict[str, Any]:
    """Validate and describe the portable core-v1 fixtures deterministically.

    This is a bootstrap audit command. It does not claim that the implementation
    path has stopped depending on Python.
    """
    project = Path(root).resolve()
    fixtures: list[dict[str, Any]] = []
    for relative in sorted(_REQUIRED_FIXTURES):
        path = _fixture_path(project, relative)
        value, raw = _read_canonical(path)
        _validate_fixture_shape(relative, value)
        fixtures.append(
            {
                "bytes": len(raw),
                "path": relative,
                "sha256": hashlib.sha256(raw).hexdigest(),
                "version": value["version"],
            }
        )
    return {
        "contract": CORE_CONTRACT,
        "fixtures": fixtures,
        "implementation": "Python bootstrap reference",
        "version": CORE_VERSION,
    }


def validate_core_contract(root: str | Path) -> None:
    core_contract_report(root)


def fixture_paths(root: str | Path) -> Iterable[Path]:
    project = Path(root).resolve()
    return (_fixture_path(project, relative) for relative in _REQUIRED_FIXTURES)
