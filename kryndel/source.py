"""Source text and position helpers."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from .diagnostics import Span


@dataclass(frozen=True)
class SourceFile:
    name: str
    text: str

    @classmethod
    def from_path(cls, path: str | Path) -> "SourceFile":
        resolved = Path(path)
        return cls(str(resolved), resolved.read_text(encoding="utf-8"))

    def span(self, start: int, end: int | None = None) -> Span:
        end = start + 1 if end is None else max(start + 1, end)
        prefix = self.text[:start]
        line = prefix.count("\n") + 1
        line_start = prefix.rfind("\n") + 1
        column = start - line_start + 1
        return Span(start, end, line, column)

    def line_text(self, line: int) -> str:
        lines = self.text.splitlines()
        if not lines:
            return ""
        return lines[max(0, min(line - 1, len(lines) - 1))]
