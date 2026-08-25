"""Recursive-descent parser for Kryndel."""

from __future__ import annotations

from dataclasses import dataclass

from . import ast
from .diagnostics import DiagnosticBag, Span
from .source import SourceFile
from .tokens import Token


class ParseAbort(Exception):
    """Abort the current construct and recover at a statement boundary."""


@dataclass
class Parser:
    source: SourceFile
    tokens: list[Token]

    def __post_init__(self) -> None:
        self.index = 0
        self.diagnostics = DiagnosticBag()

    @property
    def current(self) -> Token:
        return self.tokens[min(self.index, len(self.tokens) - 1)]

    def peek(self, offset: int = 1) -> Token:
        return self.tokens[min(self.index + offset, len(self.tokens) - 1)]

    def advance(self) -> Token:
        token = self.current
        if token.kind != "EOF":
            self.index += 1
        return token

    def check(self, *kinds: str) -> bool:
        return self.current.kind in kinds

    def match(self, *kinds: str) -> Token | None:
        if self.current.kind in kinds:
            return self.advance()
        return None

    def expect(self, kind: str, message: str, code: str = "KRY2001") -> Token:
        if self.check(kind):
            return self.advance()
        token = self.current
        self.diagnostics.error(message, token.span, code=code)
        raise ParseAbort

    def parse(self) -> ast.Program:
        items: list[ast.FunctionDecl | ast.Stmt] = []
        start = self.current.span
        while not self.check("EOF"):
            try:
                if self.check("FN"):
                    items.append(self.parse_function())
                elif self.check("STRUCT"):
                    items.append(self.parse_struct())
                elif self.check("ENUM"):
                    items.append(self.parse_enum())
                else:
                    items.append(self.parse_statement())
            except ParseAbort:
                self.synchronize()
        end = self.current.span
        program = ast.Program(self.merge(start, end), items)
        self.rewrite_enum_values(program)
        return program

    def parse_function(self) -> ast.FunctionDecl:
        start = self.expect("FN", "expected fn").span
        name = self.expect("IDENT", "expected a function name after fn", "KRY2002")
        self.expect("LPAREN", "expected ( after function name")
        parameters: list[ast.Parameter] = []
        if not self.check("RPAREN"):
            while True:
                parameter_name = self.expect("IDENT", "expected a parameter name", "KRY2003")
                self.expect("COLON", "expected : after parameter name")
                parameter_type = self.parse_type_name()
                parameters.append(ast.Parameter(self.merge(parameter_name.span, parameter_type.span), str(parameter_name.value), parameter_type))
                if not self.match("COMMA"):
                    break
        self.expect("RPAREN", "expected ) after function parameters")
        self.expect("ARROW", "expected -> followed by the return type")
        return_type = self.parse_type_name()
        body = self.parse_block()
        return ast.FunctionDecl(self.merge(start, body.span), str(name.value), parameters, return_type, body)

    def parse_struct(self) -> ast.StructDecl:
        start = self.expect("STRUCT", "expected struct").span
        name = self.expect("IDENT", "expected a struct name after struct", "KRY2008")
        self.expect("LBRACE", "expected { after struct name")
        fields: list[ast.StructFieldDecl] = []
        while not self.check("RBRACE") and not self.check("EOF"):
            field_name = self.expect("IDENT", "expected a field name", "KRY2009")
            self.expect("COLON", "expected : after field name")
            field_type = self.parse_type_name()
            fields.append(
                ast.StructFieldDecl(
                    self.merge(field_name.span, field_type.span),
                    str(field_name.value),
                    field_type,
                    field_name.span,
                )
            )
        closing = self.expect("RBRACE", "expected } to close struct declaration")
        return ast.StructDecl(self.merge(start, closing.span), str(name.value), fields)

    def parse_enum(self) -> ast.EnumDecl:
        start = self.expect("ENUM", "expected enum").span
        name = self.expect("IDENT", "expected an enum name after enum", "KRY2011")
        self.expect("LBRACE", "expected { after enum name")
        variants: list[ast.EnumVariantDecl] = []
        while not self.check("RBRACE") and not self.check("EOF"):
            variant = self.expect("IDENT", "expected an enum variant name", "KRY2012")
            payload_types: list[ast.TypeName] = []
            if self.match("LPAREN"):
                if not self.check("RPAREN"):
                    while True:
                        payload_types.append(self.parse_type_name())
                        if not self.match("COMMA"):
                            break
                self.expect("RPAREN", "expected ) after enum payload types", "KRY2013")
            end = payload_types[-1].span if payload_types else variant.span
            variants.append(ast.EnumVariantDecl(self.merge(variant.span, end), str(variant.value), variant.span, payload_types))
        closing = self.expect("RBRACE", "expected } to close enum declaration")
        return ast.EnumDecl(self.merge(start, closing.span), str(name.value), variants)

    def parse_type_name(self) -> ast.TypeName:
        token = self.expect("IDENT", "expected a type name", "KRY2004")
        return ast.TypeName(token.span, str(token.value))

    def parse_statement(self) -> ast.Stmt:
        if self.match("LET"):
            return self.parse_let(self.tokens[self.index - 1].span)
        if self.match("IF"):
            return self.parse_if(self.tokens[self.index - 1].span)
        if self.match("WHILE"):
            return self.parse_while(self.tokens[self.index - 1].span)
        if self.match("RETURN"):
            return self.parse_return(self.tokens[self.index - 1].span)
        if self.match("BREAK"):
            token = self.tokens[self.index - 1]
            self.optional_semicolon(token.span)
            return ast.BreakStmt(token.span)
        if self.match("CONTINUE"):
            token = self.tokens[self.index - 1]
            self.optional_semicolon(token.span)
            return ast.ContinueStmt(token.span)
        if self.match("MATCH"):
            return self.parse_match(self.tokens[self.index - 1].span)
        if self.match("IMPORT"):
            start = self.tokens[self.index - 1].span
            first = self.expect("IDENT", "expected a package name after import", "KRY2050")
            path = [str(first.value)]
            end = first.span
            while self.match("DOT"):
                member = self.expect("IDENT", "expected a module name after .", "KRY2051")
                path.append(str(member.value))
                end = member.span
            end = self.optional_semicolon(end)
            return ast.ImportStmt(self.merge(start, end), ".".join(path))
        if self.check("LBRACE"):
            return self.parse_block()
        expression = self.parse_expression()
        end = self.optional_semicolon(expression.span)
        return ast.ExprStmt(self.merge(expression.span, end), expression)

    def parse_let(self, start: Span) -> ast.LetStmt:
        mutable = bool(self.match("MUT"))
        name = self.expect("IDENT", "expected a variable name after let", "KRY2005")
        annotation = None
        if self.match("COLON"):
            annotation = self.parse_type_name()
        self.expect("EQUAL", "expected = in variable declaration")
        initializer = self.parse_expression()
        end = self.optional_semicolon(initializer.span)
        return ast.LetStmt(self.merge(start, end), str(name.value), annotation, initializer, mutable)

    def parse_if(self, start: Span) -> ast.IfStmt:
        condition = self.parse_expression()
        then_block = self.parse_block()
        else_branch: ast.Block | ast.IfStmt | None = None
        if else_token := self.match("ELSE"):
            if self.match("IF"):
                else_branch = self.parse_if(else_token.span)
            else:
                else_branch = self.parse_block()
        end = else_branch.span if else_branch else then_block.span
        return ast.IfStmt(self.merge(start, end), condition, then_block, else_branch)

    def parse_while(self, start: Span) -> ast.WhileStmt:
        condition = self.parse_expression()
        body = self.parse_block()
        return ast.WhileStmt(self.merge(start, body.span), condition, body)

    def parse_return(self, start: Span) -> ast.ReturnStmt:
        if self.check("SEMICOLON") or self.check("RBRACE") or self.check("EOF"):
            end = self.optional_semicolon(start)
            return ast.ReturnStmt(self.merge(start, end), None)
        value = self.parse_expression()
        end = self.optional_semicolon(value.span)
        return ast.ReturnStmt(self.merge(start, end), value)

    def parse_match(self, start: Span) -> ast.MatchStmt:
        value = self.parse_expression()
        self.expect("LBRACE", "expected { after match value", "KRY2052")
        arms: list[ast.MatchArm] = []
        while not self.check("RBRACE", "EOF"):
            pattern_start = self.current.span
            if self.check("IDENT") and self.current.value == "_":
                self.advance()
                pattern = ast.MatchPattern(pattern_start, None, None, [], True)
            else:
                enum_name = str(self.expect("IDENT", "expected an enum name in match pattern", "KRY2053").value)
                self.expect("DOT", "expected . between enum and variant", "KRY2054")
                variant = self.expect("IDENT", "expected a variant name in match pattern", "KRY2055")
                bindings: list[str] = []
                end = variant.span
                if self.match("LPAREN"):
                    if not self.check("RPAREN"):
                        while True:
                            binding = self.expect("IDENT", "expected a binding name in match pattern", "KRY2056")
                            bindings.append(str(binding.value))
                            if not self.match("COMMA"):
                                break
                    end = self.expect("RPAREN", "expected ) after match bindings", "KRY2057").span
                pattern = ast.MatchPattern(self.merge(pattern_start, end), enum_name, str(variant.value), bindings)
            self.expect("FATARROW", "expected => after match pattern", "KRY2058")
            body = self.parse_block() if self.check("LBRACE") else self.parse_statement()
            arms.append(ast.MatchArm(self.merge(pattern.span, body.span), pattern, body))
        closing = self.expect("RBRACE", "expected } to close match", "KRY2059")
        return ast.MatchStmt(self.merge(start, closing.span), value, arms)

    def parse_block(self) -> ast.Block:
        opening = self.expect("LBRACE", "expected { to start a block")
        statements: list[ast.Stmt] = []
        while not self.check("RBRACE") and not self.check("EOF"):
            try:
                statements.append(self.parse_statement())
            except ParseAbort:
                self.synchronize()
        closing = self.expect("RBRACE", "expected } to close the block")
        return ast.Block(self.merge(opening.span, closing.span), statements)

    def optional_semicolon(self, fallback: Span) -> Span:
        token = self.match("SEMICOLON")
        return token.span if token else fallback

    def parse_expression(self) -> ast.Expr:
        return self.parse_assignment()

    def parse_assignment(self) -> ast.Expr:
        expression = self.parse_or()
        if equal := self.match("EQUAL"):
            right = self.parse_assignment()
            return ast.Binary(self.merge(expression.span, right.span), expression, "=", right)
        return expression

    def parse_or(self) -> ast.Expr:
        expression = self.parse_and()
        while self.match("OR", "OROR"):
            right = self.parse_and()
            expression = ast.Binary(self.merge(expression.span, right.span), expression, "or", right)
        return expression

    def parse_and(self) -> ast.Expr:
        expression = self.parse_equality()
        while self.match("AND", "ANDAND"):
            right = self.parse_equality()
            expression = ast.Binary(self.merge(expression.span, right.span), expression, "and", right)
        return expression

    def parse_equality(self) -> ast.Expr:
        expression = self.parse_comparison()
        while token := self.match("EQEQ", "NEQ"):
            right = self.parse_comparison()
            expression = ast.Binary(self.merge(expression.span, right.span), expression, str(token.value), right)
        return expression

    def parse_comparison(self) -> ast.Expr:
        expression = self.parse_term()
        while token := self.match("LT", "LTE", "GT", "GTE"):
            right = self.parse_term()
            expression = ast.Binary(self.merge(expression.span, right.span), expression, str(token.value), right)
        return expression

    def parse_term(self) -> ast.Expr:
        expression = self.parse_factor()
        while token := self.match("PLUS", "MINUS"):
            right = self.parse_factor()
            expression = ast.Binary(self.merge(expression.span, right.span), expression, str(token.value), right)
        return expression

    def parse_factor(self) -> ast.Expr:
        expression = self.parse_unary()
        while token := self.match("STAR", "SLASH", "PERCENT"):
            right = self.parse_unary()
            expression = ast.Binary(self.merge(expression.span, right.span), expression, str(token.value), right)
        return expression

    def parse_unary(self) -> ast.Expr:
        if token := self.match("BANG", "MINUS", "NOT"):
            operand = self.parse_unary()
            return ast.Unary(self.merge(token.span, operand.span), str(token.value), operand)
        return self.parse_call()

    def parse_call(self) -> ast.Expr:
        expression = self.parse_primary()
        while True:
            if self.match("LPAREN"):
                arguments: list[ast.Expr] = []
                if not self.check("RPAREN"):
                    while True:
                        arguments.append(self.parse_expression())
                        if not self.match("COMMA"):
                            break
                closing = self.expect("RPAREN", "expected ) after arguments")
                expression = ast.Call(self.merge(expression.span, closing.span), expression, arguments)
            elif self.match("DOT"):
                member = self.expect("IDENT", "expected a member name after .", "KRY2006")
                expression = ast.Member(
                    self.merge(expression.span, member.span),
                    expression,
                    str(member.value),
                    member.span,
                )
            else:
                break
        return expression

    def parse_primary(self) -> ast.Expr:
        token = self.current
        if self.match("INT", "FLOAT", "STRING", "TRUE", "FALSE", "NIL"):
            return ast.Literal(token.span, token.value, token.kind)
        if self.is_struct_literal_start():
            self.advance()
            return self.parse_struct_literal(token)
        if self.match("IDENT"):
            return ast.Name(token.span, str(token.value))
        if self.match("LPAREN"):
            expression = self.parse_expression()
            self.expect("RPAREN", "expected ) after expression")
            return expression
        self.diagnostics.error(
            "expected an expression",
            token.span,
            code="KRY2007",
            help="Use a literal, variable, function call, or parenthesized expression.",
        )
        raise ParseAbort

    def is_struct_literal_start(self) -> bool:
        if not self.check("IDENT") or self.peek().kind != "LBRACE":
            return False
        first_field = self.tokens[min(self.index + 2, len(self.tokens) - 1)]
        if first_field.kind == "RBRACE":
            return True
        next_token = self.tokens[min(self.index + 3, len(self.tokens) - 1)]
        return first_field.kind == "IDENT" and next_token.kind == "COLON"

    def parse_struct_literal(self, type_token: Token) -> ast.StructLiteral:
        self.expect("LBRACE", "expected { after struct type name")
        fields: list[ast.StructFieldInit] = []
        if not self.check("RBRACE"):
            while True:
                field_name = self.expect("IDENT", "expected a field name in struct literal", "KRY2010")
                self.expect("COLON", "expected : after struct field name")
                value = self.parse_expression()
                fields.append(
                    ast.StructFieldInit(
                        self.merge(field_name.span, value.span),
                        str(field_name.value),
                        value,
                        field_name.span,
                    )
                )
                if not self.match("COMMA"):
                    break
                if self.check("RBRACE"):
                    break
        closing = self.expect("RBRACE", "expected } to close struct literal")
        type_name = ast.TypeName(type_token.span, str(type_token.value))
        return ast.StructLiteral(self.merge(type_token.span, closing.span), type_name, fields)

    def synchronize(self) -> None:
        while not self.check("EOF"):
            if self.match("SEMICOLON"):
                return
            if self.check("RBRACE", "FN", "STRUCT", "ENUM", "LET", "IF", "WHILE", "RETURN"):
                return
            self.advance()

    def rewrite_enum_values(self, program: ast.Program) -> None:
        enum_names = {item.name for item in program.items if isinstance(item, ast.EnumDecl)}

        def expression(value: ast.Expr) -> ast.Expr:
            if isinstance(value, ast.Member):
                value.target = expression(value.target)
                if isinstance(value.target, ast.Name) and value.target.value in enum_names:
                    return ast.EnumValue(value.span, value.target.value, value.name, value.target.span, value.name_span)
                return value
            if isinstance(value, ast.Call):
                value.callee = expression(value.callee)
                value.arguments = [expression(argument) for argument in value.arguments]
                if isinstance(value.callee, ast.EnumValue):
                    value.callee.payloads = value.arguments
                    return value.callee
                return value
            if isinstance(value, ast.Binary):
                value.left, value.right = expression(value.left), expression(value.right)
            elif isinstance(value, ast.Unary):
                value.operand = expression(value.operand)
            elif isinstance(value, ast.Call):
                value.callee = expression(value.callee)
                value.arguments = [expression(argument) for argument in value.arguments]
            elif isinstance(value, ast.StructLiteral):
                for field in value.fields:
                    field.value = expression(field.value)
            return value

        def statement(value: ast.Stmt) -> None:
            if isinstance(value, ast.LetStmt):
                value.initializer = expression(value.initializer)
            elif isinstance(value, ast.ExprStmt):
                value.expression = expression(value.expression)
            elif isinstance(value, ast.ReturnStmt) and value.value is not None:
                value.value = expression(value.value)
            elif isinstance(value, ast.IfStmt):
                value.condition = expression(value.condition)
                for nested in value.then_block.statements:
                    statement(nested)
                if isinstance(value.else_branch, ast.Block):
                    for nested in value.else_branch.statements:
                        statement(nested)
            elif isinstance(value, ast.WhileStmt):
                value.condition = expression(value.condition)
                for nested in value.body.statements:
                    statement(nested)
            elif isinstance(value, ast.Block):
                for nested in value.statements:
                    statement(nested)
            elif isinstance(value, ast.MatchStmt):
                value.value = expression(value.value)
                for arm in value.arms:
                    statement(arm.body)

        for item in program.items:
            if isinstance(item, ast.FunctionDecl):
                for nested in item.body.statements:
                    statement(nested)
            elif not isinstance(item, (ast.StructDecl, ast.EnumDecl)):
                statement(item)

    @staticmethod
    def merge(left: Span, right: Span) -> Span:
        return Span(left.start, right.end, left.line, left.column)


def parse(source: SourceFile, tokens: list[Token]) -> tuple[ast.Program, DiagnosticBag]:
    parser = Parser(source, tokens)
    program = parser.parse()
    return program, parser.diagnostics
