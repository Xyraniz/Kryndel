"""Small host-side toolchain commands with language-independent contracts."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from .compiler import compile_project, compile_source
from .diagnostics import DiagnosticError
from .parser import parse
from .source import SourceFile
from .tokens import lex
from .vm import VM


@dataclass(frozen=True)
class KryndelTestResult:
    path: Path
    name: str


def format_source(text: str) -> str:
    """Apply the first conservative formatter contract.

    The initial formatter only removes trailing horizontal whitespace and
    canonicalizes the final newline. It does not rewrite token spacing, so it
    cannot change program meaning while the grammar is still evolving.
    """
    lines = [line.rstrip(" \t") for line in text.splitlines()]
    while lines and not lines[-1]:
        lines.pop()
    return "\n".join(lines) + "\n"


def format_file(path: str | Path, *, check: bool = False) -> bool:
    source_path = Path(path)
    original = source_path.read_text(encoding="utf-8")
    formatted = format_source(original)
    if check:
        return original == formatted
    if original != formatted:
        source_path.write_text(formatted, encoding="utf-8", newline="\n")
    return True


def discover_kryndel_tests(root: str | Path) -> list[tuple[Path, str]]:
    test_root = Path(root).resolve() / "tests"
    discovered: list[tuple[Path, str]] = []
    if not test_root.is_dir():
        return discovered
    for path in sorted(test_root.rglob("*.kry")):
        source = SourceFile(str(path), path.read_text(encoding="utf-8"))
        tokens, lexical = lex(source)
        program, parsing = parse(source, tokens)
        if lexical.has_errors or parsing.has_errors:
            raise DiagnosticError(lexical.items + parsing.items, source.text, str(path))
        for item in program.items:
            if getattr(item, "test", False):
                if not hasattr(item, "parameters") or item.parameters:
                    raise ValueError(f"test function {item.name!r} must not take parameters: {path}")
                discovered.append((path, item.name))
    return discovered


def run_kryndel_tests(root: str | Path) -> list[KryndelTestResult]:
    project = Path(root).resolve()
    has_manifest = (project / "kry.toml").is_file()
    results: list[KryndelTestResult] = []
    for path, name in discover_kryndel_tests(project):
        text = path.read_text(encoding="utf-8")
        module = compile_project(project, text, path) if has_manifest else compile_source(text, str(path))
        VM(module, output=lambda _text: None).execute(name, [])
        results.append(KryndelTestResult(path, name))
    return results


def check_reproducible(
    source: str | Path,
    root: str | Path | None = None,
) -> bool:
    source_path = Path(source)
    text = source_path.read_text(encoding="utf-8")
    if root is not None:
        first = compile_project(root, text, source_path).dumps()
        second = compile_project(root, text, source_path).dumps()
    else:
        first = compile_source(text, str(source_path)).dumps()
        second = compile_source(text, str(source_path)).dumps()
    return first == second


def verify_module(module) -> None:
    """Validate the structural invariants needed before VM execution."""
    from .bytecode import BytecodeFunction

    if module.version != 1 or not isinstance(module.name, str) or not isinstance(module.entry, str):
        raise ValueError("unsupported or malformed bytecode module header")
    if module.entry not in module.functions:
        raise ValueError(f"bytecode entry function {module.entry!r} is missing")
    for name in sorted(module.functions):
        function = module.functions[name]
        if not isinstance(function, BytecodeFunction) or function.name != name:
            raise ValueError(f"bytecode function key/name mismatch for {name!r}")
        if function.arity < 0 or len(function.parameters) < function.arity:
            raise ValueError(f"invalid arity or parameter metadata for {name!r}")
        for instruction in function.instructions:
            if not isinstance(instruction.op, str) or not instruction.op:
                raise ValueError(f"invalid instruction in {name!r}")
            if instruction.line < 0:
                raise ValueError(f"invalid source line in {name!r}")
            if instruction.op == "PUSH_CONST":
                if not isinstance(instruction.arg, int) or not 0 <= instruction.arg < len(function.constants):
                    raise ValueError(f"invalid constant index in {name!r}")
            elif instruction.op in {"JUMP", "JUMP_IF_FALSE", "JUMP_IF_TRUE"}:
                if not isinstance(instruction.arg, int) or not 0 <= instruction.arg <= len(function.instructions):
                    raise ValueError(f"invalid jump target in {name!r}")
            elif instruction.op == "CALL":
                if (
                    not isinstance(instruction.arg, (list, tuple))
                    or len(instruction.arg) != 2
                    or not isinstance(instruction.arg[0], str)
                    or not isinstance(instruction.arg[1], int)
                    or instruction.arg[1] < 0
                ):
                    raise ValueError(f"invalid CALL metadata in {name!r}")
            elif instruction.op == "MAKE_ENUM":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) not in ({"type", "variant"}, {"type", "variant", "arity"}):
                    raise ValueError(f"invalid MAKE_ENUM metadata in {name!r}")
            elif instruction.op == "MATCH_ENUM":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) != {"type", "variant", "arity"}:
                    raise ValueError(f"invalid MATCH_ENUM metadata in {name!r}")
            elif instruction.op == "BIND_ENUM":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) != {"source", "bindings", "arity"}:
                    raise ValueError(f"invalid BIND_ENUM metadata in {name!r}")
            elif instruction.op in {"MAKE_ARRAY", "MAKE_TUPLE"}:
                if not isinstance(instruction.arg, int) or instruction.arg < 0:
                    raise ValueError(f"invalid sequence arity in {name!r}")
            elif instruction.op == "INDEX" and instruction.arg is not None:
                raise ValueError(f"invalid INDEX metadata in {name!r}")


def abi_description() -> dict[str, object]:
    return {
        "format": "kryndel-abi",
        "version": 1,
        "bytecode": "kryndel-bytecode-v1",
        "entry": "main",
        "calling_convention": {
            "arguments": "left-to-right values on the operand stack",
            "return": "one value on the operand stack, with nil representing Void",
            "qualified_functions": "package.module.function",
        },
        "runtime": {
            "artifact": "KEXE v1 portable VM container",
            "host_primitives": ["memory", "io", "clock", "filesystem", "process", "bytecode reader"],
        },
        "layouts": {
            "String": "host UTF-8 scalar sequence; length counts Unicode code points",
            "Array": "immutable homogeneous sequence, MAKE_ARRAY arity then source-order values",
            "Tuple": "immutable fixed-width sequence, MAKE_TUPLE arity then source-order values",
            "Option": "nominal enum; None or Some(payload)",
            "Result": "nominal enum; Ok(payload) or Error(payload)",
        },
        "runtime_errors": {
            "KRY6101": "invalid sequence arity",
            "KRY6102": "sequence index is not Int",
            "KRY6103": "indexing requires String, Array, or Tuple",
            "KRY6104": "sequence index out of bounds",
            "KRY6105": "len requires String, Array, or Tuple",
        },
    }
