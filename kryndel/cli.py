"""Command-line interface for Kryndel."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from .artifact import ArtifactError, read_artifact, write_artifact
from .bytecode import Module
from .compiler import compile_file
from .diagnostics import DiagnosticError
from .version import __codename__, __version__
from .vm import RuntimeKryndelError, VM


DESCRIPTION = "Kryndel: a strongly typed language and native desktop toolkit."


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="kryndel", description=DESCRIPTION)
    parser.add_argument("--version", action="version", version=f"Kryndel {__version__} ({__codename__})")
    subparsers = parser.add_subparsers(dest="command", required=True)

    for name, help_text in (
        ("check", "Parse and type-check a source file."),
        ("run", "Compile and execute a source file."),
        ("dump", "Compile a source file and print deterministic bytecode JSON."),
    ):
        command = subparsers.add_parser(name, help=help_text)
        command.add_argument("source", type=Path)

    build = subparsers.add_parser("build", help="Compile a source file into a portable .kexe artifact.")
    build.add_argument("source", type=Path)
    build.add_argument("-o", "--output", type=Path)

    inspect = subparsers.add_parser("inspect", help="Inspect a portable .kexe artifact.")
    inspect.add_argument("artifact", type=Path)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    arguments = parser.parse_args(argv)
    try:
        if arguments.command == "check":
            compile_file(arguments.source)
            print(f"checked {arguments.source}")
            return 0
        if arguments.command == "run":
            module = read_artifact(arguments.source) if arguments.source.suffix == ".kexe" else compile_file(arguments.source)
            VM(module).run()
            return 0
        if arguments.command == "dump":
            print(compile_file(arguments.source).dumps(), end="")
            return 0
        if arguments.command == "build":
            module = compile_file(arguments.source)
            output = arguments.output or arguments.source.with_suffix(".kexe")
            write_artifact(module, output)
            print(f"built {output}")
            return 0
        if arguments.command == "inspect":
            module = read_artifact(arguments.artifact)
            print(f"artifact: {arguments.artifact}")
            print(f"module: {module.name}")
            print(f"entry: {module.entry}")
            for name, function in module.functions.items():
                print(f"function {name}({function.arity} parameter(s)): {len(function.instructions)} instruction(s)")
            return 0
    except (DiagnosticError, RuntimeKryndelError, ArtifactError, OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 1
    parser.error("unknown command")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
