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

func TestNewProjectInstallsAndRunsEveryOfficialPackage(t *testing.T) {
	packages := []struct {
		name, version, source, want string
	}{
		{"async", "1.2.1", `import "async"
fn main() -> Nil {
    yield_now()
    println("async-ok")
}
`, "async-ok\n"},
		{"crypto", "1.3.1", `import "crypto"
fn main() -> Nil {
    println(sha256_hex(string_to_bytes("abc")))
    println(to_base64url(string_to_bytes("hello world")))
}
`, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\naGVsbG8gd29ybGQ\n"},
		{"discord", "2.2.1", `import "discord"
fn main() -> Nil {
    let response: Result[String, String] = interaction_message_response_json("hello")
    match response {
        ok(value) => { println(value) }
        err(problem) => { println(problem) }
    }
}
`, "{\"type\":4,\"data\":{\"content\":\"hello\",\"allowed_mentions\":{\"parse\":[]}}}\n"},
		{"fs", "1.2.1", `import "fs"
fn main() -> Nil {
    let written: Result[Nil, String] = write_text("probe.txt", "fs-ok")
    match written {
        ok(_) => {
            let content: Result[String, String] = read_text("probe.txt")
            match content {
                ok(value) => { println(value) }
                err(problem) => { println(problem) }
            }
        }
        err(problem) => { println(problem) }
    }
}
`, "fs-ok\n"},
		{"json", "1.1.1", `import "json"
fn main() -> Nil {
    let parsed: Result[Json, String] = parse("{\"ok\":true}")
    match parsed {
        ok(value) => { println(stringify(value)) }
        err(problem) => { println(problem) }
    }
}
`, "{\"ok\":true}\n"},
	}

	repoRoot := filepath.Join("..", "..")
	indexes := make(map[string][]byte, len(packages))
	archives := make(map[string][]byte, len(packages))
	for _, pkg := range packages {
		indexPath := filepath.Join(repoRoot, "registry", "index", pkg.name+".json")
		indexData, err := os.ReadFile(indexPath)
		if err != nil {
			t.Fatal(err)
		}
		var index kry.RegistryIndex
		if err := json.Unmarshal(indexData, &index); err != nil {
			t.Fatal(err)
		}
		var release *kry.RegistryVersion
		for i := range index.Versions {
			if index.Versions[i].Version == pkg.version {
				release = &index.Versions[i]
				break
			}
		}
		if release == nil {
			t.Fatalf("%s registry has no %s release", pkg.name, pkg.version)
		}
		archive, err := os.ReadFile(filepath.Join(repoRoot, "registry", "packages", pkg.name+"-"+pkg.version+".tar.gz"))
		if err != nil {
			t.Fatal(err)
		}
		indexes["/index/"+pkg.name+".json"] = indexData
		archives[release.URL] = archive
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if data, ok := indexes[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		if data, ok := archives[r.URL.Path]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
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

	packageNames := make([]string, len(packages))
	for i, pkg := range packages {
		packageNames[i] = pkg.name
	}
	if status := run(append([]string{"install"}, packageNames...)); status != 0 {
		t.Fatalf("kry install %v returned status %d", packageNames, status)
	}
	lockData, err := os.ReadFile(filepath.Join(project, "kry.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lock kry.LockFile
	if err := json.Unmarshal(lockData, &lock); err != nil {
		t.Fatal(err)
	}
	installed := make(map[string]string, len(lock.Packages))
	for _, pkg := range lock.Packages {
		installed[pkg.Name] = pkg.Version
	}
	for _, pkg := range packages {
		if installed[pkg.name] != pkg.version {
			t.Fatalf("kry.lock resolved %s@%q, want %s", pkg.name, installed[pkg.name], pkg.version)
		}
	}

	for _, pkg := range packages {
		if err := os.WriteFile("main.kry", []byte(pkg.source), 0o644); err != nil {
			t.Fatal(err)
		}
		output, status := captureKryStdout(t, func() int { return run([]string{"run", "main.kry"}) })
		if status != 0 || output != pkg.want {
			t.Errorf("%s package run = (%d, %q), want (0, %q)", pkg.name, status, output, pkg.want)
		}
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
