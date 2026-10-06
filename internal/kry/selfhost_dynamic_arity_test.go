package kry

import (
	"path/filepath"
	"testing"
)

func TestSelfhostLinuxBootstrapFunctionsFitDynamicBackendArity(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "selfhost", "source_kir_compiler.kry"))
	if err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(root, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load selfhost compiler module graph: %s", diagnostic.Message)
	}
	for _, function := range program.Functions {
		if len(function.Params) > 6 {
			t.Errorf("selfhost function %q has %d parameters; the Linux amd64 dynamic backend supports at most six", function.Name, len(function.Params))
		}
	}
}
