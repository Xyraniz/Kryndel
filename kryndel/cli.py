"""Deterministic Kryndel compiler, runtime, artifact, and package CLI."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

from .artifact import ArtifactError, read_artifact, write_artifact
from .compiler import compile_file, compile_project, compile_source
from .contracts import canonical_json, core_contract_report
from .diagnostics import Diagnostic, DiagnosticError, Severity, Span
from .source import SourceFile
from .packages import Lockfile, add_dependency, init_project, install, list_packages, read_manifest, remove_dependency, validate_imports
from .version import __codename__, __version__
from .tooling import abi_description, check_reproducible, compare_lexer_fixture, document_project, format_file, host_boundary_report, lexer_snapshot, pack_project, run_kryndel_tests, verify_module
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
    for name in ("init", "new"):
        init = subparsers.add_parser(name, help="Create a Kryndel project manifest.")
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
    fmt = subparsers.add_parser("fmt", help="Format Kryndel source files.")
    fmt.add_argument("source", nargs="?", type=Path)
    fmt.add_argument("--check", action="store_true")
    test = subparsers.add_parser("test", help="Run tests written in Kryndel.")
    test.add_argument("--format", choices=("human", "json"), default="human")
    reproducible = subparsers.add_parser("reproducible", help="Check deterministic compilation.")
    reproducible.add_argument("source", nargs="?", type=Path)
    for name, help_text in (
        ("inspect-bytecode", "Inspect a bytecode JSON module."),
        ("verify-bytecode", "Verify a bytecode JSON module."),
        ("verify-artifact", "Verify a portable .kexe artifact."),
    ):
        command = subparsers.add_parser(name, help=help_text)
        command.add_argument("path", nargs="?", type=Path)
    subparsers.add_parser("abi", help="Print the stable Kryndel ABI description.")
    subparsers.add_parser("core-report", help="Validate the deterministic core-v1 fixtures.")
    doc = subparsers.add_parser("doc", help="Emit deterministic source documentation.")
    doc.add_argument("source", nargs="?", type=Path)
    doc.add_argument("-o", "--output", type=Path)
    doc.add_argument("--format", choices=("human", "json"), default="json")
    pack = subparsers.add_parser("pack", help="Create a deterministic source package.")
    pack.add_argument("-o", "--output", type=Path)
    lex = subparsers.add_parser("lex", help="Emit deterministic lexer tokens and diagnostics.")
    lex.add_argument("source", type=Path)
    lex.add_argument("--fixture", type=Path)
    subparsers.add_parser("host-report", help="Print the deterministic host-boundary inventory.")
    subparsers.add_parser("clean", help="Remove generated project bytecode artifacts.")
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
        return compile_project(root, source.read_text(encoding="utf-8"), source, locked=False)
    return compile_file(source, allowed)


def _structured_error(error: Exception, filename: str = "<kryndel>") -> str:
    if isinstance(error, DiagnosticError):
        return error.as_json()
    if isinstance(error, RuntimeKryndelError):
        match = re.search(r"\b(KRY\d{4})\b", str(error))
        code = match.group(1) if match else "KRY6002" if "bytecode" in str(error) or "malformed" in str(error) else "KRY6000"
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
        if arguments.command in {"init", "new"}:
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
        if arguments.command == "fmt":
            root = _project_root(arguments.source) or Path.cwd().resolve()
            if arguments.source is not None:
                paths = [arguments.source]
            else:
                paths = sorted(root.glob("src/**/*.kry"))
                if not paths:
                    paths = sorted(root.glob("examples/**/*.kry"))
            if not paths:
                raise OSError("no Kryndel source files found")
            statuses = [(path, format_file(path, check=arguments.check)) for path in paths]
            if arguments.check and not all(status for _, status in statuses):
                for path, status in statuses:
                    if not status:
                        print(f"would format {path}")
                return 1
            print(f"{'checked' if arguments.check else 'formatted'} {len(paths)} source file(s)")
            return 0
        if arguments.command == "test":
            root = _project_root() or Path.cwd().resolve()
            results = run_kryndel_tests(root, continue_on_failure=True)
            passed = sum(result.status == "passed" for result in results)
            failed = len(results) - passed
            if output_format == "json":
                payload = {
                    "failed": failed,
                    "format": "kryndel-test-results",
                    "passed": passed,
                    "tests": [result.as_dict(root) for result in results],
                    "version": 1,
                }
                print(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True))
            else:
                for result in results:
                    suffix = "ok" if result.status == "passed" else f"FAILED: {result.error}"
                    print(f"test {result.path}::{result.name} ... {suffix}")
                print(f"{passed} Kryndel test(s) passed; {failed} failed")
            return 1 if failed else 0
        if arguments.command == "reproducible":
            if arguments.source is None and _project_root() is None:
                candidate = Path("examples/hello.kry")
                if not candidate.is_file():
                    raise OSError("a source file is required outside a Kryndel project")
                source, root = candidate, None
            else:
                source, root = _source_path(arguments.source)
            if not check_reproducible(source, root):
                raise ValueError("compilation is not reproducible")
            print(f"reproducible {source}")
            return 0
        if arguments.command == "abi":
            print(canonical_json(abi_description()), end="")
            return 0
        if arguments.command == "core-report":
            root = _project_root() or Path.cwd().resolve()
            print(canonical_json(core_contract_report(root)), end="")
            return 0
        if arguments.command == "lex":
            source = SourceFile.from_path(arguments.source)
            if arguments.fixture is not None:
                compare_lexer_fixture(source, arguments.fixture)
            print(canonical_json(lexer_snapshot(source)), end="")
            return 0
        if arguments.command == "host-report":
            print(json.dumps(host_boundary_report(), ensure_ascii=False, indent=2, sort_keys=True))
            return 0
        if arguments.command == "doc":
            root = _project_root(arguments.source) or Path.cwd().resolve()
            payload = document_project(root, arguments.source)
            rendered = json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
            if arguments.output is not None:
                arguments.output.write_text(rendered, encoding="utf-8", newline="\n")
                print(f"documented {arguments.output}")
            else:
                print(rendered, end="")
            return 0
        if arguments.command == "pack":
            root = _project_root() or Path.cwd().resolve()
            target, checksum = pack_project(root, arguments.output)
            print(f"packed {target}")
            print(f"checksum {checksum}")
            return 0
        if arguments.command in {"inspect-bytecode", "verify-bytecode", "verify-artifact"}:
            path = arguments.path
            if path is None:
                path = Path("examples/hello.kry")
                if not path.is_file():
                    raise OSError("a bytecode or artifact path is required")
                module = compile_source(path.read_text(encoding="utf-8"), str(path))
            elif arguments.command == "verify-artifact":
                module = read_artifact(path)
            else:
                from .bytecode import Module
                module = Module.load(path)
            verify_module(module)
            if arguments.command == "inspect-bytecode":
                print(json.dumps({"module": module.name, "entry": module.entry, "functions": sorted(module.functions)}, indent=2, sort_keys=True))
            elif arguments.command == "verify-bytecode":
                print(f"verified bytecode {path or 'examples/hello.kry'}")
            else:
                print(f"verified artifact {path}")
            return 0
        if arguments.command == "clean":
            root = _project_root() or Path.cwd().resolve()
            removed = 0
            for path in sorted(root.rglob("*.kexe")):
                if path.is_file():
                    path.unlink()
                    removed += 1
            print(f"removed {removed} generated artifact(s)")
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
