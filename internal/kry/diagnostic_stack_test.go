package kry

import (
	"strings"
	"testing"
)

func TestRuntimeDiagnosticsCarryCallStack(t *testing.T) {
	source := `fn inner(value: Int) -> Int {
    let zero: Int = 0
    return value / zero
}
fn outer() -> Int { return inner(4) }
fn main() -> Nil {
    println(outer())
    return nil
}`
	_, d := runInterpreterCapture(t, source)
	if d == nil || len(d.Stack) != 3 {
		t.Fatalf("expected a three-frame runtime stack, got %#v", d)
	}
	want := []string{"inner", "outer", "main"}
	for i, name := range want {
		if d.Stack[i].Function != name {
			t.Fatalf("stack frame %d: got %q, want %q", i, d.Stack[i].Function, name)
		}
		if d.Stack[i].Source != "differential.kry" || d.Stack[i].Line < 1 || d.Stack[i].Column < 1 {
			t.Fatalf("stack frame %d has incomplete source coordinates: %#v", i, d.Stack[i])
		}
	}
	if formatted := d.Format(false); !strings.Contains(formatted, "at inner (") || !strings.Contains(formatted, "at main (") {
		t.Fatalf("human diagnostic did not render its stack: %s", formatted)
	}
	if formatted := d.Format(true); !strings.Contains(formatted, `"stack":[`) || !strings.Contains(formatted, `"function":"outer"`) {
		t.Fatalf("JSON diagnostic did not serialize its stack: %s", formatted)
	}
}
