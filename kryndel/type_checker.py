"""Static name and type checking for Kryndel."""

from __future__ import annotations

from dataclasses import dataclass, field
from difflib import get_close_matches

from . import ast
from .diagnostics import DiagnosticBag
from .source import SourceFile
from .types import (
    ANY,
    BOOL,
    BUILTIN_FUNCTIONS,
    FLOAT,
    FunctionType,
    INT,
    PRIMITIVE_TYPES,
    STRING,
    StructType,
    EnumType,
    Type,
    UI,
    UNKNOWN,
    VOID,
    compatible,
    numeric,
    resolve_type,
)


@dataclass
class Symbol:
    type: Type
    mutable: bool
    declared_at: object


@dataclass
class FunctionInfo:
    type: FunctionType
    declaration: ast.FunctionDecl | None = None


@dataclass
class TypeChecker:
    source: SourceFile
    program: ast.Program
    diagnostics: DiagnosticBag = field(default_factory=DiagnosticBag)
    allowed_imports: set[str] | None = None

    def __post_init__(self) -> None:
        self.scopes: list[dict[str, Symbol]] = [{}]
        self.functions: dict[str, FunctionInfo] = {
            name: FunctionInfo(signature) for name, signature in BUILTIN_FUNCTIONS.items()
        }
        self.struct_declarations: dict[str, ast.StructDecl] = {}
        self.struct_types: dict[str, StructType] = {}
        self.enum_declarations: dict[str, ast.EnumDecl] = {}
        self.enum_types: dict[str, EnumType] = {}
        self.current_function: FunctionInfo | None = None
        self.loop_depth = 0

    def check(self) -> DiagnosticBag:
        self.register_structs()
        self.register_enums()
        declarations = [item for item in self.program.items if isinstance(item, ast.FunctionDecl)]
        for declaration in declarations:
            if declaration.name in self.functions:
                self.error(f"function {declaration.name!r} is already defined", declaration.span, "KRY3001")
                continue
            parameter_types = tuple(self.resolve_declared_type(parameter.type_name.name) for parameter in declaration.parameters)
            for parameter, type_ in zip(declaration.parameters, parameter_types):
                self.ensure_known_type(type_, parameter.type_name.span, parameter.type_name.name)
            return_type = self.resolve_declared_type(declaration.return_type.name)
            self.ensure_known_type(return_type, declaration.return_type.span, declaration.return_type.name)
            self.functions[declaration.name] = FunctionInfo(FunctionType(parameter_types, return_type), declaration)

        top_level = [
            item
            for item in self.program.items
            if not isinstance(item, (ast.FunctionDecl, ast.StructDecl, ast.EnumDecl))
        ]
        self.check_block_like(top_level, VOID)
        for declaration in declarations:
            self.check_function(declaration)
        return self.diagnostics

    def register_structs(self) -> None:
        for declaration in (item for item in self.program.items if isinstance(item, ast.StructDecl)):
            if declaration.name in PRIMITIVE_TYPES or declaration.name in self.struct_declarations:
                self.error(
                    f"struct {declaration.name!r} is already defined",
                    declaration.span,
                    "KRY3025",
                )
                continue
            self.struct_declarations[declaration.name] = declaration
            self.struct_types[declaration.name] = StructType(declaration.name)

        for declaration in self.struct_declarations.values():
            fields: list[tuple[str, Type]] = []
            seen: set[str] = set()
            for field in declaration.fields:
                if field.name in seen:
                    self.error(
                        f"field {field.name!r} is already defined in struct {declaration.name!r}",
                        field.name_span,
                        "KRY3026",
                    )
                    continue
                seen.add(field.name)
                field_type = self.resolve_declared_type(field.type_name.name)
                self.ensure_known_type(field_type, field.type_name.span, field.type_name.name)
                fields.append((field.name, field_type))
            self.struct_types[declaration.name] = StructType(declaration.name, tuple(fields))

    def register_enums(self) -> None:
        function_names = {item.name for item in self.program.items if isinstance(item, ast.FunctionDecl)}
        for declaration in (item for item in self.program.items if isinstance(item, ast.EnumDecl)):
            if (declaration.name in PRIMITIVE_TYPES or declaration.name in self.struct_declarations
                    or declaration.name in self.enum_declarations or declaration.name in BUILTIN_FUNCTIONS
                    or declaration.name in function_names):
                self.error(f"enum {declaration.name!r} is already defined", declaration.span, "KRY3034")
                continue
            self.enum_declarations[declaration.name] = declaration
            self.enum_types[declaration.name] = EnumType(declaration.name)
            seen: set[str] = set()
            variants: list[str] = []
            payloads: list[tuple[str, tuple[Type, ...]]] = []
            for variant in declaration.variants:
                if variant.name in seen:
                    self.error(f"variant {variant.name!r} is already defined in enum {declaration.name!r}", variant.name_span, "KRY3035")
                    continue
                seen.add(variant.name)
                variants.append(variant.name)
                resolved_payloads: list[Type] = []
                for payload_type in variant.payload_types:
                    payload = self.resolve_declared_type(payload_type.name)
                    self.ensure_known_type(payload, payload_type.span, payload_type.name)
                    resolved_payloads.append(payload)
                payloads.append((variant.name, tuple(resolved_payloads)))
            self.enum_types[declaration.name] = EnumType(declaration.name, tuple(variants), tuple(payloads))

    def check_function(self, declaration: ast.FunctionDecl) -> None:
        info = self.functions[declaration.name]
        previous = self.current_function
        self.current_function = info
        self.push_scope()
        for parameter, type_ in zip(declaration.parameters, info.type.parameters):
            self.define(parameter.name, Symbol(type_, False, parameter.span), parameter.span)
        self.check_block(declaration.body)
        if info.type.return_type != VOID and not self.block_contains_return(declaration.body):
            self.error(
                f"function {declaration.name!r} may finish without returning {info.type.return_type}",
                declaration.body.span,
                "KRY3002",
                help="Add a return statement on every control-flow path.",
            )
        self.pop_scope()
        self.current_function = previous

    def block_contains_return(self, block: ast.Block) -> bool:
        for statement in block.statements:
            if isinstance(statement, ast.ReturnStmt):
                return True
            if isinstance(statement, ast.IfStmt):
                if isinstance(statement.else_branch, ast.Block) and self.block_contains_return(statement.then_block) and self.block_contains_return(statement.else_branch):
                    return True
            if isinstance(statement, ast.Block) and self.block_contains_return(statement):
                return True
        return False

    def check_block_like(self, statements: list[ast.Stmt], expected_return: Type) -> None:
        for statement in statements:
            self.check_statement(statement, expected_return)

    def check_block(self, block: ast.Block) -> None:
        self.push_scope()
        self.check_block_like(block.statements, self.current_function.type.return_type if self.current_function else VOID)
        self.pop_scope()

    def check_statement(self, statement: ast.Stmt, expected_return: Type) -> None:
        if isinstance(statement, (ast.StructDecl, ast.EnumDecl)):
            return
        if isinstance(statement, ast.ImportStmt):
            if self.allowed_imports is not None and statement.path.split(".", 1)[0] not in self.allowed_imports:
                self.error(
                    f"package {statement.path.split('.', 1)[0]!r} is not declared in kry.toml",
                    statement.span,
                    "KRY5013",
                    help="Add the package with kry add before importing it.",
                )
            return
        if isinstance(statement, ast.LetStmt):
            actual = self.check_expression(statement.initializer)
            declared = self.resolve_declared_type(statement.annotation.name) if statement.annotation else actual
            if statement.annotation:
                self.ensure_known_type(declared, statement.annotation.span, statement.annotation.name)
                if not compatible(declared, actual):
                    self.error(
                        f"cannot initialize {statement.name!r} with {actual}; expected {declared}",
                        statement.initializer.span,
                        "KRY3003",
                        help=f"Change the expression to {declared} or declare the variable as {actual}.",
                    )
            self.define(statement.name, Symbol(declared, statement.mutable, statement.span), statement.span)
            return
        if isinstance(statement, ast.ExprStmt):
            self.check_expression(statement.expression)
            return
        if isinstance(statement, ast.Block):
            self.check_block(statement)
            return
        if isinstance(statement, ast.IfStmt):
            condition = self.check_expression(statement.condition)
            self.require(BOOL, condition, statement.condition.span, "if condition must be Bool", "KRY3004")
            self.check_block(statement.then_block)
            if isinstance(statement.else_branch, ast.Block):
                self.check_block(statement.else_branch)
            elif isinstance(statement.else_branch, ast.IfStmt):
                self.check_statement(statement.else_branch, expected_return)
            return
        if isinstance(statement, ast.WhileStmt):
            condition = self.check_expression(statement.condition)
            self.require(BOOL, condition, statement.condition.span, "while condition must be Bool", "KRY3005")
            self.loop_depth += 1
            self.check_block(statement.body)
            self.loop_depth -= 1
            return
        if isinstance(statement, ast.ReturnStmt):
            actual = VOID if statement.value is None else self.check_expression(statement.value)
            if not compatible(expected_return, actual):
                self.error(
                    f"return type mismatch: expected {expected_return}, found {actual}",
                    statement.span,
                    "KRY3006",
                )
            return
        if isinstance(statement, (ast.BreakStmt, ast.ContinueStmt)):
            if self.loop_depth == 0:
                keyword = "break" if isinstance(statement, ast.BreakStmt) else "continue"
                self.error(f"{keyword} is only valid inside a while loop", statement.span, "KRY3007")
            return
        if isinstance(statement, ast.MatchStmt):
            self.check_match(statement, expected_return)

    def check_expression(self, expression: ast.Expr) -> Type:
        if isinstance(expression, ast.Literal):
            return {"INT": INT, "FLOAT": FLOAT, "STRING": STRING, "TRUE": BOOL, "FALSE": BOOL, "NIL": VOID}.get(expression.kind, UNKNOWN)
        if isinstance(expression, ast.Name):
            if expression.value in self.enum_types:
                self.error(f"enum {expression.value!r} is a type, not a value; use Enum.Variant", expression.span, "KRY3042")
                return UNKNOWN
            symbol = self.lookup(expression.value)
            if symbol is None:
                names = {name for scope in self.scopes for name in scope}
                suggestion = get_close_matches(expression.value, sorted(names), n=1, cutoff=0.78)
                self.error(
                    f"unknown name {expression.value!r}",
                    expression.span,
                    "KRY3008",
                    help=f"Did you mean {suggestion[0]!r}?" if suggestion else "Declare the variable before using it.",
                )
                return UNKNOWN
            return symbol.type
        if isinstance(expression, ast.Member):
            return self.check_member(expression)
        if isinstance(expression, ast.EnumValue):
            return self.check_enum_value(expression)
        if isinstance(expression, ast.StructLiteral):
            return self.check_struct_literal(expression)
        if isinstance(expression, ast.Unary):
            operand = self.check_expression(expression.operand)
            if expression.operator in ("!", "not"):
                self.require(BOOL, operand, expression.operand.span, "logical negation requires Bool", "KRY3009")
                return BOOL
            if expression.operator == "-":
                if not numeric(operand):
                    self.error("unary - requires Int or Float", expression.span, "KRY3010")
                return operand
            return UNKNOWN
        if isinstance(expression, ast.Binary):
            if expression.operator == "=":
                return self.check_assignment(expression)
            left = self.check_expression(expression.left)
            right = self.check_expression(expression.right)
            return self.check_binary(expression, left, right)
        if isinstance(expression, ast.Call):
            if isinstance(expression.callee, ast.EnumValue):
                expression.callee.payloads = expression.arguments
                return self.check_enum_value(expression.callee)
            return self.check_call(expression)
        return UNKNOWN

    def check_enum_value(self, expression: ast.EnumValue) -> Type:
        enum_type = self.enum_types.get(expression.enum_name)
        if enum_type is None:
            self.error(f"unknown enum {expression.enum_name!r}", expression.enum_span, "KRY3036")
            for payload in expression.payloads:
                self.check_expression(payload)
            return UNKNOWN
        if expression.variant_name not in enum_type.variants:
            suggestion = get_close_matches(expression.variant_name, list(enum_type.variants), n=1, cutoff=0.78)
            self.error(
                f"enum {expression.enum_name!r} has no variant {expression.variant_name!r}",
                expression.variant_span,
                "KRY3037",
                help=f"Did you mean {suggestion[0]!r}?" if suggestion else None,
            )
            for payload in expression.payloads:
                self.check_expression(payload)
            return UNKNOWN
        expected = enum_type.payload_types(expression.variant_name)
        if len(expected) != len(expression.payloads):
            self.error(
                f"variant {expression.enum_name}.{expression.variant_name} expects {len(expected)} payload(s), found {len(expression.payloads)}",
                expression.span,
                "KRY3043",
                help="Provide exactly the declared positional payloads.",
            )
        for index, payload in enumerate(expression.payloads):
            actual = self.check_expression(payload)
            if index < len(expected) and not compatible(expected[index], actual):
                self.error(
                    f"payload {index + 1} of {expression.enum_name}.{expression.variant_name} expects {expected[index]}, found {actual}",
                    payload.span,
                    "KRY3044",
                )
        return enum_type

    def check_match(self, statement: ast.MatchStmt, expected_return: Type) -> None:
        value_type = self.check_expression(statement.value)
        if not isinstance(value_type, EnumType):
            self.error("match requires an enum value", statement.value.span, "KRY3045")
            for arm in statement.arms:
                self.check_statement(arm.body, expected_return)
            return
        covered: set[str] = set()
        wildcard = False
        for arm in statement.arms:
            pattern = arm.pattern
            if pattern.wildcard:
                if wildcard:
                    self.error("duplicate wildcard match arm", pattern.span, "KRY3046")
                wildcard = True
                self.push_scope()
                self.check_statement(arm.body, expected_return)
                self.pop_scope()
                continue
            if pattern.enum_name != value_type.name:
                self.error(
                    f"match pattern belongs to enum {pattern.enum_name!r}, expected {value_type.name!r}",
                    pattern.span,
                    "KRY3045",
                )
            variant = pattern.variant_name or ""
            if variant in covered:
                self.error(f"duplicate match arm for {value_type.name}.{variant}", pattern.span, "KRY3046")
            covered.add(variant)
            if variant not in value_type.variants:
                self.error(f"enum {value_type.name!r} has no variant {variant!r}", pattern.span, "KRY3047")
                payload_types: tuple[Type, ...] = ()
            else:
                payload_types = value_type.payload_types(variant)
            if len(payload_types) != len(pattern.bindings):
                self.error(
                    f"pattern {value_type.name}.{variant} expects {len(payload_types)} binding(s), found {len(pattern.bindings)}",
                    pattern.span,
                    "KRY3048",
                )
            self.push_scope()
            for index, binding in enumerate(pattern.bindings):
                if binding == "_":
                    continue
                if index < len(payload_types):
                    self.define(binding, Symbol(payload_types[index], False, pattern.span), pattern.span)
                else:
                    self.define(binding, Symbol(UNKNOWN, False, pattern.span), pattern.span)
            self.check_statement(arm.body, expected_return)
            self.pop_scope()
        if not wildcard:
            missing = [variant for variant in value_type.variants if variant not in covered]
            if missing:
                self.error(
                    f"non-exhaustive match; missing variant(s): {', '.join(f'{value_type.name}.{name}' for name in missing)}",
                    statement.span,
                    "KRY3049",
                    help="Add a branch for each missing variant or add a _ arm.",
                )

    def check_struct_literal(self, expression: ast.StructLiteral) -> Type:
        struct_type = self.struct_types.get(expression.type_name.name)
        if struct_type is None:
            self.error(
                f"unknown struct type {expression.type_name.name!r}",
                expression.type_name.span,
                "KRY3023",
                help="Declare the struct before constructing a value.",
            )
            for field in expression.fields:
                self.check_expression(field.value)
            return UNKNOWN

        field_types = dict(struct_type.fields)
        provided: set[str] = set()
        for field in expression.fields:
            if field.name in provided:
                self.error(
                    f"field {field.name!r} is provided more than once",
                    field.name_span,
                    "KRY3028",
                )
            provided.add(field.name)
            actual = self.check_expression(field.value)
            expected = field_types.get(field.name)
            if expected is None:
                self.error(
                    f"unknown field {field.name!r} for struct {struct_type.name!r}",
                    field.name_span,
                    "KRY3029",
                    help=f"Use one of: {', '.join(name for name, _ in struct_type.fields)}.",
                )
            elif not compatible(expected, actual):
                self.error(
                    f"field {field.name!r} expects {expected}, found {actual}",
                    field.value.span,
                    "KRY3030",
                )

        missing = [name for name, _ in struct_type.fields if name not in provided]
        if missing:
            self.error(
                f"struct {struct_type.name!r} is missing field(s): {', '.join(missing)}",
                expression.span,
                "KRY3031",
                help="Provide every declared field in the constructor.",
            )
        return struct_type

    def check_member(self, expression: ast.Member) -> Type:
        if isinstance(expression.target, ast.Name) and expression.target.value[:1].isupper():
            if expression.target.value not in self.struct_types and expression.target.value not in self.enum_types:
                self.error(f"unknown enum {expression.target.value!r}", expression.target.span, "KRY3036")
                return UNKNOWN
        target_type = self.check_expression(expression.target)
        if target_type == UNKNOWN:
            return UNKNOWN
        if not isinstance(target_type, StructType):
            self.error(
                f"field access requires a struct value; found {target_type}",
                expression.target.span,
                "KRY3032",
            )
            return UNKNOWN
        resolved_type = self.struct_types.get(target_type.name, target_type)
        field_type = resolved_type.field_type(expression.name)
        if field_type is None:
            self.error(
                f"struct {resolved_type.name!r} has no field {expression.name!r}",
                expression.name_span,
                "KRY3033",
                help=f"Use one of: {', '.join(name for name, _ in resolved_type.fields)}.",
            )
            return UNKNOWN
        return field_type

    def callable_name(self, expression: ast.Expr) -> str | None:
        if isinstance(expression, ast.Name):
            return expression.value
        if isinstance(expression, ast.Member):
            prefix = self.callable_name(expression.target)
            return f"{prefix}.{expression.name}" if prefix else None
        return None

    def check_call(self, expression: ast.Call) -> Type:
        name = self.callable_name(expression.callee)
        info = self.functions.get(name or "")
        if info is None:
            self.error("expression is not callable", expression.callee.span, "KRY3011")
            for argument in expression.arguments:
                self.check_expression(argument)
            return UNKNOWN
        if not info.type.variadic and len(expression.arguments) != len(info.type.parameters):
            help_text = (
                f"Provide {len(info.type.parameters) - len(expression.arguments)} more argument(s)."
                if len(expression.arguments) < len(info.type.parameters)
                else f"Remove {len(expression.arguments) - len(info.type.parameters)} extra argument(s)."
            )
            self.error(
                f"function {name!r} expects {len(info.type.parameters)} argument(s), found {len(expression.arguments)}",
                expression.span,
                "KRY3012",
                help=help_text,
            )
        for index, argument in enumerate(expression.arguments):
            actual = self.check_expression(argument)
            if index < len(info.type.parameters) and not compatible(info.type.parameters[index], actual):
                self.error(
                    f"argument {index + 1} of {name!r} expects {info.type.parameters[index]}, found {actual}",
                    argument.span,
                    "KRY3013",
                )
        return info.type.return_type

    def check_assignment(self, expression: ast.Binary) -> Type:
        if not isinstance(expression.left, ast.Name):
            self.error("assignment target must be a variable name", expression.left.span, "KRY3014")
            self.check_expression(expression.right)
            return UNKNOWN
        symbol = self.lookup(expression.left.value)
        actual = self.check_expression(expression.right)
        if symbol is None:
            self.error(f"unknown name {expression.left.value!r}", expression.left.span, "KRY3008")
            return UNKNOWN
        if not symbol.mutable:
            self.error(
                f"cannot assign to immutable variable {expression.left.value!r}",
                expression.left.span,
                "KRY3015",
                help="Declare it with let mut if reassignment is intended.",
            )
        if not compatible(symbol.type, actual):
            self.error(f"cannot assign {actual} to {symbol.type}", expression.right.span, "KRY3016")
        return symbol.type

    def check_binary(self, expression: ast.Binary, left: Type, right: Type) -> Type:
        operator = expression.operator
        if operator in ("and", "or"):
            self.require(BOOL, left, expression.left.span, f"left side of {operator} must be Bool", "KRY3017")
            self.require(BOOL, right, expression.right.span, f"right side of {operator} must be Bool", "KRY3018")
            return BOOL
        if operator in ("==", "!="):
            if not compatible(left, right) and not compatible(right, left):
                self.error(f"cannot compare {left} with {right}", expression.span, "KRY3019")
            return BOOL
        if operator in ("<", "<=", ">", ">="):
            if not ((numeric(left) and numeric(right)) or (left == STRING and right == STRING)):
                self.error("ordering comparison requires two numbers or two strings", expression.span, "KRY3020")
            return BOOL
        if operator == "+" and left == STRING and right == STRING:
            return STRING
        if operator in ("+", "-", "*", "/", "%"):
            if not numeric(left) or not numeric(right):
                self.error(f"operator {operator} requires numeric operands", expression.span, "KRY3021")
                return UNKNOWN
            return FLOAT if FLOAT in (left, right) or operator == "/" else INT
        self.error(f"unknown operator {operator!r}", expression.span, "KRY3022")
        return UNKNOWN

    def require(self, expected: Type, actual: Type, span: object, message: str, code: str) -> None:
        if not compatible(expected, actual):
            self.error(f"{message}; found {actual}", span, code)

    def resolve_declared_type(self, name: str) -> Type:
        if name in self.struct_types:
            return self.struct_types[name]
        if name in self.enum_types:
            return self.enum_types[name]
        if any(isinstance(item, ast.EnumDecl) and item.name == name for item in self.program.items):
            return EnumType(name)
        return resolve_type(name)

    def ensure_known_type(self, type_: Type, span: object, name: str | None = None) -> None:
        if type_ == UNKNOWN:
            candidates = sorted(set(PRIMITIVE_TYPES) | set(self.struct_types) | set(self.enum_types))
            close = get_close_matches(name or "", candidates, n=1, cutoff=0.72)
            help_text = f"Did you mean {close[0]!r}?" if close else "Use a built-in type, struct, or enum name."
            self.error("unknown type name", span, "KRY3023", help=help_text)

    def define(self, name: str, symbol: Symbol, span: object) -> None:
        if name in self.scopes[-1]:
            self.error(f"name {name!r} is already defined in this scope", span, "KRY3024")
        self.scopes[-1][name] = symbol

    def lookup(self, name: str) -> Symbol | None:
        for scope in reversed(self.scopes):
            if name in scope:
                return scope[name]
        return None

    def push_scope(self) -> None:
        self.scopes.append({})

    def pop_scope(self) -> None:
        self.scopes.pop()

    def error(self, message: str, span: object, code: str, *, help: str | None = None) -> None:
        self.diagnostics.error(message, span, code=code, help=help)


def check_types(source: SourceFile, program: ast.Program, allowed_imports: set[str] | None = None) -> DiagnosticBag:
    return TypeChecker(source, program, allowed_imports=allowed_imports).check()
