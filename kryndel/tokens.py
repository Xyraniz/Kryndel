"""Token definitions and the dependency-free Kryndel lexer."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Final

from .diagnostics import DiagnosticBag, Span
from .source import SourceFile


@dataclass(frozen=True)
class Token:
    kind: str
    value: object
    span: Span

    def __repr__(self) -> str:
        return f"Token({self.kind!r}, {self.value!r}, {self.span.line}:{self.span.column})"


KEYWORDS: Final[dict[str, str]] = {
    "let": "LET",
    "mut": "MUT",
    "fn": "FN",
    "if": "IF",
    "else": "ELSE",
    "while": "WHILE",
    "return": "RETURN",
    "true": "TRUE",
    "false": "FALSE",
    "nil": "NIL",
    "and": "AND",
    "or": "OR",
    "not": "NOT",
    "break": "BREAK",
    "continue": "CONTINUE",
    "struct": "STRUCT",
    "enum": "ENUM",
}

TWO_CHAR_TOKENS: Final[dict[str, str]] = {
    "->": "ARROW",
    "==": "EQEQ",
    "!=": "NEQ",
    "<=": "LTE",
    ">=": "GTE",
    "&&": "ANDAND",
    "||": "OROR",
}

ONE_CHAR_TOKENS: Final[dict[str, str]] = {
    "(": "LPAREN",
    ")": "RPAREN",
    "{": "LBRACE",
    "}": "RBRACE",
    "[": "LBRACKET",
    "]": "RBRACKET",
    ",": "COMMA",
    ":": "COLON",
    ";": "SEMICOLON",
    ".": "DOT",
    "+": "PLUS",
    "-": "MINUS",
    "*": "STAR",
    "/": "SLASH",
    "%": "PERCENT",
    "=": "EQUAL",
    "<": "LT",
    ">": "GT",
    "!": "BANG",
}


class Lexer:
    """Turn source text into tokens while collecting recoverable errors."""

    def __init__(self, source: SourceFile) -> None:
        self.source = source
        self.text = source.text
        self.index = 0
        self.diagnostics = DiagnosticBag()
        self.tokens: list[Token] = []

    def scan(self) -> list[Token]:
        while not self.at_end:
            self.scan_token()
        eof = self.source.span(len(self.text), len(self.text) + 1)
        self.tokens.append(Token("EOF", None, eof))
        return self.tokens

    @property
    def at_end(self) -> bool:
        return self.index >= len(self.text)

    def peek(self, offset: int = 0) -> str:
        position = self.index + offset
        return "\0" if position >= len(self.text) else self.text[position]

    def advance(self) -> str:
        character = self.peek()
        self.index += 1
        return character

    def add(self, kind: str, value: object, start: int) -> None:
        self.tokens.append(Token(kind, value, self.source.span(start, self.index)))

    def scan_token(self) -> None:
        character = self.advance()
        start = self.index - 1

        if character in " \t\r\n":
            return
        if character == "/" and self.peek() == "/":
            self.advance()
            while self.peek() not in ("\n", "\0"):
                self.advance()
            return
        if character == "/" and self.peek() == "*":
            self.advance()
            self.scan_block_comment(start)
            return
        if character.isalpha() or character == "_":
            self.scan_identifier(start)
            return
        if character.isdigit():
            self.scan_number(start)
            return
        if character == '"':
            self.scan_string(start)
            return

        pair = character + self.peek()
        if pair in TWO_CHAR_TOKENS:
            self.advance()
            self.add(TWO_CHAR_TOKENS[pair], pair, start)
            return
        if character in ONE_CHAR_TOKENS:
            self.add(ONE_CHAR_TOKENS[character], character, start)
            return

        self.diagnostics.error(
            f"unexpected character {character!r}",
            self.source.span(start, self.index),
            code="KRY1001",
            help="Remove the character or use a supported operator.",
        )

    def scan_block_comment(self, start: int) -> None:
        depth = 1
        while not self.at_end and depth:
            if self.peek() == "/" and self.peek(1) == "*":
                self.advance()
                self.advance()
                depth += 1
            elif self.peek() == "*" and self.peek(1) == "/":
                self.advance()
                self.advance()
                depth -= 1
            else:
                self.advance()
        if depth:
            self.diagnostics.error(
                "unterminated block comment",
                self.source.span(start, self.index),
                code="KRY1002",
                help="Close the comment with */.",
            )

    def scan_identifier(self, start: int) -> None:
        while self.peek().isalnum() or self.peek() == "_":
            self.advance()
        value = self.text[start:self.index]
        self.add(KEYWORDS.get(value, "IDENT"), value, start)

    def scan_number(self, start: int) -> None:
        while self.peek().isdigit():
            self.advance()
        kind = "INT"
        if self.peek() == "." and self.peek(1).isdigit():
            kind = "FLOAT"
            self.advance()
            while self.peek().isdigit():
                self.advance()
        raw = self.text[start:self.index]
        try:
            value: int | float = float(raw) if kind == "FLOAT" else int(raw)
        except ValueError:
            value = 0
            self.diagnostics.error(
                f"invalid numeric literal {raw!r}",
                self.source.span(start, self.index),
                code="KRY1003",
            )
        self.add(kind, value, start)

    def scan_string(self, start: int) -> None:
        pieces: list[str] = []
        escape_names = {"n": "\n", "r": "\r", "t": "\t", '"': '"', "\\": "\\", "0": "\0"}
        while not self.at_end and self.peek() != '"':
            character = self.advance()
            if character == "\n":
                self.diagnostics.error(
                    "newline in string literal",
                    self.source.span(start, self.index),
                    code="KRY1004",
                    help="Use \\n for a newline or close the string before the end of the line.",
                )
                return
            if character == "\\":
                escaped = self.advance()
                if escaped not in escape_names:
                    self.diagnostics.error(
                        f"unknown escape sequence \\{escaped}",
                        self.source.span(self.index - 2, self.index),
                        code="KRY1005",
                        help=r'Supported escapes are \\n, \\r, \\t, \\0, \", and \\\\.',
                    )
                    pieces.append(escaped)
                else:
                    pieces.append(escape_names[escaped])
            else:
                pieces.append(character)
        if self.at_end:
            self.diagnostics.error(
                "unterminated string literal",
                self.source.span(start, self.index),
                code="KRY1006",
                help="Close the string with a double quote.",
            )
            return
        self.advance()
        self.add("STRING", "".join(pieces), start)


def lex(source: SourceFile) -> tuple[list[Token], DiagnosticBag]:
    lexer = Lexer(source)
    tokens = lexer.scan()
    return tokens, lexer.diagnostics
