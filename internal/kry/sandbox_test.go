package kry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxFilesystemOperations(t *testing.T) {
	root := t.TempDir()
	sb := Sandbox{Root: root, Restricted: true}

	if err := sb.MkdirAll("nested", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sb.WriteFile("nested/input.txt", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := sb.ReadFile("nested/input.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("read %q, want hello", data)
	}
	entries, err := sb.ReadDir("nested")
	if err != nil || len(entries) != 1 || entries[0].Name() != "input.txt" {
		t.Fatalf("unexpected directory entries: %v, %v", entries, err)
	}
	if err := sb.Rename("nested/input.txt", "nested/output.txt"); err != nil {
		t.Fatal(err)
	}
	if info, err := sb.Stat("nested/output.txt"); err != nil || info.IsDir() {
		t.Fatalf("unexpected stat result: %v, %v", info, err)
	}
	if err := sb.RemoveAll("nested"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested directory still exists: %v", err)
	}
}

func TestSandboxCannotRemoveRoot(t *testing.T) {
	sb := Sandbox{Root: t.TempDir(), Restricted: true}
	if err := sb.RemoveAll("."); err == nil {
		t.Fatal("expected removing sandbox root to fail")
	}
}

func TestSandboxRejectsParentPaths(t *testing.T) {
	sb := Sandbox{Root: t.TempDir(), Restricted: true}
	if _, err := sb.ReadFile("../outside"); err == nil {
		t.Fatal("expected parent path to be rejected")
	}
}
