#!/usr/bin/env python3
"""Build a deterministic PyQt API inventory and native Qt QObject factories."""

from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
from hashlib import sha256
from html import unescape
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import time
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


# This is the QObject constructor surface exposed by the Qt 6.11 components
# supported by the native adapter. ActiveX and WebEngine are optional add-ons.
NATIVE_FACTORY_MODULES = {
    "qaxcontainer", "qt3danimation", "qt3dcore", "qt3dextras", "qt3dinput", "qt3dlogic", "qt3drender",
    "qtbluetooth", "qtcharts", "qtcore", "qtdatavisualization", "qtdbus", "qtdesigner",
    "qtgraphs", "qtgraphswidgets", "qtgui", "qthelp", "qtmultimedia", "qtmultimediawidgets",
    "qtnetwork", "qtnetworkauth", "qtnfc", "qtopengl", "qtopenglwidgets", "qtpdf",
    "qtpdfwidgets", "qtpositioning", "qtprintsupport", "qtqml", "qtquick", "qtquick3d",
    "qtquickwidgets", "qtremoteobjects", "qtsensors", "qtserialport", "qtspatialaudio",
    "qtsql", "qtstatemachine", "qtsvg", "qtsvgwidgets", "qttest", "qttexttospeech",
    "qtwebchannel", "qtwebenginecore", "qtwebenginequick", "qtwebenginewidgets",
    "qtwebsockets", "qtwidgets", "qtxml",
}
NON_FACTORY_CLASSES = {
    "QMutexLocker", "QStringConverterBase", "QDBusPendingReply", "QDBusReply",
    "QAudio", "QPasswordDigestor", "QSsl", "QSql", "QTest", "QWebSocketProtocol",
}


class ModuleIndexParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.modules: dict[str, str] = {}
        self.anchor: dict[str, str] | None = None

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag == "a":
            values = dict(attrs)
            href = values.get("href", "") or ""
            match = re.fullmatch(r"api/([^/]+)/[^/]+-module\.html", href)
            if match:
                self.anchor = {"slug": match.group(1), "name": ""}

    def handle_data(self, data: str) -> None:
        if self.anchor is not None:
            self.anchor["name"] += data

    def handle_endtag(self, tag: str) -> None:
        if tag == "a" and self.anchor is not None:
            slug = self.anchor["slug"]
            name = " ".join(self.anchor["name"].split())
            if slug in self.modules and self.modules[slug] != name:
                raise ValueError(f"duplicate module slug {slug!r}")
            self.modules[slug] = name
            self.anchor = None


class ClassIndexParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.classes: list[dict[str, str]] = []
        self.anchor: dict[str, str] | None = None
        self.in_row = False

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if tag == "tr":
            self.in_row = True
        elif tag == "a" and self.in_row:
            href = values.get("href", "") or ""
            match = re.fullmatch(r"api/([^/]+)/([^/#]+)\.html(?:#.*)?", href)
            if match:
                self.anchor = {"moduleSlug": match.group(1), "href": href, "name": ""}

    def handle_data(self, data: str) -> None:
        if self.anchor is not None:
            self.anchor["name"] += data

    def handle_endtag(self, tag: str) -> None:
        if tag == "a" and self.anchor is not None:
            name = " ".join(self.anchor["name"].split())
            self.anchor["name"] = name
            if name:
                self.classes.append(self.anchor)
            self.anchor = None
        elif tag == "tr":
            self.in_row = False


class ClassPageParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.sections: list[str] = []
        self.current_signature: dict[str, str] | None = None
        self.signatures: dict[str, list[str]] = {"methods": [], "signals": []}
        self.enums: list[dict[str, object]] = []
        self.current_enum: dict[str, object] | None = None
        self.capturing_enum_name = False
        self.enum_table_depth = 0
        self.current_enum_row: list[str] | None = None
        self.current_enum_cell: str | None = None

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        values = dict(attrs)
        if tag == "section":
            self.sections.append(values.get("id", "") or "")
        elif tag == "table" and "sip-enum-table" in (values.get("class", "") or "").split():
            self.enum_table_depth += 1
        elif tag == "tr" and self.enum_table_depth and self.current_enum is not None:
            self.current_enum_row = []
        elif tag == "td" and self.current_enum_row is not None:
            self.current_enum_cell = ""
        elif tag == "dt":
            classes = set((values.get("class", "") or "").split())
            section = self.sections[-1] if self.sections else ""
            if "sip-declaration" in classes and section == "enums" and values.get("id"):
                self._finish_enum()
                self.current_enum = {"name": "", "members": []}
                self.capturing_enum_name = True
            if "sip-declaration" in classes and section in self.signatures:
                self.current_signature = {"section": section, "text": ""}

    def handle_data(self, data: str) -> None:
        if self.current_signature is not None:
            self.current_signature["text"] += data
        if self.capturing_enum_name and self.current_enum is not None:
            self.current_enum["name"] = str(self.current_enum["name"]) + data
        if self.current_enum_cell is not None:
            self.current_enum_cell += data

    def handle_endtag(self, tag: str) -> None:
        if tag == "dt" and self.current_signature is not None:
            signature = " ".join(self.current_signature["text"].split())
            if signature:
                self.signatures[self.current_signature["section"]].append(signature)
            self.current_signature = None
        if tag == "dt" and self.capturing_enum_name:
            self.capturing_enum_name = False
        elif tag == "td" and self.current_enum_cell is not None:
            assert self.current_enum_row is not None
            self.current_enum_row.append(" ".join(self.current_enum_cell.split()))
            self.current_enum_cell = None
        elif tag == "tr" and self.current_enum_row is not None:
            if len(self.current_enum_row) >= 2:
                members = self.current_enum["members"]
                assert isinstance(members, list)
                members.append({"name": self.current_enum_row[0], "value": self.current_enum_row[1]})
            self.current_enum_row = None
        elif tag == "table" and self.enum_table_depth:
            self.enum_table_depth -= 1
        elif tag == "section" and self.sections:
            if self.sections[-1] == "enums":
                self._finish_enum()
            self.sections.pop()

    def _finish_enum(self) -> None:
        if self.current_enum is None:
            return
        name = " ".join(str(self.current_enum["name"]).split())
        if name:
            self.enums.append({"name": name, "members": self.current_enum["members"]})
        self.current_enum = None
        self.capturing_enum_name = False


def read_versioned_html(path: Path, expected_version: str, expected_title: str) -> str:
    source = path.read_text(encoding="utf-8")
    title_match = re.search(r"<title>(.*?)</title>", source, flags=re.IGNORECASE | re.DOTALL)
    title = " ".join(unescape(re.sub(r"<[^>]+>", "", title_match.group(1))).split()) if title_match else ""
    if expected_title not in title or f"v{expected_version}" not in title:
        raise ValueError(f"{path} is not the expected {expected_title} for PyQt {expected_version}: {title!r}")
    return source


def page_cache_path(cache_dir: Path, source: str) -> Path:
    return cache_dir / (sha256(source.encode("utf-8")).hexdigest() + ".html")


def write_native_factory(output: Path, modules: list[dict[str, object]]) -> None:
    def write_if_changed(path: Path, content: str) -> None:
        if path.exists() and path.read_text(encoding="utf-8") == content:
            return
        path.write_text(content, encoding="utf-8", newline="\n")

    selected_modules = [module for module in modules if module["slug"] in NATIVE_FACTORY_MODULES]
    explicit_by_module = {
        str(module["slug"]): sorted(
            str(item["name"]) for item in module["classes"]
            if item["constructorStatus"] == "native-partial"
        )
        for module in selected_modules
    }
    classes_by_module = {
        str(module["slug"]): sorted({
            str(item["name"]) for item in module["classes"]
            if (
                item["constructorStatus"] != "not-bound"
                or item.get("api", {}).get("constructorSignatures")
            )
            and re.fullmatch(r"Q[A-Z][A-Za-z0-9_]+", str(item["name"]))
            and item["name"] not in NON_FACTORY_CLASSES
            and not str(item["name"]).startswith("QPy")
            and not str(item["name"]).startswith("QVulkan")
        })
        for module in selected_modules
    }

    lines = [
        "// Generated by packages/qt6/tools/generate_inventory.py; do not edit by hand.",
        "#ifndef KRY_QT6_GENERATED_DEFAULT_CONSTRUCTORS_H",
        "#define KRY_QT6_GENERATED_DEFAULT_CONSTRUCTORS_H",
        "",
        "#include <QtCore/QJsonArray>",
        "#include <QtCore/QMap>",
        "#include <QtCore/QMetaType>",
        "#include <QtCore/QMetaObject>",
        "#include <QtCore/QMetaMethod>",
        "#include <QtCore/QByteArray>",
        "#include <QtCore/QObject>",
        "#include <QtCore/QSet>",
        "#include <QtCore/QString>",
        "#include <QtCore/QStringList>",
        "#include <QtWidgets/QLayout>",
        "#include <QtWidgets/QWidget>",
        "#include <QtGui/QWindow>",
        "#include <Qt3DCore/QNode>",
        "#include <algorithm>",
        "#include <type_traits>",
        "#include <vector>",
        "",
        "namespace kry_qt6_generated {",
        "using Factory = QObject *(*)(QObject *, QString *);",
        "",
        "template <typename T, typename = void>",
        "struct IsComplete : std::false_type {};",
        "template <typename T>",
        "struct IsComplete<T, std::void_t<decltype(sizeof(T))>> : std::true_type {};",
        "template <typename T, bool Complete = IsComplete<T>::value>",
        "struct GadgetMetaType { static QMetaType get() { return {}; } };",
        "template <typename T>",
        "struct GadgetMetaType<T, true> {",
        "    static QMetaType get() {",
        "        if constexpr (QtPrivate::IsGadgetHelper<T>::IsGadgetOrDerivedFrom) { const QMetaType type = QMetaType::fromType<T>(); return type.isDefaultConstructible() ? type : QMetaType(); }",
        "        else return {};",
        "    }",
        "};",
        "template <typename T> QMetaType gadgetMetaType() { return GadgetMetaType<T>::get(); }",
        "template <typename T, bool Complete = IsComplete<T>::value>",
        "struct QObjectMetaObject { static const QMetaObject *get() { return nullptr; } };",
        "template <typename T>",
        "struct QObjectMetaObject<T, true> {",
        "    static const QMetaObject *get() { if constexpr (std::is_base_of_v<QObject, T>) return &T::staticMetaObject; else return nullptr; }",
        "};",
        "template <typename T> const QMetaObject *qObjectMetaObject() { return QObjectMetaObject<T>::get(); }",
        "template <typename T> const QMetaObject *exactQObjectMetaObject(const QString &name) { const QMetaObject *meta = qObjectMetaObject<T>(); if (!meta) return nullptr; const QByteArray fullName(meta->className()); const qsizetype separator = fullName.lastIndexOf(\"::\"); const QByteArray shortName = separator < 0 ? fullName : fullName.mid(separator + 2); return shortName == name.toLatin1() ? meta : nullptr; }",
        "inline bool hasPublicInvokableConstructor(const QMetaObject *metaObject) { if (!metaObject) return false; for (int i = 0; i < metaObject->constructorCount(); ++i) if (metaObject->constructor(i).access() == QMetaMethod::Public) return true; return false; }",
        "template <typename T, bool Complete = IsComplete<T>::value>",
        "struct InvokableConstructorSelector { static bool get(const QString &) { return false; } };",
        "template <typename T>",
        "struct InvokableConstructorSelector<T, true> { static bool get(const QString &name) { if constexpr (std::is_base_of_v<QObject, T> && !std::is_abstract_v<T>) return hasPublicInvokableConstructor(exactQObjectMetaObject<T>(name)); else return false; } };",
        "template <typename T> bool hasInvokableConstructor(const QString &name) { return InvokableConstructorSelector<T>::get(name); }",
        "",
        "template <typename T>",
        "QObject *create(QObject *parent, QString *error) {",
        "    if constexpr (std::is_base_of_v<QWidget, T>) {",
        "        QWidget *widgetParent = parent == nullptr ? nullptr : qobject_cast<QWidget *>(parent);",
        "        if (parent != nullptr && widgetParent == nullptr) { *error = QStringLiteral(\"class requires a QWidget parent\"); return nullptr; }",
        "        if constexpr (std::is_constructible_v<T, QWidget *>) return new T(widgetParent);",
        "        else if constexpr (std::is_default_constructible_v<T>) { T *instance = new T(); if (widgetParent != nullptr) instance->setParent(widgetParent); return instance; }",
        "    } else if constexpr (std::is_base_of_v<QWindow, T>) {",
        "        QWindow *windowParent = parent == nullptr ? nullptr : qobject_cast<QWindow *>(parent);",
        "        if (parent != nullptr && windowParent == nullptr) { *error = QStringLiteral(\"window constructors require a QWindow parent\"); return nullptr; }",
        "        if constexpr (std::is_constructible_v<T, QWindow *>) return new T(windowParent);",
        "        else if constexpr (std::is_default_constructible_v<T>) { T *instance = new T(); if (windowParent != nullptr) instance->setParent(windowParent); return instance; }",
        "    } else if constexpr (std::is_base_of_v<QLayout, T>) {",
        "        QWidget *widgetParent = parent == nullptr ? nullptr : qobject_cast<QWidget *>(parent);",
        "        if (parent != nullptr && widgetParent == nullptr) { *error = QStringLiteral(\"layout constructors require a QWidget parent\"); return nullptr; }",
        "        if (widgetParent != nullptr && widgetParent->layout() != nullptr) { *error = QStringLiteral(\"QWidget already has a layout\"); return nullptr; }",
        "        if constexpr (std::is_constructible_v<T, QWidget *>) return new T(widgetParent);",
        "        else if constexpr (std::is_default_constructible_v<T>) { T *instance = new T(); if (widgetParent != nullptr) widgetParent->setLayout(instance); return instance; }",
        "    } else if constexpr (std::is_base_of_v<Qt3DCore::QNode, T>) {",
        "        Qt3DCore::QNode *nodeParent = parent == nullptr ? nullptr : qobject_cast<Qt3DCore::QNode *>(parent);",
        "        if (parent != nullptr && nodeParent == nullptr) { *error = QStringLiteral(\"Qt3D objects require a Qt3DCore.QNode parent\"); return nullptr; }",
        "        if constexpr (std::is_constructible_v<T, Qt3DCore::QNode *>) return new T(nodeParent);",
        "        else if constexpr (std::is_default_constructible_v<T>) { T *instance = new T(); if (nodeParent != nullptr) instance->setParent(nodeParent); return instance; }",
        "    } else if constexpr (std::is_base_of_v<QObject, T>) {",
        "        if constexpr (std::is_constructible_v<T, QObject *>) return new T(parent);",
        "        else if constexpr (std::is_default_constructible_v<T>) { T *instance = new T(); if (parent != nullptr) instance->setParent(parent); return instance; }",
        "    }",
        "    return nullptr;",
        "}",
        "",
        "template <typename T, bool Complete = IsComplete<T>::value>",
        "struct FactorySelector { static constexpr Factory get() { return nullptr; } };",
        "template <typename T>",
        "struct FactorySelector<T, true> {",
        "    static constexpr Factory get() {",
        "        if constexpr (std::is_base_of_v<QWidget, T> && !std::is_abstract_v<T> && (std::is_constructible_v<T, QWidget *> || std::is_default_constructible_v<T>)) return &create<T>;",
        "        else if constexpr (std::is_base_of_v<QWindow, T> && !std::is_abstract_v<T> && (std::is_constructible_v<T, QWindow *> || std::is_default_constructible_v<T>)) return &create<T>;",
        "        else if constexpr (std::is_base_of_v<QLayout, T> && !std::is_abstract_v<T> && (std::is_constructible_v<T, QWidget *> || std::is_default_constructible_v<T>)) return &create<T>;",
        "        else if constexpr (std::is_base_of_v<Qt3DCore::QNode, T> && !std::is_abstract_v<T> && (std::is_constructible_v<T, Qt3DCore::QNode *> || std::is_default_constructible_v<T>)) return &create<T>;",
        "        else if constexpr (std::is_base_of_v<QObject, T> && !std::is_abstract_v<T> && (std::is_constructible_v<T, QObject *> || std::is_default_constructible_v<T>)) return &create<T>;",
        "        else return nullptr;",
        "    }",
        "};",
        "template <typename T> constexpr Factory factory() { return FactorySelector<T>::get(); }",
        "",
        "using HasClass = bool (*)(const QString &);",
        "using Construct = QObject *(*)(const QString &, QObject *, QString *);",
        "using ConstructorNames = QJsonArray (*)();",
        "using ValueType = QMetaType (*)(const QString &);",
        "using ValueTypeNames = QJsonArray (*)();",
        "using MetaObject = const QMetaObject *(*)(const QString &);",
        "using MetaObjectNames = QJsonArray (*)();",
        "struct Module { const char *name; HasClass hasClass; Construct construct; ConstructorNames constructorNames; ValueType valueType; ValueTypeNames valueTypeNames; MetaObject metaObject; MetaObjectNames metaObjectNames; };",
    ]
    for module in selected_modules:
        slug = str(module["slug"])
        lines.extend([
            f"bool has_{slug}(const QString &name);",
            f"QObject *construct_{slug}(const QString &name, QObject *parent, QString *error);",
            f"QJsonArray constructors_{slug}();",
            f"QMetaType value_type_{slug}(const QString &name);",
            f"QJsonArray value_types_{slug}();",
            f"const QMetaObject *meta_object_{slug}(const QString &name);",
            f"QJsonArray meta_object_names_{slug}();",
        ])
    lines.extend(["", "inline const std::vector<Module> &modules() {", "    static const std::vector<Module> value = {"])
    for module in selected_modules:
        slug = str(module["slug"])
        lines.append(f'        {{"{module["name"]}", &has_{slug}, &construct_{slug}, &constructors_{slug}, &value_type_{slug}, &value_types_{slug}, &meta_object_{slug}, &meta_object_names_{slug}}},')
    lines.extend([
        "    };",
        "    return value;",
        "}",
        "inline bool hasClass(const QString &qualifiedName) {",
        "    const qsizetype separator = qualifiedName.indexOf(QLatin1Char('.'));",
        "    if (separator >= 0) {",
        "        const QString moduleName = qualifiedName.left(separator);",
        "        const QString className = qualifiedName.mid(separator + 1);",
        "        for (const Module &module : modules()) if (moduleName == QLatin1String(module.name)) return module.hasClass(className) || module.metaObject(className) != nullptr;",
        "        return false;",
        "    }",
        "    for (const Module &module : modules()) if (module.hasClass(qualifiedName) || module.metaObject(qualifiedName) != nullptr) return true;",
        "    return false;",
        "}",
        "inline QMetaType valueMetaType(const QString &qualifiedName, QString *error) {",
        "    const qsizetype separator = qualifiedName.indexOf(QLatin1Char('.'));",
        "    const QString moduleName = separator < 0 ? QString() : qualifiedName.left(separator);",
        "    const QString className = separator < 0 ? qualifiedName : qualifiedName.mid(separator + 1);",
        "    const Module *selected = nullptr;",
        "    for (const Module &module : modules()) {",
        "        if (!moduleName.isEmpty() && moduleName != QLatin1String(module.name)) continue;",
        "        if (!module.valueType(className).isValid()) continue;",
        "        if (selected != nullptr) { *error = QStringLiteral(\"value type %1 is ambiguous across Qt modules; use Module.%1\").arg(className); return {}; }",
        "        selected = &module;",
        "    }",
        "    return selected == nullptr ? QMetaType() : selected->valueType(className);",
        "}",
        "inline const QMetaObject *classMetaObject(const QString &qualifiedName, QString *error) {",
        "    const qsizetype separator = qualifiedName.indexOf(QLatin1Char('.'));",
        "    const QString moduleName = separator < 0 ? QString() : qualifiedName.left(separator);",
        "    const QString className = separator < 0 ? qualifiedName : qualifiedName.mid(separator + 1);",
        "    const Module *selected = nullptr;",
        "    for (const Module &module : modules()) {",
        "        if (!moduleName.isEmpty() && moduleName != QLatin1String(module.name)) continue;",
        "        if (!module.metaObject(className)) continue;",
        "        if (selected != nullptr) { *error = QStringLiteral(\"class %1 is ambiguous across Qt modules; use Module.%1\").arg(className); return nullptr; }",
        "        selected = &module;",
        "    }",
        "    return selected == nullptr ? nullptr : selected->metaObject(className);",
        "}",
        "inline QJsonArray availableValueTypes() {",
        "    QMap<QString, int> counts;",
        "    QStringList allNames;",
        "    for (const Module &module : modules()) {",
        "        for (const QJsonValue &value : module.valueTypeNames()) { const QString name = value.toString(); ++counts[name]; allNames.append(QString::fromLatin1(module.name) + QLatin1Char('.') + name); }",
        "    }",
        "    QSet<QString> unique;",
        "    for (const QString &name : allNames) unique.insert(name);",
        "    for (auto it = counts.cbegin(); it != counts.cend(); ++it) if (it.value() == 1) unique.insert(it.key());",
        "    QStringList sorted = unique.values();",
        "    std::sort(sorted.begin(), sorted.end());",
        "    QJsonArray result;",
        "    for (const QString &name : sorted) result.append(name);",
        "    return result;",
        "}",
        "inline QJsonArray availableMetaObjects() {",
        "    QJsonArray result;",
        "    for (const Module &module : modules()) for (const QJsonValue &name : module.metaObjectNames()) result.append(QString::fromLatin1(module.name) + QLatin1Char('.') + name.toString());",
        "    return result;",
        "}",
        "inline int classCount(const QString &name) {",
        "    int matches = 0;",
        "    for (const Module &module : modules()) if (module.hasClass(name) || module.metaObject(name) != nullptr) ++matches;",
        "    return matches;",
        "}",
        "inline bool isExplicitConstructor(const QString &moduleName, const QString &name) {",
    ])
    for module in selected_modules:
        for name in explicit_by_module[str(module["slug"])]:
            lines.append(f'    if (moduleName == QLatin1String("{module["name"]}") && name == QLatin1String("{name}")) return true;')
    lines.extend([
        "    return false;",
        "}",
        "inline QObject *construct(const QString &qualifiedName, QObject *parent, QString *error) {",
        "    const qsizetype separator = qualifiedName.indexOf(QLatin1Char('.'));",
        "    const QString moduleName = separator < 0 ? QString() : qualifiedName.left(separator);",
        "    const QString className = separator < 0 ? qualifiedName : qualifiedName.mid(separator + 1);",
        "    const Module *selected = nullptr;",
        "    for (const Module &module : modules()) {",
        "        if (!moduleName.isEmpty() && moduleName != QLatin1String(module.name)) continue;",
        "        if (!module.hasClass(className) && module.metaObject(className) == nullptr) continue;",
        "        if (selected != nullptr) { *error = QStringLiteral(\"class %1 is ambiguous across Qt modules; use Module.%1\").arg(className); return nullptr; }",
        "        selected = &module;",
        "    }",
        "    if (selected == nullptr) return nullptr;",
        "    return selected->construct(className, parent, error);",
        "}",
        "inline QJsonArray availableConstructors() {",
        "    QMap<QString, int> counts;",
        "    QStringList allNames;",
        "    for (const Module &module : modules()) {",
        "        const QJsonArray names = module.constructorNames();",
        "        for (const QJsonValue &value : names) { const QString name = value.toString(); ++counts[name]; allNames.append(QString::fromLatin1(module.name) + QLatin1Char('.') + name); }",
        "    }",
        "    QSet<QString> unique;",
        "    for (const QString &name : allNames) unique.insert(name);",
        "    for (auto it = counts.cbegin(); it != counts.cend(); ++it) if (it.value() == 1) unique.insert(it.key());",
        "    QStringList sorted = unique.values();",
        "    std::sort(sorted.begin(), sorted.end());",
        "    QJsonArray result;",
        "    for (const QString &name : sorted) result.append(name);",
        "    return result;",
        "}",
        "} // namespace kry_qt6_generated",
        "#endif",
        "",
    ])
    output.parent.mkdir(parents=True, exist_ok=True)
    write_if_changed(output, "\n".join(lines))

    module_output = output.parent / "generated_modules"
    module_output.mkdir(parents=True, exist_ok=True)
    expected_files: set[Path] = set()
    for module in selected_modules:
        slug = str(module["slug"])
        module_name = str(module["name"])
        class_names = classes_by_module[slug]
        explicit_names = explicit_by_module[slug]
        source: list[str] = [
            "// Generated by packages/qt6/tools/generate_inventory.py; do not edit by hand.",
            '#include "../generated_default_constructors.h"',
            "#include <QtCore/QSet>",
            "#include <algorithm>",
            "",
        ]
        include_module_name = "QtAxContainer" if slug == "qaxcontainer" else module_name
        ax_header_names = {
            "QAxBaseObject": "QAxObject",
            "QAxBaseWidget": "QAxWidget",
        }
        for name in class_names:
            include_name = ax_header_names.get(name, name) if slug == "qaxcontainer" else name
            source.extend([
                f"#if __has_include(<{include_module_name}/{include_name}>)",
                f"#include <{include_module_name}/{include_name}>",
                f"#define KRY_QT6_HAS_{slug.upper()}_{name} 1",
                "#endif",
            ])
        source.extend(["", "namespace kry_qt6_generated {"])
        def cpp_type(name: str) -> str:
            return f"{module_name}::{name}" if module_name.startswith("Qt3D") else name
        source.append(f"bool has_{slug}(const QString &name) {{")
        for name in class_names:
            source.extend([
                f"#ifdef KRY_QT6_HAS_{slug.upper()}_{name}",
                f'    if (name == QLatin1String("{name}") && factory<{cpp_type(name)}>() != nullptr) return true;',
                "#endif",
            ])
        for name in explicit_names:
            source.append(f'    if (name == QLatin1String("{name}")) return true;')
        source.extend(["    return false;", "}", f"QObject *construct_{slug}(const QString &name, QObject *parent, QString *error) {{"])
        for name in class_names:
            source.extend([
                f"#ifdef KRY_QT6_HAS_{slug.upper()}_{name}",
                f'    if (name == QLatin1String("{name}")) {{ const Factory createClass = factory<{cpp_type(name)}>(); if (createClass != nullptr) return createClass(parent, error); }}',
                "#endif",
            ])
        source.extend(["    Q_UNUSED(parent);", "    Q_UNUSED(error);", "    return nullptr;", "}", f"QJsonArray constructors_{slug}() {{", "    QSet<QString> names;"])
        for name in class_names:
            source.extend([
                f"#ifdef KRY_QT6_HAS_{slug.upper()}_{name}",
                f'    if (factory<{cpp_type(name)}>() != nullptr || hasInvokableConstructor<{cpp_type(name)}>(QStringLiteral("{name}"))) names.insert(QStringLiteral("{name}"));',
                "#endif",
            ])
        for name in explicit_names:
            source.append(f'    names.insert(QStringLiteral("{name}"));')
        source.extend([
            "    QStringList sorted = names.values();",
            "    std::sort(sorted.begin(), sorted.end());",
            "    QJsonArray result;",
            "    for (const QString &name : sorted) result.append(name);",
            "    return result;",
            "}",
            f"const QMetaObject *meta_object_{slug}(const QString &name) {{",
        ])
        for name in class_names:
            source.extend([
                f"#ifdef KRY_QT6_HAS_{slug.upper()}_{name}",
                f'    if (name == QLatin1String("{name}")) return exactQObjectMetaObject<{cpp_type(name)}>(name);',
                "#endif",
            ])
        source.extend([
            "    return nullptr;",
            "}",
            f"QJsonArray meta_object_names_{slug}() {{",
            "    QJsonArray names;",
        ])
        for name in class_names:
            source.extend([
                f"#ifdef KRY_QT6_HAS_{slug.upper()}_{name}",
                f'    if (exactQObjectMetaObject<{cpp_type(name)}>(QStringLiteral("{name}")) != nullptr) names.append(QStringLiteral("{name}"));',
                "#endif",
            ])
        source.extend([
            "    return names;",
            "}",
            f"QMetaType value_type_{slug}(const QString &name) {{",
        ])
        for name in class_names:
            source.extend([
                f"#ifdef KRY_QT6_HAS_{slug.upper()}_{name}",
                f'    if (name == QLatin1String("{name}")) return gadgetMetaType<{cpp_type(name)}>();',
                "#endif",
            ])
        source.extend([
            "    return QMetaType();",
            "}",
            f"QJsonArray value_types_{slug}() {{",
            "    QJsonArray names;",
        ])
        for name in class_names:
            source.extend([
                f'    if (value_type_{slug}(QStringLiteral("{name}")).isValid()) names.append(QStringLiteral("{name}"));',
            ])
        source.extend([
            "    return names;",
            "}",
            "} // namespace kry_qt6_generated",
            "",
        ])
        destination = module_output / f"qt6_factory_{slug}.cpp"
        write_if_changed(destination, "\n".join(source))
        expected_files.add(destination)
    for stale in module_output.glob("qt6_factory_*.cpp"):
        if stale not in expected_files:
            stale.unlink()


def fetch_class_page(source: str, version: str, cache_dir: Path, retries: int = 3) -> tuple[str, dict[str, object]]:
    cache_dir.mkdir(parents=True, exist_ok=True)
    cached = page_cache_path(cache_dir, source)
    if cached.exists():
        html = cached.read_text(encoding="utf-8")
    else:
        url = "https://riverbankcomputing.com/static/Docs/PyQt6/" + source.split("#", 1)[0]
        last_error: Exception | None = None
        for attempt in range(retries):
            try:
                request = Request(url, headers={"User-Agent": "Kryndel-Qt6-inventory/1.0"})
                with urlopen(request, timeout=30) as response:
                    html = response.read().decode("utf-8")
                cached.write_text(html, encoding="utf-8", newline="\n")
                break
            except (HTTPError, URLError, TimeoutError) as exc:
                last_error = exc
                if attempt + 1 == retries:
                    raise RuntimeError(f"could not download {url}: {exc}") from exc
                time.sleep(0.5 * (attempt + 1))
        else:
            raise RuntimeError(f"could not download {source}: {last_error}")

    title_match = re.search(r"<title>(.*?)</title>", html, flags=re.IGNORECASE | re.DOTALL)
    title = " ".join(unescape(re.sub(r"<[^>]+>", "", title_match.group(1))).split()) if title_match else ""
    if f"v{version}" not in title:
        raise ValueError(f"class page {source} does not identify as PyQt {version}: {title!r}")
    parser = ClassPageParser()
    parser.feed(html)
    result: dict[str, object] = {key: sorted(values) for key, values in parser.signatures.items()}
    result["enums"] = parser.enums
    return source, result


def generate(modules_html: Path, classes_html: Path, output: Path, version: str,
             include_signatures: bool = False, cache_dir: Path | None = None, workers: int = 12,
             native_factory_output: Path | None = None) -> None:
    modules_source = read_versioned_html(modules_html, version, "Modules")
    classes_source = read_versioned_html(classes_html, version, "Class Index")

    module_parser = ModuleIndexParser()
    module_parser.feed(modules_source)
    if not module_parser.modules:
        raise ValueError("the PyQt module index contains no module links")

    class_parser = ClassIndexParser()
    class_parser.feed(classes_source)
    if not class_parser.classes:
        raise ValueError("the PyQt class index contains no class links")

    native_widget_constructors = {
        "QWidget", "QMainWindow", "QPushButton", "QToolButton", "QLabel", "QLineEdit",
        "QTextEdit", "QCheckBox", "QRadioButton", "QComboBox", "QSpinBox", "QSlider",
        "QProgressBar", "QGroupBox", "QTabWidget", "QStackedWidget", "QVBoxLayout",
        "QHBoxLayout", "QFormLayout", "QGridLayout", "QStatusBar", "QPlainTextEdit",
        "QDoubleSpinBox", "QScrollBar", "QDial", "QTabBar", "QDialog", "QDialogButtonBox", "QButtonGroup",
        "QCalendarWidget", "QDateEdit", "QDateTimeEdit", "QTimeEdit", "QLCDNumber", "QScrollArea",
        "QSplitter", "QTableWidget", "QTreeWidget", "QListWidget", "QTableView", "QTreeView",
        "QListView", "QGraphicsView", "QGraphicsScene", "QMdiArea", "QToolBar", "QMenuBar", "QMenu", "QFrame",
        "QDockWidget", "QWizard", "QWizardPage", "QFontComboBox", "QKeySequenceEdit", "QMessageBox",
        "QFileDialog", "QColorDialog", "QFontDialog", "QInputDialog", "QProgressDialog", "QCommandLinkButton",
        "QTextBrowser", "QUndoView",
    }
    native_core_constructors = {
        "QObject", "QTimer", "QStringListModel", "QSortFilterProxyModel", "QBuffer",
        "QConcatenateTablesProxyModel", "QEventLoop", "QFile", "QFileSystemWatcher",
        "QIdentityProxyModel", "QItemSelectionModel", "QLibrary", "QPluginLoader", "QProcess",
        "QSaveFile", "QSettings", "QSignalMapper", "QTemporaryFile", "QThread", "QThreadPool",
        "QTranslator", "QTransposeProxyModel",
    }
    native_gui_constructors = {"QAction", "QStandardItemModel", "QMovie", "QShortcut", "QTextDocument", "QUndoGroup", "QUndoStack"}
    native_ax_constructors = {"QAxObject", "QAxWidget"}
    grouped: dict[str, dict[str, dict[str, object]]] = {slug: {} for slug in module_parser.modules}
    unknown_modules: set[str] = set()
    for item in class_parser.classes:
        module_slug = item["moduleSlug"]
        module = grouped.get(module_slug)
        if module is None:
            unknown_modules.add(module_slug)
            continue
        name = item["name"]
        has_native_constructor = (
            (module_slug == "qtwidgets" and name in native_widget_constructors)
            or (module_slug == "qtcore" and name in native_core_constructors)
            or (module_slug == "qtgui" and name in native_gui_constructors)
            or (module_slug == "qaxcontainer" and name in native_ax_constructors)
        )
        has_default_factory_candidate = (
            module_slug in NATIVE_FACTORY_MODULES
            and re.fullmatch(r"Q[A-Z][A-Za-z0-9_]+", name) is not None
            and name not in NON_FACTORY_CLASSES
            and not name.startswith("QPy")
            and not name.startswith("QVulkan")
        )
        module[name] = {
            "name": name,
            "source": item["href"],
            "constructorStatus": (
                "native-partial" if has_native_constructor else
                "runtime-default-factory-candidate" if has_default_factory_candidate else
                "not-bound"
            ),
        }
    if unknown_modules:
        raise ValueError("class links point to modules missing from module_index.html: " + ", ".join(sorted(unknown_modules)))

    api_by_source: dict[str, dict[str, object]] = {}
    if include_signatures:
        if cache_dir is None:
            raise ValueError("--cache-dir is required when fetching API signatures")
        if workers < 1:
            raise ValueError("--workers must be a positive integer")
        sources = sorted({item["href"] for item in class_parser.classes})
        with ThreadPoolExecutor(max_workers=workers) as executor:
            futures = [executor.submit(fetch_class_page, source, version, cache_dir) for source in sources]
            for future in as_completed(futures):
                source, signatures = future.result()
                api_by_source[source] = signatures

    module_records = []
    for slug, name in sorted(module_parser.modules.items(), key=lambda item: (item[1].casefold(), item[0])):
        classes = [grouped[slug][key] for key in sorted(grouped[slug], key=str.casefold)]
        if include_signatures:
            for class_record in classes:
                api = api_by_source[class_record["source"]]
                constructor_signatures = [signature for signature in api["methods"] if signature.startswith("__init__(")]
                class_record["api"] = {
                    "constructorSignatures": constructor_signatures,
                    "methodSignatures": [signature for signature in api["methods"] if not signature.startswith("__init__(")],
                    "signalSignatures": api["signals"],
                    "enumDeclarations": api["enums"],
                }
        module_records.append({"slug": slug, "name": name, "classCount": len(classes), "classes": classes})

    constructor_count = sum(len(record.get("api", {}).get("constructorSignatures", [])) for module in module_records for record in module["classes"])
    method_count = sum(len(record.get("api", {}).get("methodSignatures", [])) for module in module_records for record in module["classes"])
    signal_count = sum(len(record.get("api", {}).get("signalSignatures", [])) for module in module_records for record in module["classes"])
    enum_count = sum(len(record.get("api", {}).get("enumDeclarations", [])) for module in module_records for record in module["classes"])
    enum_value_count = sum(
        len(enumeration.get("members", []))
        for module in module_records for record in module["classes"]
        for enumeration in record.get("api", {}).get("enumDeclarations", [])
    )
    default_factory_candidate_count = sum(
        record["constructorStatus"] == "runtime-default-factory-candidate"
        for module in module_records for record in module["classes"]
    )
    inventory = {
        "formatVersion": 1,
        "pyqtVersion": version,
        "source": "PyQt6 module_index.html, sip-classes.html, and linked class documentation pages",
        "generatedBy": "packages/qt6/tools/generate_inventory.py",
        "apiSignatureStatus": "included" if include_signatures else "class-index-only",
        "moduleCount": len(module_records),
        "classCount": sum(module["classCount"] for module in module_records),
        "constructorSignatureCount": constructor_count,
        "methodSignatureCount": method_count,
        "signalSignatureCount": signal_count,
        "enumDeclarationCount": enum_count,
        "enumValueCount": enum_value_count,
        "runtimeDefaultFactoryCandidateCount": default_factory_candidate_count,
        "modules": module_records,
    }
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(inventory, indent=2, ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")
    if native_factory_output is not None:
        write_native_factory(native_factory_output, module_records)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default="6.11.0", help="Pinned PyQt documentation version (default: 6.11.0)")
    parser.add_argument("--modules-html", type=Path, required=True, help="Downloaded module_index.html for that release")
    parser.add_argument("--classes-html", type=Path, required=True, help="Downloaded sip-classes.html for that release")
    parser.add_argument("--output", type=Path, default=Path("packages/qt6/api_inventory.json"))
    parser.add_argument("--include-signatures", action="store_true", help="Fetch every class page and inventory constructors, methods, and signals")
    parser.add_argument("--cache-dir", type=Path, help="Directory for downloaded class pages; required with --include-signatures")
    parser.add_argument("--workers", type=int, default=12, help="Concurrent class page downloads (default: 12)")
    parser.add_argument("--native-factory-output", type=Path, default=Path("packages/qt6/native/generated_default_constructors.h"),
                        help="Generate the Qt QObject default-constructor factory header")
    args = parser.parse_args()
    generate(args.modules_html, args.classes_html, args.output, args.version, args.include_signatures,
             args.cache_dir, args.workers, args.native_factory_output)


if __name__ == "__main__":
    main()
