package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectProgramInvocation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "hi.kry")
	if err := os.WriteFile(source, []byte("println(\"direct\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{source}); got != 0 {
		t.Fatalf("direct source invocation returned %d", got)
	}
	if got := run([]string{"run", source}); got != 0 {
		t.Fatalf("explicit run invocation returned %d", got)
	}
	if got := run([]string{source, "unexpected"}); got != 2 {
		t.Fatalf("direct invocation accepted extra arguments with status %d", got)
	}
}

func TestProgramPathRequiresKnownExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "program.txt")
	if err := os.WriteFile(path, []byte("println(\"not direct\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if isProgramPath(path) {
		t.Fatal("non-Kryndel extension was treated as a program")
	}
}

func TestEmitAcceptsExplicitKIRTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "target.kry")
	out := filepath.Join(dir, "target.kir")
	if err := os.WriteFile(source, []byte("println(\"target\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{"emit", source, "--target=linux-x64", "-o", out}); got != 0 {
		t.Fatalf("emit with explicit target returned %d", got)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"os": "linux"`) || !strings.Contains(string(data), `"arch": "amd64"`) {
		t.Fatalf("KIR did not contain requested Linux target: %s", data)
	}
}
