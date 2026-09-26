#!/usr/bin/env python3
"""Generate direct C++ wrappers for public PyQt QObject methods.

The generator reads the matching PyQt source distribution's SIP declarations and
can merge SIP roots from separately released add-ons. Generated application code
is C++ and has no Python runtime dependency.
It currently emits overloads whose arguments and results have native JSON
conversions in qt6_bridge.cpp. Unsupported declarations stay out of the native
coverage count rather than being treated as implemented.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import sys
import tarfile
from collections import defaultdict
from typing import Any

from generate_inventory import NATIVE_FACTORY_MODULES


PRIMITIVES = {
    "bool": ("bool", "bool"),
    "int": ("int", "int"),
    "float": ("float", "number"),
    "double": ("double", "number"),
    "qreal": ("qreal", "number"),
    "QString": ("QString", "string"),
}
INTEGER_ALIASES = {
    "unsigned int": ("unsigned int", "uint"),
    "uint": ("unsigned int", "uint"),
}
WIDE_INTEGER_ALIASES = {
    "qint64": ("qint64", "int64"),
    "qlonglong": ("qint64", "int64"),
    "long long": ("qint64", "int64"),
    "qsizetype": ("qsizetype", "int64"),
    "quint64": ("quint64", "uint64"),
    "qulonglong": ("quint64", "uint64"),
    "unsigned long long": ("quint64", "uint64"),
}
VALUE_TYPES = {
    "QByteArray": "QByteArray", "QColor": "QColor", "QDate": "QDate",
    "QDateTime": "QDateTime", "QFont": "QFont", "QHostAddress": "QHostAddress",
    "QImage": "QImage", "QLocale": "QLocale", "QMatrix4x4": "QMatrix4x4",
    "QMimeType": "QMimeType", "QModelIndex": "QModelIndex",
    "QPainterPath": "QPainterPath", "QPersistentModelIndex": "QPersistentModelIndex",
    "QPolygon": "QPolygon", "QPolygonF": "QPolygonF", "QQuaternion": "QQuaternion",
    "QRegion": "QRegion",
    "QJsonArray": "QJsonArray", "QJsonObject": "QJsonObject", "QJsonValue": "QJsonValue",
    "QKeySequence": "QKeySequence", "QLine": "QLine", "QLineF": "QLineF",
    "QMargins": "QMargins", "QMarginsF": "QMarginsF", "QPoint": "QPoint",
    "QPointF": "QPointF", "QRect": "QRect", "QRectF": "QRectF",
    "QNetworkCookie": "QNetworkCookie", "QNetworkProxy": "QNetworkProxy",
    "QNetworkRequest": "QNetworkRequest", "QRegularExpression": "QRegularExpression",
    "QRegularExpressionMatch": "QRegularExpressionMatch",
    "QRegularExpressionMatchIterator": "QRegularExpressionMatchIterator", "QSize": "QSize",
    "QSizeF": "QSizeF", "QTime": "QTime", "QUrl": "QUrl",
    "QSslCertificate": "QSslCertificate", "QSslConfiguration": "QSslConfiguration",
    "QStorageInfo": "QStorageInfo", "QTextBlockFormat": "QTextBlockFormat",
    "QTextCharFormat": "QTextCharFormat", "QTextCursor": "QTextCursor",
    "QTextFormat": "QTextFormat", "QTextFrameFormat": "QTextFrameFormat",
    "QTextImageFormat": "QTextImageFormat", "QTextLength": "QTextLength",
    "QTextListFormat": "QTextListFormat", "QTextOption": "QTextOption",
    "QTextTableFormat": "QTextTableFormat", "QTimeZone": "QTimeZone",
    "QTransform": "QTransform", "QUrlQuery": "QUrlQuery", "QVersionNumber": "QVersionNumber",
    "QGeoAddress": "QGeoAddress", "QGeoCircle": "QGeoCircle",
    "QGeoCoordinate": "QGeoCoordinate", "QGeoPath": "QGeoPath",
    "QGeoPolygon": "QGeoPolygon", "QGeoRectangle": "QGeoRectangle",
    "QUuid": "QUuid", "QVariant": "QVariant", "QVector2D": "QVector2D", "QVector3D": "QVector3D",
    "QVector4D": "QVector4D",
}
BASE_VALUE_TYPES = VALUE_TYPES.copy()
VALUE_TYPE_MODULES: dict[str, str] = {}
VALUE_TYPE_EXCLUSIONS = {"QVulkanInstance"}
MAP_TYPES = {
    "QVariantMap": ("QVariantMap", "QVariant"),
    "QVariantHash": ("QVariantHash", "QVariant"),
    "QMap<QString, QVariant>": ("QMap<QString, QVariant>", "QVariant"),
    "QHash<QString, QVariant>": ("QHash<QString, QVariant>", "QVariant"),
    "QMap<QString, QString>": ("QMap<QString, QString>", "QString"),
    "QHash<QString, QString>": ("QHash<QString, QString>", "QString"),
}
SIMPLE_CPP_TYPES = set(PRIMITIVES) | set(VALUE_TYPES)
RETURN_TYPES = set(PRIMITIVES) | set(VALUE_TYPES) | {"int64", "uint64", "void"}
MODULE_HEADER_OVERRIDES = {
    "QAxContainer": "QtAxContainer",
}
CLASS_HEADER_OVERRIDES = {
    ("QAxContainer", "QAxBaseObject"): "QAxObject",
    ("QAxContainer", "QAxBaseWidget"): "QAxWidget",
}


def write_if_changed(path: Path, content: str) -> None:
    content = "\n".join(line.rstrip() for line in content.split("\n"))
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.exists() or path.read_text(encoding="utf-8") != content:
        path.write_text(content, encoding="utf-8", newline="\n")


def split_args(source: str) -> list[str]:
    result: list[str] = []
    start = 0
    depth = 0
    for i, char in enumerate(source):
        if char in "(<[":
            depth += 1
        elif char in ")>]":
            depth -= 1
        elif char == "," and depth == 0:
            result.append(source[start:i].strip())
            start = i + 1
    tail = source[start:].strip()
    if tail:
        result.append(tail)
    return result


def strip_sip_blocks(text: str) -> str:
    """Keep declarations for the selected desktop Qt 6.11 kit."""
    lines = text.splitlines()
    kept: list[str] = []
    conditions: list[tuple[bool, bool]] = []
    active = True
    code_block = False
    block_names = {
        "%TypeCode", "%TypeHeaderCode", "%ModuleCode", "%ModuleHeaderCode",
        "%MethodCode", "%VirtualCatcherCode", "%VirtualCallCode",
        "%ConvertToSubClassCode", "%PreMethodCode", "%PostMethodCode",
        "%PropertyMethods", "%Docstring",
    }

    def version_tuple(match: re.Match[str]) -> tuple[int, int, int]:
        return tuple(int(match.group(index)) for index in range(1, 4))

    def condition_enabled(expression: str) -> bool:
        expression = expression.strip()
        version_range = re.fullmatch(
            r"(?:(Qt_\d+_\d+_\d+)\s*)?-\s*(?:(Qt_\d+_\d+_\d+))?", expression)
        if version_range and any(version_range.groups()):
            current = (6, 11, 0)
            lower = version_tuple(re.fullmatch(r"Qt_(\d+)_(\d+)_(\d+)", version_range.group(1))) if version_range.group(1) else None
            upper = version_tuple(re.fullmatch(r"Qt_(\d+)_(\d+)_(\d+)", version_range.group(2))) if version_range.group(2) else None
            return (lower is None or current >= lower) and (upper is None or current < upper)

        values = {
            "Windows": sys.platform == "win32",
            "Linux": sys.platform.startswith("linux"),
            "macOS": sys.platform == "darwin",
            "Android": False,
            "iOS": False,
            "WebAssembly": False,
            "PyQt_OpenGL_ES2": False,
            "PyQt_Vulkan": False,
            "PyQt_qreal_double": True,
        }

        def atom_value(atom: str) -> bool:
            atom = atom.strip()
            if atom.startswith("!"):
                return not atom_value(atom[1:])
            if atom in values:
                return values[atom]
            if atom.startswith("PyQt_"):
                return True
            return False

        return any(all(atom_value(part) for part in group.split("&&"))
                   for group in expression.split("||"))

    for line in lines:
        token = line.strip()
        if code_block:
            if token == "%End":
                code_block = False
            continue
        if token.startswith("%If"):
            condition = condition_enabled(token[len("%If"):].strip().strip("()"))
            conditions.append((active, condition))
            active = active and condition
            continue
        if token == "%Else":
            if conditions:
                parent, condition = conditions[-1]
                conditions[-1] = (parent, not condition)
                active = parent and not condition
            continue
        if token == "%End" and conditions:
            parent, _condition = conditions.pop()
            active = parent
            continue
        if not active:
            continue
        if token.startswith("%MethodCode"):
            for index in range(len(kept) - 1, -1, -1):
                previous = kept[index].strip()
                if not previous:
                    continue
                if previous.endswith(";") and "(" in previous:
                    kept.pop(index)
                break
            code_block = True
            continue
        if any(token.startswith(name) for name in block_names):
            code_block = True
            continue
        line = re.sub(r"//.*$", "", line)
        kept.append(line)
    return "\n".join(kept)


def parse_class_declarations(sip_roots: Path | list[Path]) -> tuple[
        dict[tuple[str, str], set[str]],
        dict[tuple[str, str], list[dict[str, Any]]],
        dict[tuple[str, str], list[dict[str, Any]]]]:
    bases_by_class: dict[tuple[str, str], set[str]] = {}
    methods_by_class: dict[tuple[str, str], list[dict[str, Any]]] = defaultdict(list)
    constructors_by_class: dict[tuple[str, str], list[dict[str, Any]]] = defaultdict(list)
    roots = [sip_roots] if isinstance(sip_roots, Path) else sip_roots
    for sip_root in roots:
        if not sip_root.is_dir():
            raise ValueError(f"SIP root does not exist or is not a directory: {sip_root}")
        for module_dir in sorted(p for p in sip_root.iterdir() if p.is_dir()):
            module = module_dir.name
            for sip_path in sorted(module_dir.glob("*.sip")):
                source = strip_sip_blocks(sip_path.read_text(encoding="utf-8"))
                lines = source.splitlines()
                current_class: str | None = None
                brace_depth = 0
                access = "private"
                statement = ""
                for line in lines:
                    clean = line.strip()
                    if current_class is None:
                        match = re.match(r"class\s+([A-Za-z_]\w*)(?:\s*/[^\n]*?/)?\s*(?::\s*([^\{]+))?\s*\{?\s*$", clean)
                        if not match:
                            continue
                        current_class = match.group(1)
                        raw_bases = re.sub(r"/[^/]*/", "", match.group(2) or "")
                        bases_by_class[(module, current_class)] = {
                            re.sub(r"\b(public|protected|private|virtual)\b", "", part).strip().split("::")[-1].strip()
                            for part in raw_bases.split(",") if part.strip()
                        }
                        brace_depth = clean.count("{") - clean.count("}")
                        access = "private"
                        continue

                    if clean in {"public:", "public slots:", "public Q_SLOTS:", "signals:", "Q_SIGNALS:",
                                 "protected:", "protected slots:", "private:", "private slots:"}:
                        access = "public" if clean.startswith("public") else "nonpublic"
                        statement = ""
                    else:
                        statement += " " + clean
                        if ";" in clean:
                            # A SIP function declaration ends in one semicolon. Enum,
                            # typedef, and data-member declarations are rejected by
                            # parse_method_declaration below.
                            for piece in statement.split(";")[:-1]:
                                if access != "public":
                                    continue
                                method = parse_method_declaration(piece + ";", current_class)
                                if method is not None:
                                    methods_by_class[(module, current_class)].append(method)
                                constructor = parse_constructor_declaration(piece + ";", current_class)
                                if constructor is not None:
                                    constructors_by_class[(module, current_class)].append(constructor)
                            statement = statement.rsplit(";", 1)[-1]

                    brace_depth += clean.count("{") - clean.count("}")
                    if brace_depth <= 0:
                        current_class = None
                        access = "private"
                        statement = ""
    return bases_by_class, methods_by_class, constructors_by_class


def parse_method_declaration(source: str, class_name: str) -> dict[str, Any] | None:
    annotations = re.findall(r"/([^/]*)/", source)
    source = re.sub(r"/[^/]*/", "", source).strip().rstrip(";").strip()
    match = re.match(r"(?P<prefix>.*?)\b(?P<name>[A-Za-z_]\w*)\s*\((?P<args>.*)\)\s*(?P<suffix>(?:const\s*)*)$", source)
    if match is None:
        return None
    cpp_name = match.group("name")
    if cpp_name == class_name or cpp_name.startswith("~") or cpp_name.startswith("operator"):
        return None
    prefix = match.group("prefix").strip()
    if not prefix:
        return None
    is_static = bool(re.search(r"\bstatic\b", prefix))
    prefix = re.sub(r"\b(virtual|static|explicit|inline|constexpr|friend)\b", "", prefix).strip()
    if not prefix or "=" in prefix or "typedef" in prefix:
        return None
    py_name = cpp_name
    for annotation in annotations:
        py_name_match = re.search(r"(?:^|,)\s*PyName\s*=\s*([A-Za-z_]\w*)", annotation)
        if py_name_match:
            py_name = py_name_match.group(1)
    params: list[dict[str, str]] = []
    raw_args = match.group("args").strip()
    if raw_args and raw_args != "void":
        for raw in split_args(raw_args):
            raw = raw.split("=", 1)[0].strip()
            raw = re.sub(r"/[^/]*/", "", raw).strip()
            if not raw or "..." in raw:
                return None
            mutable_reference = "&" in raw and not re.search(r"\bconst\b", raw)
            # Remove the declarator name while preserving pointer/reference
            # suffixes, then normalize cv/ref spelling for binding selection.
            name_match = re.search(r"\b([A-Za-z_]\w*)$", raw)
            type_source = raw[:name_match.start()].strip() if name_match and len(raw.split()) > 1 else raw
            normalized = normalize_cpp_type(type_source)
            if normalized is None:
                return None
            if mutable_reference:
                normalized["mutable"] = "true"
            params.append(normalized)
    return_type = normalize_cpp_type(prefix, is_return=True)
    if return_type is None or (return_type["type"] not in RETURN_TYPES and return_type["json"] not in {"qobject", "qtenum", "qtlist", "qtmap", "uint", "int64", "uint64"}):
        return None
    return {"cppName": cpp_name, "pyName": py_name, "params": params,
            "return": return_type, "static": is_static}


def parse_constructor_declaration(source: str, class_name: str) -> dict[str, Any] | None:
    source = re.sub(r"/[^/]*/", "", source).strip().rstrip(";").strip()
    match = re.fullmatch(rf"(?:explicit\s+)?{re.escape(class_name)}\s*\((.*)\)", source)
    if match is None:
        return None
    params: list[dict[str, Any]] = []
    raw_args = match.group(1).strip()
    if raw_args and raw_args != "void":
        for raw in split_args(raw_args):
            defaulted = "=" in raw
            declaration = raw.split("=", 1)[0].strip()
            if not declaration or "..." in declaration:
                return None
            name = ""
            name_match = re.search(r"\b([A-Za-z_]\w*)$", declaration)
            if name_match and name_match.start() > 0:
                possible_type = declaration[:name_match.start()].strip()
                if normalize_cpp_type(possible_type) is not None:
                    declaration = possible_type
                    name = name_match.group(1)
            normalized = normalize_cpp_type(declaration)
            if normalized is None:
                return None
            normalized["name"] = name
            normalized["optional"] = defaulted
            params.append(normalized)
    return {"params": params}


def normalize_cpp_type(source: str, *, is_return: bool = False) -> dict[str, str] | None:
    raw = " ".join(source.split())
    raw = re.sub(r"\b(const|volatile)\b", "", raw).strip()
    raw = raw.replace("&", "").strip()
    if "[" in raw or "]" in raw or raw.count("*") > 1:
        return None
    if raw in INTEGER_ALIASES:
        cpp, kind = INTEGER_ALIASES[raw]
        return {"type": "uint", "cpp": cpp, "json": kind}
    if raw in WIDE_INTEGER_ALIASES:
        cpp, kind = WIDE_INTEGER_ALIASES[raw]
        type_name = "int64" if kind == "int64" else "uint64"
        return {"type": type_name, "cpp": cpp, "json": kind}
    if raw in {"QStringList", "QObjectList"}:
        item_source = "QString" if raw == "QStringList" else "QObject *"
        item = normalize_cpp_type(item_source)
        return {"type": f"QtList:{item['type']}", "cpp": raw, "json": "qtlist",
                "container": raw, "item": item}
    if raw in MAP_TYPES:
        cpp, item_source = MAP_TYPES[raw]
        item = normalize_cpp_type(item_source)
        return {"type": f"QtMap:{item['type']}", "cpp": cpp, "json": "qtmap",
                "container": cpp, "key": "QString", "item": item}
    list_match = re.fullmatch(r"(?:QList|QVector)\s*<\s*(.+)\s*>", raw)
    if list_match:
        item = normalize_cpp_type(list_match.group(1))
        if item is None or item.get("json") == "qtlist":
            return None
        container = raw[:raw.index("<")].strip()
        return {"type": f"QtList:{item['type']}", "cpp": f"{container}<{item['cpp']}>",
                "json": "qtlist", "container": container, "item": item}
    flag_match = re.fullmatch(r"QFlags\s*<\s*((?:[A-Za-z_]\w*::)*[A-Za-z_]\w*)\s*>", raw)
    if flag_match:
        enum_cpp = flag_match.group(1)
        enum_name = enum_cpp.split("::")[-1]
        return {"type": f"QtEnum:{enum_name}", "cpp": f"QFlags<{enum_cpp}>",
                "json": "qtenum", "scope": "::".join(enum_cpp.split("::")[:-1]),
                "enum": enum_name}
    is_pointer = "*" in raw
    raw = raw.replace("*", " ").strip()
    pieces = raw.split()
    if not pieces:
        return None
    name = pieces[-1]
    if len(pieces) > 1 and "::" not in name:
        # C++ integral aliases are intentionally added only when they have a
        # stable JSON representation in the bridge.
        return None
    if is_pointer:
        if not re.fullmatch(r"(?:[A-Za-z_]\w*::)*[A-Za-z_]\w*", name):
            return None
        return {"type": f"QObject:{name.split('::')[-1]}", "cpp": f"{name}*",
                "json": "qobject", "class": name.split("::")[-1]}
    if "::" in name and re.fullmatch(r"(?:[A-Za-z_]\w*::)+[A-Za-z_]\w*", name):
        scope, enum_name = name.rsplit("::", 1)
        return {"type": f"QtEnum:{enum_name}", "cpp": name, "json": "qtenum",
                "scope": scope, "enum": enum_name}
    if name in PRIMITIVES:
        cpp, kind = PRIMITIVES[name]
        return {"type": name, "cpp": cpp, "json": kind}
    if name == "QVariant":
        return {"type": name, "cpp": name, "json": "qvariant"}
    if name in VALUE_TYPES:
        result = {"type": name, "cpp": name, "json": "qtvalue"}
        if name in VALUE_TYPE_MODULES:
            result.update(module=VALUE_TYPE_MODULES[name], **{"class": name})
        return result
    if is_return and name == "void":
        return {"type": "void", "cpp": "void", "json": "void"}
    return None


def parse_doc_signature(signature: str, qobject_names: set[str] | None = None,
                        qtenum_names: set[str] | None = None) -> dict[str, Any] | None:
    static = signature.startswith("@staticmethod")
    raw = signature.removeprefix("@staticmethod")
    match = re.match(r"(?P<name>[^ (]+)\((?P<args>.*)\)(?: → (?P<return>.*))?$", raw)
    if match is None:
        return None
    name = match.group("name")
    if not re.fullmatch(r"[A-Za-z_]\w*", name):
        return None
    args: list[dict[str, str]] = []
    raw_args = split_args(match.group("args"))
    for raw_arg in raw_args:
        argument = raw_arg.split("=", 1)[0].strip()
        if ":" in argument:
            argument = argument.split(":", 1)[1].strip()
        mapped = doc_type(argument, qobject_names=qobject_names, qtenum_names=qtenum_names)
        if mapped is None:
            return None
        mapped["nullable"] = "true" if "None" in argument else "false"
        args.append(mapped)
    result = doc_type(match.group("return") or "void", is_return=True,
                      qobject_names=qobject_names, qtenum_names=qtenum_names)
    if result is None:
        return None
    return {"name": name, "args": args, "return": result, "static": static}


def compatible_doc_type(documented: str, declared: str) -> bool:
    if documented == declared:
        return True
    if documented.startswith("PyUnion:"):
        return any(compatible_doc_type(alternative, declared)
                   for alternative in documented.removeprefix("PyUnion:").split("|"))
    if documented == "float" and declared in {"float", "double", "qreal"}:
        return True
    if documented.startswith("QtEnum:") and declared.startswith("QtEnum:"):
        return True
    if documented == "int" and declared == "uint":
        return True
    if documented == "int" and declared in {"int64", "uint64"}:
        return True
    if documented.startswith("QtList:") and declared.startswith("QtList:"):
        return compatible_doc_type(documented.removeprefix("QtList:"), declared.removeprefix("QtList:"))
    if documented.startswith("QtMap:") and declared.startswith("QtMap:"):
        return compatible_doc_type(documented.removeprefix("QtMap:"), declared.removeprefix("QtMap:"))
    return False


def doc_type(source: str, *, is_return: bool = False,
             qobject_names: set[str] | None = None,
             qtenum_names: set[str] | None = None) -> dict[str, str] | None:
    value = source.strip()
    union_parts: list[str] = []
    depth = 0
    start = 0
    for index, char in enumerate(value):
        if char in "[<({":
            depth += 1
        elif char in "]>)}":
            depth -= 1
        elif char == "|" and depth == 0:
            union_parts.append(value[start:index].strip())
            start = index + 1
    if union_parts:
        union_parts.append(value[start:].strip())
        mapped_alternatives: list[str] = []
        for alternative in union_parts:
            if alternative in {"None", "NoneType"}:
                continue
            mapped = doc_type(alternative, is_return=is_return,
                              qobject_names=qobject_names,
                              qtenum_names=qtenum_names)
            if mapped is not None and mapped["type"] not in mapped_alternatives:
                mapped_alternatives.append(mapped["type"])
        if not mapped_alternatives:
            return None
        if len(mapped_alternatives) == 1:
            return {"type": mapped_alternatives[0], "cpp": "", "json": "pyunion"}
        return {"type": "PyUnion:" + "|".join(mapped_alternatives),
                "cpp": "", "json": "pyunion"}
    value = value.removesuffix("*").strip()
    if value in {"void", "None"} and is_return:
        return {"type": "void", "cpp": "void", "json": "void"}
    if value == "Any":
        return {"type": "QVariant", "cpp": "QVariant", "json": "qvariant"}
    aliases = {
        "str": "QString", "float": "float",
        "bytes": "QByteArray", "bytearray": "QByteArray", "memoryview": "QByteArray",
        "datetime.datetime": "QDateTime", "datetime.date": "QDate", "datetime.time": "QTime",
    }
    value = aliases.get(value, value)
    if value == "float":
        return {"type": "float", "cpp": "number", "json": "number"}
    if value in PRIMITIVES:
        cpp, kind = PRIMITIVES[value]
        return {"type": value, "cpp": cpp, "json": kind}
    if value in WIDE_INTEGER_ALIASES:
        cpp, kind = WIDE_INTEGER_ALIASES[value]
        type_name = "int64" if kind == "int64" else "uint64"
        return {"type": type_name, "cpp": cpp, "json": kind}
    if value == "QVariant":
        return {"type": value, "cpp": value, "json": "qvariant"}
    if value in {"dict[str, Any]", "dict[str, Any | None]"}:
        return normalize_cpp_type("QVariantMap")
    if value in {"dict[str, str]", "dict[str, str | None]"}:
        return normalize_cpp_type("QMap<QString, QString>")
    if value in VALUE_TYPES:
        result = {"type": value, "cpp": value, "json": "qtvalue"}
        if value in VALUE_TYPE_MODULES:
            result.update(module=VALUE_TYPE_MODULES[value], **{"class": value})
        return result
    list_match = re.fullmatch(r"(?:list|Iterable)\[(.+)\]", value)
    if list_match:
        item = doc_type(list_match.group(1), qobject_names=qobject_names,
                        qtenum_names=qtenum_names)
        if item is None:
            return None
        return {"type": f"QtList:{item['type']}", "cpp": f"QList<{item['cpp']}>",
                "json": "qtlist", "container": "QList", "item": item}
    simple_name = value.split("::")[-1]
    if qobject_names is not None and simple_name in qobject_names:
        return {"type": f"QObject:{simple_name}", "cpp": f"{value}*", "json": "qobject",
                "class": simple_name}
    enum_parts = value.replace("::", ".").split(".")
    if len(enum_parts) == 2 and all(re.fullmatch(r"[A-Za-z_]\w*", part) for part in enum_parts):
        scope, enum_name = enum_parts
        cpp_scope = "Qt" if scope == "Qt" else scope
        return {"type": f"QtEnum:{enum_name}", "cpp": f"{cpp_scope}::{enum_name}",
                "json": "qtenum", "scope": cpp_scope, "enum": enum_name}
    if qtenum_names is not None and simple_name in qtenum_names:
        return {"type": f"QtEnum:{simple_name}", "cpp": value, "json": "qtenum",
                "scope": "", "enum": simple_name}
    return None


def resolve_qobject_type(type_record: dict[str, str], preferred_module: str,
                         bases: dict[tuple[str, str], set[str]],
                         memo: dict[tuple[str, str], bool]) -> dict[str, str] | None:
    if type_record.get("json") != "qobject":
        return type_record
    class_name = type_record["class"]
    preferred = (preferred_module, class_name)
    if preferred in bases and is_qobject(preferred_module, class_name, bases, memo):
        module, resolved_name = preferred
    else:
        matches = [(module, name) for (module, name) in bases
                   if name == class_name and is_qobject(module, name, bases, memo)]
        if len(matches) != 1:
            return None
        module, resolved_name = matches[0]
    resolved = dict(type_record)
    resolved["module"] = module
    resolved["class"] = resolved_name
    resolved["cpp"] = f"{cpp_class(module, resolved_name)}*"
    return resolved


def resolve_enum_type(type_record: dict[str, str], preferred_module: str,
                      bases: dict[tuple[str, str], set[str]]) -> dict[str, str] | None:
    if type_record.get("json") != "qtenum" or type_record.get("scope") == "Qt":
        return type_record
    scope_name = type_record["scope"].split("::")[-1]
    preferred = (preferred_module, scope_name)
    if preferred in bases:
        module, class_name = preferred
    else:
        matches = [(module, name) for module, name in bases if name == scope_name]
        if len(matches) != 1:
            return None
        module, class_name = matches[0]
    resolved = dict(type_record)
    resolved["module"] = module
    resolved["scopeClass"] = class_name
    resolved["cpp"] = f"{cpp_class(module, class_name)}::{type_record['enum']}"
    return resolved


def type_include_macro(type_record: dict[str, str]) -> str:
    if type_record.get("json") == "qtvalue":
        module = type_record["module"]
        class_name = type_record["class"]
        return f"KRY_QT6_NATIVE_VALUE_TYPE_{module}_{class_name}".upper()
    if type_record.get("json") == "qtenum" and type_record.get("scope") == "Qt":
        return f"KRY_QT6_NATIVE_ENUM_QT_{type_record['enum']}".upper()
    module = type_record.get("module", type_record.get("scope"))
    class_name = type_record.get("class", type_record.get("scopeClass"))
    return f"KRY_QT6_NATIVE_TYPE_{module}_{class_name}".upper()


def nested_types(type_record: dict[str, Any]) -> list[dict[str, Any]]:
    result = [type_record]
    if type_record.get("json") in {"qtlist", "qtmap"}:
        result.extend(nested_types(type_record["item"]))
    return result


def type_include_macros(type_record: dict[str, Any]) -> set[str]:
    return {type_include_macro(record) for record in nested_types(type_record)
            if record.get("json") in {"qobject", "qtenum", "qtvalue"}
            and (record.get("json") != "qtvalue" or "module" in record)}


def value_type_include_lines(types: dict[tuple[str, str], None]) -> list[str]:
    lines: list[str] = []
    for type_module, type_name in sorted(types):
        module_header = MODULE_HEADER_OVERRIDES.get(type_module, type_module)
        header_name = CLASS_HEADER_OVERRIDES.get((type_module, type_name), type_name)
        macro = f"KRY_QT6_NATIVE_VALUE_TYPE_{type_module}_{type_name}".upper()
        lines += [f"#if __has_include(<{module_header}/{header_name}>)",
                  f"#include <{module_header}/{header_name}>", f"#define {macro} 1", "#endif"]
    return lines


def resolve_type(type_record: dict[str, Any], preferred_module: str,
                 bases: dict[tuple[str, str], set[str]],
                 memo: dict[tuple[str, str], bool]) -> dict[str, Any] | None:
    kind = type_record.get("json")
    if kind == "qobject":
        return resolve_qobject_type(type_record, preferred_module, bases, memo)
    if kind == "qtenum":
        return resolve_enum_type(type_record, preferred_module, bases)
    if kind == "qtlist":
        item = resolve_type(type_record["item"], preferred_module, bases, memo)
        if item is None:
            return None
        resolved = dict(type_record)
        resolved["item"] = item
        resolved["type"] = f"QtList:{item['type']}"
        if resolved["container"] in {"QList", "QVector"}:
            resolved["cpp"] = f"{resolved['container']}<{item['cpp']}>"
        return resolved
    return type_record


def is_qobject(module: str, name: str, bases: dict[tuple[str, str], set[str]], memo: dict[tuple[str, str], bool]) -> bool:
    key = (module, name)
    if key in memo:
        return memo[key]
    if name == "QObject":
        memo[key] = True
        return True
    own_bases = bases.get(key, set())
    result = False
    for base in own_bases:
        if base == "QObject":
            result = True
            break
        local = (module, base)
        if local in bases:
            if is_qobject(module, base, bases, memo):
                result = True
                break
            continue
        if any(other_name == base and is_qobject(other_module, other_name, bases, memo)
               for other_module, other_name in bases if other_module != module):
            result = True
            break
    memo[key] = result
    return result


def cpp_class(module: str, name: str) -> str:
    return f"{module}::{name}" if module.startswith("Qt3D") else name


def cpp_signature(name: str, params: list[dict[str, str]]) -> str:
    return f"{name}({','.join(param['cpp'] for param in params)})"


def emit_arg(index: int, param: dict[str, str]) -> list[str]:
    name = f"arg{index}"
    kind = param["json"]
    local_const = "" if param.get("mutable") == "true" else "const "
    if kind == "bool":
        return [f"            if (!args.at({index}).isBool()) {{ *error = QStringLiteral(\"argument {index} must be a boolean\"); return true; }}",
                f"            {local_const}bool {name} = args.at({index}).toBool();"]
    if kind == "int":
        return [f"            int {name} = 0;",
                f"            if (!args.at({index}).isDouble() || !std::isfinite(args.at({index}).toDouble()) || std::floor(args.at({index}).toDouble()) != args.at({index}).toDouble() || args.at({index}).toDouble() < std::numeric_limits<int>::min() || args.at({index}).toDouble() > std::numeric_limits<int>::max()) {{ *error = QStringLiteral(\"argument {index} must be a 32-bit integer\"); return true; }}",
                f"            {name} = static_cast<int>(args.at({index}).toDouble());"]
    if kind == "uint":
        return [f"            if (!args.at({index}).isDouble() || !std::isfinite(args.at({index}).toDouble()) || std::floor(args.at({index}).toDouble()) != args.at({index}).toDouble() || args.at({index}).toDouble() < 0 || args.at({index}).toDouble() > std::numeric_limits<unsigned int>::max()) {{ *error = QStringLiteral(\"argument {index} must be an unsigned 32-bit integer\"); return true; }}",
                f"            {local_const}unsigned int {name} = static_cast<unsigned int>(args.at({index}).toDouble());"]
    if kind in {"int64", "uint64"}:
        cpp = param["cpp"]
        return [f"            const QVariant converted{index} = kry_qt6_direct::fromJson(args.at({index}), QMetaType::fromType<{cpp}>(), error);",
                f"            if (!converted{index}.isValid()) return true;",
                f"            {local_const}{cpp} {name} = converted{index}.value<{cpp}>();"]
    if kind == "number":
        cpp = param["cpp"]
        return [f"            if (!args.at({index}).isDouble() || !std::isfinite(args.at({index}).toDouble())) {{ *error = QStringLiteral(\"argument {index} must be a finite number\"); return true; }}",
                f"            {local_const}{cpp} {name} = static_cast<{cpp}>(args.at({index}).toDouble());"]
    if kind == "string":
        nullable = param.get("nullable") == "true"
        condition = f"args.at({index}).isString()" + (f" || args.at({index}).isNull()" if nullable else "")
        value = f"args.at({index}).isNull() ? QString() : args.at({index}).toString()" if nullable else f"args.at({index}).toString()"
        return [f"            if (!({condition})) {{ *error = QStringLiteral(\"argument {index} must be a string{ ' or null' if nullable else ''}\"); return true; }}",
                f"            {local_const}QString {name} = {value};"]
    if kind == "qtvalue":
        cpp = param["cpp"]
        return [f"            const QVariant converted{index} = kry_qt6_direct::fromJson(args.at({index}), QMetaType::fromType<{cpp}>(), error);",
                f"            if (!converted{index}.isValid()) return true;",
                f"            {local_const}{cpp} {name} = converted{index}.value<{cpp}>();"]
    if kind == "qvariant":
        return [f"            const QVariant converted{index} = kry_qt6_direct::fromJson(args.at({index}), QMetaType::fromType<QVariant>(), error);",
                f"            {local_const}QVariant {name} = converted{index};"]
    if kind == "qobject":
        cpp = param["cpp"]
        pointer_const = " const" if param.get("mutable") != "true" else ""
        nullable = param.get("nullable") == "true"
        null_check = [] if nullable else [
            f"            if (args.at({index}).isNull()) {{ *error = QStringLiteral(\"argument {index} cannot be null\"); return true; }}"]
        return null_check + [
            f"            const QVariant converted{index} = kry_qt6_direct::fromJson(args.at({index}), QMetaType::fromType<{cpp}>(), error);",
            f"            if (!converted{index}.isValid()) return true;",
            f"            {cpp}{pointer_const} {name} = converted{index}.value<{cpp}>();"]
    if kind == "qtenum":
        cpp = param["cpp"]
        return [f"            const QVariant converted{index} = kry_qt6_direct::fromJson(args.at({index}), QMetaType::fromType<{cpp}>(), error);",
                f"            if (!converted{index}.isValid()) return true;",
                f"            {cpp} {name} = converted{index}.value<{cpp}>();"]
    if kind == "qtlist":
        cpp = param["cpp"]
        item_cpp = param["item"]["cpp"]
        if param["item"].get("json") == "qvariant":
            return [f"            if (!args.at({index}).isArray()) {{ *error = QStringLiteral(\"argument {index} must be an array\"); return true; }}",
                    f"            {cpp} {name};",
                    f"            for (const QJsonValue &entry : args.at({index}).toArray()) {name}.append(entry.toVariant());"]
        return [f"            if (!args.at({index}).isArray()) {{ *error = QStringLiteral(\"argument {index} must be an array\"); return true; }}",
                f"            {cpp} {name};",
                f"            for (const QJsonValue &entry : args.at({index}).toArray()) {{",
                f"                const QVariant converted = kry_qt6_direct::fromJson(entry, QMetaType::fromType<{item_cpp}>(), error);",
                "                if (!converted.isValid()) return true;",
                f"                {name}.append(converted.value<{item_cpp}>());",
                "            }"]
    if kind == "qtmap":
        cpp = param["cpp"]
        item_cpp = param["item"]["cpp"]
        if param["item"].get("json") == "qvariant":
            return [f"            if (!args.at({index}).isObject()) {{ *error = QStringLiteral(\"argument {index} must be a JSON object\"); return true; }}",
                    f"            {cpp} {name};",
                    f"            const QJsonObject entries{index} = args.at({index}).toObject();",
                    f"            for (auto entry = entries{index}.constBegin(); entry != entries{index}.constEnd(); ++entry) {name}.insert(entry.key(), entry.value().toVariant());"]
        return [f"            if (!args.at({index}).isObject()) {{ *error = QStringLiteral(\"argument {index} must be a JSON object\"); return true; }}",
                f"            {cpp} {name};",
                f"            const QJsonObject entries{index} = args.at({index}).toObject();",
                f"            for (auto entry = entries{index}.constBegin(); entry != entries{index}.constEnd(); ++entry) {{",
                f"                const QVariant converted = kry_qt6_direct::fromJson(entry.value(), QMetaType::fromType<{item_cpp}>(), error);",
                "                if (!converted.isValid()) return true;",
                f"                {name}.insert(entry.key(), converted.value<{item_cpp}>());",
                "            }"]
    raise ValueError(kind)


def emit_result(expression: str, result: dict[str, str]) -> list[str]:
    kind = result["json"]
    if kind == "void":
        return [f"                {expression};", "                *output = QJsonValue(QJsonValue::Null);"]
    if kind == "bool":
        return [f"                *output = QJsonValue(static_cast<bool>({expression}));"]
    if kind == "int":
        return [f"                *output = QJsonValue(static_cast<double>({expression}));"]
    if kind == "uint":
        return [f"                *output = QJsonValue(static_cast<double>({expression}));"]
    if kind in {"int64", "uint64"}:
        cpp = result["cpp"]
        return [f"                *output = kry_qt6_direct::toJson(QVariant::fromValue<{cpp}>({expression}));"]
    if kind == "number":
        return [f"                *output = QJsonValue(static_cast<double>({expression}));"]
    if kind == "string":
        return [f"                *output = QJsonValue(QString({expression}));"]
    if kind == "qtvalue":
        cpp = result["cpp"]
        return [f"                const QVariant convertedResult = kry_qt6_direct::storeValue<{cpp}>({expression});",
                "                if (!convertedResult.isValid()) { *error = QStringLiteral(\"Qt returned a value Kryndel cannot store\"); return true; }",
                "                *output = kry_qt6_direct::toJson(convertedResult);"]
    if kind == "qvariant":
        return [f"                *output = kry_qt6_direct::toJson({expression});"]
    if kind == "qobject":
        return [f"                *output = kry_qt6_direct::objectHandle(const_cast<QObject *>(static_cast<const QObject *>({expression}))); "]
    if kind == "qtenum":
        cpp = result["cpp"]
        return [f"                *output = kry_qt6_direct::toJson(QVariant::fromValue<{cpp}>({expression}));"]
    if kind == "qtlist":
        item = result["item"]
        item_cpp = item["cpp"]
        lines = ["                QJsonArray values;"]
        lines.append(f"                for (const auto &item : {expression}) {{")
        if item.get("json") == "qobject":
            lines.append("                    values.append(kry_qt6_direct::objectHandle(const_cast<QObject *>(static_cast<const QObject *>(item))));")
        elif item.get("json") == "qvariant":
            lines.append("                    values.append(kry_qt6_direct::toJson(item));")
        elif item.get("json") == "qtvalue":
            lines += [f"                    const QVariant convertedItem = kry_qt6_direct::storeValue<{item_cpp}>(item);",
                      "                    if (!convertedItem.isValid()) { *error = QStringLiteral(\"Qt returned a list value Kryndel cannot store\"); return true; }",
                      "                    values.append(kry_qt6_direct::toJson(convertedItem));"]
        else:
            lines.append(f"                    values.append(kry_qt6_direct::toJson(QVariant::fromValue<{item_cpp}>(item))); ")
        lines += ["                }", "                *output = values;"]
        return lines
    if kind == "qtmap":
        item = result["item"]
        item_cpp = item["cpp"]
        map_cpp = result["cpp"]
        lines = ["                QJsonObject values;"]
        lines.append(f"                const {map_cpp} mapResult = {expression};")
        lines.append("                for (auto item = mapResult.constBegin(); item != mapResult.constEnd(); ++item) {")
        if item.get("json") == "qvariant":
            lines.append("                    values.insert(item.key(), kry_qt6_direct::toJson(item.value()));")
        elif item.get("json") == "qtvalue":
            lines += [f"                    const QVariant convertedItem = kry_qt6_direct::storeValue<{item_cpp}>(item.value());",
                      "                    if (!convertedItem.isValid()) { *error = QStringLiteral(\"Qt returned a map value Kryndel cannot store\"); return true; }",
                      "                    values.insert(item.key(), kry_qt6_direct::toJson(convertedItem));"]
        else:
            lines.append(f"                    values.insert(item.key(), kry_qt6_direct::toJson(QVariant::fromValue<{item_cpp}>(item.value())));")
        lines += ["                }", "                *output = values;"]
        return lines
    raise ValueError(kind)


def generate_module(module_name: str, module_classes: dict[str, list[dict[str, Any]]],
                    output_dir: Path, shard_index: int, *, value_mode: bool = False) -> tuple[int, int]:
    module_header = MODULE_HEADER_OVERRIDES.get(module_name, module_name)
    slug = module_name.lower()
    function_slug = f"{slug}_{shard_index}"
    source: list[str] = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QMetaType>",
        "#include <QtCore/QObject>",
        "#include <QtCore/QVariant>",
        "#include <QtCore/QString>",
        "#include <QtCore/QList>",
        "#include <QtCore/QMap>",
        "#include <QtCore/QHash>",
        "#include <QtCore/QStringList>",
        "#include <type_traits>",
        "#include <cmath>",
        "#include <limits>",
        "",
    ]
    class_records: list[tuple[str, str, str, list[dict[str, Any]]]] = []
    object_types: dict[tuple[str, str], None] = {}
    enum_types: dict[tuple[str, str], None] = {}
    value_types: dict[tuple[str, str], None] = {}
    for name, wrappers in sorted(module_classes.items()):
        header_name = CLASS_HEADER_OVERRIDES.get((module_name, name), name)
        macro = f"KRY_QT6_NATIVE_METHODS_{slug.upper()}_{name}"
        source += [f"#if __has_include(<{module_header}/{header_name}>)",
                   f"#include <{module_header}/{header_name}>", f"#define {macro} 1", "#endif"]
        class_records.append((name, cpp_class(module_name, name), macro, wrappers))
        for wrapper in wrappers:
            for root_type in [*wrapper["params"], wrapper["return"]]:
                for type_record in nested_types(root_type):
                    if type_record.get("json") == "qobject":
                        object_types[(type_record["module"], type_record["class"])] = None
                    elif type_record.get("json") == "qtenum":
                        if type_record.get("scope") == "Qt":
                            enum_types[("Qt", type_record["enum"])] = None
                        else:
                            enum_types[(type_record["module"], type_record["scopeClass"])] = None
                    elif type_record.get("json") == "qtvalue" and "module" in type_record:
                        value_types[(type_record["module"], type_record["class"])] = None
    for type_module, type_name in sorted(object_types):
        type_module_header = MODULE_HEADER_OVERRIDES.get(type_module, type_module)
        type_header_name = CLASS_HEADER_OVERRIDES.get((type_module, type_name), type_name)
        type_macro = f"KRY_QT6_NATIVE_TYPE_{type_module}_{type_name}".upper()
        source += [f"#if __has_include(<{type_module_header}/{type_header_name}>)",
                   f"#include <{type_module_header}/{type_header_name}>", f"#define {type_macro} 1", "#endif"]
    for enum_scope, enum_type in sorted(enum_types):
        if enum_scope == "Qt":
            source += ["#include <QtCore/Qt>", f"#define KRY_QT6_NATIVE_ENUM_QT_{enum_type.upper()} 1"]
        else:
            enum_module_header = MODULE_HEADER_OVERRIDES.get(enum_scope, enum_scope)
            enum_header_name = CLASS_HEADER_OVERRIDES.get((enum_scope, enum_type), enum_type)
            enum_macro = f"KRY_QT6_NATIVE_TYPE_{enum_scope}_{enum_type}".upper()
            source += [f"#if __has_include(<{enum_module_header}/{enum_header_name}>)",
                       f"#include <{enum_module_header}/{enum_header_name}>", f"#define {enum_macro} 1", "#endif"]
    source += value_type_include_lines(value_types)
    entrypoint = f"invoke_value_{function_slug}" if value_mode else f"invoke_{function_slug}"
    receiver_type = "QVariant *value" if value_mode else "QObject *object"
    source += ["", "namespace kry_qt6_direct {",
               f"bool {entrypoint}({receiver_type}, const QString &className, const QString &signature, const QJsonArray &args, QJsonValue *output, QString *error) {{"]
    wrapper_count = 0
    groups: dict[tuple[str, tuple[str, ...], str, tuple[bool, ...], bool], list[tuple[str, str, list[dict[str, Any]], dict[str, Any], str, bool]]] = defaultdict(list)
    for name, cpp, macro, wrappers in class_records:
        for wrapper in wrappers:
            params = wrapper["params"]
            result = wrapper["return"]
            nullable = tuple(p.get("nullable", "false") == "true" for p in params)
            is_static = wrapper["static"]
            groups[(cpp_signature(wrapper["apiName"], params), tuple(p["cpp"] for p in params),
                    result["cpp"], nullable, is_static)].append((name, cpp, params, result,
                                                                  wrapper["cppName"], is_static))
    for signature_index, ((signature, _, _, _, is_static), candidates) in enumerate(sorted(groups.items())):
        # A group may contain several overload implementations in different
        # QObject classes; all share the same JSON argument and result types.
        first_params = candidates[0][2]
        result_type = candidates[0][3]
        type_macros = sorted({
            macro
            for type_record in [*first_params, result_type]
            for macro in type_include_macros(type_record)
        })
        if type_macros:
            source.append("#if " + " && ".join(f"defined({macro})" for macro in type_macros))
        value_arg_types = {
            record["cpp"]
            for root_type in first_params
            for record in nested_types(root_type)
            if record.get("json") in {"qtvalue", "qtenum"}
        }
        value_arg_checks = [
            f"QMetaTypeId2<{cpp}>::Defined && std::is_copy_constructible_v<{cpp}> && std::is_default_constructible_v<{cpp}>"
            for cpp in sorted(value_arg_types)
        ]
        guard_condition = "Enable::value" + (" && " + " && ".join(value_arg_checks)
                                                   if value_arg_checks else "")
        invocation_lambda = f"invoke_signature_{signature_index}"
        source.append(f"    auto {invocation_lambda} = [&](auto enable) -> bool {{")
        source.append("        using Enable = decltype(enable);")
        source.append(f"        if constexpr ({guard_condition}) {{")
        source.append(f'    if (signature == QLatin1String("{signature}")) {{')
        source.append(f"        if (args.size() != {len(first_params)}) {{ *error = QStringLiteral(\"{signature} expects {len(first_params)} argument(s)\"); return true; }}")
        for i, param in enumerate(first_params):
            source.extend(emit_arg(i, param))
        for name, cpp, params, result, call_name, candidate_static in candidates:
            macro = f"KRY_QT6_NATIVE_METHODS_{slug.upper()}_{name}"
            args = ", ".join(f"arg{i}" for i in range(len(params)))
            if candidate_static:
                qualified_name = f"{module_name}.{name}"
                source += [f"#ifdef {macro}", f'        if (className == QLatin1String("{qualified_name}")) {{']
                source.extend(emit_result(f"{cpp}::{call_name}({args})", result))
                source += ["            return true;", "        }", "#endif"]
            elif value_mode:
                source += [f"#ifdef {macro}",
                           f'        if (className == QLatin1String("{name}") && value != nullptr && value->metaType() == QMetaType::fromType<{cpp}>()) {{',
                           f"            auto *target = static_cast<{cpp} *>(value->data());"]
                source.extend(emit_result(f"target->{call_name}({args})", result))
                source += ["            return true;", "        }", "#endif"]
            else:
                source += [f"#ifdef {macro}", f"        if (auto *target = dynamic_cast<{cpp} *>(object); target != nullptr) {{"]
                source.extend(emit_result(f"target->{call_name}({args})", result))
                source += ["            return true;", "        }", "#endif"]
            wrapper_count += 1
        source += ["    }"]
        source += ["        }", "        return false;", "    };",
                   f"    if ({invocation_lambda}(std::true_type{{}})) return true;"]
        if type_macros:
            source.append("#endif")
    source += ["    return false;", "}"]
    if value_mode:
        source += [f"QStringList direct_value_types_{function_slug}() {{", "    QStringList result;"]
        for name, cpp, macro, _wrappers in class_records:
            source += [f"#ifdef {macro}",
                       f"    if (QMetaTypeId2<{cpp}>::Defined && std::is_copy_constructible_v<{cpp}>) result.append(QStringLiteral(\"{name}\"));",
                       "#endif"]
        source += ["    return result;", "}"]
    source += ["} // namespace kry_qt6_direct", ""]
    filename_prefix = "qt6_value_methods_" if value_mode else "qt6_methods_"
    write_if_changed(output_dir / f"{filename_prefix}{function_slug}.cpp", "\n".join(source))
    return wrapper_count, len(groups)


def generate_constructor_module(module_name: str,
                               module_classes: dict[str, list[dict[str, Any]]],
                               output_dir: Path, shard_index: int) -> int:
    module_header = MODULE_HEADER_OVERRIDES.get(module_name, module_name)
    slug = module_name.lower()
    function_slug = f"{slug}_{shard_index}"
    source: list[str] = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QStringList>",
        "#include <cmath>",
        "#include <limits>",
        "#include <type_traits>",
        "#include <utility>",
        "",
    ]
    class_records: list[tuple[str, str, str, list[dict[str, Any]]]] = []
    object_types: dict[tuple[str, str], None] = {}
    enum_types: dict[tuple[str, str], None] = {}
    value_types: dict[tuple[str, str], None] = {}
    for name, constructors in sorted(module_classes.items()):
        header_name = CLASS_HEADER_OVERRIDES.get((module_name, name), name)
        macro = f"KRY_QT6_NATIVE_CONSTRUCTORS_{slug.upper()}_{name}"
        source += [f"#if __has_include(<{module_header}/{header_name}>)",
                   f"#include <{module_header}/{header_name}>", f"#define {macro} 1", "#endif"]
        class_records.append((name, cpp_class(module_name, name), macro, constructors))
        for constructor in constructors:
            for parameter in constructor["params"]:
                for type_record in nested_types(parameter):
                    if type_record.get("json") == "qobject":
                        object_types[(type_record["module"], type_record["class"])] = None
                    elif type_record.get("json") == "qtenum":
                        if type_record.get("scope") == "Qt":
                            enum_types[("Qt", type_record["enum"])] = None
                        else:
                            enum_types[(type_record["module"], type_record["scopeClass"])] = None
                    elif type_record.get("json") == "qtvalue" and "module" in type_record:
                        value_types[(type_record["module"], type_record["class"])] = None
    for type_module, type_name in sorted(object_types):
        type_module_header = MODULE_HEADER_OVERRIDES.get(type_module, type_module)
        type_header_name = CLASS_HEADER_OVERRIDES.get((type_module, type_name), type_name)
        type_macro = f"KRY_QT6_NATIVE_TYPE_{type_module}_{type_name}".upper()
        source += [f"#if __has_include(<{type_module_header}/{type_header_name}>)",
                   f"#include <{type_module_header}/{type_header_name}>", f"#define {type_macro} 1", "#endif"]
    for enum_scope, enum_type in sorted(enum_types):
        if enum_scope == "Qt":
            source += ["#include <QtCore/Qt>", f"#define KRY_QT6_NATIVE_ENUM_QT_{enum_type.upper()} 1"]
        else:
            enum_module_header = MODULE_HEADER_OVERRIDES.get(enum_scope, enum_scope)
            enum_header_name = CLASS_HEADER_OVERRIDES.get((enum_scope, enum_type), enum_type)
            enum_macro = f"KRY_QT6_NATIVE_TYPE_{enum_scope}_{enum_type}".upper()
            source += [f"#if __has_include(<{enum_module_header}/{enum_header_name}>)",
                       f"#include <{enum_module_header}/{enum_header_name}>", f"#define {enum_macro} 1", "#endif"]
    source += value_type_include_lines(value_types)

    source += [
        "",
        "namespace {",
        "template <typename T, typename... Args>",
        "QObject *makeQObject(Args &&...args) {",
        "    if constexpr (std::is_abstract_v<T> || !std::is_constructible_v<T, Args...>) return nullptr;",
        "    else return new T(std::forward<Args>(args)...);",
        "}",
        "}",
        "",
        "namespace kry_qt6_generated {",
        f"bool construct_{function_slug}(const QString &moduleName, const QString &className, QObject *parent, const QJsonArray &args, QObject **output, QString *error) {{",
    ]

    groups: dict[tuple[str, int], list[tuple[str, str, list[dict[str, Any]], str, list[dict[str, Any]], int | None, bool, int]]] = defaultdict(list)
    candidate_count = 0
    for name, cpp, macro, constructors in class_records:
        for constructor in constructors:
            params = constructor["params"]
            parent_indexes = [index for index, parameter in enumerate(params)
                              if parameter.get("name") == "parent" and parameter.get("json") == "qobject"]
            if len(parent_indexes) > 1 or (parent_indexes and parent_indexes[0] != len(params) - 1 and
                                           any(not parameter.get("optional", False)
                                               for parameter in params[parent_indexes[0] + 1:])):
                continue
            parent_index = parent_indexes[0] if parent_indexes else None
            parent_optional = bool(params[parent_index].get("optional", False)) if parent_index is not None else True
            user_params = [parameter for index, parameter in enumerate(params) if index != parent_index]
            required = 0
            for index, parameter in enumerate(user_params):
                if not parameter.get("optional", False):
                    required = index + 1
            for count in range(required, len(user_params) + 1):
                groups[(name, count)].append((name, cpp, params, macro, user_params,
                                              parent_index, parent_optional, count))
                candidate_count += 1

    for (name, count), candidates in sorted(groups.items()):
        source += [
            f'    if ((moduleName.isEmpty() || moduleName == QLatin1String("{module_name}")) &&',
            f'        className == QLatin1String("{name}") && args.size() == {count}) {{',
            "        QString lastCandidateError;",
        ]
        for _candidate_name, cpp, params, macro, user_params, parent_index, parent_optional, arg_count in candidates:
            type_macros = sorted({
                include_macro
                for parameter in params
                for include_macro in type_include_macros(parameter)
            })
            conditions = [f"defined({macro})", *(f"defined({item})" for item in type_macros)]
            source.append("#if " + " && ".join(conditions))
            value_arg_types = {
                record["cpp"]
                for parameter in user_params[:arg_count]
                for record in nested_types(parameter)
                if record.get("json") in {"qtvalue", "qtenum"}
            }
            value_arg_checks = [
                f"QMetaTypeId2<{arg_cpp}>::Defined && std::is_copy_constructible_v<{arg_cpp}> && std::is_default_constructible_v<{arg_cpp}>"
                for arg_cpp in sorted(value_arg_types)
            ]
            guard_condition = "Enable::value" + (" && " + " && ".join(value_arg_checks)
                                                       if value_arg_checks else "")
            source += [
                "        {",
                "        QString candidateError;",
                "        auto construct_candidate = [&](auto enable) -> QObject * {",
                "            using Enable = decltype(enable);",
                f"            if constexpr ({guard_condition}) {{",
                "            QString *error = &candidateError;",
            ]
            for index, parameter in enumerate(user_params[:arg_count]):
                source.extend(line.replace("return true;", "return nullptr;")
                              for line in emit_arg(index, parameter))

            call_args: list[str] = []
            parent_in_call = False
            user_index = 0
            params_for_call = params
            if arg_count != count:
                raise AssertionError("constructor candidate count changed during generation")
            for original_index, parameter in enumerate(params_for_call):
                if original_index == parent_index:
                    args_before_parent = sum(1 for previous_index in range(original_index)
                                             if previous_index != parent_index)
                    include_parent = not parent_optional or arg_count > args_before_parent
                    if include_parent:
                        parent_cpp = parameter["cpp"].removesuffix("*")
                        source += [
                            f"            auto *constructorParent = qobject_cast<{parent_cpp} *>(parent);",
                            f"            if (parent != nullptr && constructorParent == nullptr) {{ *error = QStringLiteral(\"parent must be a {parameter['class']} object\"); return nullptr; }}",
                        ]
                        call_args.append("constructorParent")
                        parent_in_call = True
                    continue
                if user_index < arg_count:
                    call_args.append(f"arg{user_index}")
                    user_index += 1
                # Omitted constructor arguments are trailing and use C++ defaults.
            expression = f"makeQObject<{cpp}>({', '.join(call_args)})"
            source += [
                f"            QObject *instance = {expression};",
                "            if (instance == nullptr) return nullptr;",
                "            QObject *attached = kry_qt6_direct::adopt(instance, parent, error);",
                "            if (attached == nullptr) { delete instance; return nullptr; }",
                "            return attached;",
                "            }",
                "            return nullptr;",
                "        };",
                "        QObject *created = construct_candidate(std::true_type{});",
                "        if (created != nullptr) { *output = created; return true; }",
                "        if (!candidateError.isEmpty()) lastCandidateError = candidateError;",
                "        }",
                "#endif",
            ]
        source += [
            "        if (!lastCandidateError.isEmpty()) *error = lastCandidateError;",
            "        return true;",
            "    }",
        ]
    source += ["    return false;", "}",
               f"QStringList constructor_classes_{function_slug}() {{",
               "    QStringList result;"]
    for name, cpp, macro, constructors in class_records:
        constructible_checks = []
        required_macros = {macro}
        for constructor in constructors:
            constructor_params = constructor["params"]
            required_macros.update(include_macro
                                   for parameter in constructor_params
                                   for include_macro in type_include_macros(parameter))
            template_params = ", ".join(parameter["cpp"] for parameter in constructor_params)
            constructible_checks.append(
                f"std::is_constructible_v<{cpp}{', ' + template_params if template_params else ''}>")
        source.append("#if " + " && ".join(f"defined({item})" for item in sorted(required_macros)))
        source.append(f'    if (!std::is_abstract_v<{cpp}> && ({" || ".join(constructible_checks)})) result.append(QStringLiteral("{name}"));')
        source.append("#endif")
    source += ["    return result;", "}", "} // namespace kry_qt6_generated", ""]
    write_if_changed(output_dir / f"qt6_constructors_{function_slug}.cpp", "\n".join(source))
    return candidate_count


def generate_value_constructor_module(module_name: str,
                                     module_classes: dict[str, list[dict[str, Any]]],
                                     output_dir: Path, shard_index: int) -> int:
    module_header = MODULE_HEADER_OVERRIDES.get(module_name, module_name)
    slug = module_name.lower()
    function_slug = f"{slug}_{shard_index}"
    source: list[str] = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QStringList>",
        "#include <type_traits>",
        "#include <utility>",
        "",
    ]
    class_records: list[tuple[str, str, str, list[dict[str, Any]]]] = []
    object_types: dict[tuple[str, str], None] = {}
    enum_types: dict[tuple[str, str], None] = {}
    value_types: dict[tuple[str, str], None] = {}
    for name, constructors in sorted(module_classes.items()):
        header_name = CLASS_HEADER_OVERRIDES.get((module_name, name), name)
        macro = f"KRY_QT6_NATIVE_VALUE_CONSTRUCTORS_{slug.upper()}_{name}"
        source += [f"#if __has_include(<{module_header}/{header_name}>)",
                   f"#include <{module_header}/{header_name}>", f"#define {macro} 1", "#endif"]
        class_records.append((name, cpp_class(module_name, name), macro, constructors))
        for constructor in constructors:
            for parameter in constructor["params"]:
                for type_record in nested_types(parameter):
                    if type_record.get("json") == "qobject":
                        object_types[(type_record["module"], type_record["class"])] = None
                    elif type_record.get("json") == "qtenum":
                        if type_record.get("scope") == "Qt":
                            enum_types[("Qt", type_record["enum"])] = None
                        else:
                            enum_types[(type_record["module"], type_record["scopeClass"])] = None
                    elif type_record.get("json") == "qtvalue" and "module" in type_record:
                        value_types[(type_record["module"], type_record["class"])] = None
    for type_module, type_name in sorted(object_types):
        type_module_header = MODULE_HEADER_OVERRIDES.get(type_module, type_module)
        type_header_name = CLASS_HEADER_OVERRIDES.get((type_module, type_name), type_name)
        type_macro = f"KRY_QT6_NATIVE_TYPE_{type_module}_{type_name}".upper()
        source += [f"#if __has_include(<{type_module_header}/{type_header_name}>)",
                   f"#include <{type_module_header}/{type_header_name}>", f"#define {type_macro} 1", "#endif"]
    for enum_scope, enum_type in sorted(enum_types):
        if enum_scope == "Qt":
            source += ["#include <QtCore/Qt>", f"#define KRY_QT6_NATIVE_ENUM_QT_{enum_type.upper()} 1"]
        else:
            enum_module_header = MODULE_HEADER_OVERRIDES.get(enum_scope, enum_scope)
            enum_header_name = CLASS_HEADER_OVERRIDES.get((enum_scope, enum_type), enum_type)
            enum_macro = f"KRY_QT6_NATIVE_TYPE_{enum_scope}_{enum_type}".upper()
            source += [f"#if __has_include(<{enum_module_header}/{enum_header_name}>)",
                       f"#include <{enum_module_header}/{enum_header_name}>", f"#define {enum_macro} 1", "#endif"]
    source += value_type_include_lines(value_types)

    source += [
        "",
        "namespace {",
        "template <typename T, typename... Args>",
        "QVariant makeQtValue(Args &&...args) {",
        "    if constexpr (QMetaTypeId2<T>::Defined && std::is_copy_constructible_v<T> && std::is_constructible_v<T, Args...>) return QVariant::fromValue<T>(T(std::forward<Args>(args)...));",
        "    else return {};",
        "}",
        "}",
        "",
        "namespace kry_qt6_generated {",
        f"bool construct_value_{function_slug}(const QString &moduleName, const QString &className, const QJsonArray &args, QVariant *output, QString *error) {{",
    ]

    groups: dict[tuple[str, int], list[tuple[str, str, str, list[dict[str, Any]], int]]] = defaultdict(list)
    candidate_count = 0
    for name, cpp, macro, constructors in class_records:
        for constructor in constructors:
            params = constructor["params"]
            required = 0
            for index, parameter in enumerate(params):
                if not parameter.get("optional", False):
                    required = index + 1
            for count in range(required, len(params) + 1):
                groups[(name, count)].append((name, cpp, macro, params, count))
                candidate_count += 1

    for (name, count), candidates in sorted(groups.items()):
        source += [
            f'    if ((moduleName.isEmpty() || moduleName == QLatin1String("{module_name}")) &&',
            f'        className == QLatin1String("{name}") && args.size() == {count}) {{',
            "        QString lastCandidateError;",
        ]
        for _candidate_name, cpp, macro, params, arg_count in candidates:
            type_macros = sorted({
                include_macro
                for parameter in params
                for include_macro in type_include_macros(parameter)
            })
            source.append("#if " + " && ".join(f"defined({item})" for item in [macro, *type_macros]))
            value_arg_types = {
                record["cpp"]
                for parameter in params[:arg_count]
                for record in nested_types(parameter)
                if record.get("json") in {"qtvalue", "qtenum"}
            }
            value_arg_checks = [
                f"QMetaTypeId2<{arg_cpp}>::Defined && std::is_copy_constructible_v<{arg_cpp}> && std::is_default_constructible_v<{arg_cpp}>"
                for arg_cpp in sorted(value_arg_types)
            ]
            guard_condition = "Enable::value" + (" && " + " && ".join(value_arg_checks)
                                                       if value_arg_checks else "")
            source += [
                "        {",
                "        QString candidateError;",
                "        auto build_candidate = [&](auto enable) -> QVariant {",
                "            using Enable = decltype(enable);",
                f"            if constexpr ({guard_condition}) {{",
                "            QString *error = &candidateError;",
            ]
            for index, parameter in enumerate(params[:arg_count]):
                source.extend(line.replace("return true;", "return QVariant();")
                              for line in emit_arg(index, parameter))
            expression = f"makeQtValue<{cpp}>({', '.join(f'arg{index}' for index in range(arg_count))})"
            source += [
                f"            return {expression};",
                "            }",
                "            return {};",
                "        };",
                "        QVariant created = build_candidate(std::true_type{});",
                "        if (created.isValid()) { *output = std::move(created); return true; }",
                "        if (!candidateError.isEmpty()) lastCandidateError = candidateError;",
                "        }",
                "#endif",
            ]
        source += [
            "        if (!lastCandidateError.isEmpty()) *error = lastCandidateError;",
            "        return true;",
            "    }",
        ]
    source += ["    return false;", "}",
               f"QStringList value_constructor_classes_{function_slug}() {{",
               "    QStringList result;"]
    for name, cpp, macro, constructors in class_records:
        checks = []
        required_macros = {macro}
        for constructor in constructors:
            params = constructor["params"]
            required_macros.update(include_macro
                                   for parameter in params
                                   for include_macro in type_include_macros(parameter))
            template_params = ", ".join(parameter["cpp"] for parameter in params)
            checks.append(f"(QMetaTypeId2<{cpp}>::Defined && std::is_copy_constructible_v<{cpp}> && std::is_constructible_v<{cpp}{', ' + template_params if template_params else ''}>)")
        source.append("#if " + " && ".join(f"defined({item})" for item in sorted(required_macros)))
        source.append(f'    if ({" || ".join(checks)}) result.append(QStringLiteral("{name}"));')
        source.append("#endif")
    source += ["    return result;", "}", "} // namespace kry_qt6_generated", ""]
    write_if_changed(output_dir / f"qt6_value_constructors_{function_slug}.cpp", "\n".join(source))
    return candidate_count


def generate(inventory_path: Path, sip_root: Path, output_dir: Path,
             constructors_output_dir: Path | None = None,
             value_output_dir: Path | None = None,
             value_constructors_output_dir: Path | None = None,
             extra_sip_roots: list[Path] | None = None) -> dict[str, int]:
    inventory = json.loads(inventory_path.read_text(encoding="utf-8"))
    VALUE_TYPES.clear()
    VALUE_TYPES.update(BASE_VALUE_TYPES)
    VALUE_TYPE_MODULES.clear()
    SIMPLE_CPP_TYPES.clear()
    SIMPLE_CPP_TYPES.update(PRIMITIVES)
    SIMPLE_CPP_TYPES.update(VALUE_TYPES)
    RETURN_TYPES.clear()
    RETURN_TYPES.update(PRIMITIVES)
    RETURN_TYPES.update(VALUE_TYPES)
    RETURN_TYPES.update({"int64", "uint64", "void"})
    if constructors_output_dir is None:
        constructors_output_dir = output_dir.parent / "generated_constructors"
    if value_output_dir is None:
        value_output_dir = output_dir.parent / "generated_value_methods"
    if value_constructors_output_dir is None:
        value_constructors_output_dir = output_dir.parent / "generated_value_constructors"
    meta: dict[tuple[str, str], dict[str, Any]] = {}
    for module in inventory["modules"]:
        for class_record in module.get("classes", []):
            meta[(module["name"], class_record["name"])] = class_record

    sip_roots = [sip_root, *(extra_sip_roots or [])]
    bases, _initial_methods, _initial_constructors = parse_class_declarations(sip_roots)
    initial_memo: dict[tuple[str, str], bool] = {}
    value_modules_by_name: dict[str, set[str]] = defaultdict(set)
    for module_name, class_name in bases:
        if (module_name.lower() not in NATIVE_FACTORY_MODULES or
                (module_name, class_name) not in meta or
                class_name in VALUE_TYPE_EXCLUSIONS or
                class_name.startswith("QPy") or
                is_qobject(module_name, class_name, bases, initial_memo)):
            continue
        value_modules_by_name[class_name].add(module_name)
    for class_name, modules in value_modules_by_name.items():
        if len(modules) != 1:
            continue
        module_name = next(iter(modules))
        VALUE_TYPES.setdefault(class_name, class_name)
        VALUE_TYPE_MODULES[class_name] = module_name
    SIMPLE_CPP_TYPES.update(VALUE_TYPES)
    RETURN_TYPES.update(VALUE_TYPES)
    bases, sip_methods, sip_constructors = parse_class_declarations(sip_roots)

    qobject_memo: dict[tuple[str, str], bool] = {}
    qobject_names = {name for module, name in bases
                     if not name.startswith("QPy")
                     and is_qobject(module, name, bases, qobject_memo)}
    qtenum_names = {type_record["enum"] for declarations in sip_methods.values()
                    for declaration in declarations
                    for type_record in [*declaration["params"], declaration["return"]]
                    if type_record.get("json") == "qtenum"}
    counts: dict[str, int] = {}
    constructor_count = 0
    constructor_modules: list[str] = []
    expected_constructors: set[str] = set()
    value_method_count = 0
    value_method_modules: list[str] = []
    expected_value_methods: set[str] = set()
    direct_value_signatures: dict[str, set[str]] = defaultdict(set)
    value_constructor_count = 0
    value_constructor_modules: list[str] = []
    expected_value_constructors: set[str] = set()
    expected: set[str] = set()
    dispatch_names: list[str] = []
    for module in inventory["modules"]:
        module_name = module["name"]
        if module["slug"] not in NATIVE_FACTORY_MODULES:
            continue
        module_classes: dict[str, list[dict[str, Any]]] = defaultdict(list)
        value_classes: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for (sip_module, class_name), declarations in sip_methods.items():
            if sip_module != module_name or class_name.startswith("QPy"):
                continue
            class_record = meta.get((module_name, class_name))
            if class_record is None:
                continue
            documented: dict[tuple[str, tuple[str, ...], str], list[dict[str, Any]]] = defaultdict(list)
            for signature in class_record.get("api", {}).get("methodSignatures", []):
                parsed = parse_doc_signature(signature, qobject_names, qtenum_names)
                if parsed is None:
                    continue
                key = (parsed["name"], tuple(p["type"] for p in parsed["args"]), parsed["return"]["type"])
                documented[key].append(parsed)
            for declaration in declarations:
                qobject_class = is_qobject(sip_module, class_name, bases, qobject_memo)
                if not declaration["static"] and not qobject_class and class_name not in VALUE_TYPES:
                    continue
                parsed_args = [resolve_type(param, module_name, bases, qobject_memo)
                               for param in declaration["params"]]
                if any(param is None for param in parsed_args):
                    continue
                if any(param["type"] not in SIMPLE_CPP_TYPES and param["json"] not in {"qobject", "qtenum", "qtlist", "qtmap", "uint", "int64", "uint64"}
                       for param in parsed_args):
                    continue
                result = declaration["return"]
                result = resolve_type(result, module_name, bases, qobject_memo)
                if result is None or (result["type"] not in RETURN_TYPES and result["json"] not in {"qobject", "qtenum", "qtlist", "qtmap", "uint", "int64", "uint64"}):
                    continue
                matching_docs = [doc for (name, arg_types, result_type), docs in documented.items()
                                 if name == declaration["pyName"] and len(arg_types) == len(parsed_args)
                                 and compatible_doc_type(result_type, result["type"])
                                 and all(compatible_doc_type(doc_type, sip_param["type"])
                                         for doc_type, sip_param in zip(arg_types, parsed_args))
                                 for doc in docs if doc["static"] == declaration["static"]]
                if not matching_docs:
                    continue
                for doc in matching_docs:
                    params = [dict(param, nullable=doc_param.get("nullable", "false"))
                              for param, doc_param in zip(parsed_args, doc["args"])]
                    wrapper = {
                        "apiName": declaration["pyName"],
                        "cppName": declaration["cppName"],
                        "params": params,
                        "return": result,
                        "static": declaration["static"],
                    }
                    if not declaration["static"] and not qobject_class:
                        value_classes[class_name].append(wrapper)
                        direct_value_signatures[class_name].add(cpp_signature(declaration["pyName"], params))
                    else:
                        module_classes[class_name].append(wrapper)
        class_items = sorted(module_classes.items())
        shards = [class_items[index:index + 24] for index in range(0, len(class_items), 24)] or [[]]
        module_count = 0
        for shard_index, shard_items in enumerate(shards):
            shard_classes = dict(shard_items)
            count, _groups = generate_module(module_name, shard_classes, output_dir, shard_index)
            module_count += count
            dispatch_names.append(f"{module_name.lower()}_{shard_index}")
            expected.add(f"qt6_methods_{module_name.lower()}_{shard_index}.cpp")
        counts[module_name] = module_count

        value_items = sorted(value_classes.items())
        value_shards = [value_items[index:index + 8]
                        for index in range(0, len(value_items), 8)]
        for shard_index, shard_items in enumerate(value_shards):
            shard_classes = dict(shard_items)
            count, _groups = generate_module(module_name, shard_classes, value_output_dir,
                                             shard_index, value_mode=True)
            value_method_count += count
            value_method_modules.append(f"{module_name.lower()}_{shard_index}")
            expected_value_methods.add(f"qt6_value_methods_{module_name.lower()}_{shard_index}.cpp")

        constructor_classes: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for (sip_module, class_name), declarations in sip_constructors.items():
            if (sip_module != module_name or
                    not is_qobject(sip_module, class_name, bases, qobject_memo)):
                continue
            if (module_name, class_name) not in meta:
                continue
            for declaration in declarations:
                resolved_params = [resolve_type(parameter, module_name, bases, qobject_memo)
                                   for parameter in declaration["params"]]
                if any(parameter is None for parameter in resolved_params):
                    continue
                if any(parameter["type"] not in SIMPLE_CPP_TYPES and
                       parameter["json"] not in {"qobject", "qtenum", "qtlist", "qtmap", "uint", "int64", "uint64"}
                       for parameter in resolved_params):
                    continue
                resolved = []
                for original, parameter in zip(declaration["params"], resolved_params):
                    resolved.append(dict(parameter, name=original.get("name", ""),
                                         optional=original.get("optional", False)))
                constructor_classes[class_name].append({"params": resolved})
        constructor_items = sorted(constructor_classes.items())
        constructor_shards = [constructor_items[index:index + 24]
                              for index in range(0, len(constructor_items), 24)]
        for shard_index, shard_items in enumerate(constructor_shards):
            shard_classes = dict(shard_items)
            constructor_count += generate_constructor_module(
                module_name, shard_classes, constructors_output_dir, shard_index)
            constructor_modules.append(f"{module_name.lower()}_{shard_index}")
            expected_constructors.add(f"qt6_constructors_{module_name.lower()}_{shard_index}.cpp")

        value_constructor_classes: dict[str, list[dict[str, Any]]] = defaultdict(list)
        for (sip_module, class_name), declarations in sip_constructors.items():
            if sip_module != module_name or class_name not in VALUE_TYPES:
                continue
            if (module_name, class_name) not in meta:
                continue
            for declaration in declarations:
                resolved_params = [resolve_type(parameter, module_name, bases, qobject_memo)
                                   for parameter in declaration["params"]]
                if any(parameter is None for parameter in resolved_params):
                    continue
                if any(parameter["type"] not in SIMPLE_CPP_TYPES and
                       parameter["json"] not in {"qobject", "qtenum", "qtlist", "qtmap", "uint", "int64", "uint64"}
                       for parameter in resolved_params):
                    continue
                resolved = []
                for original, parameter in zip(declaration["params"], resolved_params):
                    resolved.append(dict(parameter, name=original.get("name", ""),
                                         optional=original.get("optional", False)))
                value_constructor_classes[class_name].append({"params": resolved})
        value_constructor_items = sorted(value_constructor_classes.items())
        value_constructor_shards = [value_constructor_items[index:index + 8]
                                    for index in range(0, len(value_constructor_items), 8)]
        for shard_index, shard_items in enumerate(value_constructor_shards):
            shard_classes = dict(shard_items)
            value_constructor_count += generate_value_constructor_module(
                module_name, shard_classes, value_constructors_output_dir, shard_index)
            value_constructor_modules.append(f"{module_name.lower()}_{shard_index}")
            expected_value_constructors.add(
                f"qt6_value_constructors_{module_name.lower()}_{shard_index}.cpp")

    for stale in output_dir.glob("qt6_methods_*.cpp") if output_dir.exists() else []:
        if stale.name not in expected:
            stale.unlink()

    for stale in constructors_output_dir.glob("qt6_constructors_*.cpp") if constructors_output_dir.exists() else []:
        if stale.name not in expected_constructors:
            stale.unlink()

    for stale in value_output_dir.glob("qt6_value_methods_*.cpp") if value_output_dir.exists() else []:
        if stale.name not in expected_value_methods:
            stale.unlink()
    for stale in value_constructors_output_dir.glob("qt6_value_constructors_*.cpp") if value_constructors_output_dir.exists() else []:
        if stale.name not in expected_value_constructors:
            stale.unlink()

    modules = sorted(dispatch_names, key=str.lower)
    header = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        "#ifndef KRY_QT6_GENERATED_METHODS_H",
        "#define KRY_QT6_GENERATED_METHODS_H",
        '#include "../qt6_direct.h"',
        "namespace kry_qt6_direct {",
        "bool invoke(QObject *object, const QString &className, const QString &signature, const QJsonArray &args, QJsonValue *output, QString *error);",
        "}",
        "#endif",
        "",
    ]
    write_if_changed(output_dir / "qt6_methods.h", "\n".join(header))

    dispatch = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "namespace kry_qt6_direct {",
    ]
    for module_name in modules:
        dispatch.append(f"bool invoke_{module_name}(QObject *, const QString &, const QString &, const QJsonArray &, QJsonValue *, QString *);")
    dispatch += ["bool invoke(QObject *object, const QString &className, const QString &signature, const QJsonArray &args, QJsonValue *output, QString *error) {"]
    for module_name in modules:
        dispatch.append(f"    if (invoke_{module_name}(object, className, signature, args, output, error)) return true;")
    dispatch += ["    return false;", "}", "} // namespace kry_qt6_direct", ""]
    write_if_changed(output_dir / "qt6_methods_dispatch.cpp", "\n".join(dispatch))

    value_method_modules = sorted(value_method_modules, key=str.lower)
    value_header = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        "#ifndef KRY_QT6_GENERATED_VALUE_METHODS_H",
        "#define KRY_QT6_GENERATED_VALUE_METHODS_H",
        '#include "../qt6_direct.h"',
        "namespace kry_qt6_direct {",
        "bool invokeValue(QVariant *value, const QString &className, const QString &signature, const QJsonArray &args, QJsonValue *output, QString *error);",
        "QStringList availableDirectValueTypes();",
        "QJsonValue describeDirectValueType(const QString &className);",
        "}",
        "#endif",
        "",
    ]
    write_if_changed(value_output_dir / "qt6_value_methods.h", "\n".join(value_header))
    value_dispatch = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QMetaType>",
        "#include <QtCore/QJsonObject>",
        "#include <QtCore/QSet>",
        "#include <QtCore/QStringList>",
        "#include <algorithm>",
        "namespace kry_qt6_direct {",
    ]
    for module_slug in value_method_modules:
        value_dispatch.append(f"bool invoke_value_{module_slug}(QVariant *, const QString &, const QString &, const QJsonArray &, QJsonValue *, QString *);")
        value_dispatch.append(f"QStringList direct_value_types_{module_slug}();")
    value_dispatch += [
        "bool invokeValue(QVariant *value, const QString &className, const QString &signature, const QJsonArray &args, QJsonValue *output, QString *error) {",
    ]
    for module_slug in value_method_modules:
        value_dispatch.append(f"    if (invoke_value_{module_slug}(value, className, signature, args, output, error)) return true;")
    value_dispatch += ["    return false;", "}",
                       "QStringList availableDirectValueTypes() {",
                       "    QSet<QString> types;"]
    for module_slug in value_method_modules:
        value_dispatch.append(f"    for (const QString &name : direct_value_types_{module_slug}()) types.insert(name);")
    value_dispatch += [
        "    QStringList result = types.values();",
        "    std::sort(result.begin(), result.end());",
        "    return result;",
        "}",
        "QJsonValue describeDirectValueType(const QString &className) {",
    ]
    for class_name, signatures in sorted(direct_value_signatures.items()):
        unique_signatures = sorted(signatures)
        value_dispatch += [
            f'    if (className == QLatin1String("{class_name}")) {{',
            "        QJsonArray methods;",
        ]
        for signature in unique_signatures:
            value_dispatch.append(f'        methods.append(QStringLiteral("{signature.replace(chr(34), chr(92) + chr(34))}"));')
        value_dispatch += [
            f'        return QJsonObject{{{{QStringLiteral("type"), QStringLiteral("{class_name}")}}, {{QStringLiteral("kind"), QStringLiteral("value")}}, {{QStringLiteral("methods"), methods}}}};',
            "    }",
        ]
    value_dispatch += ["    return QJsonValue();", "}", "} // namespace kry_qt6_direct", ""]
    write_if_changed(value_output_dir / "qt6_value_methods_dispatch.cpp", "\n".join(value_dispatch))

    value_constructor_modules = sorted(value_constructor_modules, key=str.lower)
    value_constructor_header = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        "#ifndef KRY_QT6_GENERATED_VALUE_CONSTRUCTORS_H",
        "#define KRY_QT6_GENERATED_VALUE_CONSTRUCTORS_H",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QStringList>",
        "namespace kry_qt6_generated {",
        "bool constructValueArguments(const QString &moduleName, const QString &className, const QJsonArray &args, QVariant *output, QString *error);",
        "QStringList availableDirectValueConstructors();",
        "}",
        "#endif",
        "",
    ]
    write_if_changed(value_constructors_output_dir / "qt6_value_constructors.h",
                     "\n".join(value_constructor_header))
    value_constructor_dispatch = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QSet>",
        "#include <QtCore/QStringList>",
        "#include <algorithm>",
        "namespace kry_qt6_generated {",
    ]
    for module_slug in value_constructor_modules:
        value_constructor_dispatch.append(
            f"bool construct_value_{module_slug}(const QString &, const QString &, const QJsonArray &, QVariant *, QString *);")
        value_constructor_dispatch.append(f"QStringList value_constructor_classes_{module_slug}();")
    value_constructor_dispatch += [
        "bool constructValueArguments(const QString &moduleName, const QString &className, const QJsonArray &args, QVariant *output, QString *error) {",
        "    *output = QVariant();",
    ]
    for module_slug in value_constructor_modules:
        module_slug_name = module_slug.rsplit("_", 1)[0]
        qt_module = next(str(module["name"]) for module in inventory["modules"]
                         if str(module["slug"]).lower() == module_slug_name)
        value_constructor_dispatch.append(
            f'    if ((moduleName.isEmpty() || moduleName == QLatin1String("{qt_module}")) && construct_value_{module_slug}(moduleName, className, args, output, error)) return true;')
    value_constructor_dispatch += ["    return false;", "}",
                                   "QStringList availableDirectValueConstructors() {",
                                   "    QSet<QString> types;"]
    for module_slug in value_constructor_modules:
        value_constructor_dispatch.append(
            f"    for (const QString &name : value_constructor_classes_{module_slug}()) types.insert(name);")
    value_constructor_dispatch += [
        "    QStringList result = types.values();",
        "    std::sort(result.begin(), result.end());",
        "    return result;",
        "}",
        "} // namespace kry_qt6_generated",
        "",
    ]
    write_if_changed(value_constructors_output_dir / "qt6_value_constructors_dispatch.cpp",
                     "\n".join(value_constructor_dispatch))

    constructor_modules = sorted(constructor_modules, key=str.lower)
    constructor_header = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        "#ifndef KRY_QT6_GENERATED_CONSTRUCTORS_H",
        "#define KRY_QT6_GENERATED_CONSTRUCTORS_H",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QStringList>",
        "namespace kry_qt6_generated {",
        "bool constructArguments(const QString &moduleName, const QString &className, QObject *parent, const QJsonArray &args, QObject **output, QString *error);",
        "bool hasArgumentConstructor(const QString &moduleName, const QString &className);",
        "QStringList argumentConstructorModules(const QString &className);",
        "QJsonArray availableArgumentConstructors();",
        "}",
        "#endif",
        "",
    ]
    write_if_changed(constructors_output_dir / "qt6_constructors.h", "\n".join(constructor_header))
    constructor_dispatch = [
        "// Generated by generate_native_methods.py; do not edit by hand.",
        '#include "../qt6_direct.h"',
        "#include <QtCore/QMap>",
        "#include <QtCore/QStringList>",
        "#include <algorithm>",
        "namespace kry_qt6_generated {",
    ]
    for module_slug in constructor_modules:
        constructor_dispatch.append(f"bool construct_{module_slug}(const QString &, const QString &, QObject *, const QJsonArray &, QObject **, QString *);")
        constructor_dispatch.append(f"QStringList constructor_classes_{module_slug}();")
    constructor_dispatch += [
        "bool constructArguments(const QString &moduleName, const QString &className, QObject *parent, const QJsonArray &args, QObject **output, QString *error) {",
        "    *output = nullptr;",
    ]
    for module_slug in constructor_modules:
        constructor_dispatch.append(f"    if (construct_{module_slug}(moduleName, className, parent, args, output, error)) return true;")
    constructor_dispatch += ["    return false;", "}",
                             "bool hasArgumentConstructor(const QString &moduleName, const QString &className) {"]
    for module_slug in constructor_modules:
        module_slug_name = module_slug.rsplit("_", 1)[0]
        qt_module = next(str(module["name"]) for module in inventory["modules"]
                         if str(module["slug"]).lower() == module_slug_name)
        constructor_dispatch.append(
            f'    if ((moduleName.isEmpty() || moduleName == QLatin1String("{qt_module}")) && constructor_classes_{module_slug}().contains(className)) return true;')
    constructor_dispatch += ["    return false;", "}",
                             "QStringList argumentConstructorModules(const QString &className) {",
                             "    QStringList result;"]
    for module_slug in constructor_modules:
        module_slug_name = module_slug.rsplit("_", 1)[0]
        qt_module = next(str(module["name"]) for module in inventory["modules"]
                         if str(module["slug"]).lower() == module_slug_name)
        constructor_dispatch.append(
            f'    if (constructor_classes_{module_slug}().contains(className)) result.append(QStringLiteral("{qt_module}"));')
    constructor_dispatch += ["    return result;", "}",
                             "QJsonArray availableArgumentConstructors() {",
                             "    QMap<QString, int> counts;",
                             "    QStringList qualifiedNames;"]
    for module_slug in constructor_modules:
        module_slug_name = module_slug.rsplit("_", 1)[0]
        qt_module = next(str(module["name"]) for module in inventory["modules"]
                         if str(module["slug"]).lower() == module_slug_name)
        constructor_dispatch += [
            f"    for (const QString &name : constructor_classes_{module_slug}()) {{",
            f'        ++counts[name]; qualifiedNames.append(QStringLiteral("{qt_module}.") + name);',
            "    }",
        ]
    constructor_dispatch += [
        "    QStringList sorted = qualifiedNames;",
        "    for (auto it = counts.cbegin(); it != counts.cend(); ++it) if (it.value() == 1) sorted.append(it.key());",
        "    std::sort(sorted.begin(), sorted.end());",
        "    QJsonArray result;",
        "    for (const QString &name : sorted) result.append(name);",
        "    return result;",
        "}",
        "} // namespace kry_qt6_generated",
        "",
    ]
    write_if_changed(constructors_output_dir / "qt6_constructors_dispatch.cpp", "\n".join(constructor_dispatch))
    return {"modules": len(counts),
            "modules_with_method_bindings": sum(count > 0 for count in counts.values()),
            "bindings": sum(counts.values()),
            "constructor_bindings": constructor_count,
            "constructor_modules": len(constructor_modules),
            "value_method_bindings": value_method_count,
            "value_method_modules": len(value_method_modules),
            "value_method_classes": len(direct_value_signatures),
            "value_constructor_bindings": value_constructor_count,
            "value_constructor_modules": len(value_constructor_modules)}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", type=Path, default=Path("packages/qt6/api_inventory.json"))
    parser.add_argument("--sip-root", type=Path, required=True,
                        help="Path to the sip/ directory from the matching PyQt6 source distribution")
    parser.add_argument("--extra-sip-root", type=Path, action="append", default=[],
                        help="Additional SIP root from an optional PyQt6 add-on package; may be repeated")
    parser.add_argument("--output-dir", type=Path, default=Path("packages/qt6/native/generated_methods"))
    parser.add_argument("--constructors-output-dir", type=Path,
                        default=Path("packages/qt6/native/generated_constructors"))
    parser.add_argument("--value-output-dir", type=Path,
                        default=Path("packages/qt6/native/generated_value_methods"))
    parser.add_argument("--value-constructors-output-dir", type=Path,
                        default=Path("packages/qt6/native/generated_value_constructors"))
    args = parser.parse_args()
    result = generate(args.inventory, args.sip_root, args.output_dir,
                      args.constructors_output_dir, args.value_output_dir,
                      args.value_constructors_output_dir, args.extra_sip_root)
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
