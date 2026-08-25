import io
import json
import os
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

from kryndel.artifact import read_artifact, write_artifact
from kryndel.bytecode import Module
from kryndel.compiler import compile_project, compile_source
from kryndel.modules import ModuleGraph
from kryndel.parser import parse
from kryndel.tooling import format_file, run_kryndel_tests
from kryndel.diagnostics import DiagnosticError
from kryndel.cli import main as cli_main
from kryndel.packages import (
    Lockfile,
    SemVer,
    VersionRequirement,
    add_dependency,
    init_project,
    install,
    package_checksum,
    read_manifest,
    validate_imports,
)
from kryndel.source import SourceFile
from kryndel.tokens import lex
from kryndel.vm import ArrayValue, BytesValue, EnumValue, RuntimeKryndelError, StructValue, TupleValue, VM


class KryndelTests(unittest.TestCase):
    def run_source(self, source: str) -> str:
        output = io.StringIO()
        with redirect_stdout(output):
            VM(compile_source(source, "test.kry")).run()
        return output.getvalue()

    def test_value_runtime_v1_fixture_is_deterministic_and_complete(self) -> None:
        fixture_path = Path(__file__).parent / "fixtures" / "value-runtime-v1.json"
        raw = fixture_path.read_text(encoding="utf-8")
        fixture = json.loads(raw)
        self.assertEqual(fixture["contract"], "kryndel-value-runtime")
        self.assertEqual(fixture["version"], 1)
        self.assertEqual(raw, json.dumps(fixture, ensure_ascii=False, indent=2, sort_keys=True) + "\n")
        self.assertEqual(
            {case["error"] for case in fixture["invalid"]},
            {"KRY6102", "KRY6104", "KRY6201", "KRY6202", "KRY6301", "KRY6303", "KRY6305"},
        )
        self.assertEqual(
            {case["kind"] for case in fixture["valid"]},
            {"String", "Bytes", "Array", "Tuple", "Option", "Result", "Void"},
        )
        self.assertEqual(len(ArrayValue((1, 2, 3)).items), 3)
        self.assertEqual(len(TupleValue(("answer", 42)).items), 2)
        self.assertEqual(BytesValue((0x41, 0xF0, 0x9F, 0x98, 0x80)).hex, "41f09f9880")
        self.assertEqual(VM.stringify(None), "nil")

    def test_bytes_utf8_api_executes_from_kryndel(self) -> None:
        source = """
        fn make() -> Bytes {
            return string_to_bytes("A😀")
        }
        fn append_mark(value: Bytes) -> Bytes {
            return value + bytes([33])
        }
        fn decode(value: Bytes) -> String {
            return bytes_to_string(value)
        }
        """
        module = compile_source(source, "bytes.kry")
        runtime = VM(module)
        value = runtime.execute("make", [])
        self.assertIsInstance(value, BytesValue)
        self.assertEqual(value.items, (0x41, 0xF0, 0x9F, 0x98, 0x80))
        self.assertEqual(runtime.execute("append_mark", [value]).items, value.items + (33,))
        self.assertEqual(runtime.execute("decode", [value]), "A😀")
        self.assertEqual(runtime.execute("decode", [BytesValue((0xE2, 0x82, 0xAC))]), "€")
        self.assertEqual(runtime.execute("decode", [BytesValue((0x41, 0xF0, 0x9F, 0x98, 0x80))]), "A😀")
        first = compile_source(source, "bytes.kry")
        second = compile_source(source, "bytes.kry")
        self.assertEqual(first.dumps(), second.dumps())
        self.assertIn("CALL", [instruction.op for instruction in first.functions["make"].instructions])

    def test_bytes_length_indexing_and_range_errors_are_stable(self) -> None:
        source = """
        fn sample() -> Bytes { return bytes([0, 65, 255]) }
        fn octet(value: Bytes) -> Int { return value[1] }
        fn length(value: Bytes) -> Int { return len(value) }
        """
        runtime = VM(compile_source(source, "bytes-index.kry"))
        value = runtime.execute("sample", [])
        self.assertEqual(runtime.execute("length", [value]), 3)
        self.assertEqual(runtime.execute("octet", [value]), 65)
        with self.assertRaisesRegex(RuntimeKryndelError, "KRY6104"):
            VM(compile_source("println(bytes([1])[1])", "bytes-bounds.kry")).run()
        with self.assertRaisesRegex(DiagnosticError, "KRY6102|KRY3054"):
            compile_source("println(bytes([1])[true])", "bytes-index-type.kry")
        with self.assertRaisesRegex(RuntimeKryndelError, "KRY6202"):
            VM(compile_source("println(bytes([256]))", "bytes-range.kry")).run()
        with self.assertRaisesRegex(RuntimeKryndelError, "KRY6202"):
            VM(compile_source("println(bytes([-1]))", "bytes-negative.kry")).run()
        with self.assertRaisesRegex(DiagnosticError, "KRY3013"):
            compile_source("println(bytes([1.0]))", "bytes-float.kry")

    def test_invalid_utf8_reports_offset_and_sequence_length(self) -> None:
        source = "fn decode(value: Bytes) -> String { return bytes_to_string(value) }"
        runtime = VM(compile_source(source, "invalid-utf8.kry"))
        for invalid in ((0xC0, 0xAF), (0xE2, 0x82), (0x41, 0x80), (0xF4, 0x90, 0x80, 0x80)):
            with self.subTest(invalid=invalid), self.assertRaisesRegex(RuntimeKryndelError, r"KRY6201.*byte offset"):
                runtime.execute("decode", [BytesValue(invalid)])
        with self.assertRaisesRegex(RuntimeKryndelError, r"KRY6201.*offset 0.*length 2"):
            runtime.execute("decode", [BytesValue((0xC0, 0xAF))])
        with self.assertRaisesRegex(ValueError, "0..255"):
            BytesValue((256,))

    def test_bytes_v1_fixture_is_deterministic_and_matches_runtime(self) -> None:
        fixture_path = Path(__file__).parent / "fixtures" / "bytes-v1.json"
        raw = fixture_path.read_text(encoding="utf-8")
        fixture = json.loads(raw)
        self.assertEqual(fixture["contract"], "kryndel-bytes")
        self.assertEqual(fixture["version"], 1)
        self.assertEqual(raw, json.dumps(fixture, ensure_ascii=False, indent=2, sort_keys=True) + "\n")
        from_array = BytesValue(tuple(fixture["construction"]["from_array"]["input"]))
        from_string = BytesValue(tuple("A😀".encode("utf-8")))
        self.assertEqual(from_array.hex, fixture["construction"]["from_array"]["hex"])
        self.assertEqual(from_string.hex, fixture["construction"]["from_string"]["hex"])
        self.assertEqual(len(from_string.items), fixture["operations"]["length_octets"])
        self.assertEqual(from_string.items[0], fixture["operations"]["index"]["0"])
        self.assertEqual(from_string.items[4], fixture["operations"]["index"]["4"])
        self.assertEqual(VM.stringify(from_string), fixture["serialization"]["stringify"])
        concat_module = compile_source("fn concat(left: Bytes, right: Bytes) -> Bytes { return left + right }", "bytes-concat.kry")
        concat_result = VM(concat_module).execute("concat", [BytesValue((0x41,)), BytesValue((0xFF,))])
        self.assertEqual(concat_result.hex, fixture["operations"]["concat"]["result"])

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

    def test_arrays_tuples_and_safe_indexing(self) -> None:
        source = """
        let values: Array = [1, 2] + [3]
        let pair: Tuple = ("a", 7)
        println(len(values))
        println(values[1])
        println(pair[0])
        println("Kryndel"[2])
        """
        module = compile_source(source, "sequences.kry")
        captured = io.StringIO()
        VM(module, output=captured.write).run()
        self.assertEqual(captured.getvalue(), "3\n2\na\ny\n")
        values = VM(module).module.functions["main"].instructions
        self.assertIn("MAKE_ARRAY", [instruction.op for instruction in values])
        self.assertIn("MAKE_TUPLE", [instruction.op for instruction in values])
        self.assertIn("INDEX", [instruction.op for instruction in values])

    def test_sequence_runtime_layout_and_deterministic_bytecode(self) -> None:
        source = "fn make() -> Array { return [1, 2] }\nfn pair() -> Tuple { return (1, 2) }\n"
        first = compile_source(source, "sequences.kry")
        second = compile_source(source, "sequences.kry")
        self.assertEqual(first.dumps(), second.dumps())
        runtime = VM(first)
        self.assertEqual(first.functions["main"].instructions[-1].op, "RETURN")
        self.assertIsInstance(runtime.execute("make", []), ArrayValue)
        self.assertIsInstance(runtime.execute("pair", []), TupleValue)

    def test_sequence_index_errors_have_stable_runtime_codes(self) -> None:
        with self.assertRaisesRegex(RuntimeKryndelError, "KRY6104"):
            VM(compile_source("let x: Array = [1]\nprintln(x[1])\n", "index.kry")).run()
        with self.assertRaisesRegex(DiagnosticError, "KRY3054"):
            compile_source('println("x"[true])\n', "index-type.kry")

    def test_option_and_result_are_real_kryndel_enums(self) -> None:
        source = """
        enum Option { None Some(Int) }
        enum Result { Ok(Int) Error(String) }
        let maybe: Option = Option.Some(9)
        let result: Result = Result.Error("bad")
        println(maybe)
        println(result)
        """
        module = compile_source(source, "option-result.kry")
        captured = io.StringIO()
        VM(module, output=captured.write).run()
        self.assertEqual(captured.getvalue(), "Option.Some(9)\nResult.Error(\"bad\")\n")
        self.assertIsInstance(
            VM(compile_source("enum Option { None }\nfn make() -> Option { return Option.None }\n", "option.kry")).execute("make", []),
            EnumValue,
        )

    def test_option_and_result_stdlib_apis_are_kryndel_native_and_total(self) -> None:
        root = Path(__file__).parents[1] / "stdlib"
        option_module = compile_source((root / "core" / "option.kry").read_text(encoding="utf-8"), "stdlib/core/option.kry")
        result_module = compile_source((root / "core" / "result.kry").read_text(encoding="utf-8"), "stdlib/core/result.kry")
        option = VM(option_module)
        result = VM(result_module)
        none = EnumValue("Option", "None")
        some = EnumValue("Option", "Some", (9,))
        ok = EnumValue("Result", "Ok", (7,))
        error = EnumValue("Result", "Error", ("bad",))

        self.assertEqual(option.execute("none", []), none)
        self.assertEqual(option.execute("some", [11]), EnumValue("Option", "Some", (11,)))
        self.assertFalse(option.execute("is_some", [none]))
        self.assertTrue(option.execute("is_some", [some]))
        self.assertTrue(option.execute("is_none", [none]))
        self.assertFalse(option.execute("is_none", [some]))
        self.assertEqual(option.execute("unwrap_or", [none, 4]), 4)
        self.assertEqual(option.execute("unwrap_or", [some, 4]), 9)
        self.assertEqual(option.execute("get_or", [none, 4]), 4)

        self.assertEqual(result.execute("ok", [11]), EnumValue("Result", "Ok", (11,)))
        self.assertEqual(result.execute("error", ["bad"]), error)
        self.assertTrue(result.execute("is_ok", [ok]))
        self.assertFalse(result.execute("is_ok", [error]))
        self.assertFalse(result.execute("is_error", [ok]))
        self.assertTrue(result.execute("is_error", [error]))
        self.assertEqual(result.execute("unwrap_or", [ok, 4]), 7)
        self.assertEqual(result.execute("unwrap_or", [error, 4]), 4)
        self.assertEqual(result.execute("get_or", [error, 4]), 4)

        for module in (option_module, result_module):
            calls = [instruction.arg[0] for function in module.functions.values() for instruction in function.instructions if instruction.op == "CALL"]
            self.assertNotIn("len", calls)
            self.assertNotIn("str", calls)
            self.assertNotIn("int", calls)
            self.assertNotIn("float", calls)

    def test_kryndel_stdlib_sources_compile_and_execute(self) -> None:
        root = Path(__file__).parents[1] / "stdlib"
        string_module = compile_source((root / "string" / "string.kry").read_text(encoding="utf-8"), "stdlib/string/string.kry")
        collections_module = compile_source((root / "collections" / "sequences.kry").read_text(encoding="utf-8"), "stdlib/collections/sequences.kry")
        option_module = compile_source((root / "core" / "option.kry").read_text(encoding="utf-8"), "stdlib/core/option.kry")
        result_module = compile_source((root / "core" / "result.kry").read_text(encoding="utf-8"), "stdlib/core/result.kry")
        utf8_module = compile_source((root / "string" / "utf8.kry").read_text(encoding="utf-8"), "stdlib/string/utf8.kry")
        bytes_module = compile_source((root / "collections" / "bytes.kry").read_text(encoding="utf-8"), "stdlib/collections/bytes.kry")
        self.assertEqual(VM(string_module).execute("length", ["Kryndel"]), 7)
        self.assertEqual(VM(string_module).execute("joined", ["Kry", "ndel"]), "Kryndel")
        self.assertEqual(VM(collections_module).execute("array_length", [ArrayValue((1, 2))]), 2)
        self.assertEqual(VM(collections_module).execute("tuple_length", [TupleValue((1, 2, 3))]), 3)
        encoded = VM(utf8_module).execute("encode", ["A😀"])
        self.assertEqual(encoded, BytesValue((0x41, 0xF0, 0x9F, 0x98, 0x80)))
        self.assertEqual(VM(utf8_module).execute("decode", [encoded]), "A😀")
        self.assertEqual(VM(bytes_module).execute("octet", [encoded, 1]), 0xF0)
        self.assertEqual(len(option_module.functions), 7)
        self.assertEqual(len(result_module.functions), 7)

    def test_boolean_literals_are_runtime_booleans(self) -> None:
        module = compile_source("fn truth() -> Bool { return true }\nfn falsehood() -> Bool { return false }\n", "booleans.kry")
        runtime = VM(module)
        self.assertIs(runtime.execute("truth", []), True)
        self.assertIs(runtime.execute("falsehood", []), False)

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

    def test_argument_arity_errors_include_actionable_help(self) -> None:
        with self.assertRaises(DiagnosticError) as missing:
            compile_source("println()\nfn take(value: Int) -> Int { return value }\nprintln(take())", "arity-missing.kry")
        self.assertIn("Provide 1 more argument(s)", str(missing.exception))
        with self.assertRaises(DiagnosticError) as extra:
            compile_source("fn take(value: Int) -> Int { return value }\nprintln(take(1, 2))", "arity-extra.kry")
        self.assertIn("Remove 1 extra argument(s)", str(extra.exception))

    def test_unknown_type_suggestion_is_conservative(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source("let value: Intr = 1", "type-suggestion.kry")
        self.assertIn("KRY3023", str(context.exception))
        self.assertIn("Did you mean 'Int'?", str(context.exception))

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

    def test_structured_diagnostic_json_contains_stable_fields(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source("let value: Int = \"wrong\"", "structured.kry")
        payload = json.loads(context.exception.as_json())
        diagnostic = payload["diagnostics"][0]
        self.assertEqual(diagnostic["code"], "KRY3003")
        self.assertEqual(diagnostic["phase"], "semantic")
        self.assertEqual(diagnostic["file"], "structured.kry")
        self.assertEqual(diagnostic["severity"], "error")
        self.assertIn("span", diagnostic)
        self.assertIn("suggestion", diagnostic)

    def test_parser_recovers_multiple_independent_errors(self) -> None:
        source = "let broken =\nlet other: Missing = 1\nfn bad( {\nlet third: Unknown = true"
        with self.assertRaises(DiagnosticError) as context:
            compile_source(source, "recovery.kry")
        self.assertGreaterEqual(len(context.exception.diagnostics), 2)
        self.assertTrue({item.code for item in context.exception.diagnostics} & {"KRY2007", "KRY3023"})

    def test_enum_payloads_are_nominal_structural_and_deterministic(self) -> None:
        source = "enum Message { Quit Move(Int, Int) Text(String) }\nprintln(Message.Move(10, 20))\nprintln(Message.Text(\"hello\") == Message.Text(\"hello\"))"
        first = compile_source(source, "payload-one.kry")
        second = compile_source(source, "payload-two.kry")
        self.assertEqual(json.loads(first.dumps())["functions"], json.loads(second.dumps())["functions"])
        self.assertEqual(self.run_source(source), "Message.Move(10, 20)\ntrue\n")

    def test_enum_payloads_support_structs_and_nested_enums(self) -> None:
        source = "struct Point { x: Int y: Int } enum Box { Empty Value(Point) Nested(Box) } println(Box.Value(Point { x: 2, y: 3 }))"
        self.assertEqual(self.run_source(source), "Box.Value(Point { x: 2, y: 3 })\n")

    def test_enum_payload_types_and_values_work_before_declaration(self) -> None:
        source = "fn identity(value: Box) -> Box { return value } let value: Box = identity(Box.Inner(7)) println(value) enum Box { Inner(Int) }"
        self.assertEqual(self.run_source(source), "Box.Inner(7)\n")

    def test_enum_payload_arity_and_type_errors_are_stable(self) -> None:
        with self.assertRaises(DiagnosticError) as arity:
            compile_source("enum Maybe { None Some(Int) }\nlet value: Maybe = Maybe.Some()", "payload-arity.kry")
        self.assertIn("KRY3043", str(arity.exception))
        with self.assertRaises(DiagnosticError) as mismatch:
            compile_source("enum Maybe { None Some(Int) }\nlet value: Maybe = Maybe.Some(\"x\")", "payload-type.kry")
        self.assertIn("KRY3044", str(mismatch.exception))

    def test_match_binds_payloads_and_wildcard_is_ordered(self) -> None:
        source = "enum Maybe { None Some(Int) } let value: Maybe = Maybe.Some(42) match value { Maybe.None => println(\"none\") Maybe.Some(number) => println(number) }"
        self.assertEqual(self.run_source(source), "42\n")
        wildcard = "enum Color { Red Green } match Color.Red { _ => println(\"any\") Color.Green => println(\"never\") }"
        self.assertEqual(self.run_source(wildcard), "any\n")

    def test_match_exhaustiveness_duplicates_and_binding_arity(self) -> None:
        with self.assertRaises(DiagnosticError) as missing:
            compile_source("enum Color { Red Green Blue } match Color.Red { Color.Red => println(1) }", "match-missing.kry")
        self.assertIn("KRY3049", str(missing.exception))
        self.assertIn("Color.Green", str(missing.exception))
        with self.assertRaises(DiagnosticError) as duplicate:
            compile_source("enum Color { Red } match Color.Red { Color.Red => println(1) Color.Red => println(2) }", "match-duplicate.kry")
        self.assertIn("KRY3046", str(duplicate.exception))
        with self.assertRaises(DiagnosticError) as bindings:
            compile_source("enum Maybe { Some(Int) } match Maybe.Some(1) { Maybe.Some => println(1) }", "match-bindings.kry")
        self.assertIn("KRY3048", str(bindings.exception))

    def test_malformed_payload_bytecode_is_a_runtime_diagnostic(self) -> None:
        from kryndel.bytecode import BytecodeFunction, Instruction

        function = BytecodeFunction("main", 0, [Instruction("MAKE_ENUM", {"type": "Maybe", "variant": "Some", "arity": 1}, 1)])
        with self.assertRaises(RuntimeKryndelError) as context:
            VM(Module("malformed", "main", {"main": function})).run()
        self.assertIn("stack underflow", str(context.exception))

    def _write_package(self, root: Path, name: str, version: str, dependencies: dict[str, str] | None = None) -> Path:
        package = root if root.name == version else root / name
        (package / "src").mkdir(parents=True)
        (package / "src" / "lib.kry").write_text("println(1)\n", encoding="utf-8")
        deps = dependencies or {}
        lines = ["[package]", f'name = "{name}"', f'version = "{version}"', 'edition = "2026"', "", "[dependencies]"]
        lines.extend(f'{key} = "{value}"' for key, value in sorted(deps.items()))
        (package / "kry.toml").write_text("\n".join(lines) + "\n", encoding="utf-8")
        (package / "checksum").write_text(package_checksum(package) + "\n", encoding="utf-8")
        return package

    def test_manifest_parser_rejects_unsupported_syntax_and_accepts_subset(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = init_project(root / "demo")
            self.assertEqual(read_manifest(manifest.path).edition, "2026")
            manifest.path.write_text('[package]\nname = "demo"\nversion = "0.1.0"\nedition = "2026"\n\n[unsupported]\nvalue = "x"\n', encoding="utf-8")
            with self.assertRaises(DiagnosticError) as context:
                read_manifest(manifest.path)
            self.assertIn("KRY5001", str(context.exception))

    def test_semver_requirements_and_invalid_versions(self) -> None:
        self.assertTrue(VersionRequirement.parse("^1.2").matches(SemVer(1, 9, 0)))
        self.assertFalse(VersionRequirement.parse("^1.2").matches(SemVer(2, 0, 0)))
        self.assertTrue(VersionRequirement.parse("~1.2").matches(SemVer(1, 2, 9)))
        self.assertFalse(VersionRequirement.parse(">=1.0.0,<2.0.0").matches(SemVer(2, 0, 0)))
        with self.assertRaises(DiagnosticError) as context:
            VersionRequirement.parse("not-semver")
        self.assertIn("KRY5012", str(context.exception))

    def test_local_registry_transitive_resolution_and_deterministic_lockfile(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "project"
            init_project(root)
            registry = root / ".kryndel" / "registry"
            request = self._write_package(registry / "request" / "1.0.0", "request", "1.0.0", {"core": "1.0.0"})
            core = self._write_package(registry / "core" / "1.0.0", "core", "1.0.0")
            self.assertTrue(request.is_dir() and core.is_dir())
            add_dependency(root, "request", version="1.0.0")
            lock = install(root, offline=True)
            self.assertEqual([entry.name for entry in lock.entries], ["core", "request"])
            first = (root / "kry.lock").read_text(encoding="utf-8")
            install(root, offline=True)
            self.assertEqual(first, (root / "kry.lock").read_text(encoding="utf-8"))
            self.assertTrue((root / ".kryndel" / "packages" / "request" / "kry.toml").is_file())

    def test_local_path_dependency_checksum_cycle_and_traversal_errors(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            dep = self._write_package(base, "request", "1.0.0")
            add_dependency(project, "request", path=Path("..") / "request")
            lock = install(project, offline=True)
            self.assertEqual(lock.entries[0].source, "path")
            (dep / "src" / "lib.kry").write_text("tampered\n", encoding="utf-8")
            with self.assertRaises(DiagnosticError) as checksum:
                install(project, offline=True)
            self.assertIn("KRY5008", str(checksum.exception))
            with self.assertRaises(DiagnosticError) as traversal:
                add_dependency(project, "escape", path=base.parent)
            self.assertIn("KRY5010", str(traversal.exception))

    def test_circular_dependency_is_rejected_without_partial_lock(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            self._write_package(base, "a", "1.0.0", {"b": "path:../b"})
            self._write_package(base, "b", "1.0.0", {"a": "path:../a"})
            add_dependency(project, "a", path=Path("..") / "a")
            with self.assertRaises(DiagnosticError) as context:
                install(project, offline=True)
            self.assertIn("KRY5007", str(context.exception))
            self.assertFalse((project / "kry.lock").exists())

    def test_imports_require_declared_project_dependencies(self) -> None:
        with self.assertRaises(DiagnosticError) as context:
            compile_source("import request\nprintln(1)", "imports.kry", allowed_imports=set())
        self.assertIn("KRY5013", str(context.exception))
        self.assertEqual(self.run_source("import request\nprintln(1)"), "1\n")

    def test_import_resolution_requires_installation_and_rejects_ambiguity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            add_dependency(project, "request", path=Path("..") / "request")
            with self.assertRaises(DiagnosticError) as missing:
                validate_imports(project, "import request\n")
            self.assertIn("KRY5014", str(missing.exception))
            install(project, offline=True)
            validate_imports(project, "import request\n")
            installed = project / ".kryndel" / "packages" / "request"
            (installed / "src" / "http.kry").write_text("", encoding="utf-8")
            (installed / "src" / "http").mkdir()
            (installed / "src" / "http" / "lib.kry").write_text("", encoding="utf-8")
            with self.assertRaises(DiagnosticError) as ambiguous:
                validate_imports(project, "import request.http\n")
            self.assertIn("KRY5015", str(ambiguous.exception))

    def _refresh_checksum(self, package: Path) -> None:
        (package / "checksum").write_text(package_checksum(package) + "\n", encoding="utf-8")

    def test_real_nested_import_links_public_function(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text("pub fn root() -> Int { return 1 }\n", encoding="utf-8")
            (package / "src" / "http").mkdir()
            (package / "src" / "http" / "mod.kry").write_text("pub fn get(value: Int) -> Int { return value + 1 }\n", encoding="utf-8")
            (package / "src" / "http" / "client.kry").write_text("pub fn send(value: Int) -> Int { return value + 2 }\n", encoding="utf-8")
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            source = "import request.http.client\nprintln(request.http.client.send(40))\n"
            module = compile_project(project, source, project / "src" / "main.kry")
            output = io.StringIO()
            with redirect_stdout(output):
                VM(module).run()
            self.assertEqual(output.getvalue(), "42\n")
            self.assertIn("request.http.client.send", module.functions)

    def test_import_module_missing_and_deep_ambiguity_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            with self.assertRaises(DiagnosticError) as missing:
                validate_imports(project, "import request.http.client\n")
            self.assertIn("KRY5014", str(missing.exception))
            (package / "src" / "http").mkdir()
            (package / "src" / "http" / "mod.kry").write_text("pub fn get() -> Int { return 1 }\n", encoding="utf-8")
            (package / "src" / "http" / "client").mkdir()
            (package / "src" / "http" / "client.kry").write_text("pub fn get() -> Int { return 1 }\n", encoding="utf-8")
            (package / "src" / "http" / "client" / "mod.kry").write_text("pub fn get() -> Int { return 1 }\n", encoding="utf-8")
            self._refresh_checksum(package)
            install(project, offline=True)
            with self.assertRaises(DiagnosticError) as ambiguous:
                validate_imports(project, "import request.http.client\n")
            self.assertIn("KRY5015", str(ambiguous.exception))

    def test_private_and_missing_imported_symbols_have_stable_codes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text(
                "fn secret() -> Int { return 1 }\npub fn answer() -> Int { return 42 }\n",
                encoding="utf-8",
            )
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            with self.assertRaises(DiagnosticError) as private:
                compile_project(project, "import request\nprintln(request.secret())\n", project / "src" / "main.kry")
            self.assertIn("KRY3051", str(private.exception))
            with self.assertRaises(DiagnosticError) as missing:
                compile_project(project, "import request\nprintln(request.unknown())\n", project / "src" / "main.kry")
            self.assertIn("KRY3050", str(missing.exception))
            module = compile_project(project, "import request\nprintln(request.answer())\n", project / "src" / "main.kry")
            output = io.StringIO()
            with redirect_stdout(output):
                VM(module).run()
            self.assertEqual(output.getvalue(), "42\n")

    def test_import_cycle_and_local_package_collision_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            with self.assertRaises(DiagnosticError) as collision:
                compile_project(project, "import request\nfn request() -> Void { }\n", project / "src" / "main.kry")
            self.assertIn("KRY3052", str(collision.exception))
            (package / "src" / "lib.kry").write_text("import request.http\n", encoding="utf-8")
            (package / "src" / "http").mkdir()
            (package / "src" / "http" / "mod.kry").write_text("import request\n", encoding="utf-8")
            self._refresh_checksum(package)
            install(project, offline=True)
            with self.assertRaises(DiagnosticError) as cycle:
                validate_imports(project, "import request\n")
            self.assertIn("KRY5016", str(cycle.exception))

    def test_public_declaration_syntax_and_deterministic_linking(self) -> None:
        source = "pub fn answer() -> Int { return 42 }\nprintln(answer())\n"
        standalone = compile_source(source, "public.kry")
        self.assertIn("answer", standalone.functions)
        with self.assertRaises(DiagnosticError) as invalid:
            compile_source("pub let value = 1\n", "invalid-public.kry")
        self.assertIn("KRY2014", str(invalid.exception))
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text("pub fn answer() -> Int { return 42 }\n", encoding="utf-8")
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            text = "import request\nprintln(request.answer())\n"
            first = compile_project(project, text, project / "src" / "main.kry").dumps()
            second = compile_project(project, text, project / "src" / "main.kry").dumps()
            self.assertEqual(first, second)

    def test_current_package_nested_module_is_resolved_and_linked(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            project = Path(directory) / "app"
            init_project(project)
            (project / "src" / "lib.kry").write_text("pub fn root() -> Int { return 1 }\n", encoding="utf-8")
            (project / "src" / "http").mkdir()
            (project / "src" / "http" / "mod.kry").write_text("pub fn status() -> Int { return 200 }\n", encoding="utf-8")
            module = compile_project(
                project,
                "import app.http\nprintln(app.http.status())\n",
                project / "src" / "main.kry",
            )
            output = io.StringIO()
            with redirect_stdout(output):
                VM(module).run()
            self.assertEqual(output.getvalue(), "200\n")
            self.assertIn("app.http.status", module.functions)

    def test_duplicate_imports_are_idempotent(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text("pub fn answer() -> Int { return 42 }\n", encoding="utf-8")
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            source = "import request\nimport request\nprintln(request.answer())\n"
            module = compile_project(project, source, project / "src" / "main.kry")
            self.assertEqual(list(module.functions).count("request.answer"), 1)

    def test_parser_preserves_arbitrary_dotted_import_paths(self) -> None:
        source = SourceFile("imports.kry", "import request.http.client\n")
        tokens, lexical = lex(source)
        program, parsing = parse(source, tokens)
        self.assertFalse(lexical.has_errors)
        self.assertFalse(parsing.has_errors)
        self.assertEqual(program.items[0].path, "request.http.client")

    def test_public_visibility_is_recorded_for_all_supported_declarations(self) -> None:
        source = SourceFile(
            "visibility.kry",
            "pub struct Response { code: Int }\npub enum Error { Missing }\npub fn get() -> Int { return 1 }\n",
        )
        tokens, _ = lex(source)
        program, diagnostics = parse(source, tokens)
        self.assertFalse(diagnostics.has_errors)
        declarations = program.items[:3]
        self.assertTrue(all(getattr(item, "public", False) for item in declarations))

    def test_imported_function_signature_is_checked(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text("pub fn answer() -> Int { return 42 }\n", encoding="utf-8")
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            with self.assertRaises(DiagnosticError) as context:
                compile_project(project, "import request\nprintln(request.answer(1))\n", project / "src" / "main.kry")
            self.assertIn("KRY3012", str(context.exception))

    def test_undeclared_nested_import_reports_package_and_module_context(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            project = Path(directory) / "project"
            init_project(project)
            with self.assertRaises(DiagnosticError) as context:
                validate_imports(project, "import missing.http.client\n", project / "src" / "main.kry")
            message = str(context.exception)
            self.assertIn("KRY5013", message)
            self.assertIn("package: missing", message)
            self.assertIn("module: missing.http.client", message)

    def test_private_dependency_function_remains_callable_inside_its_module(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text(
                "fn secret() -> Int { return 40 }\npub fn exposed() -> Int { return secret() + 2 }\n",
                encoding="utf-8",
            )
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            module = compile_project(project, "import request\nprintln(request.exposed())\n", project / "src" / "main.kry")
            output = io.StringIO()
            with redirect_stdout(output):
                VM(module).run()
            self.assertEqual(output.getvalue(), "42\n")

    def test_linked_dependency_functions_have_deterministic_sorted_names(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            project = base / "project"
            init_project(project)
            package = self._write_package(base, "request", "1.0.0")
            (package / "src" / "lib.kry").write_text(
                "pub fn zed() -> Int { return 2 }\npub fn alpha() -> Int { return 1 }\n",
                encoding="utf-8",
            )
            self._refresh_checksum(package)
            add_dependency(project, "request", path=Path("..") / "request")
            install(project, offline=True)
            module = compile_project(project, "import request\nprintln(request.alpha())\n", project / "src" / "main.kry")
            self.assertEqual(list(module.functions), ["main", "request.alpha", "request.zed"])

    def test_kryndel_test_runner_and_formatter_contract(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            project = Path(directory) / "project"
            init_project(project)
            tests = project / "tests"
            tests.mkdir()
            test_file = tests / "basic.kry"
            test_file.write_text("@test\nfn test_answer() -> Void { println(42) }\n", encoding="utf-8")
            self.assertEqual([(result.path.name, result.name) for result in run_kryndel_tests(project)], [("basic.kry", "test_answer")])
            source = project / "src" / "main.kry"
            source.write_text("println(1)  \n\n", encoding="utf-8")
            self.assertFalse(format_file(source, check=True))
            self.assertTrue(format_file(source, check=False))
            self.assertTrue(format_file(source, check=True))

    def test_cli_reproducibility_and_verification_commands(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            project = Path(directory) / "project"
            init_project(project)
            source = project / "src" / "main.kry"
            source.write_text("println(7)\n", encoding="utf-8")
            old = Path.cwd()
            try:
                os.chdir(project)
                self.assertEqual(cli_main(["fmt", "--check"]), 0)
                self.assertEqual(cli_main(["test"]), 0)
                self.assertEqual(cli_main(["reproducible"]), 0)
                artifact = project / "program.kexe"
                self.assertEqual(cli_main(["build", "src/main.kry", "-o", str(artifact)]), 0)
                self.assertEqual(cli_main(["verify-artifact", str(artifact)]), 0)
                module = compile_project(project, source.read_text(encoding="utf-8"), source)
                bytecode = project / "module.json"
                module.dump(bytecode)
                self.assertEqual(cli_main(["inspect-bytecode", str(bytecode)]), 0)
                self.assertEqual(cli_main(["verify-bytecode", str(bytecode)]), 0)
                self.assertEqual(cli_main(["abi"]), 0)
            finally:
                os.chdir(old)

    def test_cli_package_commands_and_artifact_commands_have_codes(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "demo"
            old = Path.cwd()
            try:
                cli_main(["init", str(root)])
                (root / "src" / "main.kry").write_text("println(9)\n", encoding="utf-8")
                os.chdir(root)
                self.assertEqual(cli_main(["check"]), 0)
                artifact = root / "program.kexe"
                self.assertEqual(cli_main(["build", "src/main.kry", "-o", str(artifact)]), 0)
                self.assertEqual(cli_main(["inspect", str(artifact)]), 0)
                self.assertEqual(cli_main(["run", str(artifact)]), 0)
                stderr = io.StringIO()
                with redirect_stderr(stderr):
                    self.assertEqual(cli_main(["check", "missing.kry", "--format", "json"]), 1)
                self.assertIn('"code": "KRY5000"', stderr.getvalue())
            finally:
                os.chdir(old)


if __name__ == "__main__":
    unittest.main()
