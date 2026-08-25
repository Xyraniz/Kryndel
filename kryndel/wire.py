from __future__ import annotations

from dataclasses import fields, is_dataclass
from enum import Enum
from typing import Any


class WireSerializationError(ValueError):
    """Raised when a bootstrap value cannot cross the deterministic wire boundary."""


def to_wire(value: Any) -> Any:
    """Convert a finite bootstrap record tree to deterministic JSON-shaped data."""
    if value is None or isinstance(value, (str, int, float, bool)):
        if isinstance(value, float) and (value != value or value in {float("inf"), float("-inf")}):
            raise WireSerializationError("non-finite Float is not portable in v1")
        return value
    if isinstance(value, Enum):
        return value.value
    if isinstance(value, bytes):
        return {"kind": "Bytes", "hex": value.hex()}
    if is_dataclass(value):
        result: dict[str, Any] = {"record": type(value).__name__}
        for field in fields(value):
            result[field.name] = to_wire(getattr(value, field.name))
        return result
    if isinstance(value, (list, tuple)):
        return [to_wire(item) for item in value]
    if isinstance(value, dict):
        if any(not isinstance(key, str) for key in value):
            raise WireSerializationError("record keys must be strings")
        return {key: to_wire(value[key]) for key in sorted(value)}
    raise WireSerializationError(f"unsupported wire value: {type(value).__name__}")


def token_records(tokens: list[Any]) -> list[dict[str, Any]]:
    return [to_wire(token) for token in tokens]


def ast_record(program: Any) -> dict[str, Any]:
    return to_wire(program)
