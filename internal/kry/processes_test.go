package kry

import (
	"encoding/json"
	"fmt"
	"os"
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
	got := runInterp(t, src)
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
