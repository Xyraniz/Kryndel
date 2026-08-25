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
    payloads: list["Expr"] = field(default_factory=list)


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


@dataclass
class ArrayLiteral(Node):
    elements: list["Expr"] = field(default_factory=list)


@dataclass
class TupleLiteral(Node):
    elements: list["Expr"] = field(default_factory=list)


@dataclass
class Index(Node):
    target: "Expr"
    index: "Expr"


Expr = Union[
    Literal, Name, Member, EnumValue, Unary, Binary, Call,
    ArrayLiteral, TupleLiteral, Index, "StructLiteral"
]


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
    """Exit the innermost while loop."""


@dataclass
class ContinueStmt(Node):
    """Continue the innermost while loop."""


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
    public: bool = False
    test: bool = False


@dataclass
class StructFieldDecl(Node):
    name: str
    type_name: TypeName
    name_span: Span


@dataclass
class StructDecl(Node):
    name: str
    fields: list[StructFieldDecl] = field(default_factory=list)
    public: bool = False


@dataclass
class EnumVariantDecl(Node):
    name: str
    name_span: Span
    payload_types: list[TypeName] = field(default_factory=list)


@dataclass
class EnumDecl(Node):
    name: str
    variants: list[EnumVariantDecl] = field(default_factory=list)
    public: bool = False


@dataclass
class StructFieldInit(Node):
    name: str
    value: Expr
    name_span: Span


@dataclass
class StructLiteral(Node):
    type_name: TypeName
    fields: list[StructFieldInit] = field(default_factory=list)


@dataclass
class MatchPattern(Node):
    enum_name: str | None
    variant_name: str | None
    bindings: list[str] = field(default_factory=list)
    wildcard: bool = False


@dataclass
class MatchArm(Node):
    pattern: MatchPattern
    body: "Stmt"


@dataclass
class MatchStmt(Node):
    value: Expr
    arms: list[MatchArm] = field(default_factory=list)


@dataclass
class ImportStmt(Node):
    path: str


Stmt = Union[
    LetStmt,
    ExprStmt,
    Block,
    IfStmt,
    WhileStmt,
    ReturnStmt,
    BreakStmt,
    ContinueStmt,
    MatchStmt,
    ImportStmt,
    StructDecl,
    EnumDecl,
]


@dataclass
class Program(Node):
    items: list[FunctionDecl | StructDecl | EnumDecl | Stmt] = field(default_factory=list)
