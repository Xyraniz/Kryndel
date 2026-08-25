"""Deterministic project module discovery and exported-symbol interfaces.

This module is deliberately separate from the package installer. It reads and
parses Kryndel source files, but it never executes package code or performs
network access. The Python bootstrap owns this implementation for now; the
module graph and visibility rules are language contracts for a future Kryndel
implementation.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from . import ast
from .diagnostics import Diagnostic, DiagnosticError, Severity, Span
from .packages import Manifest, read_manifest
from .parser import parse
from .source import SourceFile
from .tokens import lex
from .types import FunctionType, resolve_type


@dataclass(frozen=True)
class ModuleSource:
    path: Path
    module_id: str
    package_name: str
    package_root: Path
    manifest: Manifest
    text: str
    program: ast.Program
    imports: tuple[ast.ImportStmt, ...]


@dataclass(frozen=True)
class ModuleInterfaces:
    """Stable interfaces extracted from all resolved dependency modules."""

    function_types: dict[str, FunctionType]
    all_functions: dict[str, frozenset[str]]
    public_functions: dict[str, frozenset[str]]
    exported_symbols: dict[str, tuple[str, ...]]


class ModuleGraph:
    """Load a project root and every imported package module exactly once."""

    def __init__(self, project: str | Path, text: str, filename: str | Path | None = None) -> None:
        self.project = Path(project).resolve()
        self.manifest = read_manifest(self.project)
        self.root_path = (
            Path(filename).resolve()
            if filename is not None and not str(filename).startswith("<")
            else self.project / "src" / "main.kry"
        )
        self.root = self._parse_record(
            self.root_path,
            "__root__",
            self.manifest.name,
            self.project,
            self.manifest,
            text,
        )
        self.records: dict[str, ModuleSource] = {"__root__": self.root}
        self._active: list[str] = []

    def load(self) -> "ModuleGraph":
        self._visit("__root__", self.root)
        return self

    @property
    def dependency_modules(self) -> list[ModuleSource]:
        return [self.records[key] for key in sorted(self.records) if key != "__root__"]

    def interfaces(self) -> ModuleInterfaces:
        function_types: dict[str, FunctionType] = {}
        all_functions: dict[str, frozenset[str]] = {}
        public_functions: dict[str, frozenset[str]] = {}
        exported_symbols: dict[str, tuple[str, ...]] = {}
        for key in sorted(self.records):
            if key == "__root__":
                continue
            record = self.records[key]
            functions = [item for item in record.program.items if isinstance(item, ast.FunctionDecl)]
            all_names = frozenset(item.name for item in functions)
            public_names = frozenset(item.name for item in functions if item.public)
            all_functions[record.module_id] = all_names
            public_functions[record.module_id] = public_names
            exported_symbols[record.module_id] = tuple(
                sorted(
                    item.name
                    for item in record.program.items
                    if isinstance(item, (ast.FunctionDecl, ast.StructDecl, ast.EnumDecl)) and item.public
                )
            )
            for function in functions:
                if function.public:
                    function_types[f"{record.module_id}.{function.name}"] = self._function_type(function)
        return ModuleInterfaces(function_types, all_functions, public_functions, exported_symbols)

    def _visit(self, key: str, record: ModuleSource) -> None:
        if key in self._active:
            return
        self._active.append(key)
        try:
            for statement in sorted(record.imports, key=lambda item: (item.path, item.span.start)):
                imported = self._resolve_import(record, statement)
                imported_key = self._record_key(imported)
                if imported_key in self._active:
                    cycle_start = self._active.index(imported_key)
                    chain = self._active[cycle_start:] + [imported_key]
                    chain_text = " -> ".join(self._display_key(item) for item in chain)
                    raise self._error(
                        record,
                        statement.span,
                        f"circular import detected: {chain_text}",
                        "KRY5016",
                        help="Move the shared declarations to a third module or remove the cycle.",
                        notes=(f"package: {imported.package_name}", f"module: {imported.module_id}"),
                    )
                if imported_key not in self.records:
                    self.records[imported_key] = imported
                    self._visit(imported_key, imported)
        finally:
            self._active.pop()

    def _resolve_import(self, importer: ModuleSource, statement: ast.ImportStmt) -> ModuleSource:
        parts = tuple(statement.path.split("."))
        package_name = parts[0]
        module_parts = parts[1:]
        is_current_package = package_name == importer.package_name
        if not is_current_package and package_name not in importer.manifest.dependencies:
            raise self._error(
                importer,
                statement.span,
                f"package {package_name!r} is not declared in kry.toml",
                "KRY5013",
                help="Add the package with kry add before importing it.",
                notes=(f"package: {package_name}", f"module: {statement.path}"),
            )

        if is_current_package:
            if not module_parts and importer is self.root and package_name == self.manifest.name:
                return self.root
            package_root = importer.package_root
            manifest = importer.manifest
        else:
            package_root = self.project / ".kryndel" / "packages" / package_name
            if not package_root.is_dir():
                raise self._error(
                    importer,
                    statement.span,
                    f"declared package {package_name!r} is not installed",
                    "KRY5014",
                    help="Run kry install --offline before checking imports.",
                    notes=(f"package: {package_name}", f"module: {statement.path}"),
                )
            manifest = read_manifest(package_root)

        candidates = self._module_candidates(package_root, module_parts)
        if len(candidates) > 1:
            raise self._error(
                importer,
                statement.span,
                f"import {statement.path!r} is ambiguous",
                "KRY5015",
                help="Keep exactly one module path for this import.",
                notes=(f"package: {package_name}", f"module: {statement.path}"),
            )
        if not candidates:
            raise self._error(
                importer,
                statement.span,
                f"module {statement.path!r} was not found in the installed package",
                "KRY5014",
                help="Provide src/lib.kry for the package root or a unique module file/mod.kry.",
                notes=(f"package: {package_name}", f"module: {statement.path}"),
            )
        path = candidates[0]
        module_id = package_name if not module_parts else f"{package_name}.{'.'.join(module_parts)}"
        return self._parse_record(path, module_id, package_name, package_root, manifest)

    @staticmethod
    def _module_candidates(package_root: Path, module_parts: tuple[str, ...]) -> list[Path]:
        source_root = package_root / "src"
        if not module_parts:
            candidates = [source_root / "lib.kry"]
        else:
            relative = Path(*module_parts)
            candidates = [
                source_root / relative.with_suffix(".kry"),
                source_root / relative / "mod.kry",
                source_root / relative / "lib.kry",
            ]
        root = package_root.resolve()
        safe: list[Path] = []
        for candidate in candidates:
            if not candidate.is_file():
                continue
            resolved = candidate.resolve()
            try:
                resolved.relative_to(root)
            except ValueError:
                continue
            safe.append(resolved)
        return safe

    @staticmethod
    def _parse_record(
        path: Path,
        module_id: str,
        package_name: str,
        package_root: Path,
        manifest: Manifest,
        text: str | None = None,
    ) -> ModuleSource:
        source_text = path.read_text(encoding="utf-8") if text is None else text
        source = SourceFile(str(path), source_text)
        tokens, lexical = lex(source)
        program, parsing = parse(source, tokens)
        diagnostics = lexical.items + parsing.items
        if diagnostics:
            raise DiagnosticError(diagnostics, source_text, str(path))
        imports = tuple(item for item in program.items if isinstance(item, ast.ImportStmt))
        return ModuleSource(path, module_id, package_name, package_root.resolve(), manifest, source_text, program, imports)

    @staticmethod
    def _function_type(function: ast.FunctionDecl) -> FunctionType:
        return FunctionType(
            tuple(resolve_type(parameter.type_name.name) for parameter in function.parameters),
            resolve_type(function.return_type.name),
        )

    @staticmethod
    def _record_key(record: ModuleSource) -> str:
        return record.module_id

    @staticmethod
    def _display_key(key: str) -> str:
        return "<root>" if key == "__root__" else key

    @staticmethod
    def _error(
        record: ModuleSource,
        span: Span,
        message: str,
        code: str,
        *,
        help: str | None = None,
        notes: tuple[str, ...] = (),
    ) -> DiagnosticError:
        diagnostic = Diagnostic(Severity.ERROR, message, span, code=code, help=help, notes=list(notes))
        return DiagnosticError([diagnostic], record.text, str(record.path))
