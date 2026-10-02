package kry

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func requireJSONNativeParity(t *testing.T, source string) {
	t.Helper()
	if !((runtime.GOOS == "linux" || runtime.GOOS == "windows") && runtime.GOARCH == "amd64") {
		t.Skip("C AOT JSON differential tests require linux/amd64 or windows/amd64")
	}
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("JSON outputs differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
}

func TestCAOTJSONAccessorsMatchInterpreter(t *testing.T) {
	source := `fn main() -> Result[Nil, String] {
    let doc: Json = json_parse("{\"z\":1,\"a\":2,\"z\":3,\"name\":\"é\\n🙂<>&\",\"surrogate\":\"\\ud83d\\ude42\",\"unpaired\":\"\\ud800\",\"controls\":\"\\b\\f\",\"items\":[null,true,false,1.25,18446744073709551615]}")?
    println(json_kind(doc))
    println(json_stringify(doc))
    let name: Json = json_object_get(doc, "name")?
    println(json_kind(name))
    println(json_string(name))
    let surrogate: Json = json_object_get(doc, "surrogate")?
    println(json_string(surrogate))
    let unpaired: Json = json_object_get(doc, "unpaired")?
    println(json_string(unpaired))
    let controls: Json = json_object_get(doc, "controls")?
    println(json_string(controls))
    let items: Json = json_object_get(doc, "items")?
    println(json_kind(items))
    println(json_array_len(items))
    let nil_value: Json = json_array_get(items, 0)?
    println(json_kind(nil_value))
    println(json_is_null(nil_value))
    let true_value: Json = json_array_get(items, 1)?
    println(json_bool(true_value))
    let false_value: Json = json_array_get(items, 2)?
    println(json_bool(false_value))
    let fraction: Json = json_array_get(items, 3)?
    println(json_float(fraction))
    let unsigned_value: Json = json_array_get(items, 4)?
    println(json_uint(unsigned_value))
    let minimum: Json = json_parse("-9223372036854775808")?
    println(json_int(minimum))
    let number: Json = json_parse("1")?
    println(json_kind(number))
    return ok(nil)
}
`
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONObjectKeysMatchInterpreter(t *testing.T) {
	source := `fn main() -> Result[Nil, String] {
    let doc: Json = json_parse("{\"z\":1,\"é\":2,\"a\":3,\"💩\":4,\"z\":5}")?
    let keys: Array[String] = json_object_keys(doc)?
    assert_eq(unwrap_or(array_get(keys, 0), ""), "a")
    assert_eq(unwrap_or(array_get(keys, 1), ""), "z")
    assert_eq(unwrap_or(array_get(keys, 2), ""), "é")
    assert_eq(unwrap_or(array_get(keys, 3), ""), "💩")
    println(str(keys))
    println(json_object_keys(json_parse("[]")?))
    return ok(nil)
}
`
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONObjectKeysAllowedMatchesInterpreter(t *testing.T) {
	source := `fn main() -> Result[Nil, String] {
    let object: Json = json_parse("{\"kind\":\"binary\",\"left\":1,\"right\":2}")?
    println(json_object_keys_allowed(object, "|kind|left|right|"))
    println(json_object_keys_allowed(object, "|kind|left|"))
    println(json_object_keys_allowed(object, "kind|left|right|"))
    println(json_object_keys_allowed(object, "|kind||left|right|"))
    println(json_object_keys_allowed(json_parse("{}")?, ""))
    println(json_object_keys_allowed(json_parse("{\"x\":1}")?, ""))
    println(json_object_keys_allowed(json_parse("[]")?, "|length|"))
    println(json_object_keys_allowed(json_parse("{\"é\":1}")?, "|é|"))
    return ok(nil)
}`
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONObjectFieldsEmptyExceptMatchInterpreter(t *testing.T) {
	source := `fn main() -> Result[Nil, String] {
    let valid: Json = json_parse("{\"kind\":\"binary\",\"left\":1,\"right\":2,\"args\":[],\"map_keys\":[]}")?
    let scalar_child: Json = json_parse("{\"kind\":\"int\",\"left\":1}")?
    let null_child: Json = json_parse("{\"kind\":\"int\",\"left\":null}")?
    println(json_object_fields_empty_except(valid, "|left|right|args|map_keys|", "|kind|left|right|", "|args|map_keys|"))
    println(json_object_fields_empty_except(valid, "|left|right|args|map_keys|", "|kind|left|right|", "|args|"))
    println(json_object_fields_empty_except(scalar_child, "|left|", "|kind|", ""))
    println(json_object_fields_empty_except(null_child, "|left|", "|kind|", ""))
    println(json_object_fields_empty_except(json_parse("[]")?, "|left|", "|kind|", ""))
    println(json_object_fields_empty_except(valid, "|left|right|args|map_keys|", "kind", "|args|map_keys|"))
    return ok(nil)
}`
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONObjectKeysRespectArrayElementLimit(t *testing.T) {
	if !((runtime.GOOS == "linux" || runtime.GOOS == "windows") && runtime.GOARCH == "amd64") {
		t.Skip("C AOT JSON differential tests require linux/amd64 or windows/amd64")
	}
	source := `fn main() -> Result[Nil, String] {
    let within: Json = json_parse("{\"e\":1,\"d\":2,\"c\":3,\"b\":4,\"a\":5}")?
    let over: Json = json_parse("{\"a\":1,\"b\":2,\"c\":3,\"d\":4,\"e\":5,\"f\":6}")?
    println(is_ok(json_object_keys(within)))
    println(json_object_keys(over))
    return ok(nil)
}
`
	limits := DefaultLimits()
	limits.MaxArrayElements = 5
	interpreted, diagnostic := runInterpreterCaptureWithLimits(t, source, limits)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOTWithLimits(t, source, limits)
	if err != nil || status != 0 {
		t.Fatalf("C AOT failed: status=%d err=%v output=%q", status, err, native)
	}
	if native != interpreted {
		t.Fatalf("object key limit differs:\ninterpreter: %q\nC AOT:       %q", interpreted, native)
	}
}

func TestCAOTJSONAccessorErrorsMatchInterpreter(t *testing.T) {
	source := `fn main() -> Result[Nil, String] {
    let object: Json = json_parse("{\"a\":1}")?
    let array: Json = json_parse("[1]")?
    let number: Json = json_parse("1")?
    let fraction: Json = json_parse("1.5")?
    let too_large_int: Json = json_parse("9223372036854775808")?
    let too_large_float: Json = json_parse("1e400")?
    let too_small_float: Json = json_parse("1e-4000")?
    let too_large_uint: Json = json_parse("18446744073709551616")?
    let too_large_negative_int: Json = json_parse("-9223372036854775809")?
    let negative: Json = json_parse("-1")?
    let text: Json = json_parse("\"text\"")?
    println(json_object_get(array, "missing"))
    println(json_object_get(object, "missing"))
    println(json_array_len(object))
    println(json_array_get(object, 0))
    println(json_array_get(array, -1))
    println(json_string(number))
    println(json_int(fraction))
    println(json_int(too_large_int))
    println(json_uint(negative))
    println(json_uint(too_large_uint))
    println(json_int(too_large_negative_int))
    println(json_float(number))
    println(json_float(too_large_float))
    println(json_kind(too_large_float))
    println(json_string(too_large_float))
    println(json_float(too_small_float))
    println(json_bool(text))
    println(json_is_null(number))
    return ok(nil)
}
`
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONCanonicalKeyOrdering(t *testing.T) {
	jsonText := `{"\"":1,"#":2}`
	source := "fn main() -> Result[Nil, String] {\n" +
		"    let doc: Json = json_parse(" + strconv.Quote(jsonText) + ")?\n" +
		"    println(json_stringify(doc))\n" +
		"    return ok(nil)\n}\n"
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONUIntSemanticsMatchInterpreter(t *testing.T) {
	source := `fn main() -> Result[Nil, String] {
    let zero: UInt64 = json_uint(json_parse("0")?)?
    let one: UInt64 = json_uint(json_parse("1")?)?
    let max: UInt64 = json_uint(json_parse("18446744073709551615")?)?
    println(bool(zero))
    println(bool(max))
    println(int(zero))
    println(max + one)
    println(max - one)
    println(max * one)
    println(max / one)
    println(max % one)
    println(zero < one)
    println(max > one)
    println(max >= max)
    println(max == max)
    return ok(nil)
}
`
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONLargeObjectDuplicateKeysMatchInterpreter(t *testing.T) {
	var document strings.Builder
	document.Grow(200_000)
	document.WriteByte('{')
	for i := 0; i < 12_000; i++ {
		if i != 0 {
			document.WriteByte(',')
		}
		document.WriteString(strconv.Quote("key" + strconv.Itoa(i)))
		document.WriteByte(':')
		document.WriteString(strconv.Itoa(i))
	}
	document.WriteString(`,"same":1,"\u0073ame":2}`)
	source := "fn main() -> Result[Nil, String] {\n" +
		"    let doc: Json = json_parse(" + strconv.Quote(document.String()) + ")?\n" +
		"    let value: Json = json_object_get(doc, \"same\")?\n" +
		"    println(json_int(value)?)\n" +
		"    return ok(nil)\n}\n"
	requireJSONNativeParity(t, source)
}

func TestCAOTJSONRejectsInvalidEscapesControlsAndExcessDepth(t *testing.T) {
	invalidDocuments := []string{`"\q"`, `"\u12XZ"`, "\"line\nbreak\""}
	for _, document := range invalidDocuments {
		source := "fn main() -> Nil {\n    println(is_err(json_parse(" + strconv.Quote(document) + ")))\n    return nil\n}\n"
		requireJSONNativeParity(t, source)
	}
	for _, depth := range []int{maxJSONNestingDepth, maxJSONNestingDepth + 1, 10_001} {
		deepDocument := strings.Repeat("[", depth) + strings.Repeat("]", depth)
		var source string
		if depth <= maxJSONNestingDepth {
			source = "fn main() -> Nil {\n    println(is_ok(json_parse(" + strconv.Quote(deepDocument) + ")))\n    return nil\n}\n"
		} else {
			source = "fn main() -> Nil {\n    println(json_parse(" + strconv.Quote(deepDocument) + "))\n    return nil\n}\n"
		}
		requireJSONNativeParity(t, source)
	}
}

func TestCAOTJSONInvalidUTF8StringMatchesInterpreter(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("C AOT invalid UTF-8 argument test requires linux/amd64")
	}
	source := `fn main() -> Result[Nil, String] {
    let args: Array[String] = process_args()
    let doc: Json = json_parse(args[0])?
    println(json_stringify(doc))
    println(json_string(doc)?)
    return ok(nil)
}
`
	invalidJSON := string([]byte{'"', 0xff, '"'})
	program, diagnostic := Parse(&Source{Name: "json-invalid-utf8.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	interpretedRuntime, diagnostic := NewRuntimeWithArgs(program, checker, DefaultLimits(), Sandbox{}, []string{invalidJSON})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	oldStdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	runDiagnostic := interpretedRuntime.run()
	_ = write.Close()
	os.Stdout = oldStdout
	interpretedBytes, readErr := io.ReadAll(read)
	_ = read.Close()
	if runDiagnostic != nil {
		t.Fatalf("interpreter failed: %s", runDiagnostic.Message)
	}
	if readErr != nil {
		t.Fatal(readErr)
	}
	cSource := strings.Join([]string{
		"#define _XOPEN_SOURCE 700",
		cRuntimeSource(),
		`int main(void) {
    static const char raw_json[] = {'"', (char)0xff, '"'};
    KValue parsed = k_json_parse(kv_strn(raw_json, sizeof(raw_json)));
    if (!parsed.u.res.ok) return 2;
    KValue doc = *parsed.u.res.inner;
    KValue encoded = k_json_stringify(doc);
    fwrite(encoded.u.s.data, 1, encoded.u.s.len, stdout);
    putchar('\n');
    KValue decoded = k_json_string_value(doc);
    if (!decoded.u.res.ok) return 3;
    KValue text = *decoded.u.res.inner;
    fwrite(text.u.s.data, 1, text.u.s.len, stdout);
    putchar('\n');
    return 0;
}`,
	}, "\n")
	cPath := filepath.Join(t.TempDir(), "json-runtime.c")
	binaryPath := filepath.Join(t.TempDir(), "json-runtime")
	if err := os.WriteFile(cPath, []byte(cSource), 0o600); err != nil {
		t.Fatal(err)
	}
	compileOutput, err := exec.Command("cc", "-std=c11", "-O0", "-o", binaryPath, cPath, "-lm").CombinedOutput()
	if err != nil {
		t.Fatalf("C runtime harness build failed: %v; output: %s", err, compileOutput)
	}
	nativeBytes, err := exec.Command(binaryPath).CombinedOutput()
	if err != nil {
		t.Fatalf("C runtime harness failed: %v; output: %q", err, nativeBytes)
	}
	if string(nativeBytes) != string(interpretedBytes) {
		t.Fatalf("invalid UTF-8 JSON differs:\ninterpreter: %q\nC AOT: %q", interpretedBytes, nativeBytes)
	}
}

func TestDirectELFRejectsJSONBuiltinsWithExplicitDiagnostics(t *testing.T) {
	builtins := map[string]string{
		"json_parse":       `fn use() -> Result[Json, String] { return json_parse("null") }`,
		"json_stringify":   `fn use(doc: Json) -> String { return json_stringify(doc) }`,
		"json_kind":        `fn use(doc: Json) -> String { return json_kind(doc) }`,
		"json_object_get":  `fn use(doc: Json) -> Result[Json, String] { return json_object_get(doc, "x") }`,
		"json_object_keys": `fn use(doc: Json) -> Result[Array[String], String] { return json_object_keys(doc) }`,
		"json_array_len":   `fn use(doc: Json) -> Result[Int, String] { return json_array_len(doc) }`,
		"json_array_get":   `fn use(doc: Json) -> Result[Json, String] { return json_array_get(doc, 0) }`,
		"json_string":      `fn use(doc: Json) -> Result[String, String] { return json_string(doc) }`,
		"json_int":         `fn use(doc: Json) -> Result[Int, String] { return json_int(doc) }`,
		"json_uint":        `fn use(doc: Json) -> Result[UInt64, String] { return json_uint(doc) }`,
		"json_float":       `fn use(doc: Json) -> Result[Float, String] { return json_float(doc) }`,
		"json_bool":        `fn use(doc: Json) -> Result[Bool, String] { return json_bool(doc) }`,
		"json_is_null":     `fn use(doc: Json) -> Bool { return json_is_null(doc) }`,
	}
	for builtin, useFunction := range builtins {
		t.Run(builtin, func(t *testing.T) {
			if got := nativeBuiltinBackendStatus(builtin, "elf-direct", NativeTarget{OS: "linux", Arch: "amd64"}); got != "unsupported" {
				t.Fatalf("ELF-direct capability for %s is %q, want unsupported", builtin, got)
			}
			source := useFunction + "\nfn main() -> Nil { return nil }\n"
			program, diagnostic := Parse(&Source{Name: "direct-json-rejected.kry", Text: source}, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			checker, diagnostic := Check(program, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			_, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			want := `builtin "` + builtin + `" is not listed as supported by the elf-direct backend`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("ELF-direct should reject %s explicitly, got %v", builtin, err)
			}
		})
	}
}
