"""Kryndel bytecode data structures and portable serialization."""

from __future__ import annotations

import json
import math
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .contracts import strict_json_loads


BYTECODE_VERSION = 1
BYTECODE_OPCODES = frozenset(
    {
        "PUSH_CONST",
        "PUSH_NIL",
        "PUSH_CALLABLE",
        "LOAD",
        "STORE",
        "STORE_RESULT",
        "MAKE_STRUCT",
        "MAKE_ENUM",
        "MAKE_ARRAY",
        "MAKE_TUPLE",
        "INDEX",
        "MATCH_ENUM",
        "BIND_ENUM",
        "GET_FIELD",
        "POP",
        "DUP",
        "UNARY",
        "BINARY",
        "JUMP",
        "JUMP_IF_FALSE",
        "JUMP_IF_TRUE",
        "CALL",
        "RETURN",
    }
)


@dataclass
class Instruction:
    op: str
    arg: Any = None
    line: int = 0

    def as_dict(self) -> dict[str, Any]:
        return {"op": self.op, "arg": self.arg, "line": self.line}

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> "Instruction":
        if not isinstance(value, dict):
            raise ValueError("malformed bytecode instruction: expected an object")
        if set(value) != {"op", "arg", "line"}:
            raise ValueError("malformed bytecode instruction metadata")
        op = value["op"]
        line = value.get("line", 0)
        if type(op) is not str or not op:
            raise ValueError("malformed bytecode instruction opcode")
        if type(line) is not int or line < 0:
            raise ValueError("malformed bytecode instruction source line")
        return cls(op, value.get("arg"), line)


def _portable_constant(value: Any) -> bool:
    if value is None or type(value) is bool or type(value) is int or type(value) is str:
        return True
    return type(value) is float and math.isfinite(value)


def _require_string_list(value: Any, field_name: str) -> list[str]:
    if not isinstance(value, list) or any(type(item) is not str or not item for item in value):
        raise ValueError(f"malformed bytecode function {field_name}")
    if len(set(value)) != len(value):
        raise ValueError(f"duplicate bytecode function {field_name}")
    return list(value)


@dataclass
class BytecodeFunction:
    name: str
    arity: int
    instructions: list[Instruction] = field(default_factory=list)
    constants: list[Any] = field(default_factory=list)
    parameters: list[str] = field(default_factory=list)

    def add_constant(self, value: Any) -> int:
        for index, existing in enumerate(self.constants):
            if type(existing) is type(value) and existing == value:
                return index
        self.constants.append(value)
        return len(self.constants) - 1

    def as_dict(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "arity": self.arity,
            "constants": self.constants,
            "parameters": self.parameters,
            "instructions": [instruction.as_dict() for instruction in self.instructions],
        }

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> "BytecodeFunction":
        if not isinstance(value, dict) or set(value) != {"name", "arity", "constants", "parameters", "instructions"}:
            raise ValueError("malformed bytecode function metadata")
        name = value["name"]
        arity = value["arity"]
        constants = value["constants"]
        parameters = _require_string_list(value["parameters"], "parameters")
        instructions = value["instructions"]
        if type(name) is not str or not name:
            raise ValueError("malformed bytecode function name")
        if type(arity) is not int or arity < 0 or arity != len(parameters):
            raise ValueError(f"invalid bytecode function arity for {name!r}")
        if not isinstance(constants, list) or any(not _portable_constant(item) for item in constants):
            raise ValueError(f"malformed bytecode constants for {name!r}")
        if not isinstance(instructions, list):
            raise ValueError(f"malformed bytecode instructions for {name!r}")
        return cls(
            name,
            arity,
            [Instruction.from_dict(item) for item in instructions],
            list(constants),
            parameters,
        )


@dataclass
class Module:
    name: str
    entry: str
    functions: dict[str, BytecodeFunction]
    version: int = 1

    def as_dict(self) -> dict[str, Any]:
        return {
            "format": "kryndel-bytecode",
            "version": self.version,
            "name": self.name,
            "entry": self.entry,
            "functions": {name: function.as_dict() for name, function in self.functions.items()},
        }

    def dumps(self) -> str:
        return json.dumps(self.as_dict(), ensure_ascii=False, indent=2, sort_keys=True) + "\n"

    def dump(self, path: str | Path) -> None:
        Path(path).write_text(self.dumps(), encoding="utf-8")

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> "Module":
        if not isinstance(value, dict) or set(value) != {"format", "version", "name", "entry", "functions"}:
            raise ValueError("malformed bytecode module metadata")
        if value["format"] != "kryndel-bytecode":
            raise ValueError("not a Kryndel bytecode module")
        if type(value["version"]) is not int or value["version"] != BYTECODE_VERSION:
            raise ValueError(f"unsupported Kryndel bytecode version: {value['version']}")
        if type(value["name"]) is not str or not value["name"]:
            raise ValueError("malformed Kryndel bytecode module name")
        if type(value["entry"]) is not str or not value["entry"]:
            raise ValueError("malformed Kryndel bytecode entry")
        raw_functions = value["functions"]
        if not isinstance(raw_functions, dict) or not raw_functions:
            raise ValueError("malformed Kryndel bytecode function table")
        if any(type(name) is not str or not name for name in raw_functions):
            raise ValueError("malformed Kryndel bytecode function name")
        functions: dict[str, BytecodeFunction] = {}
        for name in sorted(raw_functions):
            if type(name) is not str or not name:
                raise ValueError("malformed Kryndel bytecode function name")
            function = BytecodeFunction.from_dict(raw_functions[name])
            if function.name != name:
                raise ValueError(f"bytecode function key/name mismatch for {name!r}")
            functions[name] = function
        return cls(value["name"], value["entry"], functions, BYTECODE_VERSION)

    @classmethod
    def load(cls, path: str | Path) -> "Module":
        text = Path(path).read_text(encoding="utf-8")
        return cls.from_dict(strict_json_loads(text, context=f"bytecode module {path}"))
