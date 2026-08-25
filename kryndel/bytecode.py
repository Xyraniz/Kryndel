"""Kryndel bytecode data structures and portable serialization."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass
class Instruction:
    op: str
    arg: Any = None
    line: int = 0

    def as_dict(self) -> dict[str, Any]:
        return {"op": self.op, "arg": self.arg, "line": self.line}

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> "Instruction":
        if not isinstance(value, dict) or "op" not in value:
            raise ValueError("malformed bytecode instruction")
        return cls(str(value["op"]), value.get("arg"), int(value.get("line", 0)))


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
        return cls(
            str(value["name"]),
            int(value["arity"]),
            [Instruction.from_dict(item) for item in value.get("instructions", [])],
            list(value.get("constants", [])),
            list(value.get("parameters", [])),
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
        if value.get("format") != "kryndel-bytecode":
            raise ValueError("not a Kryndel bytecode module")
        if int(value.get("version", 0)) != 1:
            raise ValueError(f"unsupported Kryndel bytecode version: {value.get('version')}")
        functions = {name: BytecodeFunction.from_dict(item) for name, item in value["functions"].items()}
        return cls(str(value["name"]), str(value["entry"]), functions, 1)

    @classmethod
    def load(cls, path: str | Path) -> "Module":
        return cls.from_dict(json.loads(Path(path).read_text(encoding="utf-8")))
