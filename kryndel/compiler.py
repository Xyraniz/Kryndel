"""AST-to-bytecode compiler for Kryndel."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

from . import ast
from .bytecode import BytecodeFunction, Instruction, Module
from .diagnostics import DiagnosticError
from .parser import parse
from .source import SourceFile
from .tokens import lex
from .type_checker import check_types


@dataclass
class FunctionCompiler:
    declaration: ast.FunctionDecl | None
    name: str
    arity: int
    structs: dict[str, ast.StructDecl] = field(default_factory=dict)
    enums: dict[str, ast.EnumDecl] = field(default_factory=dict)
    namespace: str = ""
    local_functions: set[str] = field(default_factory=set)
    function: BytecodeFunction = field(init=False)
    scopes: list[dict[str, str]] = field(default_factory=lambda: [{}])
    loop_stack: list[tuple[int, list[int], list[int]]] = field(default_factory=list)

    def __post_init__(self) -> None:
        self.function = BytecodeFunction(self.name, self.arity)

    def emit(self, op: str, arg: object = None, line: int = 0) -> int:
        self.function.instructions.append(Instruction(op, arg, line))
        return len(self.function.instructions) - 1

    def constant(self, value: object) -> int:
        return self.function.add_constant(value)

    def patch(self, index: int, target: int) -> None:
        self.function.instructions[index].arg = target

    def compile(self, statements: list[ast.Stmt], parameters: list[str] = ()) -> BytecodeFunction:
        self.scopes = [{name: name for name in parameters}]
        self.function.parameters = list(parameters)
        for statement in statements:
            self.compile_statement(statement)
        self.emit("PUSH_NIL")
        self.emit("RETURN")
        return self.function

    def compile_block(self, block: ast.Block) -> None:
        self.scopes.append({})
        for statement in block.statements:
            self.compile_statement(statement)
        self.scopes.pop()

    def compile_statement(self, statement: ast.Stmt) -> None:
        if isinstance(statement, (ast.StructDecl, ast.EnumDecl)):
            return
        if isinstance(statement, ast.ImportStmt):
            return
        if isinstance(statement, ast.LetStmt):
            internal = f"{statement.name}#{len(self.scopes)}#{len(self.function.instructions)}"
            self.compile_expression(statement.initializer)
            self.emit("STORE", internal, statement.span.line)
            self.scopes[-1][statement.name] = internal
            return
        if isinstance(statement, ast.ExprStmt):
            self.compile_expression(statement.expression)
            self.emit("POP", line=statement.span.line)
            return
        if isinstance(statement, ast.Block):
            self.compile_block(statement)
            return
        if isinstance(statement, ast.IfStmt):
            self.compile_expression(statement.condition)
            false_jump = self.emit("JUMP_IF_FALSE", None, statement.condition.span.line)
            self.compile_block(statement.then_block)
            if statement.else_branch is None:
                self.patch(false_jump, len(self.function.instructions))
            else:
                end_jump = self.emit("JUMP", None, statement.span.line)
                self.patch(false_jump, len(self.function.instructions))
                if isinstance(statement.else_branch, ast.Block):
                    self.compile_block(statement.else_branch)
                else:
                    self.compile_statement(statement.else_branch)
                self.patch(end_jump, len(self.function.instructions))
            return
        if isinstance(statement, ast.WhileStmt):
            start = len(self.function.instructions)
            self.compile_expression(statement.condition)
            false_jump = self.emit("JUMP_IF_FALSE", None, statement.condition.span.line)
            self.loop_stack.append((start, [], []))
            self.compile_block(statement.body)
            self.emit("JUMP", start, statement.span.line)
            end = len(self.function.instructions)
            self.patch(false_jump, end)
            _, breaks, continues = self.loop_stack.pop()
            for index in breaks:
                self.patch(index, end)
            for index in continues:
                self.patch(index, start)
            return
        if isinstance(statement, ast.ReturnStmt):
            if statement.value is None:
                self.emit("PUSH_NIL", line=statement.span.line)
            else:
                self.compile_expression(statement.value)
            self.emit("RETURN", line=statement.span.line)
            return
        if isinstance(statement, ast.BreakStmt):
            if self.loop_stack:
                self.loop_stack[-1][1].append(self.emit("JUMP", None, statement.span.line))
            return
        if isinstance(statement, ast.ContinueStmt):
            if self.loop_stack:
                self.loop_stack[-1][2].append(self.emit("JUMP", None, statement.span.line))
            return
        if isinstance(statement, ast.MatchStmt):
            self.compile_match(statement)

    def compile_match(self, statement: ast.MatchStmt) -> None:
        value_slot = f"__match_value#{len(self.function.instructions)}"
        self.compile_expression(statement.value)
        self.emit("STORE", value_slot, statement.value.span.line)
        end_jumps: list[int] = []
        for arm in statement.arms:
            pattern = arm.pattern
            false_jump: int | None = None
            if not pattern.wildcard:
                self.emit("LOAD", value_slot, pattern.span.line)
                payload_types = self.enums.get(pattern.enum_name or "")
                arity = 0
                if payload_types is not None:
                    variant = next((item for item in payload_types.variants if item.name == pattern.variant_name), None)
                    arity = len(variant.payload_types) if variant else 0
                false_jump = self.emit(
                    "MATCH_ENUM",
                    {"type": pattern.enum_name, "variant": pattern.variant_name, "arity": arity},
                    pattern.span.line,
                )
                false_jump = self.emit("JUMP_IF_FALSE", None, pattern.span.line)
            self.scopes.append({})
            bindings: list[str] = []
            for binding in pattern.bindings:
                if binding == "_":
                    bindings.append("")
                    continue
                internal = f"{binding}#{len(self.scopes)}#{len(self.function.instructions)}"
                self.scopes[-1][binding] = internal
                bindings.append(internal)
            if bindings and not pattern.wildcard:
                self.emit(
                    "BIND_ENUM",
                    {"source": value_slot, "bindings": bindings, "arity": len(bindings)},
                    pattern.span.line,
                )
            if isinstance(arm.body, ast.Block):
                for nested in arm.body.statements:
                    self.compile_statement(nested)
            else:
                self.compile_statement(arm.body)
            self.scopes.pop()
            if not pattern.wildcard:
                end_jumps.append(self.emit("JUMP", None, arm.span.line))
                assert false_jump is not None
                self.patch(false_jump, len(self.function.instructions))
        end = len(self.function.instructions)
        for index in end_jumps:
            self.patch(index, end)

    def compile_expression(self, expression: ast.Expr) -> None:
        if isinstance(expression, ast.Literal):
            if expression.kind == "NIL":
                self.emit("PUSH_NIL", line=expression.span.line)
            else:
                self.emit("PUSH_CONST", self.constant(expression.value), expression.span.line)
            return
        if isinstance(expression, ast.Name):
            self.emit("LOAD", self.resolve(expression.value), expression.span.line)
            return
        if isinstance(expression, ast.Member):
            self.compile_expression(expression.target)
            self.emit("GET_FIELD", expression.name, expression.span.line)
            return
        if isinstance(expression, ast.EnumValue):
            for payload in expression.payloads:
                self.compile_expression(payload)
            metadata = {"type": expression.enum_name, "variant": expression.variant_name}
            if expression.payloads:
                metadata["arity"] = len(expression.payloads)
            self.emit("MAKE_ENUM", metadata, expression.span.line)
            return
        if isinstance(expression, ast.StructLiteral):
            declaration = self.structs.get(expression.type_name.name)
            if declaration is None:
                raise RuntimeError(f"cannot compile unknown struct {expression.type_name.name!r}")
            provided = {field.name: field for field in expression.fields}
            if len(provided) != len(expression.fields):
                raise RuntimeError("cannot compile a struct literal with duplicate fields")
            ordered_fields: list[str] = []
            for field in declaration.fields:
                initializer = provided.get(field.name)
                if initializer is None:
                    raise RuntimeError(
                        f"cannot compile struct {declaration.name!r} without field {field.name!r}"
                    )
                self.compile_expression(initializer.value)
                ordered_fields.append(field.name)
            self.emit(
                "MAKE_STRUCT",
                {"type": declaration.name, "fields": ordered_fields},
                expression.span.line,
            )
            return
        if isinstance(expression, ast.Unary):
            self.compile_expression(expression.operand)
            self.emit("UNARY", expression.operator, expression.span.line)
            return
        if isinstance(expression, ast.Binary):
            if expression.operator == "=":
                self.compile_expression(expression.right)
                target = self.resolve(expression.left.value) if isinstance(expression.left, ast.Name) else "<invalid>"
                self.emit("STORE_RESULT", target, expression.span.line)
                return
            if expression.operator == "and":
                self.compile_expression(expression.left)
                self.emit("DUP", line=expression.span.line)
                false_jump = self.emit("JUMP_IF_FALSE", None, expression.span.line)
                self.emit("POP", line=expression.span.line)
                self.compile_expression(expression.right)
                self.patch(false_jump, len(self.function.instructions))
                return
            if expression.operator == "or":
                self.compile_expression(expression.left)
                self.emit("DUP", line=expression.span.line)
                true_jump = self.emit("JUMP_IF_TRUE", None, expression.span.line)
                self.emit("POP", line=expression.span.line)
                self.compile_expression(expression.right)
                self.patch(true_jump, len(self.function.instructions))
                return
            self.compile_expression(expression.left)
            self.compile_expression(expression.right)
            self.emit("BINARY", expression.operator, expression.span.line)
            return
        if isinstance(expression, ast.Call):
            for argument in expression.arguments:
                self.compile_expression(argument)
            name = self.callable_name(expression.callee) or "<invalid>"
            if self.namespace and "." not in name and name in self.local_functions:
                name = f"{self.namespace}.{name}"
            self.emit("CALL", (name, len(expression.arguments)), expression.span.line)
            return
        raise RuntimeError(f"unhandled AST expression: {type(expression).__name__}")

    def resolve(self, name: str) -> str:
        for scope in reversed(self.scopes):
            if name in scope:
                return scope[name]
        return name

    def callable_name(self, expression: ast.Expr) -> str | None:
        if isinstance(expression, ast.Name):
            return expression.value
        if isinstance(expression, ast.Member):
            prefix = self.callable_name(expression.target)
            return f"{prefix}.{expression.name}" if prefix else None
        return None


@dataclass
class Compiler:
    source: SourceFile
    program: ast.Program

    def compile(self, *, namespace: str = "", include_entry: bool = True) -> Module:
        structs = {
            item.name: item for item in self.program.items if isinstance(item, ast.StructDecl)
        }
        enums = {item.name: item for item in self.program.items if isinstance(item, ast.EnumDecl)}
        declarations = [item for item in self.program.items if isinstance(item, ast.FunctionDecl)]
        local_functions = {item.name for item in declarations}
        functions: dict[str, BytecodeFunction] = {}
        if include_entry:
            entry_name = f"{namespace}.main" if namespace else "main"
            entry_statements = [
                item for item in self.program.items
                if not isinstance(item, (ast.FunctionDecl, ast.StructDecl, ast.EnumDecl))
            ]
            entry = FunctionCompiler(
                None,
                entry_name,
                0,
                structs,
                enums,
                namespace,
                local_functions,
            ).compile(entry_statements)
            functions[entry_name] = entry
        for declaration in declarations:
            function_name = f"{namespace}.{declaration.name}" if namespace else declaration.name
            function = FunctionCompiler(
                declaration,
                function_name,
                len(declaration.parameters),
                structs,
                enums,
                namespace,
                local_functions,
            ).compile(
                declaration.body.statements,
                [parameter.name for parameter in declaration.parameters],
            )
            functions[function_name] = function
        entry_name = f"{namespace}.main" if namespace else "main"
        return Module(self.source.name, entry_name, functions)


def compile_source(text: str, filename: str = "<source>", allowed_imports: set[str] | None = None) -> Module:
    source = SourceFile(filename, text)
    tokens, lexical_diagnostics = lex(source)
    program, parse_diagnostics = parse(source, tokens)
    type_diagnostics = check_types(source, program, allowed_imports)
    diagnostics = lexical_diagnostics.items + parse_diagnostics.items + type_diagnostics.items
    if diagnostics:
        raise DiagnosticError(diagnostics, text, filename)
    return Compiler(source, program).compile()


def compile_file(path: str | Path, allowed_imports: set[str] | None = None) -> Module:
    source = SourceFile.from_path(path)
    return compile_source(source.text, source.name, allowed_imports)


def compile_project(
    root: str | Path,
    text: str,
    filename: str | Path | None = None,
    *,
    locked: bool = False,
) -> Module:
    """Compile the project root and its imported modules into one VM module."""
    from .modules import ModuleGraph
    from .packages import install

    project = Path(root).resolve()
    if locked:
        install(project, offline=True, locked=True)
    graph = ModuleGraph(project, text, filename).load()
    interfaces = graph.interfaces()

    def visible_interfaces(record):
        imported_functions = {}
        imported_modules = {}
        public_modules = {}
        for statement in record.imports:
            module_id = statement.path
            if module_id not in graph.records:
                continue
            imported_modules[module_id] = interfaces.all_functions.get(module_id, frozenset())
            public_modules[module_id] = interfaces.public_functions.get(module_id, frozenset())
            for name in sorted(interfaces.public_functions.get(module_id, frozenset())):
                key = f"{module_id}.{name}"
                if key in interfaces.function_types:
                    imported_functions[key] = interfaces.function_types[key]
        return imported_functions, imported_modules, public_modules

    modules = [graph.root, *graph.dependency_modules]
    for record in modules:
        imported_functions, imported_modules, public_modules = visible_interfaces(record)
        allowed = set(record.manifest.dependencies) | {record.package_name}
        diagnostics = check_types(
            SourceFile(str(record.path), record.text),
            record.program,
            allowed,
            imported_functions,
            imported_modules,
            public_modules,
        )
        if diagnostics.items:
            raise DiagnosticError(diagnostics.items, record.text, str(record.path))

    root_module = Compiler(SourceFile(str(graph.root.path), graph.root.text), graph.root.program).compile()
    functions = dict(root_module.functions)
    for record in graph.dependency_modules:
        module = Compiler(
            SourceFile(str(record.path), record.text),
            record.program,
        ).compile(namespace=record.module_id, include_entry=False)
        for name in sorted(module.functions):
            if name in functions:
                raise ValueError(f"duplicate linked function name {name!r}")
            functions[name] = module.functions[name]
    return Module(root_module.name, root_module.entry, dict(sorted(functions.items())))
