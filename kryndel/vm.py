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
    """A nominal enum value with immutable positional payloads."""

    type_name: str
    variant_name: str
    payloads: tuple[Any, ...] = ()


@dataclass(frozen=True)
class ArrayValue:
    """Immutable homogeneous sequence with a stable Kryndel runtime tag."""

    items: tuple[Any, ...] = ()


@dataclass(frozen=True)
class TupleValue:
    """Immutable fixed-width positional value."""

    items: tuple[Any, ...] = ()


@dataclass(frozen=True)
class BytesValue:
    """Nominal immutable sequence of octets in the range 0..255."""

    items: tuple[int, ...] = ()

    def __post_init__(self) -> None:
        if any(not isinstance(item, int) or isinstance(item, bool) or not 0 <= item <= 255 for item in self.items):
            raise ValueError("BytesValue items must be Int octets in the range 0..255")

    @property
    def hex(self) -> str:
        return bytes(self.items).hex()


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
                elif op == "MAKE_ARRAY":
                    self.make_sequence(stack, function, instruction, ArrayValue)
                elif op == "MAKE_TUPLE":
                    self.make_sequence(stack, function, instruction, TupleValue)
                elif op == "INDEX":
                    self.index(stack, function, instruction)
                elif op == "MATCH_ENUM":
                    self.match_enum(stack, function, instruction)
                elif op == "BIND_ENUM":
                    self.bind_enum(locals_, function, instruction)
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
        if not isinstance(metadata, dict) or set(metadata) not in ({"type", "variant"}, {"type", "variant", "arity"}):
            raise self.error(function, instruction, "malformed MAKE_ENUM metadata")
        type_name = metadata["type"]
        variant_name = metadata["variant"]
        arity = metadata.get("arity", 0)
        if (
            not isinstance(type_name, str)
            or not type_name
            or not isinstance(variant_name, str)
            or not variant_name
            or not isinstance(arity, int)
            or arity < 0
        ):
            raise self.error(function, instruction, "malformed MAKE_ENUM metadata")
        self.require_stack(stack, function, instruction, arity)
        payloads = tuple(stack[-arity:]) if arity else ()
        if arity:
            del stack[-arity:]
        stack.append(EnumValue(type_name, variant_name, payloads))

    def make_sequence(self, stack: list[Any], function: BytecodeFunction, instruction: Any, value_type: type) -> None:
        count = instruction.arg
        if not isinstance(count, int) or count < 0:
            raise self.error(function, instruction, "KRY6101 invalid sequence arity")
        self.require_stack(stack, function, instruction, count)
        values = tuple(stack[-count:]) if count else ()
        if count:
            del stack[-count:]
        stack.append(value_type(values))

    def index(self, stack: list[Any], function: BytecodeFunction, instruction: Any) -> None:
        self.require_stack(stack, function, instruction, 2)
        index = stack.pop()
        target = stack.pop()
        if not isinstance(index, int) or isinstance(index, bool):
            raise self.error(function, instruction, "KRY6102 sequence index must be Int")
        items = (
            target.items
            if isinstance(target, (ArrayValue, TupleValue, BytesValue))
            else target
            if isinstance(target, str)
            else None
        )
        if items is None:
            raise self.error(function, instruction, "KRY6103 indexing requires String, Array, Tuple, or Bytes")
        if index < 0 or index >= len(items):
            raise self.error(function, instruction, "KRY6104 sequence index out of bounds")
        stack.append(items[index])

    def match_enum(self, stack: list[Any], function: BytecodeFunction, instruction: Any) -> None:
        metadata = instruction.arg
        if not isinstance(metadata, dict) or set(metadata) != {"type", "variant", "arity"}:
            raise self.error(function, instruction, "malformed MATCH_ENUM metadata")
        type_name = metadata.get("type")
        variant_name = metadata.get("variant")
        arity = metadata.get("arity")
        if not isinstance(type_name, str) or not isinstance(variant_name, str) or not isinstance(arity, int) or arity < 0:
            raise self.error(function, instruction, "malformed MATCH_ENUM metadata")
        self.require_stack(stack, function, instruction, 1)
        value = stack.pop()
        if not isinstance(value, EnumValue):
            stack.append(False)
            return
        stack.append(value.type_name == type_name and value.variant_name == variant_name and len(value.payloads) == arity)

    def bind_enum(self, locals_: dict[str, Any], function: BytecodeFunction, instruction: Any) -> None:
        metadata = instruction.arg
        if not isinstance(metadata, dict) or set(metadata) != {"source", "bindings", "arity"}:
            raise self.error(function, instruction, "malformed BIND_ENUM metadata")
        source = metadata.get("source")
        bindings = metadata.get("bindings")
        arity = metadata.get("arity")
        if not isinstance(source, str) or not isinstance(bindings, list) or not isinstance(arity, int) or arity != len(bindings):
            raise self.error(function, instruction, "malformed BIND_ENUM metadata")
        value = locals_.get(source)
        if not isinstance(value, EnumValue) or len(value.payloads) != arity:
            raise self.error(function, instruction, "cannot bind payloads from a non-matching enum value")
        for name, payload in zip(bindings, value.payloads):
            if name:
                if not isinstance(name, str):
                    raise self.error(function, instruction, "malformed BIND_ENUM metadata")
                locals_[name] = payload

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
            value = arguments[0]
            if isinstance(value, (str, ArrayValue, TupleValue, BytesValue)):
                return len(value) if isinstance(value, str) else len(value.items)
            raise RuntimeKryndelError("KRY6105 len requires String, Array, Tuple, or Bytes")
        if name == "bytes":
            value = arguments[0]
            if not isinstance(value, ArrayValue):
                raise RuntimeKryndelError("KRY6202 bytes requires an Array of Int octets")
            items: list[int] = []
            for index, item in enumerate(value.items):
                if not isinstance(item, int) or isinstance(item, bool) or not 0 <= item <= 255:
                    raise RuntimeKryndelError(
                        f"KRY6202 byte at array index {index} must be an Int in the range 0..255"
                    )
                items.append(item)
            return BytesValue(tuple(items))
        if name == "string_to_bytes":
            value = arguments[0]
            if not isinstance(value, str):
                raise RuntimeKryndelError("KRY6202 string_to_bytes requires a String")
            try:
                return BytesValue(tuple(value.encode("utf-8", "strict")))
            except UnicodeEncodeError as exc:
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {exc.start} (sequence length {max(1, exc.end - exc.start)})"
                ) from exc
        if name == "bytes_to_string":
            value = arguments[0]
            if not isinstance(value, BytesValue):
                raise RuntimeKryndelError("KRY6202 bytes_to_string requires Bytes")
            return self.decode_utf8(value.items)
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
            def payload_text(item: Any) -> str:
                if isinstance(item, str):
                    return '"' + item.replace('\\', '\\\\').replace('"', '\\"').replace('\n', '\\n') + '"'
                return VM.stringify(item)
            suffix = "" if not value.payloads else "(" + ", ".join(payload_text(item) for item in value.payloads) + ")"
            return f"{value.type_name}.{value.variant_name}{suffix}"
        if isinstance(value, ArrayValue):
            return "[" + ", ".join(VM.stringify(item) for item in value.items) + "]"
        if isinstance(value, TupleValue):
            return "(" + ", ".join(VM.stringify(item) for item in value.items) + ")"
        if isinstance(value, BytesValue):
            return f"Bytes({value.hex})"
        if isinstance(value, float) and value.is_integer():
            return str(int(value))
        return str(value)

    @staticmethod
    def decode_utf8(items: tuple[int, ...]) -> str:
        """Decode canonical UTF-8 and report the first invalid sequence precisely."""
        data = tuple(items)
        codepoints: list[str] = []
        offset = 0
        while offset < len(data):
            lead = data[offset]
            if lead <= 0x7F:
                codepoints.append(chr(lead))
                offset += 1
                continue
            if 0xC2 <= lead <= 0xDF:
                length = 2
                minimum = 0x80
                value = lead & 0x1F
            elif 0xE0 <= lead <= 0xEF:
                length = 3
                minimum = 0x800
                value = lead & 0x0F
            elif 0xF0 <= lead <= 0xF4:
                length = 4
                minimum = 0x10000
                value = lead & 0x07
            elif lead in (0xC0, 0xC1):
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length 2)"
                )
            else:
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length 1)"
                )
            if offset + length > len(data):
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length {length})"
                )
            for continuation in data[offset + 1 : offset + length]:
                if not 0x80 <= continuation <= 0xBF:
                    raise RuntimeKryndelError(
                        f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length {length})"
                    )
            first_continuation = data[offset + 1]
            if length == 3 and ((lead == 0xE0 and first_continuation < 0xA0) or (lead == 0xED and first_continuation > 0x9F)):
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length {length})"
                )
            if length == 4 and ((lead == 0xF0 and first_continuation < 0x90) or (lead == 0xF4 and first_continuation > 0x8F)):
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length {length})"
                )
            for continuation in data[offset + 1 : offset + length]:
                value = (value << 6) | (continuation & 0x3F)
            if value < minimum or value > 0x10FFFF or 0xD800 <= value <= 0xDFFF:
                raise RuntimeKryndelError(
                    f"KRY6201 invalid UTF-8 at byte offset {offset} (sequence length {length})"
                )
            codepoints.append(chr(value))
            offset += length
        return "".join(codepoints)

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
                if isinstance(left, ArrayValue) and isinstance(right, ArrayValue):
                    return ArrayValue(left.items + right.items)
                if isinstance(left, BytesValue) and isinstance(right, BytesValue):
                    return BytesValue(left.items + right.items)
                if isinstance(left, BytesValue) or isinstance(right, BytesValue):
                    raise RuntimeKryndelError("KRY6203 Bytes concatenation requires two Bytes values")
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
