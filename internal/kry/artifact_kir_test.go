package kry

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKexeEmbedsAndValidatesTypedKIR(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.kry")
	artifactPath := filepath.Join(dir, "main.kexe")
	if err := os.WriteFile(sourcePath, []byte("let value: Int = 41\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}

	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, diagnostic := DecodeArtifact(data, engine.Limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if len(artifact.KIR) == 0 {
		t.Fatal("new .kexe is missing its typed KIR")
	}
	if artifact.Target != "portable/any" {
		t.Fatalf("new .kexe is target-bound: %q", artifact.Target)
	}
	document, err := DecodeKIR(artifact.KIR, engine.Limits)
	if err != nil {
		t.Fatalf("embedded KIR is invalid: %v", err)
	}
	if document.Target.OS != "portable" || document.Target.Arch != "any" {
		t.Fatalf("new .kexe KIR is target-bound: %#v", document.Target)
	}
	if _, _, diagnostic := engine.CheckPath(artifactPath); diagnostic != nil {
		t.Fatalf("valid .kexe failed validation: %s", diagnostic.Message)
	}

	changedKIR := bytes.ReplaceAll(artifact.KIR, []byte(`"int": 41`), []byte(`"int": 42`))
	if bytes.Equal(changedKIR, artifact.KIR) {
		t.Fatal("test KIR mutation did not find the integer literal")
	}
	kirOffset := bytes.LastIndex(data, artifact.KIR)
	if kirOffset < 32 {
		t.Fatal("embedded KIR trailer was not found")
	}
	badHash := append([]byte(nil), data...)
	badHash[kirOffset-1] ^= 1
	if _, diagnostic := DecodeArtifact(badHash, engine.Limits); diagnostic == nil || !strings.Contains(diagnostic.Message, "invalid KIR hash") {
		t.Fatalf("artifact accepted a damaged KIR hash: %#v", diagnostic)
	}
	if _, diagnostic := DecodeArtifact(data[:len(data)-1], engine.Limits); diagnostic == nil || !strings.Contains(diagnostic.Message, "invalid KIR length") {
		t.Fatalf("artifact accepted truncated KIR: %#v", diagnostic)
	}
	copy(data[kirOffset:], changedKIR)
	changedHash := sha256.Sum256(changedKIR)
	copy(data[kirOffset-32:kirOffset], changedHash[:])
	if err := os.WriteFile(artifactPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, diagnostic := engine.CheckPath(artifactPath); diagnostic == nil || !strings.Contains(diagnostic.Message, "typed KIR does not match embedded sources") {
		t.Fatalf("artifact accepted KIR with different semantics: %#v", diagnostic)
	}
}

func TestKexeKIRRoundTripKeepsImportedSources(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.kry")
	modulePath := filepath.Join(dir, "math.kry")
	artifactPath := filepath.Join(dir, "main.kexe")
	if err := os.WriteFile(sourcePath, []byte("import \"math\"\nprintln(add(2, 3))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modulePath, []byte("pub fn add(left: Int, right: Int) -> Int { return left + right }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := NewEngine()
	if diagnostic := engine.BuildPath(sourcePath, artifactPath); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, diagnostic := DecodeArtifact(artifactBytes, engine.Limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if len(artifact.Entries) != 2 || len(artifact.KIR) == 0 {
		t.Fatalf("artifact did not retain source module and KIR: entries=%d KIR=%d", len(artifact.Entries), len(artifact.KIR))
	}
	if _, _, diagnostic := engine.CheckPath(artifactPath); diagnostic != nil {
		t.Fatalf("artifact with imported source failed KIR validation: %s", diagnostic.Message)
	}
}
