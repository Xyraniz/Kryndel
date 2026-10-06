package kry

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestSelfhostFlatKIRArenaPreservesTypedValuesAndBounds(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "selfhost", "kir_arena_test.kry"))
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	program, diagnostic := LoadProgram(root, limits, "")
	if diagnostic != nil {
		t.Fatalf("load KIR arena fixture: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatalf("check KIR arena fixture: %s", diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create KIR arena runtime: %s", diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("KIR arena fixture failed: %s; stdout=%q", diagnostic.Message, output.String())
	}
	if output.Len() != 0 {
		t.Fatalf("KIR arena fixture unexpectedly wrote %q", output.String())
	}
}
