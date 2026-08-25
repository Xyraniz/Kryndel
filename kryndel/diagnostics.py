"""Structured diagnostics used by every compiler phase."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum
from typing import Iterable


class Severity(str, Enum):
    ERROR = "error"
    WARNING = "warning"
    NOTE = "note"


@dataclass(frozen=True)
class Span:
    """A half-open source range."""

    start: int
    end: int
    line: int
    column: int

    @property
    def length(self) -> int:
        return max(1, self.end - self.start)


@dataclass
class Diagnostic:
    """A compiler message with enough context for a human or an editor."""

    severity: Severity
    message: str
    span: Span
    code: str | None = None
    notes: list[str] = field(default_factory=list)
    help: str | None = None

    def render(self, source: str, filename: str = "<source>") -> str:
        lines = source.splitlines() or [""]
        line_index = max(0, min(self.span.line - 1, len(lines) - 1))
        text = lines[line_index]
        caret_column = max(0, self.span.column - 1)
        underline = " " * caret_column + "^" * max(1, min(self.span.length, max(1, len(text) - caret_column)))
        label = self.severity.value
        code = f"[{self.code}] " if self.code else ""
        output = [
            f"{filename}:{self.span.line}:{self.span.column}: {label}: {code}{self.message}",
            f" {self.span.line:>4} | {text}",
            f"      | {underline}",
        ]
        for note in self.notes:
            output.append(f"      = note: {note}")
        if self.help:
            output.append(f"      = help: {self.help}")
        return "\n".join(output)


class DiagnosticBag:
    """Collects diagnostics while allowing compilation to continue."""

    def __init__(self) -> None:
        self.items: list[Diagnostic] = []

    def add(self, diagnostic: Diagnostic) -> None:
        self.items.append(diagnostic)

    def error(
        self,
        message: str,
        span: Span,
        *,
        code: str | None = None,
        notes: Iterable[str] = (),
        help: str | None = None,
    ) -> None:
        self.add(Diagnostic(Severity.ERROR, message, span, code, list(notes), help))

    def warning(self, message: str, span: Span, *, code: str | None = None) -> None:
        self.add(Diagnostic(Severity.WARNING, message, span, code))

    @property
    def has_errors(self) -> bool:
        return any(item.severity == Severity.ERROR for item in self.items)


class DiagnosticError(Exception):
    """Raised when a compilation phase produces one or more errors."""

    def __init__(self, diagnostics: Iterable[Diagnostic], source: str, filename: str = "<source>") -> None:
        self.diagnostics = list(diagnostics)
        self.source = source
        self.filename = filename
        super().__init__(self.render())

    def render(self) -> str:
        return "\n\n".join(d.render(self.source, self.filename) for d in self.diagnostics)
