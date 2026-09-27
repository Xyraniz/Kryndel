package kry

import "testing"

func TestRuntimeWithoutDebuggerExecutesEveryTopLevelStatement(t *testing.T) {
	p, checker := testProgram(t, "let first: Int = 1\nlet second: Int = 2\n")
	runtime, diagnostic := NewRuntime(p, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	if diagnostic = runtime.run(); diagnostic != nil {
		t.Fatal(diagnostic)
	}
	for name, expected := range map[string]int64{"first": 1, "second": 2} {
		binding, ok := runtime.Global.get(name)
		if !ok || binding.Value.Kind != VInt || binding.Value.I != expected {
			t.Errorf("top-level %s = %#v, want Int %d", name, binding, expected)
		}
	}
}
