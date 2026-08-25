"""Type representations and built-in signatures for Kryndel."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Type:
    name: str

    def __str__(self) -> str:
        return self.name


@dataclass(frozen=True)
class StructType(Type):
    """A nominal struct type with declaration-ordered field metadata."""

    fields: tuple[tuple[str, Type], ...] = ()

    def field_type(self, name: str) -> Type | None:
        for field_name, field_type in self.fields:
            if field_name == name:
                return field_type
        return None


@dataclass(frozen=True)
class EnumType(Type):
    """A nominal enum with declaration-ordered positional payload metadata."""

    variants: tuple[str, ...] = ()
    payloads: tuple[tuple[str, tuple[Type, ...]], ...] = ()

    def payload_types(self, variant: str) -> tuple[Type, ...]:
        for name, types in self.payloads:
            if name == variant:
                return types
        return ()


@dataclass(frozen=True)
class ArrayType(Type):
    """A homogeneous sequence; element metadata is part of the type."""

    element: Type = Type("Unknown")


@dataclass(frozen=True)
class TupleType(Type):
    """A fixed-width positional sequence with declaration-order elements."""

    elements: tuple[Type, ...] = ()


INT = Type("Int")
FLOAT = Type("Float")
BOOL = Type("Bool")
STRING = Type("String")
VOID = Type("Void")
UI = Type("UiNode")
ANY = Type("Any")
UNKNOWN = Type("Unknown")
ARRAY = Type("Array")
TUPLE = Type("Tuple")
BYTES = Type("Bytes")

PRIMITIVE_TYPES = {t.name: t for t in (INT, FLOAT, BOOL, STRING, VOID, UI, ARRAY, TUPLE, BYTES)}


@dataclass(frozen=True)
class FunctionType:
    parameters: tuple[Type, ...]
    return_type: Type
    variadic: bool = False


BUILTIN_FUNCTIONS: dict[str, FunctionType] = {
    "print": FunctionType((ANY,), VOID, variadic=True),
    "println": FunctionType((ANY,), VOID, variadic=True),
    "str": FunctionType((ANY,), STRING),
    "int": FunctionType((ANY,), INT),
    "float": FunctionType((ANY,), FLOAT),
    "len": FunctionType((ANY,), INT),
    "bytes": FunctionType((ArrayType("Array", INT),), BYTES),
    "string_to_bytes": FunctionType((STRING,), BYTES),
    "bytes_to_string": FunctionType((BYTES,), STRING),
    "abs": FunctionType((INT,), INT),
    "sqrt": FunctionType((FLOAT,), FLOAT),
    "clock": FunctionType((), FLOAT),
    "ui.window": FunctionType((STRING, INT, INT), UI),
    "ui.label": FunctionType((UI, STRING), UI),
    "ui.button": FunctionType((UI, STRING), UI),
    "ui.vbox": FunctionType((UI,), UI),
    "ui.hbox": FunctionType((UI,), UI),
    "ui.set_text": FunctionType((UI, STRING), VOID),
    "ui.on_click": FunctionType((UI, ANY), VOID),
    "ui.show": FunctionType((UI,), VOID),
    "ui.run": FunctionType((), VOID),
}


def resolve_type(name: str) -> Type:
    return PRIMITIVE_TYPES.get(name, UNKNOWN)


def compatible(expected: Type, actual: Type) -> bool:
    if expected in (ANY, UNKNOWN) or actual in (ANY, UNKNOWN):
        return True
    if expected == ARRAY and isinstance(actual, ArrayType):
        return True
    if expected == TUPLE and isinstance(actual, TupleType):
        return True
    if actual == ARRAY and isinstance(expected, ArrayType):
        return True
    if actual == TUPLE and isinstance(expected, TupleType):
        return True
    if expected == actual:
        return True
    if isinstance(expected, StructType) and isinstance(actual, StructType):
        return expected.name == actual.name
    if isinstance(expected, EnumType) and isinstance(actual, EnumType):
        return expected.name == actual.name
    if isinstance(expected, ArrayType) and isinstance(actual, ArrayType):
        return compatible(expected.element, actual.element)
    if isinstance(expected, TupleType) and isinstance(actual, TupleType):
        return len(expected.elements) == len(actual.elements) and all(
            compatible(left, right) for left, right in zip(expected.elements, actual.elements)
        )
    return expected == FLOAT and actual == INT


def numeric(type_: Type) -> bool:
    return type_ in (INT, FLOAT)
