"""Standard-library-only project manifests and local Kryndel package resolution."""

from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

from .diagnostics import Diagnostic, DiagnosticError, Severity, Span


PACKAGE_NAME = re.compile(r"^[A-Za-z][A-Za-z0-9_-]*$")
VERSION = re.compile(r"^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
PARTIAL_VERSION = re.compile(r"^(0|[1-9][0-9]*)(?:\.(0|[1-9][0-9]*))?$")


def package_error(message: str, code: str, path: Path, help_text: str | None = None) -> DiagnosticError:
    diagnostic = Diagnostic(
        Severity.ERROR,
        message,
        Span(0, 1, 1, 1),
        code=code,
        help=help_text,
    )
    return DiagnosticError([diagnostic], "", str(path))


@dataclass(frozen=True, order=True)
class SemVer:
    major: int
    minor: int
    patch: int

    @classmethod
    def parse(cls, value: str, path: Path | None = None) -> "SemVer":
        if not VERSION.fullmatch(value):
            raise package_error(f"invalid semantic version {value!r}", "KRY5003", path or Path("kry.toml"), "Use MAJOR.MINOR.PATCH, for example 1.2.3.")
        major, minor, patch = (int(part) for part in value.split("."))
        return cls(major, minor, patch)

    def __str__(self) -> str:
        return f"{self.major}.{self.minor}.{self.patch}"


@dataclass(frozen=True)
class VersionRequirement:
    raw: str
    lower: SemVer
    upper: SemVer | None = None
    exact: bool = False

    @classmethod
    def parse(cls, value: str, path: Path | None = None) -> "VersionRequirement":
        location = path or Path("kry.toml")
        if VERSION.fullmatch(value):
            exact = SemVer.parse(value, location)
            return cls(value, exact, exact, True)
        if value.startswith("^") or value.startswith("~"):
            match = PARTIAL_VERSION.fullmatch(value[1:])
            if not match:
                raise package_error(f"invalid version requirement {value!r}", "KRY5012", location, "Use 1.2.3, ^1.2, ~1.2, or a comma-separated range.")
            major = int(match.group(1))
            minor = int(match.group(2) or 0)
            lower = SemVer(major, minor, 0)
            if value[0] == "~":
                upper = SemVer(major, minor + 1, 0)
            elif major > 0:
                upper = SemVer(major + 1, 0, 0)
            elif minor > 0:
                upper = SemVer(0, minor + 1, 0)
            else:
                upper = SemVer(0, 0, 1)
            return cls(value, lower, upper)
        parts = value.split(",")
        if len(parts) == 2 and all(part.startswith((">=", "<")) for part in parts):
            lower = SemVer.parse(parts[0][2:], location)
            upper = SemVer.parse(parts[1][1:], location)
            if lower >= upper:
                raise package_error(f"empty version range {value!r}", "KRY5012", location)
            return cls(value, lower, upper)
        raise package_error(f"invalid version requirement {value!r}", "KRY5012", location, "Use 1.2.3, ^1.2, ~1.2, or >=1.0.0,<2.0.0.")

    def matches(self, version: SemVer) -> bool:
        if self.exact:
            return version == self.lower
        return version >= self.lower and (self.upper is None or version < self.upper)


@dataclass
class Manifest:
    path: Path
    name: str
    version: SemVer
    edition: str
    dependencies: dict[str, str] = field(default_factory=dict)

    def dumps(self) -> str:
        lines = ["[package]", f'name = "{self.name}"', f'version = "{self.version}"', f'edition = "{self.edition}"', "", "[dependencies]"]
        lines.extend(f'{name} = "{self.dependencies[name]}"' for name in sorted(self.dependencies))
        return "\n".join(lines) + "\n"


def _parse_assignment(line: str, path: Path, line_number: int) -> tuple[str, str]:
    if "=" not in line:
        raise package_error(f"manifest line {line_number} must be key = \"value\"", "KRY5001", path)
    key, raw = (part.strip() for part in line.split("=", 1))
    if not key or not re.fullmatch(r"[A-Za-z][A-Za-z0-9_-]*", key) or len(raw) < 2 or not (raw.startswith('"') and raw.endswith('"')):
        raise package_error(f"unsupported manifest syntax on line {line_number}", "KRY5001", path, "This bootstrap accepts only plain string assignments.")
    value = raw[1:-1]
    if "\\" in value or '"' in value:
        raise package_error(f"unsupported string escape on line {line_number}", "KRY5001", path)
    return key, value


def read_manifest(path: str | Path) -> Manifest:
    manifest_path = Path(path)
    if manifest_path.is_dir():
        manifest_path = manifest_path / "kry.toml"
    if not manifest_path.is_file():
        raise package_error("manifest file kry.toml is missing", "KRY5001", manifest_path, "Run kry init or create a supported [package] manifest.")
    section = ""
    values: dict[str, dict[str, str]] = {"package": {}, "dependencies": {}}
    for number, raw_line in enumerate(manifest_path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1]
            if section not in values:
                raise package_error(f"unsupported manifest section [{section}]", "KRY5001", manifest_path)
            continue
        if section not in values:
            raise package_error(f"manifest assignment is outside a supported section on line {number}", "KRY5001", manifest_path)
        key, value = _parse_assignment(line, manifest_path, number)
        if key in values[section]:
            raise package_error(f"duplicate manifest key {key!r}", "KRY5001", manifest_path)
        values[section][key] = value
    package = values["package"]
    if set(package) != {"name", "version", "edition"}:
        raise package_error("[package] must contain exactly name, version, and edition", "KRY5001", manifest_path)
    name = package["name"]
    if not PACKAGE_NAME.fullmatch(name) or "/" in name or "\\" in name or name in {".", ".."}:
        raise package_error(f"invalid package name {name!r}", "KRY5002", manifest_path)
    version = SemVer.parse(package["version"], manifest_path)
    if not package["edition"].isdigit():
        raise package_error(f"invalid edition {package['edition']!r}", "KRY5002", manifest_path)
    for dependency, requirement in values["dependencies"].items():
        if not PACKAGE_NAME.fullmatch(dependency) or "/" in dependency or "\\" in dependency:
            raise package_error(f"invalid dependency name {dependency!r}", "KRY5002", manifest_path)
        if not requirement.startswith("path:"):
            VersionRequirement.parse(requirement, manifest_path)
        elif Path(requirement[5:]).is_absolute():
            raise package_error("path dependencies must be relative", "KRY5010", manifest_path)
    return Manifest(manifest_path, name, version, package["edition"], dict(sorted(values["dependencies"].items())))


def write_manifest(manifest: Manifest) -> None:
    manifest.path.write_text(manifest.dumps(), encoding="utf-8", newline="\n")


def package_checksum(root: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(item for item in root.rglob("*") if item.is_file() and item.name != "checksum"):
        relative = path.relative_to(root).as_posix()
        if ".." in Path(relative).parts:
            raise package_error("package file escapes its root", "KRY5010", path)
        digest.update(relative.encode("utf-8"))
        digest.update(b"\0")
        digest.update(path.read_bytes())
        digest.update(b"\0")
    return digest.hexdigest()


@dataclass(frozen=True)
class LockEntry:
    name: str
    version: str
    checksum: str
    source: str
    dependencies: tuple[str, ...]


@dataclass
class Lockfile:
    entries: list[LockEntry]

    def dumps(self) -> str:
        payload = {
            "format": "kryndel-lock",
            "version": 1,
            "packages": [
                {
                    "name": entry.name,
                    "version": entry.version,
                    "checksum": entry.checksum,
                    "source": entry.source,
                    "dependencies": list(entry.dependencies),
                }
                for entry in sorted(self.entries, key=lambda item: (item.name, item.version))
            ],
        }
        return json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n"

    @classmethod
    def load(cls, path: Path) -> "Lockfile":
        try:
            value = json.loads(path.read_text(encoding="utf-8"))
            if value.get("format") != "kryndel-lock" or value.get("version") != 1:
                raise ValueError("unsupported lockfile format")
            entries = [LockEntry(str(item["name"]), str(item["version"]), str(item["checksum"]), str(item["source"]), tuple(sorted(str(dep) for dep in item.get("dependencies", [])))) for item in value["packages"]]
        except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
            raise package_error(f"invalid lockfile: {exc}", "KRY5009", path, "Delete the lockfile and run kry update to resolve again.") from exc
        return cls(entries)


class Resolver:
    def __init__(self, root: Path, *, offline: bool = False) -> None:
        self.root = root.resolve()
        self.offline = offline
        self.registry = self.root / ".kryndel" / "registry"
        self.entries: dict[str, LockEntry] = {}
        self.manifests: dict[str, Manifest] = {}
        self.paths: dict[str, Path] = {}

    def resolve(self, manifest: Manifest) -> Lockfile:
        for name in sorted(manifest.dependencies):
            self.visit(name, manifest.dependencies[name], manifest.path.parent, [])
        return Lockfile(sorted(self.entries.values(), key=lambda item: (item.name, item.version)))

    def visit(self, name: str, requirement: str, parent: Path, chain: list[str]) -> None:
        if name in chain:
            cycle = " -> ".join(chain + [name])
            raise package_error(f"circular dependency detected: {cycle}", "KRY5007", parent / "kry.toml")
        package_path, package_manifest, source = self.select(name, requirement, parent)
        existing = self.entries.get(name)
        if existing:
            if existing.version != str(package_manifest.version):
                raise package_error(f"incompatible versions for package {name!r}: {existing.version} and {package_manifest.version}", "KRY5005", parent / "kry.toml")
            return
        checksum = package_checksum(package_path)
        declared_checksum = package_path / "checksum"
        if declared_checksum.exists():
            expected = declared_checksum.read_text(encoding="utf-8").strip()
            if expected != checksum:
                raise package_error(f"checksum mismatch for package {name!r} {package_manifest.version}", "KRY5008", declared_checksum)
        elif source == "registry":
            raise package_error(f"checksum file is missing for package {name!r}", "KRY5008", package_path / "checksum")
        dependencies = tuple(sorted(package_manifest.dependencies))
        self.entries[name] = LockEntry(name, str(package_manifest.version), checksum, source, dependencies)
        self.manifests[name] = package_manifest
        self.paths[name] = package_path
        for dependency in dependencies:
            self.visit(dependency, package_manifest.dependencies[dependency], package_path, chain + [name])

    def select(self, name: str, requirement: str, parent: Path) -> tuple[Path, Manifest, str]:
        if requirement.startswith("path:"):
            relative = Path(requirement[5:])
            candidate = (parent / relative).resolve()
            try:
                candidate.relative_to(self.root.parent)
            except ValueError as exc:
                raise package_error("path dependency escapes the project workspace", "KRY5010", parent / "kry.toml") from exc
            selected = read_manifest(candidate)
            if selected.name != name:
                raise package_error(f"path dependency is named {selected.name!r}, expected {name!r}", "KRY5002", selected.path)
            return candidate, selected, "path"
        wanted = VersionRequirement.parse(requirement, parent / "kry.toml")
        if not self.registry.is_dir():
            if self.offline:
                raise package_error(f"package {name!r} is not present in the offline registry", "KRY5004", self.registry)
            raise package_error("remote registries are not implemented by this bootstrap", "KRY5004", self.registry, "Use a local registry or add a path dependency.")
        candidates: list[tuple[SemVer, Path, Manifest]] = []
        package_root = self.registry / name
        if package_root.is_dir():
            for directory in package_root.iterdir():
                if not directory.is_dir() or not VERSION.fullmatch(directory.name):
                    continue
                candidate = read_manifest(directory)
                if candidate.name == name and wanted.matches(candidate.version):
                    candidates.append((candidate.version, directory, candidate))
        if not candidates:
            raise package_error(f"package {name!r} has no version compatible with {requirement!r}", "KRY5004", parent / "kry.toml", "Add the package to the local registry or change the requirement.")
        _, path, selected = sorted(candidates, key=lambda item: item[0], reverse=True)[0]
        return path, selected, "registry"


def install(root: str | Path, *, offline: bool = False, locked: bool = False) -> Lockfile:
    project = Path(root).resolve()
    manifest = read_manifest(project)
    resolver = Resolver(project, offline=offline)
    lock = resolver.resolve(manifest)
    lock_path = project / "kry.lock"
    if locked:
        if not lock_path.is_file():
            raise package_error("--locked requires an existing kry.lock", "KRY5009", lock_path)
        previous = Lockfile.load(lock_path).dumps()
        if previous != lock.dumps():
            raise package_error("kry.lock is out of date", "KRY5009", lock_path, "Run kry update to refresh the deterministic lockfile.")
    packages = project / ".kryndel" / "packages"
    packages.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="install-", dir=packages.parent) as temporary:
        stage = Path(temporary) / "packages"
        stage.mkdir()
        for entry in sorted(lock.entries, key=lambda item: item.name):
            source = resolver.paths[entry.name]
            target = stage / entry.name
            _copy_package(source, target, project)
        if packages.exists():
            shutil.rmtree(packages)
        os.replace(stage, packages)
    lock_path.write_text(lock.dumps(), encoding="utf-8", newline="\n")
    return lock


def _copy_package(source: Path, target: Path, project: Path) -> None:
    source = source.resolve()
    for item in source.rglob("*"):
        if item.is_symlink():
            resolved = item.resolve()
            try:
                resolved.relative_to(source)
            except ValueError as exc:
                raise package_error("package symlink escapes its package root", "KRY5010", item) from exc
    shutil.copytree(source, target, symlinks=False)


def init_project(path: str | Path) -> Manifest:
    root = Path(path).resolve()
    root.mkdir(parents=True, exist_ok=True)
    manifest_path = root / "kry.toml"
    if manifest_path.exists():
        raise package_error("kry.toml already exists", "KRY5001", manifest_path)
    name = root.name or "demo"
    if not PACKAGE_NAME.fullmatch(name):
        name = "demo"
    manifest = Manifest(manifest_path, name, SemVer(0, 1, 0), "2026")
    write_manifest(manifest)
    (root / "src").mkdir(exist_ok=True)
    return manifest


def add_dependency(root: str | Path, name: str, *, version: str | None = None, path: str | Path | None = None) -> Manifest:
    project = Path(root).resolve()
    manifest = read_manifest(project)
    if not PACKAGE_NAME.fullmatch(name):
        raise package_error(f"invalid package name {name!r}", "KRY5002", manifest.path)
    if name in manifest.dependencies:
        raise package_error(f"package {name!r} is already declared", "KRY5006", manifest.path)
    if path is not None:
        candidate = (project / Path(path)).resolve()
        try:
            candidate.relative_to(project.parent)
        except ValueError as exc:
            raise package_error("path dependency escapes the project workspace", "KRY5010", manifest.path) from exc
        selected = read_manifest(candidate)
        if selected.name != name:
            raise package_error(f"path dependency is named {selected.name!r}, expected {name!r}", "KRY5002", selected.path)
        value = "path:" + os.path.relpath(candidate, project).replace("\\", "/")
    else:
        if version is None:
            raise package_error("kry add requires --version or --path", "KRY5012", manifest.path)
        VersionRequirement.parse(version, manifest.path)
        value = version
    manifest.dependencies[name] = value
    manifest.dependencies = dict(sorted(manifest.dependencies.items()))
    write_manifest(manifest)
    return manifest


def remove_dependency(root: str | Path, name: str) -> Manifest:
    manifest = read_manifest(root)
    if name not in manifest.dependencies:
        raise package_error(f"package {name!r} is not declared", "KRY5004", manifest.path)
    del manifest.dependencies[name]
    write_manifest(manifest)
    return manifest


def list_packages(root: str | Path) -> list[LockEntry]:
    lock_path = Path(root).resolve() / "kry.lock"
    if not lock_path.is_file():
        return []
    return Lockfile.load(lock_path).entries


def validate_imports(root: str | Path, source: str) -> None:
    """Validate declared/installed package imports without executing package code."""
    project = Path(root).resolve()
    manifest = read_manifest(project)
    pattern = re.compile(r"^\s*import\s+([A-Za-z][A-Za-z0-9_-]*(?:\.[A-Za-z][A-Za-z0-9_-]*)?)\s*(?:;|$)", re.MULTILINE)
    for match in pattern.finditer(source):
        path = match.group(1)
        package_name, _, module_name = path.partition(".")
        if package_name not in manifest.dependencies:
            raise package_error(f"package {package_name!r} is not declared in kry.toml", "KRY5013", manifest.path, "Add the dependency with kry add.")
        package_root = project / ".kryndel" / "packages" / package_name
        if not package_root.is_dir():
            raise package_error(f"declared package {package_name!r} is not installed", "KRY5014", package_root, "Run kry install --offline before checking imports.")
        if not module_name:
            continue
        module = package_root / "src" / f"{module_name}.kry"
        directory = package_root / "src" / module_name
        candidates = [candidate for candidate in (module, directory / "lib.kry") if candidate.is_file()]
        if len(candidates) > 1:
            raise package_error(f"import {path!r} is ambiguous", "KRY5015", package_root / "src", "Keep exactly one module path for this import.")
        if not candidates:
            raise package_error(f"module {path!r} was not found in the installed package", "KRY5014", package_root / "src", "Export the module from the package src directory.")
