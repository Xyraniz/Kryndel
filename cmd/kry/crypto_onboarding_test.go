package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Xyraniz/Kryndel/internal/kry"
)

func TestNewInstallImportAndRunCrypto(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	indexData, err := os.ReadFile(filepath.Join(repoRoot, "registry", "index", "crypto.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index kry.RegistryIndex
	if err := json.Unmarshal(indexData, &index); err != nil {
		t.Fatal(err)
	}
	const version = "1.3.1"
	var release *kry.RegistryVersion
	for i := range index.Versions {
		if index.Versions[i].Version == version {
			release = &index.Versions[i]
			break
		}
	}
	if release == nil {
		t.Fatalf("crypto registry has no %s release", version)
	}
	archive, err := os.ReadFile(filepath.Join(repoRoot, "registry", "packages", "crypto-"+version+".tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/crypto.json":
			_, _ = w.Write(indexData)
		case release.URL:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	t.Cleanup(func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KRY_REGISTRY", server.URL)
	t.Setenv("KRY_CACHE", filepath.Join(workspace, "cache"))
	t.Setenv("KRY_OFFLINE", "0")

	if status := run([]string{"new", "my-first-app"}); status != 0 {
		t.Fatalf("kry new returned status %d", status)
	}
	project := filepath.Join(workspace, "my-first-app")
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	defaultOutput, status := captureKryStdout(t, func() int { return run([]string{"run", "main.kry"}) })
	if status != 0 || defaultOutput != "Hello from Kryndel\n" {
		t.Fatalf("fresh project run = (%d, %q), want (0, %q)", status, defaultOutput, "Hello from Kryndel\n")
	}
	if status := run([]string{"install", "crypto"}); status != 0 {
		t.Fatalf("kry install crypto returned status %d", status)
	}
	lockData, err := os.ReadFile(filepath.Join(project, "kry.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lock kry.LockFile
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 1 || lock.Packages[0].Version != version {
		t.Fatalf("kry.lock packages = %#v, want crypto@%s", lock.Packages, version)
	}

	mainSource := "import \"crypto\"\n\nfn main() -> Nil {\n    println(sha256_hex(string_to_bytes(\"abc\")))\n    println(to_base64url(string_to_bytes(\"hello world\")))\n}\n"
	if err := os.WriteFile("main.kry", []byte(mainSource), 0o644); err != nil {
		t.Fatal(err)
	}
	output, status := captureKryStdout(t, func() int { return run([]string{"run", "main.kry"}) })
	if status != 0 {
		t.Fatalf("kry run returned status %d; output %q", status, output)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\naGVsbG8gd29ybGQ\n"
	if output != want {
		t.Fatalf("kry run output = %q, want %q", output, want)
	}
}

func captureKryStdout(t *testing.T, action func() int) (string, int) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	var status int
	func() {
		defer func() {
			os.Stdout = originalStdout
			_ = writer.Close()
		}()
		status = action()
	}()
	output, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(output), status
}
