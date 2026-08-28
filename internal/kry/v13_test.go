package kry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestV13LanguageSurface(t *testing.T) {
	src := &Source{Name: "feature.kry", Text: `
struct Journal { count: Int }
impl Journal { fn read() -> Int { return self.count } }
fn maybe() -> Result[Int, String] { let x: Result[Int, String] = ok(7); return x? }
let values: Array[Int] = [1, 2, 3]
let mut total: Int = 0
for item in values { total = total + item }
let m: Map[String, Int] = {"answer": total}
let s: Set[Int] = |{1, 2, 2}|
let j: Journal = Journal{count: 9}
println(str(m["answer"]))
println(str(set_len(s)))
println(str(j.read()))
unsafe { println("boundary") }
`}
	p, d := Parse(src, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatalf("check: %s", d.Message)
	}
}

func TestPackageInstallHTTPAndHash(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "source")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "main.kry"), []byte("pub fn answer() -> Int { return 42 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "util.tar.gz")
	if err := PackageArchive(packageDir, archive); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index/util.json":
			_ = json.NewEncoder(w).Encode(RegistryIndex{Name: "util", Versions: []RegistryVersion{{Version: "1.0.0", URL: serverURLPlaceholder, SHA256: hex.EncodeToString(sum[:])}}})
		case "/packages/util.tar.gz":
			_, _ = w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	indexURL := server.URL + "/packages/util.tar.gz"
	// Replace the placeholder through a second request-independent fixture handler.
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index/util.json" {
			_ = json.NewEncoder(w).Encode(RegistryIndex{Name: "util", Versions: []RegistryVersion{{Version: "1.0.0", URL: indexURL, SHA256: hex.EncodeToString(sum[:])}}})
			return
		}
		if r.URL.Path == "/packages/util.tar.gz" {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	})
	project := filepath.Join(root, "project")
	if err := NewProject(project, "consumer"); err != nil {
		t.Fatal(err)
	}
	pm := &PackageManager{Registry: server.URL, Client: server.Client(), CacheDir: filepath.Join(root, "cache")}
	lock, err := pm.Install(project, []string{"util"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 1 || lock.Packages[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	if _, err := os.Stat(filepath.Join(project, "vendor", "util", "main.kry")); err != nil {
		t.Fatalf("vendor package missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "main.kry"), []byte("import \"util\"\nlet answer: Int = answer()\nprintln(str(answer))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	program, diag := LoadProgram(filepath.Join(project, "main.kry"), DefaultLimits(), "")
	if diag != nil {
		t.Fatal(diag)
	}
	if _, diag := Check(program, DefaultLimits()); diag != nil {
		t.Fatal(diag)
	}
}

const serverURLPlaceholder = "http://invalid.local/packages/util.tar.gz"

func TestNativeBackendHeaders(t *testing.T) {
	p, d := Parse(&Source{Name: "main.kry", Text: "println(\"ok\")\n"}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatal(d)
	}
	pe, err := BuildNative(p, NativeTarget{OS: "windows", Arch: "amd64"}, "exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(pe) < 64 || pe[0] != 'M' || pe[1] != 'Z' {
		t.Fatal("missing MZ signature")
	}
	if _, err := InspectNative(pe); err != nil {
		t.Fatal(err)
	}
	elf, err := BuildNative(p, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatal(err)
	}
	if len(elf) < 20 || !bytes.Equal(elf[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		t.Fatal("missing ELF signature")
	}
}

func TestPackageVersionResolution(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		{"1.4.2", "^1.2.0", true}, {"2.0.0", "^1.2.0", false}, {"1.2.9", "~1.2.0", true}, {"1.3.0", "~1.2.0", false}, {"2.0.0", ">=1.5.0", true}, {"1.0.0", "1.0.0", true},
	}
	for _, tc := range cases {
		if got := satisfies(tc.version, tc.constraint); got != tc.want {
			t.Errorf("satisfies(%q,%q)=%v, want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
	if validVersion("1.2.3") != true || validVersion("1.2") || validVersion("1.x.0") {
		t.Fatal("semver validation regression")
	}
}

func TestPackageRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("bad")
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.kry", Mode: 0o644, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := extractPackage(buf.Bytes(), t.TempDir()); err == nil {
		t.Fatal("expected traversal rejection")
	}
}
