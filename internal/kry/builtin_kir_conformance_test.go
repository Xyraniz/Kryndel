package kry

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJSONBuiltinCorpusMatchesRuntimeAndKIRForSourceAndKexe(t *testing.T) {
	corpus := []struct {
		name             string
		source           string
		wantOutputText   string
		wantCanonicalDoc string
	}{
		{
			name: "accessors and canonicalization",
			source: `fn main() -> Nil {
    let doc: Json = result_unwrap(json_parse("{\"z\":1,\"a\":2,\"z\":3,\"name\":\"é\\n🙂<>&\",\"surrogate\":\"\\ud83d\\ude42\",\"unpaired\":\"\\ud800\",\"controls\":\"\\b\\f\",\"items\":[null,true,false,1.25,18446744073709551615]}"))
    println(json_kind(doc))
    println(json_stringify(doc))
    let name: Json = result_unwrap(json_object_get(doc, "name"))
    println(json_kind(name))
    println(json_string(name))
    let surrogate: Json = result_unwrap(json_object_get(doc, "surrogate"))
    println(json_string(surrogate))
    let unpaired: Json = result_unwrap(json_object_get(doc, "unpaired"))
    println(json_string(unpaired))
    let controls: Json = result_unwrap(json_object_get(doc, "controls"))
    println(json_string(controls))
    let items: Json = result_unwrap(json_object_get(doc, "items"))
    println(json_kind(items))
    println(json_array_len(items))
    let nil_value: Json = result_unwrap(json_array_get(items, 0))
    println(json_kind(nil_value))
    println(json_is_null(nil_value))
    let true_value: Json = result_unwrap(json_array_get(items, 1))
    println(json_bool(true_value))
    let false_value: Json = result_unwrap(json_array_get(items, 2))
    println(json_bool(false_value))
    let fraction: Json = result_unwrap(json_array_get(items, 3))
    println(json_float(fraction))
    let unsigned_value: Json = result_unwrap(json_array_get(items, 4))
    println(json_uint(unsigned_value))
    let minimum: Json = result_unwrap(json_parse("-9223372036854775808"))
    println(json_int(minimum))
    let number: Json = result_unwrap(json_parse("1"))
    println(json_kind(number))
    return nil
}
`,
			wantOutputText:   "ok(18446744073709551615)\nok(-9223372036854775808)\n",
			wantCanonicalDoc: `"a":2`,
		},
		{
			name: "type and range errors",
			source: `fn main() -> Nil {
    let object: Json = result_unwrap(json_parse("{\"a\":1}"))
    let array: Json = result_unwrap(json_parse("[1]"))
    let number: Json = result_unwrap(json_parse("1"))
    let fraction: Json = result_unwrap(json_parse("1.5"))
    let too_large_int: Json = result_unwrap(json_parse("9223372036854775808"))
    let too_large_float: Json = result_unwrap(json_parse("1e400"))
    let too_small_float: Json = result_unwrap(json_parse("1e-4000"))
    let too_large_uint: Json = result_unwrap(json_parse("18446744073709551616"))
    let too_large_negative_int: Json = result_unwrap(json_parse("-9223372036854775809"))
    let negative: Json = result_unwrap(json_parse("-1"))
    let text: Json = result_unwrap(json_parse("\"text\""))
    println(json_parse("not json"))
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
    return nil
}
`,
			wantOutputText: "invalid JSON",
		},
		{
			name: "escaped key ordering",
			source: `fn main() -> Nil {
    let doc: Json = result_unwrap(json_parse("{\"\\\"\":1,\"#\":2}"))
    println(json_stringify(doc))
    return nil
}
`,
			wantCanonicalDoc: `{"\"":1,"#":2}`,
		},
	}

	for _, testCase := range corpus {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, directKIR, diagnostic := compareKIRExecutionWithRuntime(t, testCase.source, DefaultLimits())
			if diagnostic != nil {
				t.Fatalf("Runtime reported diagnostic: %#v", diagnostic)
			}
			if testCase.wantOutputText != "" && !strings.Contains(string(directKIR.Output), testCase.wantOutputText) {
				t.Fatalf("KIR output %q does not contain expected JSON corpus result %q", directKIR.Output, testCase.wantOutputText)
			}
			if testCase.wantCanonicalDoc != "" && !strings.Contains(string(directKIR.Output), testCase.wantCanonicalDoc) {
				t.Fatalf("KIR output %q does not contain canonical JSON fragment %q", directKIR.Output, testCase.wantCanonicalDoc)
			}

			directory := t.TempDir()
			sourcePath := filepath.Join(directory, "json.kry")
			artifactPath := filepath.Join(directory, "json.kexe")
			if err := os.WriteFile(sourcePath, []byte(testCase.source), 0o600); err != nil {
				t.Fatal(err)
			}
			engine := NewEngine()
			if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
				t.Fatalf("build .kexe: %s", diagnostic.Message)
			}

			for _, path := range []string{sourcePath, artifactPath} {
				t.Run(filepath.Ext(path), func(t *testing.T) {
					direct := runEngineKIRDocument(t, engine, path)
					if string(direct.Output) != string(directKIR.Output) || !sameRuntimeDiagnostic(direct.Diagnostic, directKIR.Diagnostic) {
						t.Fatalf("direct KIR output/diagnostic differ: got (%q, %#v), corpus (%q, %#v)", direct.Output, direct.Diagnostic, directKIR.Output, directKIR.Diagnostic)
					}

					wantOutput, wantDiagnostic := runASTPath(t, engine, path, nil)
					gotOutput, gotDiagnostic := captureEngineRun(t, func() *Diagnostic {
						_, runDiagnostic := engine.RunPath(path)
						return runDiagnostic
					})
					if gotOutput != wantOutput || !reflect.DeepEqual(gotDiagnostic, wantDiagnostic) {
						t.Fatalf("Engine output/diagnostic differ from Runtime: Engine (%q, %#v), Runtime (%q, %#v)", gotOutput, gotDiagnostic, wantOutput, wantDiagnostic)
					}
					if gotOutput != string(directKIR.Output) || !sameRuntimeDiagnostic(gotDiagnostic, directKIR.Diagnostic) {
						t.Fatalf("Engine output/diagnostic differ from direct KIR: Engine (%q, %#v), KIR (%q, %#v)", gotOutput, gotDiagnostic, directKIR.Output, directKIR.Diagnostic)
					}
				})
			}
		})
	}
}

func TestProcessRunCAOTMatchesRuntimeForSuccessAndNonzeroExit(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KRY_TEST_PROCESS_RUN_CONFORMANCE_HELPER", "1")
	source := fmt.Sprintf(`fn main() -> Nil {
    println(process_run(%s, ["-test.run=^TestProcessRunNativeConformanceHelper$", "--", "success"]))
    println(process_run(%s, ["-test.run=^TestProcessRunNativeConformanceHelper$", "--", "exit-23"]))
    return nil
}
`, kryStringLiteral(executable), kryStringLiteral(executable))

	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("Runtime process_run failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil || status != 0 {
		t.Fatalf("C AOT process_run failed: status=%d err=%v output=%q", status, err, native)
	}
	if native != interpreted {
		t.Fatalf("C AOT and Runtime process_run differ: Runtime %q, C AOT %q", interpreted, native)
	}
	want := "ok(0)\nerr(process exited with code 23)\n"
	if interpreted != want {
		t.Fatalf("process_run results = %q, want %q", interpreted, want)
	}
}

func TestProcessRunNativeConformanceHelper(t *testing.T) {
	if os.Getenv("KRY_TEST_PROCESS_RUN_CONFORMANCE_HELPER") != "1" {
		return
	}
	for _, argument := range os.Args {
		switch argument {
		case "success":
			os.Exit(0)
		case "exit-23":
			os.Exit(23)
		}
	}
	os.Exit(24)
}
