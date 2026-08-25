"""Small host-side toolchain commands with language-independent contracts."""

from __future__ import annotations

import ast as python_ast
import hashlib
import inspect
import json
import textwrap
import zipfile
from dataclasses import dataclass
from pathlib import Path

from . import ast as kry_ast
from .bytecode import BYTECODE_OPCODES, BYTECODE_VERSION
from .compiler import compile_project, compile_source
from .diagnostics import DiagnosticError
from .packages import read_manifest
from .parser import parse
from .source import SourceFile
from .tokens import lex
from .types import ArrayType, FunctionType, Type
from .vm import RuntimeKryndelError, VM


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
            VM(module, output=lambda _text: None).execute(name, [])
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

    if module.version != BYTECODE_VERSION or not isinstance(module.name, str) or not isinstance(module.entry, str):
        raise ValueError("unsupported or malformed bytecode module header")
    if module.entry not in module.functions:
        raise ValueError(f"bytecode entry function {module.entry!r} is missing")
    for name in sorted(module.functions):
        function = module.functions[name]
        if not isinstance(function, BytecodeFunction) or function.name != name:
            raise ValueError(f"bytecode function key/name mismatch for {name!r}")
        if function.arity < 0 or len(function.parameters) != function.arity or any(not isinstance(parameter, str) or not parameter for parameter in function.parameters) or len(set(function.parameters)) != len(function.parameters):
            raise ValueError(f"invalid arity or parameter metadata for {name!r}")
        for instruction in function.instructions:
            if not isinstance(instruction.op, str) or not instruction.op:
                raise ValueError(f"invalid instruction in {name!r}")
            if instruction.line < 0:
                raise ValueError(f"invalid source line in {name!r}")
            if instruction.op not in BYTECODE_OPCODES:
                raise ValueError(f"unknown bytecode opcode {instruction.op!r} in {name!r}")
            if instruction.op in {"PUSH_NIL", "POP", "DUP", "INDEX", "RETURN"} and instruction.arg is not None:
                raise ValueError(f"invalid argument for {instruction.op} in {name!r}")
            if instruction.op in {"LOAD", "STORE", "STORE_RESULT", "GET_FIELD", "UNARY", "BINARY", "PUSH_CALLABLE"} and (not isinstance(instruction.arg, str) or not instruction.arg):
                raise ValueError(f"invalid string argument for {instruction.op} in {name!r}")
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
            elif instruction.op == "MAKE_STRUCT":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) != {"type", "fields"} or not isinstance(instruction.arg["type"], str) or not isinstance(instruction.arg["fields"], list) or any(not isinstance(field, str) or not field for field in instruction.arg["fields"]) or len(set(instruction.arg["fields"])) != len(instruction.arg["fields"]):
                    raise ValueError(f"invalid MAKE_STRUCT metadata in {name!r}")
            elif instruction.op == "MAKE_ENUM":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) not in ({"type", "variant"}, {"type", "variant", "arity"}) or not isinstance(instruction.arg.get("type"), str) or not isinstance(instruction.arg.get("variant"), str) or ("arity" in instruction.arg and (not isinstance(instruction.arg["arity"], int) or instruction.arg["arity"] < 0)):
                    raise ValueError(f"invalid MAKE_ENUM metadata in {name!r}")
            elif instruction.op == "MATCH_ENUM":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) != {"type", "variant", "arity"} or not isinstance(instruction.arg["type"], str) or not isinstance(instruction.arg["variant"], str) or not isinstance(instruction.arg["arity"], int) or instruction.arg["arity"] < 0:
                    raise ValueError(f"invalid MATCH_ENUM metadata in {name!r}")
            elif instruction.op == "BIND_ENUM":
                if not isinstance(instruction.arg, dict) or set(instruction.arg) != {"source", "bindings", "arity"} or not isinstance(instruction.arg["source"], str) or not isinstance(instruction.arg["bindings"], list) or any(not isinstance(binding, str) or not binding for binding in instruction.arg["bindings"]) or len(set(instruction.arg["bindings"])) != len(instruction.arg["bindings"]) or not isinstance(instruction.arg["arity"], int) or instruction.arg["arity"] < 0 or instruction.arg["arity"] != len(instruction.arg["bindings"]):
                    raise ValueError(f"invalid BIND_ENUM metadata in {name!r}")
            elif instruction.op in {"MAKE_ARRAY", "MAKE_TUPLE"}:
                if not isinstance(instruction.arg, int) or instruction.arg < 0:
                    raise ValueError(f"invalid sequence arity in {name!r}")
            elif instruction.op == "INDEX" and instruction.arg is not None:
                raise ValueError(f"invalid INDEX metadata in {name!r}")


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
            elif isinstance(operator, python_ast.In) and isinstance(comparator, (python_ast.Tuple, python_ast.List)):
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
    return {
        "contract": "kryndel-host-boundary",
        "intrinsics": intrinsics,
        "layers": percentages,
        "state_counts": {"Kryndel": 0, "host temporal": len(intrinsics), "no implementado": 0},
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
