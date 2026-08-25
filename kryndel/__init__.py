"""Kryndel language toolchain.

Kryndel is a dependency-free, strongly typed programming language prototype
implemented with Python's standard library only.  The package exposes the
front-end, bytecode compiler, virtual machine, diagnostics, and version data.
"""

from .compiler import compile_project, compile_source
from .diagnostics import DiagnosticError
from .source import SourceFile
from .version import __version__
from .vm import VirtualMachine

__all__ = [
    "DiagnosticError",
    "SourceFile",
    "VirtualMachine",
    "__version__",
    "compile_project",
    "compile_source",
]
