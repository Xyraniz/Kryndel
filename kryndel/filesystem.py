from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Protocol

from .diagnostics import Diagnostic, DiagnosticError, Severity, Span


FILESYSTEM_VERSION = 1


def filesystem_error(message: str, code: str, path: str) -> DiagnosticError:
    diagnostic = Diagnostic(Severity.ERROR, message, Span(0, 1, 1, 1), code=code)
    return DiagnosticError([diagnostic], "", path)


def normalize_relative_path(value: str | PurePosixPath) -> PurePosixPath:
    """Return a portable relative path or a serializable KRY6303/KRY6304 error."""
    raw = value.as_posix() if isinstance(value, PurePosixPath) else str(value)
    if not raw or raw in {"/", "\\"}:
        if raw in {"", "."}:
            return PurePosixPath(".")
        raise filesystem_error("filesystem path must be relative", "KRY6303", raw)
    if raw.startswith(("/", "\\")) or (len(raw) >= 2 and raw[1] == ":"):
        raise filesystem_error("filesystem path must be relative", "KRY6303", raw)
    if "\\" in raw:
        raise filesystem_error("filesystem path must use portable separators", "KRY6304", raw)
    if "//" in raw:
        raise filesystem_error("filesystem path contains an invalid component", "KRY6304", raw)
    parts = [part for part in raw.split("/") if part not in {"", "."}]
    if any(part == ".." for part in parts):
        raise filesystem_error("filesystem path escapes project root", "KRY6303", raw)
    if any(part == "" for part in parts):
        raise filesystem_error("filesystem path contains an invalid component", "KRY6304", raw)
    return PurePosixPath(*parts) if parts else PurePosixPath(".")


def _key(value: str | PurePosixPath) -> str:
    path = normalize_relative_path(value)
    return "" if path == PurePosixPath(".") else path.as_posix()


@dataclass(frozen=True)
class FileMetadata:
    path: str
    kind: str
    size: int


class FileSystem(Protocol):
    def read_bytes(self, path: str | PurePosixPath) -> bytes: ...

    def write_bytes(self, path: str | PurePosixPath, value: bytes) -> None: ...

    def list_dir(self, path: str | PurePosixPath = ".") -> tuple[FileMetadata, ...]: ...

    def stat(self, path: str | PurePosixPath) -> FileMetadata: ...


class VirtualFileSystem:
    """Deterministic in-memory filesystem used by bootstrap and differential tests."""

    def __init__(self, files: dict[str, bytes] | None = None) -> None:
        self._files: dict[str, bytes] = {}
        for path, value in sorted((files or {}).items()):
            self.write_bytes(path, value)

    def read_bytes(self, path: str | PurePosixPath) -> bytes:
        key = _key(path)
        if key not in self._files:
            raise filesystem_error("file does not exist", "KRY6302", key or ".")
        return self._files[key]

    def write_bytes(self, path: str | PurePosixPath, value: bytes) -> None:
        key = _key(path)
        if not key:
            raise filesystem_error("cannot write the filesystem root", "KRY6301", ".")
        if any(item.startswith(key + "/") for item in self._files):
            raise filesystem_error("cannot replace a directory with a file", "KRY6301", key)
        try:
            data = bytes(value)
        except (TypeError, ValueError) as exc:
            raise filesystem_error("filesystem content must be bytes", "KRY6304", key) from exc
        self._files[key] = data

    def stat(self, path: str | PurePosixPath) -> FileMetadata:
        key = _key(path)
        if key in self._files:
            return FileMetadata(key or ".", "file", len(self._files[key]))
        prefix = key + "/" if key else ""
        if any(item.startswith(prefix) for item in self._files):
            return FileMetadata(key or ".", "directory", 0)
        if not key:
            return FileMetadata(".", "directory", 0)
        raise filesystem_error("file does not exist", "KRY6302", key)

    def list_dir(self, path: str | PurePosixPath = ".") -> tuple[FileMetadata, ...]:
        directory = self.stat(path)
        if directory.kind != "directory":
            raise filesystem_error("path is not a directory", "KRY6301", directory.path)
        key = _key(path)
        prefix = key + "/" if key else ""
        children: dict[str, FileMetadata] = {}
        for item, content in self._files.items():
            if not item.startswith(prefix):
                continue
            remainder = item[len(prefix) :]
            name, _, tail = remainder.partition("/")
            child = f"{prefix}{name}" if prefix else name
            if tail:
                children[name] = FileMetadata(child, "directory", 0)
            else:
                children[name] = FileMetadata(child, "file", len(content))
        return tuple(children[name] for name in sorted(children))


class RootedFileSystem:
    """Host adapter constrained to one resolved project root."""

    def __init__(self, root: str | Path) -> None:
        candidate = Path(root)
        if not candidate.exists() or not candidate.is_dir():
            raise filesystem_error("filesystem root is not a directory", "KRY6301", str(candidate))
        self.root = candidate.resolve()

    def _host_path(self, path: str | PurePosixPath) -> tuple[str, Path]:
        normalized = normalize_relative_path(path)
        key = _key(normalized)
        candidate = self.root if not key else self.root.joinpath(*normalized.parts)
        current = self.root
        for part in normalized.parts if key else ():
            current = current / part
            if current.is_symlink():
                raise filesystem_error("symlinks are not allowed at the filesystem boundary", "KRY6303", key)
        return key or ".", candidate

    def read_bytes(self, path: str | PurePosixPath) -> bytes:
        key, candidate = self._host_path(path)
        if not candidate.exists():
            raise filesystem_error("file does not exist", "KRY6302", key)
        if not candidate.is_file():
            raise filesystem_error("path is not a regular file", "KRY6301", key)
        try:
            return candidate.read_bytes()
        except OSError as exc:
            raise filesystem_error(f"filesystem read failed: {exc.strerror or exc}", "KRY6301", key) from exc

    def write_bytes(self, path: str | PurePosixPath, value: bytes) -> None:
        key, candidate = self._host_path(path)
        if key == ".":
            raise filesystem_error("cannot write the filesystem root", "KRY6301", key)
        parent = candidate.parent
        if not parent.exists() or not parent.is_dir():
            raise filesystem_error("parent directory does not exist", "KRY6302", key)
        try:
            candidate.write_bytes(bytes(value))
        except (OSError, TypeError, ValueError) as exc:
            raise filesystem_error(f"filesystem write failed: {exc}", "KRY6301", key) from exc

    def stat(self, path: str | PurePosixPath) -> FileMetadata:
        key, candidate = self._host_path(path)
        if not candidate.exists():
            raise filesystem_error("file does not exist", "KRY6302", key)
        if candidate.is_file():
            return FileMetadata(key, "file", candidate.stat().st_size)
        if candidate.is_dir():
            return FileMetadata(key, "directory", 0)
        raise filesystem_error("path is not a regular file or directory", "KRY6301", key)

    def list_dir(self, path: str | PurePosixPath = ".") -> tuple[FileMetadata, ...]:
        key, candidate = self._host_path(path)
        if not candidate.exists():
            raise filesystem_error("directory does not exist", "KRY6302", key)
        if not candidate.is_dir():
            raise filesystem_error("path is not a directory", "KRY6301", key)
        entries: list[FileMetadata] = []
        try:
            for item in sorted(candidate.iterdir(), key=lambda value: value.name):
                relative = item.relative_to(self.root).as_posix()
                if item.is_symlink():
                    raise filesystem_error("symlinks are not allowed at the filesystem boundary", "KRY6303", relative)
                if item.is_file():
                    entries.append(FileMetadata(relative, "file", item.stat().st_size))
                elif item.is_dir():
                    entries.append(FileMetadata(relative, "directory", 0))
                else:
                    raise filesystem_error("unsupported filesystem entry", "KRY6301", relative)
        except OSError as exc:
            raise filesystem_error(f"directory listing failed: {exc}", "KRY6301", key) from exc
        return tuple(entries)
