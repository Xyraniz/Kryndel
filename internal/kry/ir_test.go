package kry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileIRRejectsExceededInstructionLimit(t *testing.T) {
	p, _ := testProgram(t, "let value: Int = 1\n")
	limits := DefaultLimits()
	limits.MaxInstructions = 1
	if _, diagnostic := CompileIR(p, limits); diagnostic == nil || !strings.Contains(diagnostic.Message, "IR instruction limit exceeded") {
		t.Fatalf("expected instruction limit rejection, got %v", diagnostic)
	}
}

func TestCompileIRRejectsExceededNestingLimit(t *testing.T) {
	p, _ := testProgram(t, "let value: Int = 1 + 2\n")
	limits := DefaultLimits()
	limits.MaxNesting = 1
	if _, diagnostic := CompileIR(p, limits); diagnostic == nil || !strings.Contains(diagnostic.Message, "IR nesting limit exceeded") {
		t.Fatalf("expected nesting limit rejection, got %v", diagnostic)
	}
}

func TestEngineValidatesIRForPortableArtifacts(t *testing.T) {
	p, _ := testProgram(t, "let value: Int = 1\n")
	data, diagnostic := BuildArtifact(p, t.TempDir())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	path := filepath.Join(t.TempDir(), "program.kexe")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	engine.Limits.MaxInstructions = 1
	if _, _, diagnostic := engine.CheckPath(path); diagnostic == nil || !strings.Contains(diagnostic.Message, "IR instruction limit exceeded") {
		t.Fatalf("expected portable artifact to use IR validation, got %v", diagnostic)
	}
}
