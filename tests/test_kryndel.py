import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

from kryndel.artifact import read_artifact, write_artifact
from kryndel.bytecode import Module
from kryndel.compiler import compile_source
from kryndel.diagnostics import DiagnosticError
from kryndel.source import SourceFile
from kryndel.tokens import lex
from kryndel.vm import EnumValue, RuntimeKryndelError, StructValue, VM


class KryndelTests(unittest.TestCase):
    def run_source(self, source: str) -> str:
        output = io.StringIO()
        with redirect_stdout(output):
            VM(compile_source(source, "test.kry")).run()
        return output.getvalue()

    def test_lexer_handles_comments_literals_and_operators(self) -> None:
        source = SourceFile("test.kry", '// comment\nlet value: Int = 42\n/* nested /* block */ comment */\n')
        tokens, diagnostics = lex(source)
        self.assertFalse(diagnostics.has_errors)
        self.assertEqual([token.kind for token in tokens], ["LET", "IDENT", "COLON", "IDENT", "EQUAL", "INT", "EOF"])
        self.assertEqual(tokens[5].value, 42)

    def test_arithmetic_precedence(self) -> None:
        self.assertEqual(self.run_source("println(2 + 3 * 4)"), "14\n")

    def test_string_concatenation_and_conversion(self) -> None:
        self.assertEqual(self.run_source('println("Kry" + str(7))'), "Kry7\n")

    def test_functions_and_recursion(self) -> None:
        source = """
        fn factorial(n: Int) -> Int {
            if n <= 1 { return 1 }
            return n * factorial(n - 1)
        }
        println(factorial(5))
        """
        self.assertEqual(self.run_source(source), "120\n")

    def test_while_break_continue_and_mutability(self) -> None:
        source = """
        let mut i: Int = 0
        let mut total: Int = 0
        while i < 10 {
            i = i + 1
            if i == 3 { continue }
            if i == 7 { break }
            total = total + i
        }
        println(total)
        """
        self.assertEqual(self.run_source(source), "18\n")

    def test_boolean_short_circuit(self) -> None:
        source = """
        let mut value: Bool = false
        if value and (1 / 0 > 0) { println("bad") }
        if not value or true { println("ok") }
        """
        self.assertEqual(self.run_source(source), "ok\n")

    def test_type_error_has_code_and_location(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source('let answer: Int = "wrong"', "types.kry")
        message = str(context.exception)
        self.assertIn("types.kry:1:19", message)
        self.assertIn("KRY3003", message)
        self.assertIn("cannot initialize", message)

    def test_unknown_variable_is_rejected(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source("println(missing)", "unknown.kry")
        self.assertIn("KRY3008", str(context.exception))

    def test_immutable_assignment_is_rejected(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source("let value: Int = 1\nvalue = 2", "mut.kry")
        self.assertIn("KRY3015", str(context.exception))

    def test_runtime_division_error_contains_stack(self) -> None:
        source = """
        fn explode() -> Float { return 1 / 0 }
        println(explode())
        """
        with self.assertRaises(Exception) as context:
            self.run_source(source)
        self.assertIn("division by zero", str(context.exception))
        self.assertIn("call stack", str(context.exception))

    def test_ui_tree(self) -> None:
        source = """
        let window: UiNode = ui.window("Demo", 800, 600)
        let content: UiNode = ui.vbox(window)
        let title: UiNode = ui.label(content, "Hello")
        let button: UiNode = ui.button(content, "Open")
        ui.on_click(button, "open")
        ui.show(window)
        """
        result = self.run_source(source)
        self.assertIn("Window (title='Demo' width=800 height=600)", result)
        self.assertIn("VBox", result)
        self.assertIn("Button (text='Open') [on_click=open]", result)

    def test_struct_constructor_and_field_access(self) -> None:
        source = """
        let point: Point = Point { x: 3, y: 4 }
        println(point.x)
        println(point.y)
        struct Point {
            x: Int
            y: Int
        }
        """
        self.assertEqual(self.run_source(source), "3\n4\n")

    def test_structs_work_through_functions_and_retain_runtime_metadata(self) -> None:
        source = """
        struct Point { x: Int y: Int }
        fn total(point: Point) -> Int { return point.x + point.y }
        println(total(Point { y: 4, x: 3 }))
        """
        module = compile_source(source, "structs.kry")
        captured: list[str] = []
        self.assertEqual(VM(module, captured.append).run(), None)
        self.assertEqual(captured, ["7\n"])
        value = VM(module).module
        self.assertTrue(any(instruction.op == "MAKE_STRUCT" for instruction in value.functions["main"].instructions))

    def test_nested_struct_field_access(self) -> None:
        source = """
        struct Envelope { point: Point }
        let envelope: Envelope = Envelope { point: Point { x: 3, y: 4 } }
        println(envelope.point.x + envelope.point.y)
        struct Point { x: Int y: Int }
        """
        self.assertEqual(self.run_source(source), "7\n")

    def test_struct_missing_field_is_rejected(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source("struct Point { x: Int y: Int }\nlet point: Point = Point { x: 1 }", "missing.kry")
        self.assertIn("KRY3031", str(context.exception))
        self.assertIn("missing field(s): y", str(context.exception))

    def test_struct_unknown_constructor_field_is_rejected_at_field_span(self) -> None:
        source = "struct Point { x: Int }\nlet point: Point = Point { x: 1, z: 2 }"
        with self.assertRaises(DiagnosticError) as context:
            compile_source(source, "unknown-field.kry")
        message = str(context.exception)
        self.assertIn("KRY3029", message)
        self.assertIn("unknown-field.kry:2:34", message)

    def test_struct_duplicate_field_and_unknown_type_are_rejected(self) -> None:
        with self.assertRaises(DiagnosticError) as duplicate:
            compile_source("struct Point { x: Int x: Int }", "duplicate-field.kry")
        self.assertIn("KRY3026", str(duplicate.exception))
        with self.assertRaises(DiagnosticError) as unknown:
            compile_source("struct Point { x: Missing }", "unknown-type.kry")
        self.assertIn("KRY3023", str(unknown.exception))

    def test_struct_field_type_and_member_rules_are_rejected(self) -> None:
        with self.assertRaises(DiagnosticError) as mismatch:
            compile_source("struct Point { x: Int }\nlet point: Point = Point { x: true }", "field-type.kry")
        self.assertIn("KRY3030", str(mismatch.exception))
        with self.assertRaises(DiagnosticError) as missing:
            compile_source("struct Point { x: Int }\nlet point: Point = Point { x: 1 }\nprintln(point.y)", "member-field.kry")
        self.assertIn("KRY3033", str(missing.exception))
        with self.assertRaises(DiagnosticError) as assignment:
            compile_source("struct Point { x: Int }\nlet point: Point = Point { x: 1 }\npoint.x = 2", "member-assignment.kry")
        self.assertIn("KRY3014", str(assignment.exception))

    def test_struct_bytecode_is_deterministic_and_declaration_ordered(self) -> None:
        first = compile_source("struct Point { x: Int y: Int }\nprintln(Point { y: 4, x: 3 }.x)", "first.kry")
        second = compile_source("struct Point { x: Int y: Int }\nprintln(Point { y: 4, x: 3 }.x)", "second.kry")
        self.assertEqual(json.loads(first.dumps())["functions"], json.loads(second.dumps())["functions"])
        make = next(instruction for instruction in first.functions["main"].instructions if instruction.op == "MAKE_STRUCT")
        self.assertEqual(make.arg, {"type": "Point", "fields": ["x", "y"]})
        value = StructValue("Point", (("x", 3), ("y", 4)))
        self.assertEqual(VM.stringify(value), "Point { x: 3, y: 4 }")

    def test_malformed_struct_bytecode_has_explicit_runtime_error(self) -> None:
        from kryndel.bytecode import BytecodeFunction, Instruction

        function = BytecodeFunction("main", 0, [Instruction("PUSH_CONST", 0, 1), Instruction("GET_FIELD", "x", 1), Instruction("RETURN", line=1)], [1])
        module = Module("malformed", "main", {"main": function})
        with self.assertRaises(RuntimeKryndelError) as context:
            VM(module).run()
        self.assertIn("field access requires a struct value", str(context.exception))
        self.assertNotIn("KeyError", str(context.exception))

    def test_artifact_round_trip_and_checksum(self) -> None:
        module = compile_source("println(42)", "artifact.kry")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "program.kexe"
            write_artifact(module, path)
            restored = read_artifact(path)
            self.assertEqual(restored.dumps(), module.dumps())
            self.assertEqual(path.read_bytes()[:8], b"KRYNEXE\x01")

    def test_bytecode_is_deterministic_json(self) -> None:
        first = compile_source("println(42)", "a.kry").dumps()
        second = compile_source("println(42)", "b.kry").dumps()
        self.assertEqual(json.loads(first)["functions"], json.loads(second)["functions"])

    def test_enum_lexing_parsing_and_runtime_value(self) -> None:
        source = "enum Color { Red Green }\nlet color: Color = Color.Red\nprintln(color)"
        tokens, diagnostics = lex(SourceFile("enum.kry", source))
        self.assertFalse(diagnostics.has_errors)
        self.assertIn("ENUM", [token.kind for token in tokens])
        module = compile_source(source, "enum.kry")
        self.assertEqual([i.op for i in module.functions["main"].instructions].count("MAKE_ENUM"), 1)
        captured: list[str] = []
        VM(module, captured.append).run()
        self.assertEqual(captured, ["Color.Red\n"])
        self.assertIsInstance(EnumValue("Color", "Red"), EnumValue)

    def test_enum_can_be_used_before_declaration_and_in_functions(self) -> None:
        source = """
        fn choose(value: Color) -> Color { return value }
        let color: Color = choose(Color.Green)
        println(color)
        enum Color { Red Green }
        """
        self.assertEqual(self.run_source(source), "Color.Green\n")

    def test_enum_equality_and_deterministic_bytecode(self) -> None:
        source = "enum Color { Red Green }\nprintln(Color.Red == Color.Red)\nprintln(Color.Red != Color.Green)"
        first = compile_source(source, "one.kry")
        second = compile_source(source, "two.kry")
        self.assertEqual(json.loads(first.dumps())["functions"], json.loads(second.dumps())["functions"])
        self.assertEqual(self.run_source(source), "true\ntrue\n")

    def test_enum_invalid_uses_have_stable_codes_and_spans(self) -> None:
        cases = [
            ("enum Color { Red Green }\nlet color: Color = Color.Blue", "KRY3037", ":2:26"),
            ("enum Color { Red Red }", "KRY3035", ":1:18"),
            ("enum Color { Red }\nenum Color { Blue }", "KRY3034", ":2:1"),
            ("enum Color { Red }\nenum Other { Red }\nlet color: Color = Other.Red", "KRY3003", ""),
            ("enum Color { Red }\nlet color: Color = 1", "KRY3003", ""),
            ("enum Color { Red Green }\nprintln(Color.Red < Color.Green)", "KRY3020", ""),
        ]
        for source, code, location in cases:
            with self.subTest(code=code), self.assertRaises(DiagnosticError) as context:
                compile_source(source, "enum-errors.kry")
            message = str(context.exception)
            self.assertIn(code, message)
            if location:
                self.assertIn("enum-errors.kry" + location, message)

    def test_enum_names_cannot_be_bare_values_or_collide_with_types(self) -> None:
        with self.assertRaises(DiagnosticError) as bare:
            compile_source("enum Color { Red }\nprintln(Color)", "bare-enum.kry")
        self.assertIn("KRY3042", str(bare.exception))
        with self.assertRaises(DiagnosticError) as collision:
            compile_source("struct Color { value: Int }\nenum Color { Red }", "collision.kry")
        self.assertIn("KRY3034", str(collision.exception))

    def test_enum_ordering_and_malformed_bytecode_are_rejected(self) -> None:
        with self.assertRaises(DiagnosticError) as ordering:
            compile_source("enum Color { Red Green }\nprintln(Color.Red < Color.Green)", "ordering.kry")
        self.assertIn("KRY3020", str(ordering.exception))
        from kryndel.bytecode import BytecodeFunction, Instruction
        function = BytecodeFunction("main", 0, [Instruction("MAKE_ENUM", {"type": "Color"}, 1)])
        with self.assertRaises(RuntimeKryndelError) as malformed:
            VM(Module("malformed", "main", {"main": function})).run()
        self.assertIn("malformed MAKE_ENUM metadata", str(malformed.exception))


if __name__ == "__main__":
    unittest.main()
