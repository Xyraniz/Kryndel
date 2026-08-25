"""Deterministic Kryndel compiler, runtime, artifact, and package CLI."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .artifact import ArtifactError, read_artifact, write_artifact
from .compiler import compile_file
from .diagnostics import Diagnostic, DiagnosticError, Severity, Span
from .packages import Lockfile, add_dependency, init_project, install, list_packages, read_manifest, remove_dependency, validate_imports
from .version import __codename__, __version__
from .vm import RuntimeKryndelError, VM


DESCRIPTION = "Kryndel: a strongly typed language and portable runtime."


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="kry", description=DESCRIPTION)
    parser.add_argument("--version", action="version", version=f"Kryndel {__version__} ({__codename__})")
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name, help_text in (("check", "Parse and type-check a source file."), ("run", "Compile and execute a source file or .kexe artifact."), ("dump", "Compile a source file and print deterministic bytecode JSON.")):
        command = subparsers.add_parser(name, help=help_text)
        command.add_argument("source", nargs="?", type=Path)
        command.add_argument("--format", choices=("human", "json"), default="human")
        command.add_argument("--locked", action="store_true")
    build = subparsers.add_parser("build", help="Compile a source file into a portable .kexe artifact.")
    build.add_argument("source", nargs="?", type=Path)
    build.add_argument("-o", "--output", type=Path)
    build.add_argument("--locked", action="store_true")
    inspect = subparsers.add_parser("inspect", help="Inspect a portable .kexe artifact.")
    inspect.add_argument("artifact", type=Path)
    inspect.add_argument("--format", choices=("human", "json"), default="human")
    init = subparsers.add_parser("init", help="Create a Kryndel project manifest.")
    init.add_argument("path", nargs="?", type=Path, default=Path("."))
    add = subparsers.add_parser("add", help="Declare a Kryndel dependency.")
    add.add_argument("name")
    add.add_argument("--version")
    add.add_argument("--path", type=Path)
    remove = subparsers.add_parser("remove", help="Remove a declared Kryndel dependency.")
    remove.add_argument("name")
    for name, help_text in (("install", "Resolve and install local Kryndel packages."), ("update", "Resolve packages and rewrite kry.lock.")):
        command = subparsers.add_parser(name, help=help_text)
        command.add_argument("--offline", action="store_true")
        command.add_argument("--locked", action="store_true")
    subparsers.add_parser("list", help="List locked Kryndel packages.")
    subparsers.add_parser("tree", help="Show the locked Kryndel dependency tree.")
    return parser


def _project_root(path: Path | None = None) -> Path | None:
    start = (path if path and path.is_dir() else path.parent if path else Path.cwd()).resolve()
    for candidate in (start, *start.parents):
        if (candidate / "kry.toml").is_file():
            return candidate
    return None


def _source_path(value: Path | None) -> tuple[Path, Path | None]:
    if value is not None:
        source = value
        root = _project_root(source)
    else:
        root = _project_root()
        if root is None:
            raise OSError("a source file is required outside a Kryndel project")
        source = root / "src" / "main.kry"
    if not source.is_file():
        raise OSError(f"source file not found: {source}")
    return source, root


def _compile_project(source: Path, root: Path | None, locked: bool):
    allowed = None
    if root is not None:
        manifest = read_manifest(root)
        allowed = set(manifest.dependencies)
        if locked:
            install(root, offline=True, locked=True)
        validate_imports(root, source.read_text(encoding="utf-8"))
    return compile_file(source, allowed)


def _structured_error(error: Exception, filename: str = "<kryndel>") -> str:
    if isinstance(error, DiagnosticError):
        return error.as_json()
    if isinstance(error, RuntimeKryndelError):
        code = "KRY6002" if "bytecode" in str(error) or "malformed" in str(error) else "KRY6000"
    else:
        code = "KRY6001" if isinstance(error, ArtifactError) else "KRY5000"
    diagnostic = Diagnostic(Severity.ERROR, str(error), Span(0, 1, 1, 1), code=code)
    return json.dumps({"diagnostics": [diagnostic.as_dict(filename)]}, ensure_ascii=False, indent=2, sort_keys=True) + "\n"


def _print_error(error: Exception, output_format: str, filename: str = "<kryndel>") -> None:
    print(_structured_error(error, filename) if output_format == "json" else str(error), file=sys.stderr, end="" if output_format == "json" else "\n")


def _tree(root: Path) -> list[str]:
    manifest = read_manifest(root)
    lock_path = root / "kry.lock"
    if not lock_path.is_file():
        return [f"{manifest.name} {manifest.version}", "  (not installed)"]
    entries = {entry.name: entry for entry in Lockfile.load(lock_path).entries}
    lines = [f"{manifest.name} {manifest.version}"]
    visited: set[str] = set()

    def visit(name: str, prefix: str) -> None:
        entry = entries.get(name)
        if entry is None:
            lines.append(f"{prefix}{name} (unresolved)")
            return
        lines.append(f"{prefix}{entry.name} {entry.version}")
        if name in visited:
            return
        visited.add(name)
        for dependency in entry.dependencies:
            visit(dependency, prefix + "  ")

    for name in sorted(manifest.dependencies):
        visit(name, "  ")
    return lines


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    arguments = parser.parse_args(argv)
    output_format = getattr(arguments, "format", "human")
    try:
        if arguments.command == "init":
            manifest = init_project(arguments.path)
            print(f"initialized {manifest.path.parent}")
            return 0
        if arguments.command == "add":
            manifest = add_dependency(Path.cwd(), arguments.name, version=arguments.version, path=arguments.path)
            print(f"added {arguments.name} to {manifest.path}")
            return 0
        if arguments.command == "remove":
            manifest = remove_dependency(Path.cwd(), arguments.name)
            print(f"removed {arguments.name} from {manifest.path}")
            return 0
        if arguments.command == "install":
            lock = install(Path.cwd(), offline=arguments.offline, locked=arguments.locked)
            print(f"installed {len(lock.entries)} package(s)")
            return 0
        if arguments.command == "update":
            lock = install(Path.cwd(), offline=arguments.offline, locked=False)
            print(f"updated {len(lock.entries)} package(s)")
            return 0
        if arguments.command == "list":
            for entry in list_packages(Path.cwd()):
                print(f"{entry.name} {entry.version} ({entry.source}, {entry.checksum})")
            return 0
        if arguments.command == "tree":
            print("\n".join(_tree(Path.cwd())))
            return 0
        if arguments.command in {"check", "run", "dump", "build"}:
            if arguments.command == "run" and arguments.source is not None and arguments.source.suffix == ".kexe":
                VM(read_artifact(arguments.source)).run()
                return 0
            source, root = _source_path(arguments.source)
            module = _compile_project(source, root, arguments.locked)
            if arguments.command == "check":
                print(f"checked {source}")
                return 0
            if arguments.command == "run":
                VM(module).run()
                return 0
            if arguments.command == "dump":
                print(module.dumps(), end="")
                return 0
            output = arguments.output or source.with_suffix(".kexe")
            write_artifact(module, output)
            print(f"built {output}")
            return 0
        if arguments.command == "inspect":
            module = read_artifact(arguments.artifact)
            if output_format == "json":
                functions = [{"name": name, "arity": function.arity, "instructions": len(function.instructions)} for name, function in sorted(module.functions.items())]
                print(json.dumps({"artifact": str(arguments.artifact), "module": module.name, "entry": module.entry, "functions": functions}, indent=2, sort_keys=True))
            else:
                print(f"artifact: {arguments.artifact}")
                print(f"module: {module.name}")
                print(f"entry: {module.entry}")
                for name, function in module.functions.items():
                    print(f"function {name}({function.arity} parameter(s)): {len(function.instructions)} instruction(s)")
            return 0
    except (DiagnosticError, RuntimeKryndelError, ArtifactError, OSError, ValueError) as error:
        _print_error(error, output_format, str(getattr(arguments, "source", "<kryndel>")))
        return 1
    parser.error("unknown command")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
