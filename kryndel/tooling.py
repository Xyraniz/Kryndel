"""Small host-side toolchain commands with language-independent contracts."""

from __future__ import annotations

import ast as python_ast
import hashlib
import inspect
import json
import math
import re
import textwrap
import zipfile
from dataclasses import dataclass
from pathlib import Path

from . import ast as kry_ast
from .bytecode import BYTECODE_OPCODES, BYTECODE_VERSION, BytecodeFunction, Instruction
from .compiler import compile_project, compile_source
from .diagnostics import DiagnosticError
from .filesystem import RootedFileSystem
from .modules import ModuleGraph
from .packages import read_manifest
from .parser import parse
from .source import SourceFile
from .tokens import lex
from .types import ArrayType, FunctionType, Type
from .vm import RuntimeKryndelError, VM
from .wire import token_records


@dataclass(frozen=True)
class KryndelTestResult:
    path: Path
    name: str
    status: str = "passed"
    error: str | None = None

    def as_dict(self, root: str | Path) -> dict[str, str | None]:
        project = Path(root).resolve()
        try:
            relative_path = self.path.resolve().relative_to(project)
        except ValueError:
            relative_path = self.path
        return {
            "error": self.error,
            "file": relative_path.as_posix(),
            "name": self.name,
            "status": self.status,
        }


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


def run_kryndel_tests(
    root: str | Path,
    *,
    continue_on_failure: bool = False,
) -> list[KryndelTestResult]:
    project = Path(root).resolve()
    has_manifest = (project / "kry.toml").is_file()
    results: list[KryndelTestResult] = []
    for path, name in discover_kryndel_tests(project):
        try:
            text = path.read_text(encoding="utf-8")
            module = compile_project(project, text, path) if has_manifest else compile_source(text, str(path))
            verify_execution(module)
            VM(
                module,
                output=lambda _text: None,
                filesystem=RootedFileSystem(project if has_manifest else path.parent),
            ).execute(name, [])
        except (DiagnosticError, RuntimeKryndelError, OSError, ValueError) as error:
            result = KryndelTestResult(path, name, "failed", str(error))
            results.append(result)
            if not continue_on_failure:
                raise
        else:
            results.append(KryndelTestResult(path, name))
    return results


def document_source(path: str | Path, display_path: str | None = None) -> dict[str, object]:
    """Return deterministic public/source documentation for one Kryndel file."""
    source_path = Path(path)
    source = SourceFile(display_path or str(source_path), source_path.read_text(encoding="utf-8"))
    tokens, lexical = lex(source)
    program, parsing = parse(source, tokens)
    if lexical.has_errors or parsing.has_errors:
        raise DiagnosticError(lexical.items + parsing.items, source.text, source.name)
    declarations: list[dict[str, object]] = []
    for item in program.items:
        if isinstance(item, kry_ast.FunctionDecl):
            declarations.append(
                {
                    "kind": "function",
                    "name": item.name,
                    "parameters": [
                        {"name": parameter.name, "type": parameter.type_name.name}
                        for parameter in item.parameters
                    ],
                    "public": item.public,
                    "return_type": item.return_type.name,
                    "test": item.test,
                }
            )
        elif isinstance(item, kry_ast.StructDecl):
            declarations.append(
                {
                    "fields": [
                        {"name": field.name, "type": field.type_name.name}
                        for field in item.fields
                    ],
                    "kind": "struct",
                    "name": item.name,
                    "public": item.public,
                }
            )
        elif isinstance(item, kry_ast.EnumDecl):
            declarations.append(
                {
                    "kind": "enum",
                    "name": item.name,
                    "public": item.public,
                    "variants": [
                        {
                            "name": variant.name,
                            "payload_types": [payload.name for payload in variant.payload_types],
                        }
                        for variant in item.variants
                    ],
                }
            )
    return {"declarations": declarations, "file": source.name, "version": 1}


def document_project(root: str | Path, source: str | Path | None = None) -> dict[str, object]:
    project = Path(root).resolve()
    if source is not None:
        paths = [Path(source)]
    else:
        paths = sorted((project / "src").rglob("*.kry")) if (project / "src").is_dir() else []
        if not paths:
            paths = sorted((project / "examples").rglob("*.kry")) if (project / "examples").is_dir() else []
    if not paths:
        raise OSError("no Kryndel source files found")
    files = []
    for path in paths:
        absolute = path.resolve()
        try:
            display = absolute.relative_to(project).as_posix()
        except ValueError:
            display = Path(path).name
        files.append(document_source(absolute, display))
    package_name = None
    manifest_path = project / "kry.toml"
    if manifest_path.is_file():
        package_name = read_manifest(manifest_path).name
    return {
        "contract": "kryndel-documentation",
        "files": files,
        "package": package_name,
        "version": 1,
    }


def pack_project(root: str | Path, output: str | Path | None = None) -> tuple[Path, str]:
    """Create a deterministic source package without executing package files."""
    project = Path(root).resolve()
    manifest = read_manifest(project)
    source_root = project / "src"
    if manifest.path.is_symlink():
        raise ValueError("manifest must not be a symlink")
    if source_root.is_symlink() or not source_root.is_dir():
        raise OSError("project source directory src is missing or is a symlink")
    paths = [manifest.path, *sorted(source_root.rglob("*.kry"))]
    for path in paths:
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"package input is not a regular file: {path}")
    target = Path(output).resolve() if output is not None else project / f"{manifest.name}-{manifest.version}.krypkg"
    if target.exists() and target.is_symlink():
        raise ValueError(f"package output must not be a symlink: {target}")
    target.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(target, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in paths:
            relative = path.relative_to(project).as_posix()
            info = zipfile.ZipInfo(relative, date_time=(1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, path.read_bytes())
    checksum = hashlib.sha256(target.read_bytes()).hexdigest()
    return target, checksum


def lexer_snapshot(source: SourceFile) -> dict[str, object]:
    """Return a deterministic lexer snapshot for differential fixtures."""
    tokens, diagnostics = lex(source)
    filename = Path(source.name).name or "<source>"
    return {
        "contract": "kryndel-lexer",
        "diagnostics": [item.as_dict(filename) for item in diagnostics.items],
        "file": filename,
        "tokens": token_records(tokens),
        "version": 1,
    }


def parser_snapshot(source: SourceFile) -> dict[str, object]:
    """Return a deterministic parser/AST snapshot with lexer and parser diagnostics."""
    tokens, lexical = lex(source)
    program, parsing = parse(source, tokens)
    filename = Path(source.name).name or "<source>"
    return {
        "ast": program.as_dict(),
        "contract": "kryndel-parser",
        "diagnostics": [item.as_dict(filename) for item in [*lexical.items, *parsing.items]],
        "file": filename,
        "version": 1,
    }


def compare_parser_fixture(source: SourceFile, fixture_path: str | Path) -> None:
    """Compare the bootstrap parser/AST output with a frozen fixture oracle."""
    from .contracts import canonical_json

    fixture = json.loads(Path(fixture_path).read_text(encoding="utf-8"))
    expected = fixture.get("snapshot", fixture)
    actual = parser_snapshot(source)
    if expected != actual:
        raise ValueError(
            "parser fixture differs: "
            + canonical_json({"actual": actual, "expected": expected})
        )


def module_graph_snapshot(project: str | Path, source: SourceFile) -> dict[str, object]:
    """Return a path-independent module graph and exported-interface snapshot."""
    graph = ModuleGraph(project, source.text, source.name).load()
    modules = []
    for key in sorted(graph.records):
        record = graph.records[key]
        relative = record.path.relative_to(record.package_root).as_posix()
        modules.append(
            {
                "imports": sorted(statement.path for statement in record.imports),
                "module_id": record.module_id,
                "package": record.package_name,
                "path": relative,
                "public_symbols": sorted(
                    item.name
                    for item in record.program.items
                    if isinstance(item, (kry_ast.FunctionDecl, kry_ast.StructDecl, kry_ast.EnumDecl)) and item.public
                ),
            }
        )
    interfaces = graph.interfaces()
    function_types = {
        name: {
            "parameters": [parameter.name for parameter in function.parameters],
            "return": function.return_type.name,
        }
        for name, function in sorted(interfaces.function_types.items())
    }
    return {
        "contract": "kryndel-module-graph",
        "function_types": function_types,
        "modules": modules,
        "version": 1,
    }


def compiler_snapshot(source: SourceFile) -> dict[str, object]:
    """Return deterministic linked bytecode for a source snapshot."""
    module = compile_source(source.text, Path(source.name).name or "<source>")
    return {
        "bytecode": json.loads(module.dumps()),
        "contract": "kryndel-compiler",
        "version": 1,
    }


def compare_lexer_fixture(source: SourceFile, fixture_path: str | Path) -> None:
    """Compare the bootstrap lexer with a frozen fixture oracle byte for byte."""
    from .contracts import canonical_json

    fixture = json.loads(Path(fixture_path).read_text(encoding="utf-8"))
    expected = fixture.get("snapshot", fixture)
    actual = lexer_snapshot(source)
    if expected != actual:
        raise ValueError(
            "lexer fixture differs: "
            + canonical_json({"actual": actual, "expected": expected})
        )


def check_reproducible(source: str | Path, root: str | Path | None = None) -> bool:
    source_path = Path(source)
    text = source_path.read_text(encoding="utf-8")
    if root is not None:
        first = compile_project(root, text, source_path).dumps()
        second = compile_project(root, text, source_path).dumps()
    else:
        first = compile_source(text, str(source_path)).dumps()
        second = compile_source(text, str(source_path)).dumps()
    return first == second


_BUILTIN_ARITIES: dict[str, int | None] = {
    "print": None,
    "println": None,
    "str": 1,
    "int": 1,
    "float": 1,
    "len": 1,
    "bytes": 1,
    "string_to_bytes": 1,
    "bytes_to_string": 1,
    "assert": 1,
    "assert_eq": 2,
    "abs": 1,
    "sqrt": 1,
    "clock": 0,
    "array_push": 2,
    "fs.read_bytes": 1,
    "fs.read_text": 1,
    "fs.write_bytes": 2,
    "fs.list_dir": 1,
    "fs.stat": 1,
    "ui.window": 3,
    "ui.label": 2,
    "ui.button": 2,
    "ui.vbox": 1,
    "ui.hbox": 1,
    "ui.set_text": 2,
    "ui.on_click": 2,
    "ui.show": 1,
    "ui.run": 0,
}


def _portable_constant(value: object) -> bool:
    return value is None or type(value) in {bool, int, str} or (type(value) is float and math.isfinite(value))


def _require_stack_metadata(instruction: Instruction, function: BytecodeFunction) -> tuple[int, int]:
    """Return (required depth, delta) for one verified instruction."""
    op = instruction.op
    arg = instruction.arg
    if op in {"PUSH_CONST", "PUSH_NIL", "PUSH_CALLABLE", "LOAD"}:
        return 0, 1
    if op in {"STORE", "POP", "JUMP_IF_FALSE", "JUMP_IF_TRUE"}:
        return 1, -1
    if op in {"STORE_RESULT", "MATCH_ENUM", "GET_FIELD", "UNARY"}:
        return 1, 0
    if op == "DUP":
        return 1, 1
    if op == "MAKE_STRUCT":
        count = len(arg["fields"])
        return count, 1 - count
    if op in {"MAKE_ENUM", "MAKE_ARRAY", "MAKE_TUPLE"}:
        count = arg.get("arity", 0) if op == "MAKE_ENUM" else arg
        return count, 1 - count
    if op in {"INDEX", "BINARY"}:
        return 2, -1
    if op == "CALL":
        return arg[1], 1 - arg[1]
    if op == "RETURN":
        return 1, -1
    if op == "JUMP" or op == "BIND_ENUM":
        return 0, 0
    raise ValueError(f"KRY7005 unsupported stack effect for {op!r} in {function.name!r}")


def _verification_error(code: str, message: str) -> ValueError:
    return ValueError(f"{code} {message}")


def verify_module(module) -> None:
    """Validate the structural invariants needed before VM execution."""
    if (
        type(getattr(module, "version", None)) is not int
        or module.version != BYTECODE_VERSION
        or type(getattr(module, "name", None)) is not str
        or not module.name
        or type(getattr(module, "entry", None)) is not str
        or not module.entry
        or not isinstance(getattr(module, "functions", None), dict)
        or not module.functions
    ):
        raise _verification_error("KRY7001", "unsupported or malformed bytecode module header")
    if module.entry not in module.functions:
        raise _verification_error("KRY7002", f"bytecode entry function {module.entry!r} is missing")
    if any(type(name) is not str or not name for name in module.functions):
        raise _verification_error("KRY7003", "bytecode function names must be non-empty strings")
    for name in sorted(module.functions):
        function = module.functions[name]
        if type(name) is not str or not name or not isinstance(function, BytecodeFunction) or function.name != name:
            raise _verification_error("KRY7003", f"bytecode function key/name mismatch for {name!r}")
        if (
            type(function.arity) is not int
            or function.arity < 0
            or not isinstance(function.parameters, list)
            or len(function.parameters) != function.arity
            or any(type(parameter) is not str or not parameter for parameter in function.parameters)
            or len(set(function.parameters)) != len(function.parameters)
        ):
            raise _verification_error("KRY7004", f"invalid arity or parameter metadata for {name!r}")
        if not isinstance(function.constants, list) or any(not _portable_constant(value) for value in function.constants):
            raise _verification_error("KRY7004", f"invalid constant table for {name!r}")
        if not isinstance(function.instructions, list):
            raise _verification_error("KRY7005", f"invalid instruction table in {name!r}")
        for instruction in function.instructions:
            if not isinstance(instruction, Instruction) or type(instruction.op) is not str or not instruction.op:
                raise _verification_error("KRY7005", f"invalid instruction in {name!r}")
            if type(instruction.line) is not int or instruction.line < 0:
                raise _verification_error("KRY7005", f"invalid source line in {name!r}")
            if instruction.op not in BYTECODE_OPCODES:
                raise _verification_error("KRY7005", f"unknown bytecode opcode {instruction.op!r} in {name!r}")
            if instruction.op in {"PUSH_NIL", "POP", "DUP", "INDEX", "RETURN"} and instruction.arg is not None:
                raise _verification_error("KRY7006", f"invalid argument for {instruction.op} in {name!r}")
            if instruction.op in {"LOAD", "STORE", "STORE_RESULT", "GET_FIELD", "UNARY", "BINARY", "PUSH_CALLABLE"} and (type(instruction.arg) is not str or not instruction.arg):
                raise _verification_error("KRY7006", f"invalid string argument for {instruction.op} in {name!r}")
            if instruction.op == "PUSH_CONST":
                if type(instruction.arg) is not int or not 0 <= instruction.arg < len(function.constants):
                    raise _verification_error("KRY7006", f"invalid constant index in {name!r}")
            elif instruction.op in {"JUMP", "JUMP_IF_FALSE", "JUMP_IF_TRUE"}:
                if type(instruction.arg) is not int or not 0 <= instruction.arg <= len(function.instructions):
                    raise _verification_error("KRY7006", f"invalid jump target in {name!r}")
            elif instruction.op == "CALL":
                if (
                    not isinstance(instruction.arg, (list, tuple))
                    or len(instruction.arg) != 2
                    or type(instruction.arg[0]) is not str
                    or not instruction.arg[0]
                    or type(instruction.arg[1]) is not int
                    or instruction.arg[1] < 0
                ):
                    raise _verification_error("KRY7006", f"invalid CALL metadata in {name!r}")
            elif instruction.op == "MAKE_STRUCT":
                metadata = instruction.arg
                if (
                    not isinstance(metadata, dict)
                    or set(metadata) != {"type", "fields"}
                    or type(metadata["type"]) is not str
                    or not metadata["type"]
                    or not isinstance(metadata["fields"], list)
                    or any(type(field) is not str or not field for field in metadata["fields"])
                    or len(set(metadata["fields"])) != len(metadata["fields"])
                ):
                    raise _verification_error("KRY7006", f"invalid MAKE_STRUCT metadata in {name!r}")
            elif instruction.op == "MAKE_ENUM":
                metadata = instruction.arg
                if (
                    not isinstance(metadata, dict)
                    or set(metadata) not in ({"type", "variant"}, {"type", "variant", "arity"})
                    or type(metadata.get("type")) is not str
                    or not metadata.get("type")
                    or type(metadata.get("variant")) is not str
                    or not metadata.get("variant")
                    or ("arity" in metadata and (type(metadata["arity"]) is not int or metadata["arity"] < 0))
                ):
                    raise _verification_error("KRY7006", f"invalid MAKE_ENUM metadata in {name!r}")
            elif instruction.op == "MATCH_ENUM":
                metadata = instruction.arg
                if (
                    not isinstance(metadata, dict)
                    or set(metadata) != {"type", "variant", "arity"}
                    or type(metadata.get("type")) is not str
                    or not metadata.get("type")
                    or type(metadata.get("variant")) is not str
                    or not metadata.get("variant")
                    or type(metadata.get("arity")) is not int
                    or metadata["arity"] < 0
                ):
                    raise _verification_error("KRY7006", f"invalid MATCH_ENUM metadata in {name!r}")
            elif instruction.op == "BIND_ENUM":
                metadata = instruction.arg
                named_bindings = [binding for binding in metadata.get("bindings", [])] if isinstance(metadata, dict) and isinstance(metadata.get("bindings"), list) else []
                if (
                    not isinstance(metadata, dict)
                    or set(metadata) != {"source", "bindings", "arity"}
                    or type(metadata.get("source")) is not str
                    or not metadata.get("source")
                    or not isinstance(metadata.get("bindings"), list)
                    or any(type(binding) is not str for binding in metadata["bindings"])
                    or len(set(binding for binding in named_bindings if binding)) != len([binding for binding in named_bindings if binding])
                    or type(metadata.get("arity")) is not int
                    or metadata["arity"] < 0
                    or metadata["arity"] != len(metadata["bindings"])
                ):
                    raise _verification_error("KRY7006", f"invalid BIND_ENUM metadata in {name!r}")
            elif instruction.op in {"MAKE_ARRAY", "MAKE_TUPLE"}:
                if type(instruction.arg) is not int or instruction.arg < 0:
                    raise _verification_error("KRY7006", f"invalid sequence arity in {name!r}")


def _verify_calls(module) -> None:
    for name in sorted(module.functions):
        function = module.functions[name]
        for instruction in function.instructions:
            if instruction.op != "CALL":
                continue
            target, count = instruction.arg
            expected = module.functions[target].arity if target in module.functions else _BUILTIN_ARITIES.get(target, -1)
            if expected == -1:
                raise _verification_error("KRY7007", f"unknown callable {target!r} in {name!r}")
            if expected is not None and expected != count:
                raise _verification_error("KRY7007", f"CALL arity mismatch for {target!r} in {name!r}")


def verify_execution(module, *, max_states: int = 1024, max_stack: int = 4096) -> None:
    """Verify reachable operand-stack depths for every control-flow path."""
    verify_module(module)
    _verify_calls(module)
    if type(max_states) is not int or max_states <= 0 or type(max_stack) is not int or max_stack <= 0:
        raise ValueError("KRY7008 invalid verifier limits")
    for name in sorted(module.functions):
        function = module.functions[name]
        states: list[tuple[int, int]] = [(0, 0)]
        seen = {(0, 0)}
        depth_at_pc = {0: 0}
        index = 0
        while index < len(states):
            pc, depth = states[index]
            if pc == len(function.instructions):
                raise _verification_error("KRY7008", f"reachable control flow ended without RETURN in {name!r}")
            required, delta = _require_stack_metadata(function.instructions[pc], function)
            if depth < required:
                raise _verification_error("KRY7008", f"operand stack underflow in {name!r} at instruction {pc}")
            next_depth = depth + delta
            if next_depth < 0:
                raise _verification_error("KRY7008", f"operand stack became negative in {name!r} at instruction {pc}")
            if next_depth > max_stack:
                raise _verification_error("KRY7008", f"operand stack exceeded limit in {name!r}")
            instruction = function.instructions[pc]
            if instruction.op == "RETURN":
                index += 1
                continue
            if instruction.op == "JUMP":
                targets = (instruction.arg,)
            elif instruction.op in {"JUMP_IF_FALSE", "JUMP_IF_TRUE"}:
                targets = (instruction.arg, pc + 1)
            else:
                targets = (pc + 1,)
            for target in targets:
                previous_depth = depth_at_pc.get(target)
                if previous_depth is not None and previous_depth != next_depth:
                    raise _verification_error("KRY7008", f"incompatible operand stack depth at join in {name!r} at instruction {target}")
                depth_at_pc[target] = next_depth
                state = (target, next_depth)
                if state in seen:
                    continue
                seen.add(state)
                states.append(state)
                if len(states) > max_states:
                    raise _verification_error("KRY7008", f"operand stack analysis exceeded limit in {name!r}")
            index += 1


_HOST_ERROR_CODES = {
    "print": "KRY6301",
    "println": "KRY6301",
    "str": "KRY6202",
    "int": "KRY6202",
    "float": "KRY6202",
    "len": "KRY6105",
    "bytes": "KRY6202",
    "string_to_bytes": "KRY6201",
    "bytes_to_string": "KRY6201",
    "assert": "KRY6401",
    "assert_eq": "KRY6402",
    "abs": "KRY6202",
    "sqrt": "KRY6202",
    "clock": "KRY6301",
    "fs.read_bytes": "KRY6301",
    "fs.read_text": "KRY6301",
    "fs.write_bytes": "KRY6301",
    "fs.list_dir": "KRY6301",
    "fs.stat": "KRY6301",
    "array_push": "KRY6203",
    "ui.window": "KRY6301",
    "ui.label": "KRY6301",
    "ui.button": "KRY6301",
    "ui.vbox": "KRY6301",
    "ui.hbox": "KRY6301",
    "ui.set_text": "KRY6301",
    "ui.on_click": "KRY6301",
    "ui.show": "KRY6301",
    "ui.run": "KRY6301",
}

_HOST_FIXTURES = {
    "bytes": "tests/fixtures/bytes-v1.json",
    "string_to_bytes": "tests/fixtures/bytes-v1.json",
    "bytes_to_string": "tests/fixtures/bytes-v1.json",
    "assert": "tests/fixtures/stdlib-testing-v1.json",
    "assert_eq": "tests/fixtures/stdlib-testing-v1.json",
    "len": "tests/fixtures/value-runtime-v1.json",
    "fs.read_bytes": "tests/fixtures/filesystem-v1.json",
    "fs.read_text": "tests/fixtures/filesystem-v1.json",
    "fs.write_bytes": "tests/fixtures/filesystem-v1.json",
    "fs.list_dir": "tests/fixtures/filesystem-v1.json",
    "fs.stat": "tests/fixtures/filesystem-v1.json",
    "array_push": "tests/fixtures/collections-v1.json",
    "ui.window": "examples/ui_tree.kry",
    "ui.label": "examples/ui_tree.kry",
    "ui.button": "examples/ui_tree.kry",
    "ui.vbox": "examples/ui_tree.kry",
    "ui.hbox": "examples/ui_tree.kry",
    "ui.set_text": "examples/ui_tree.kry",
    "ui.on_click": "examples/ui_tree.kry",
    "ui.show": "examples/ui_tree.kry",
    "ui.run": "examples/ui_tree.kry",
}

_HOST_REPLACEMENTS = {
    "bytes": "Kryndel BytesValue and octet validator",
    "string_to_bytes": "Kryndel UTF-8 encoder over BytesValue",
    "bytes_to_string": "Kryndel UTF-8 decoder over BytesValue",
    "len": "Kryndel sequence length implementation",
    "assert": "Kryndel testing assertion primitive",
    "assert_eq": "Kryndel testing equality assertion primitive",
    "clock": "controlled monotonic-clock capability",
    "fs.read_bytes": "Kryndel-native filesystem API over explicit capability",
    "fs.read_text": "Kryndel-native UTF-8 filesystem API over explicit capability",
    "fs.write_bytes": "Kryndel-native filesystem API over explicit capability",
    "fs.list_dir": "Kryndel-native filesystem metadata values",
    "fs.stat": "Kryndel-native filesystem metadata values",
    "array_push": "Kryndel-native immutable Array value operation",
}

_AUTONOMY_STATES = (
    "Kryndel-native",
    "host capability nativa mínima",
    "bootstrap Python",
    "no implementado",
)

_AUTONOMY_COMPONENTS = (
    {
        "component": "value runtime",
        "evidence": "stdlib/core/value.kry + kryndel/vm.py",
        "replacement": "native tagged values implementing value-layout-v1",
        "status": "bootstrap Python",
    },
    {
        "component": "bytecode and KEXE reader",
        "evidence": "kryndel/bytecode.py + kryndel/artifact.py",
        "replacement": "native canonical JSON/KEXE reader and verifier",
        "status": "bootstrap Python",
    },
    {
        "component": "compiler and front end",
        "evidence": "kryndel/tokens.py, parser.py, type_checker.py, compiler.py",
        "replacement": "native lexer/parser/checker/compiler",
        "status": "bootstrap Python",
    },
    {
        "component": "runtime and VM",
        "evidence": "kryndel/vm.py",
        "replacement": "native VM with explicit capability table",
        "status": "bootstrap Python",
    },
    {
        "component": "filesystem capability",
        "evidence": "kryndel/filesystem.py + kryndel/vm.py",
        "replacement": "native rooted filesystem capability",
        "status": "bootstrap Python",
    },
    {
        "component": "package manager and module loader",
        "evidence": "kryndel/packages.py + kryndel/modules.py",
        "replacement": "native offline resolver, loader, and staging",
        "status": "bootstrap Python",
    },
    {
        "component": "productive CLI",
        "evidence": "kryndel/cli.py + kryndel/__main__.py",
        "replacement": "native kry executable isolated from bootstrap PATH",
        "status": "bootstrap Python",
    },
    {
        "component": "formatter utility",
        "evidence": "tools/kry-format",
        "replacement": "native formatter integrated into the productive CLI",
        "status": "host capability nativa mínima",
    },
    {
        "component": "seed utilities",
        "evidence": "tools/kry-seed and tools/kry-native-run",
        "replacement": "Kryndel-generated runtime replacing fixed shell materializers",
        "status": "host capability nativa mínima",
    },
    {
        "component": "KEXE framing utility",
        "evidence": "tools/kry-kexe-check",
        "replacement": "native artifact reader and verifier in the stage-2 runtime",
        "status": "host capability nativa mínima",
    },
    {
        "component": "documentation and packer",
        "evidence": "kryndel/tooling.py",
        "replacement": "native doc and reproducible pack commands",
        "status": "bootstrap Python",
    },
    {
        "component": "core bundle",
        "evidence": "no bundle command or target artifact exists",
        "replacement": "reproducible target-specific bundle without bootstrap files",
        "status": "no implementado",
    },
    {
        "component": "self-hosting",
        "evidence": "docs/roadmap-status.md formal gate",
        "replacement": "two equivalent clean native rebuilds",
        "status": "no implementado",
    },
    {
        "component": "UI backend",
        "evidence": "UINode dispatches in kryndel/vm.py",
        "replacement": "exclude UI from core or define a separate native capability",
        "status": "no implementado",
    },
)


def autonomy_status_matrix() -> dict[str, object]:
    """Return the explicit four-state implementation ownership matrix."""
    counts = {state: 0 for state in _AUTONOMY_STATES}
    for component in _AUTONOMY_COMPONENTS:
        counts[component["status"]] += 1
    return {
        "components": [dict(component) for component in _AUTONOMY_COMPONENTS],
        "counts": counts,
        "states": list(_AUTONOMY_STATES),
        "version": 1,
    }


def autonomy_audit_report(root: str | Path = ".") -> dict[str, object]:
    """Identify the normal Python route and every documented replacement gap."""
    project = Path(root).resolve()
    bootstrap_modules = sorted(
        path.relative_to(project).as_posix()
        for path in (project / "kryndel").glob("*.py")
        if path.is_file()
    )
    explicit_python_invocations = []
    pattern = re.compile(r"(?:python3?\s+-m\s+kryndel|PYTHONPATH=.*python3?\s+-m\s+kryndel)")
    for path in sorted(project.rglob("*")):
        if not path.is_file() or ".git" in path.parts or path.suffix not in {".md", ".py", ".yaml", ".yml", ".sh"}:
            continue
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except UnicodeDecodeError:
            continue
        for number, line in enumerate(lines, 1):
            if pattern.search(line):
                explicit_python_invocations.append(
                    {
                        "file": path.relative_to(project).as_posix(),
                        "line": number,
                        "text": line.strip(),
                    }
                )
    matrix = autonomy_status_matrix()
    return {
        "bootstrap_modules": bootstrap_modules,
        "contract": "kryndel-autonomy-audit",
        "normal_python_route": {
            "entrypoint": "python3 -m kryndel",
            "vm": "kryndel/vm.py",
            "source_seams": "stdlib/**/*.kry interpreted by the bootstrap VM",
        },
        "pending_replacements": [
            component for component in matrix["components"]
            if component["status"] != "Kryndel-native"
        ],
        "python_invocations": explicit_python_invocations,
        "status_matrix": matrix,
        "version": 1,
    }


def _builtin_names_in_vm() -> set[str]:
    """Extract dispatch names so a new VM intrinsic cannot evade the report."""
    tree = python_ast.parse(textwrap.dedent(inspect.getsource(VM.builtin)))
    names: set[str] = set()
    for node in python_ast.walk(tree):
        if not isinstance(node, python_ast.Compare) or not isinstance(node.left, python_ast.Name):
            continue
        if node.left.id != "name":
            continue
        for operator, comparator in zip(node.ops, node.comparators):
            if isinstance(operator, python_ast.Eq) and isinstance(comparator, python_ast.Constant) and isinstance(comparator.value, str):
                names.add(comparator.value)
            elif isinstance(operator, python_ast.In) and isinstance(comparator, (python_ast.Tuple, python_ast.List, python_ast.Set)):
                names.update(
                    item.value
                    for item in comparator.elts
                    if isinstance(item, python_ast.Constant) and isinstance(item.value, str)
                )
    return names


def _type_label(type_: Type) -> str:
    if isinstance(type_, ArrayType):
        return f"Array<{_type_label(type_.element)}>"
    return type_.name


def _signature(name: str, function: FunctionType) -> str:
    parameters = ", ".join(_type_label(parameter) for parameter in function.parameters)
    if function.variadic:
        parameters = parameters + ("..." if parameters else "...")
    return f"{name}({parameters}) -> {_type_label(function.return_type)}"


def host_boundary_report() -> dict[str, object]:
    """Return a deterministic, offline inventory of every VM intrinsic."""
    from .types import BUILTIN_FUNCTIONS

    dispatched = _builtin_names_in_vm()
    declared = set(BUILTIN_FUNCTIONS)
    missing_from_signatures = sorted(dispatched - declared)
    missing_from_dispatch = sorted(declared - dispatched)
    missing_metadata = sorted(declared - set(_HOST_ERROR_CODES))
    if missing_from_signatures or missing_from_dispatch or missing_metadata:
        raise ValueError(
            "host intrinsic inventory mismatch: "
            f"without signatures={missing_from_signatures}, without VM dispatch={missing_from_dispatch}, "
            f"without metadata={missing_metadata}"
        )
    intrinsics = []
    for name in sorted(declared):
        function = BUILTIN_FUNCTIONS[name]
        intrinsics.append(
            {
                "error_code": _HOST_ERROR_CODES.get(name),
                "fixture": _HOST_FIXTURES.get(name, "tests/fixtures/host-boundary-v1.json"),
                "host_module": "kryndel/vm.py",
                "name": name,
                "replacement": _HOST_REPLACEMENTS.get(name, "Kryndel-native implementation"),
                "signature": _signature(name, function),
                "state": "host temporal",
            }
        )
    layers = (
        "lexer",
        "parser",
        "checker",
        "compiler",
        "verifier",
        "runtime",
        "stdlib",
        "filesystem",
        "io",
        "cli",
    )
    percentages = {
        layer: {"Kryndel": 0, "host temporal": 100, "no implementado": 0}
        for layer in layers
    }
    matrix = autonomy_status_matrix()
    return {
        "autonomy": matrix,
        "contract": "kryndel-host-boundary",
        "intrinsics": intrinsics,
        "layers": percentages,
        "state_counts": {"Kryndel": 0, "host temporal": len(intrinsics), "no implementado": 0},
        "status_matrix": matrix,
        "unlisted_intrinsics": [],
        "version": 1,
    }


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
            "Bytes": "immutable octet sequence, created by bytes(Array) or string_to_bytes(String), indexed as Int octets",
            "Array": "immutable homogeneous sequence, MAKE_ARRAY arity then source-order values",
            "Tuple": "immutable fixed-width sequence, MAKE_TUPLE arity then source-order values",
            "Option": "nominal enum; None or Some(payload)",
            "Result": "nominal enum; Ok(payload) or Error(payload)",
        },
        "runtime_errors": {
            "KRY6101": "invalid sequence arity",
            "KRY6102": "sequence index is not Int",
            "KRY6103": "indexing requires String, Array, Tuple, or Bytes",
            "KRY6104": "sequence index out of bounds",
            "KRY6105": "len requires String, Array, Tuple, or Bytes",
            "KRY6201": "invalid UTF-8",
            "KRY6202": "conversion is not representable",
            "KRY6203": "incompatible collection operation",
            "KRY6204": "value is absent",
            "KRY6301": "IO failure",
            "KRY6302": "file does not exist",
            "KRY6303": "path escapes project root",
            "KRY6304": "malformed program input",
            "KRY6305": "malformed bytecode",
            "KRY6401": "assertion condition is false",
            "KRY6402": "assertion values are unequal",
        },
    }
