"""Abstract syntax tree nodes for the Kryndel language."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Union

from .diagnostics import Span


@dataclass
class Node:
    span: Span


@dataclass
class TypeName(Node):
    name: str


@dataclass
class Literal(Node):
    value: object
    kind: str


@dataclass
class Name(Node):
    value: str


@dataclass
class Member(Node):
    target: "Expr"
    name: str
    name_span: Span


@dataclass
class EnumValue(Node):
    enum_name: str
    variant_name: str
    enum_span: Span
    variant_span: Span


@dataclass
class Unary(Node):
    operator: str
    operand: "Expr"


@dataclass
class Binary(Node):
    left: "Expr"
    operator: str
    right: "Expr"


@dataclass
class Call(Node):
    callee: "Expr"
    arguments: list["Expr"] = field(default_factory=list)


Expr = Union[Literal, Name, Member, EnumValue, Unary, Binary, Call, "StructLiteral"]


@dataclass
class LetStmt(Node):
    name: str
    annotation: TypeName | None
    initializer: Expr
    mutable: bool = False


@dataclass
class ExprStmt(Node):
    expression: Expr


@dataclass
class Block(Node):
    statements: list["Stmt"] = field(default_factory=list)


@dataclass
class IfStmt(Node):
    condition: Expr
    then_block: Block
    else_branch: Block | "IfStmt" | None = None


@dataclass
class WhileStmt(Node):
    condition: Expr
    body: Block


@dataclass
class ReturnStmt(Node):
    value: Expr | None


@dataclass
class BreakStmt(Node):
    pass


@dataclass
class ContinueStmt(Node):
    pass


@dataclass
class Parameter(Node):
    name: str
    type_name: TypeName


@dataclass
class FunctionDecl(Node):
    name: str
    parameters: list[Parameter]
    return_type: TypeName
    body: Block


@dataclass
class StructFieldDecl(Node):
    name: str
    type_name: TypeName
    name_span: Span


@dataclass
class StructDecl(Node):
    name: str
    fields: list[StructFieldDecl] = field(default_factory=list)


@dataclass
class EnumVariantDecl(Node):
    name: str
    name_span: Span


@dataclass
class EnumDecl(Node):
    name: str
    variants: list[EnumVariantDecl] = field(default_factory=list)


@dataclass
class StructFieldInit(Node):
    name: str
    value: Expr
    name_span: Span


@dataclass
class StructLiteral(Node):
    type_name: TypeName
    fields: list[StructFieldInit] = field(default_factory=list)


Stmt = Union[
    LetStmt,
    ExprStmt,
    Block,
    IfStmt,
    WhileStmt,
    ReturnStmt,
    BreakStmt,
    ContinueStmt,
    StructDecl,
    EnumDecl,
]


@dataclass
class Program(Node):
    items: list[FunctionDecl | StructDecl | EnumDecl | Stmt] = field(default_factory=list)
