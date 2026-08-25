"""Kryndel bytecode virtual machine and standard library."""

from __future__ import annotations

import math
import time
from dataclasses import dataclass, field
from typing import Any, Callable

from .bytecode import BytecodeFunction, Module


class RuntimeKryndelError(Exception):
    """A user-facing runtime failure."""


@dataclass(frozen=True)
class StructValue:
    """A nominal struct instance retaining type and declaration-ordered fields."""

    type_name: str
    fields: tuple[tuple[str, Any], ...]

    def field(self, name: str) -> tuple[bool, Any]:
        for field_name, value in self.fields:
            if field_name == name:
                return True, value
        return False, None


@dataclass(frozen=True)
class EnumValue:
    """A nominal unit enum value."""

    type_name: str
    variant_name: str


@dataclass
class UINode:
    kind: str
    properties: dict[str, Any] = field(default_factory=dict)
    children: list["UINode"] = field(default_factory=list)
    callback: str | None = None

    def add(self, child: "UINode") -> "UINode":
        self.children.append(child)
        return child

    def render(self, depth: int = 0) -> str:
        indent = "  " * depth
        props = " ".join(f"{key}={value!r}" for key, value in self.properties.items())
        line = f"{indent}{self.kind}" + (f" ({props})" if props else "")
        if self.callback:
            line += f" [on_click={self.callback}]"
        return "\n".join([line, *(child.render(depth + 1) for child in self.children)])


@dataclass
class VM:
    module: Module
    output: Callable[[str], None] = print
    call_stack: list[str] = field(default_factory=list)

    def run(self) -> Any:
        return self.execute(self.module.entry, [])

    def execute(self, name: str, arguments: list[Any]) -> Any:
        function = self.module.functions.get(name)
        if function is None:
            raise RuntimeKryndelError(f"unknown function {name!r}")
        if len(arguments) != function.arity:
            raise RuntimeKryndelError(
                f"function {name!r} expected {function.arity} argument(s), found {len(arguments)}"
            )
        locals_: dict[str, Any] = {}
        if function is not None:
            parameter_names = self.parameter_names(function)
            for parameter, value in zip(parameter_names, arguments):
                locals_[parameter] = value
        stack: list[Any] = []
        instruction_pointer = 0
        self.call_stack.append(name)
        try:
            while instruction_pointer < len(function.instructions):
                instruction = function.instructions[instruction_pointer]
                op = instruction.op
                arg = instruction.arg
                instruction_pointer += 1
                if op == "PUSH_CONST":
                    if not isinstance(arg, int) or arg < 0 or arg >= len(function.constants):
                        raise self.error(function, instruction, "invalid constant index")
                    stack.append(function.constants[arg])
                elif op == "PUSH_NIL":
                    stack.append(None)
                elif op == "PUSH_CALLABLE":
                    stack.append(arg)
                elif op == "LOAD":
                    if arg not in locals_:
                        raise self.error(function, instruction, f"unknown local {arg!r}")
                    stack.append(locals_[arg])
                elif op == "STORE":
                    self.require_stack(stack, function, instruction, 1)
                    locals_[str(arg)] = stack.pop()
                elif op == "STORE_RESULT":
                    self.require_stack(stack, function, instruction, 1)
                    value = stack.pop()
                    locals_[str(arg)] = value
                    stack.append(value)
                elif op == "MAKE_STRUCT":
                    self.make_struct(stack, function, instruction)
                elif op == "MAKE_ENUM":
                    self.make_enum(stack, function, instruction)
                elif op == "GET_FIELD":
                    self.get_field(stack, function, instruction)
                elif op == "POP":
                    self.require_stack(stack, function, instruction, 1)
                    stack.pop()
                elif op == "DUP":
                    self.require_stack(stack, function, instruction, 1)
                    stack.append(stack[-1])
                elif op == "UNARY":
                    self.require_stack(stack, function, instruction, 1)
                    value = stack.pop()
                    stack.append(self.unary(str(arg), value, function, instruction))
                elif op == "BINARY":
                    self.require_stack(stack, function, instruction, 2)
                    right = stack.pop()
                    left = stack.pop()
                    try:
                        stack.append(self.binary(str(arg), left, right, function, instruction))
                    except RuntimeKryndelError as exc:
                        raise self.error(function, instruction, str(exc)) from exc
                elif op == "JUMP":
                    instruction_pointer = self.jump_target(arg, function, instruction)
                elif op == "JUMP_IF_FALSE":
                    self.require_stack(stack, function, instruction, 1)
                    condition = stack.pop()
                    if not self.truthy(condition):
                        instruction_pointer = self.jump_target(arg, function, instruction)
                elif op == "JUMP_IF_TRUE":
                    self.require_stack(stack, function, instruction, 1)
                    condition = stack.pop()
                    if self.truthy(condition):
                        instruction_pointer = self.jump_target(arg, function, instruction)
                elif op == "CALL":
                    if not isinstance(arg, (list, tuple)) or len(arg) != 2:
                        raise self.error(function, instruction, "malformed CALL instruction")
                    name_, count = arg
                    if not isinstance(name_, str) or not isinstance(count, int) or count < 0:
                        raise self.error(function, instruction, "malformed CALL instruction")
                    self.require_stack(stack, function, instruction, count)
                    values = stack[-count:] if count else []
                    if count:
                        del stack[-count:]
                    stack.append(self.call(name_, values, function, instruction))
                elif op == "RETURN":
                    self.require_stack(stack, function, instruction, 1)
                    return stack.pop()
                else:
                    raise self.error(function, instruction, f"unknown bytecode instruction {op!r}")
            return None
        finally:
            self.call_stack.pop()

    def make_struct(self, stack: list[Any], function: BytecodeFunction, instruction: Any) -> None:
        metadata = instruction.arg
        if not isinstance(metadata, dict):
            raise self.error(function, instruction, "malformed MAKE_STRUCT metadata")
        type_name = metadata.get("type")
        field_names = metadata.get("fields")
        if (
            not isinstance(type_name, str)
            or not type_name
            or not isinstance(field_names, list)
            or any(not isinstance(name, str) or not name for name in field_names)
            or len(set(field_names)) != len(field_names)
        ):
            raise self.error(function, instruction, "malformed MAKE_STRUCT metadata")
        count = len(field_names)
        self.require_stack(stack, function, instruction, count)
        values = stack[-count:] if count else []
        if count:
            del stack[-count:]
        stack.append(StructValue(type_name, tuple(zip(field_names, values))))

    def make_enum(self, stack: list[Any], function: BytecodeFunction, instruction: Any) -> None:
        metadata = instruction.arg
        if not isinstance(metadata, dict) or set(metadata) != {"type", "variant"}:
            raise self.error(function, instruction, "malformed MAKE_ENUM metadata")
        type_name = metadata["type"]
        variant_name = metadata["variant"]
        if (not isinstance(type_name, str) or not type_name or not isinstance(variant_name, str) or not variant_name):
            raise self.error(function, instruction, "malformed MAKE_ENUM metadata")
        stack.append(EnumValue(type_name, variant_name))

    def get_field(self, stack: list[Any], function: BytecodeFunction, instruction: Any) -> None:
        self.require_stack(stack, function, instruction, 1)
        field_name = instruction.arg
        if not isinstance(field_name, str) or not field_name:
            raise self.error(function, instruction, "malformed GET_FIELD instruction")
        value = stack.pop()
        if not isinstance(value, StructValue):
            raise self.error(function, instruction, f"field access requires a struct value, found {type(value).__name__}")
        found, field_value = value.field(field_name)
        if not found:
            raise self.error(
                function,
                instruction,
                f"struct {value.type_name!r} has no field {field_name!r}",
            )
        stack.append(field_value)

    @staticmethod
    def jump_target(value: Any, function: BytecodeFunction, instruction: Any) -> int:
        if not isinstance(value, int) or value < 0 or value > len(function.instructions):
            raise RuntimeKryndelError(
                f"{function.name}:{instruction.line}: invalid jump target for {instruction.op}"
            )
        return value

    def call(self, name: str, arguments: list[Any], caller: BytecodeFunction, instruction: Any) -> Any:
        if name in self.module.functions:
            return self.execute(name, arguments)
        try:
            return self.builtin(name, arguments)
        except RuntimeKryndelError:
            raise
        except Exception as exc:
            raise self.error(caller, instruction, str(exc)) from exc

    def builtin(self, name: str, arguments: list[Any]) -> Any:
        if name in ("print", "println"):
            text = " ".join(self.stringify(value) for value in arguments)
            self.emit_output(text, newline=name == "println")
            return None
        if name == "str":
            return self.stringify(arguments[0])
        if name == "int":
            value = arguments[0]
            if isinstance(value, bool):
                return int(value)
            return int(value)
        if name == "float":
            return float(arguments[0])
        if name == "len":
            return len(arguments[0])
        if name == "abs":
            return abs(arguments[0])
        if name == "sqrt":
            return math.sqrt(arguments[0])
        if name == "clock":
            return time.monotonic()
        if name == "ui.window":
            title, width, height = arguments
            return UINode("Window", {"title": title, "width": width, "height": height})
        if name in ("ui.label", "ui.button", "ui.vbox", "ui.hbox"):
            parent = arguments[0]
            if not isinstance(parent, UINode):
                raise RuntimeKryndelError(f"{name} requires a UiNode parent")
            if name == "ui.vbox":
                return parent.add(UINode("VBox"))
            if name == "ui.hbox":
                return parent.add(UINode("HBox"))
            kind = "Label" if name == "ui.label" else "Button"
            return parent.add(UINode(kind, {"text": arguments[1]}))
        if name == "ui.set_text":
            node, text = arguments
            if not isinstance(node, UINode):
                raise RuntimeKryndelError("ui.set_text requires a UiNode")
            node.properties["text"] = text
            return None
        if name == "ui.on_click":
            node, callback = arguments
            if not isinstance(node, UINode):
                raise RuntimeKryndelError("ui.on_click requires a UiNode")
            node.callback = str(callback)
            return None
        if name == "ui.show":
            node = arguments[0]
            if not isinstance(node, UINode):
                raise RuntimeKryndelError("ui.show requires a UiNode")
            self.output(node.render())
            return None
        if name == "ui.run":
            return None
        raise RuntimeKryndelError(f"unknown function {name!r}")

    def emit_output(self, text: str, *, newline: bool) -> None:
        if self.output is print:
            self.output(text, end="\n" if newline else "")
        else:
            self.output(text + ("\n" if newline else ""))

    @staticmethod
    def stringify(value: Any) -> str:
        if value is None:
            return "nil"
        if value is True:
            return "true"
        if value is False:
            return "false"
        if isinstance(value, StructValue):
            fields = ", ".join(f"{name}: {VM.stringify(field_value)}" for name, field_value in value.fields)
            return f"{value.type_name} {{ {fields} }}"
        if isinstance(value, EnumValue):
            return f"{value.type_name}.{value.variant_name}"
        if isinstance(value, float) and value.is_integer():
            return str(int(value))
        return str(value)

    @staticmethod
    def truthy(value: Any) -> bool:
        return value is True

    @staticmethod
    def unary(operator: str, value: Any, function: BytecodeFunction, instruction: Any) -> Any:
        if operator in ("!", "not"):
            return not VM.truthy(value)
        if operator == "-":
            return -value
        raise RuntimeKryndelError(f"unsupported unary operator {operator!r}")

    @staticmethod
    def binary(operator: str, left: Any, right: Any, function: BytecodeFunction, instruction: Any) -> Any:
        try:
            if operator == "+":
                return left + right
            if operator == "-":
                return left - right
            if operator == "*":
                return left * right
            if operator == "/":
                return left / right
            if operator == "%":
                return left % right
            if operator == "==":
                return left == right
            if operator == "!=":
                return left != right
            if operator == "<":
                return left < right
            if operator == "<=":
                return left <= right
            if operator == ">":
                return left > right
            if operator == ">=":
                return left >= right
        except ZeroDivisionError as exc:
            raise RuntimeKryndelError("division by zero") from exc
        except (TypeError, ValueError) as exc:
            raise RuntimeKryndelError(f"incompatible operands for {operator!r}") from exc
        raise RuntimeKryndelError(f"unsupported binary operator {operator!r}")

    def error(self, function: BytecodeFunction, instruction: Any, message: str) -> RuntimeKryndelError:
        trace = " -> ".join(self.call_stack)
        location = f"{function.name}:{instruction.line}" if instruction.line else function.name
        suffix = f" (call stack: {trace})" if trace else ""
        return RuntimeKryndelError(f"{location}: {message}{suffix}")

    @staticmethod
    def require_stack(stack: list[Any], function: BytecodeFunction, instruction: Any, count: int) -> None:
        if len(stack) < count:
            raise RuntimeKryndelError(
                f"{function.name}:{instruction.line}: bytecode stack underflow while executing {instruction.op}"
            )

    @staticmethod
    def parameter_names(function: BytecodeFunction) -> list[str]:
        return list(function.parameters[: function.arity])


VirtualMachine = VM
