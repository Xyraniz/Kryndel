"""Structured diagnostics used by every compiler phase."""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum
import json
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
    secondary_spans: list[tuple[Span, str]] = field(default_factory=list)
    suggestion: str | None = None

    @property
    def phase(self) -> str:
        number = int((self.code or "KRY0000")[3:]) if (self.code or "").startswith("KRY") and (self.code or "")[3:].isdigit() else 0
        if number < 2000:
            return "lexer"
        if number < 3000:
            return "parser"
        if number < 4000:
            return "semantic"
        if number < 5000:
            return "compiler"
        if number < 6000:
            return "package"
        return "runtime"

    def as_dict(self, filename: str = "<source>") -> dict[str, object]:
        """Return a stable, editor-friendly representation of this diagnostic."""
        return {
            "code": self.code or "KRY0000",
            "phase": self.phase,
            "severity": self.severity.value,
            "file": filename,
            "span": {
                "start": self.span.start,
                "end": self.span.end,
                "line": self.span.line,
                "column": self.span.column,
            },
            "secondary_spans": [
                {
                    "label": label,
                    "span": {
                        "start": span.start,
                        "end": span.end,
                        "line": span.line,
                        "column": span.column,
                    },
                }
                for span, label in self.secondary_spans
            ],
            "message": self.message,
            "help": self.help,
            "notes": list(self.notes),
            "suggestion": self.suggestion,
        }

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
        if self.suggestion:
            output.append(f"      = suggestion: {self.suggestion}")
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
        secondary_spans: Iterable[tuple[Span, str]] = (),
        suggestion: str | None = None,
    ) -> None:
        self.add(
            Diagnostic(
                Severity.ERROR,
                message,
                span,
                code,
                list(notes),
                help,
                list(secondary_spans),
                suggestion,
            )
        )

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

    def as_json(self) -> str:
        """Serialize diagnostics without timestamps, host paths, or unstable ordering."""
        return json.dumps(
            {"diagnostics": [item.as_dict(self.filename) for item in self.diagnostics]},
            ensure_ascii=False,
            indent=2,
            sort_keys=True,
        ) + "\n"
