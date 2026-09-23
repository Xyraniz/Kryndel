package main

import (
	"io"
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

func TestNoExternalToolchainCLIRejectsCBackendBeforeSourceIO(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writeEnd
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})

	missing := filepath.Join(t.TempDir(), "does-not-exist.kry")
	status := run([]string{"build", missing, "--format=elf", "--no-external-toolchain"})
	_ = writeEnd.Close()
	os.Stderr = oldStderr
	message, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}
	if status != 2 {
		t.Fatalf("no-external-toolchain build returned %d, want 2", status)
	}
	if !strings.Contains(string(message), "--no-external-toolchain forbids --format=elf") || !strings.Contains(string(message), "external C compiler") {
		t.Fatalf("build did not explain the forbidden backend dependency: %s", message)
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
