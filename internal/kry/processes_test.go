package kry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessBuiltinsInterpreter(t *testing.T) {
	pid := os.Getpid()
	src := fmt.Sprintf(`fn main() -> Nil {
    match process_info(%d) {
        ok(value) => {
            let text: String = json_stringify(value)
            println(str(contains(text, "pid")))
            println(str(contains(text, "name")))
        }
        err(problem) => { println("info err") }
    }
    match process_list() {
        ok(value) => { println("list ok") }
        err(problem) => { println("list err") }
    }
    match process_info(-1) {
        ok(value) => { println("invalid pid accepted") }
        err(problem) => { println("invalid pid rejected") }
    }
    return nil
}
`, pid)
	got := runInterpUnrestricted(t, src)
	want := "true\ntrue\nlist ok\ninvalid pid rejected\n"
	if got != want {
		t.Fatalf("unexpected process output: got %q want %q", got, want)
	}
}

func TestProcessInfoJSONShape(t *testing.T) {
	data, err := processInfoJSON(nil, int64(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"pid", "name", "exe", "username", "create_time_ms", "status"} {
		if _, ok := value[key]; !ok {
			t.Fatalf("process info missing %q: %s", key, data)
		}
	}
}

func TestRestrictedModeBlocksProcessRunBeforeEvaluatingArguments(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "restricted-escape")
	argumentMarker := filepath.Join(root, "argument-evaluated")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KRY_TEST_RESTRICTED_PROCESS_HELPER", "1")
	source := fmt.Sprintf(`fn make_args() -> Array[String] {
    let marker: Result[Nil, String] = fs_write_text("argument-evaluated", "yes")
    return ["-test.run=TestRestrictedProcessRunHelper", "--", %s]
}
fn main() -> Nil {
    let result: Result[Int, String] = process_run(%s, make_args())
    return nil
}
main()
`, kryStringLiteral(outside), kryStringLiteral(executable))
	sourcePath := filepath.Join(root, "main.kry")
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	engine.RestrictedRoot = root
	if _, diagnostic := engine.RunPath(sourcePath); diagnostic == nil || !strings.Contains(diagnostic.Message, `process_run" is unavailable with --restricted`) {
		t.Fatalf("restricted process_run diagnostic = %v", diagnostic)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("restricted process_run created an outside file: %v", err)
	}
	if _, err := os.Stat(argumentMarker); !os.IsNotExist(err) {
		t.Fatalf("restricted process_run evaluated its arguments before denial: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(`fn main() -> Nil {
    let result: Result[Nil, String] = fs_write_text("inside.txt", "ok")
    assert_eq(is_ok(result), true)
    return nil
}
main()
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, diagnostic := engine.RunPath(sourcePath); diagnostic != nil {
		t.Fatalf("restricted filesystem builtin failed: %v", diagnostic)
	}
	if data, err := os.ReadFile(filepath.Join(root, "inside.txt")); err != nil || string(data) != "ok" {
		t.Fatalf("restricted filesystem write = %q, %v", data, err)
	}

	for _, name := range []string{"env_get", "http_get", "ffi_library_open", "sqlite_open", "platform_hostname", "screen_capture", "win_input_read", "datetime_now", "datetime_unix_ms", "discord_cache_get"} {
		builtin := Builtins()[name]
		if builtin.Name == "" || (Sandbox{Restricted: true}).allowsBuiltin(builtin) {
			t.Errorf("restricted mode allowed unmediated builtin %q", name)
		}
	}
	for _, name := range []string{"datetime_format", "datetime_parse", "fs_write_text", "json_parse", "println"} {
		builtin := Builtins()[name]
		if builtin.Name == "" || !(Sandbox{Restricted: true}).allowsBuiltin(builtin) {
			t.Errorf("restricted mode denied safe builtin %q", name)
		}
	}
	if (Sandbox{Restricted: true}).allowsBuiltin(Builtin{Effects: "unreviewed-host-effect"}) {
		t.Fatal("restricted mode allowed an unreviewed effect category")
	}
}

func TestRestrictedProcessRunHelper(t *testing.T) {
	if os.Getenv("KRY_TEST_RESTRICTED_PROCESS_HELPER") != "1" {
		return
	}
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("escaped"), 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func kryStringLiteral(value string) string {
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + quoted + `"`
}
