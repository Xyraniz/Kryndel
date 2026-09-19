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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestPackageUninstallPrunesDirectAndKeepsTransitive(t *testing.T) {
	dir := t.TempDir()
	if err := NewProject(dir, "uninstall-test"); err != nil {
		t.Fatal(err)
	}
	if err := AddDependency(dir, "foo", "*"); err != nil {
		t.Fatal(err)
	}
	if err := AddDependency(dir, "bar", "*"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foo", "bar", "shared"} {
		if err := os.MkdirAll(filepath.Join(dir, "vendor", name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "vendor", name, "main.kry"), []byte("pub fn value() -> Int { return 1 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockData, _ := json.Marshal(LockFile{Version: 1, Packages: []LockedPackage{
		{Name: "foo", Version: "1.0.0", Dependencies: map[string]string{"shared": "*"}},
		{Name: "bar", Version: "1.0.0", Dependencies: map[string]string{"shared": "*"}},
		{Name: "shared", Version: "1.0.0", Dependencies: map[string]string{}},
	}})
	if err := os.WriteFile(filepath.Join(dir, "kry.lock"), append(lockData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, err := NewPackageManager().Uninstall(dir, []string{"foo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 2 || lock.Packages[0].Name != "bar" || lock.Packages[1].Name != "shared" {
		t.Fatalf("unexpected pruned lock: %#v", lock.Packages)
	}
	if _, err := os.Stat(filepath.Join(dir, "vendor", "foo")); !os.IsNotExist(err) {
		t.Fatalf("direct package still installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vendor", "shared", "main.kry")); err != nil {
		t.Fatalf("transitive package was removed: %v", err)
	}
	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Dependencies["foo"]; ok {
		t.Fatal("uninstalled dependency remains in manifest")
	}
}

const serverURLPlaceholder = "http://invalid.local/packages/util.tar.gz"

func TestNativeBackendHeaders(t *testing.T) {
	p, d := Parse(&Source{Name: "main.kry", Text: "println(\"ok\")\n"}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	pe, err := BuildNative(p, c, NativeTarget{OS: "windows", Arch: "amd64"}, "exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(pe) < 64 || pe[0] != 'M' || pe[1] != 'Z' {
		t.Fatal("missing MZ signature")
	}
	if _, err := InspectNative(pe); err != nil {
		t.Fatal(err)
	}
	elf, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatal(err)
	}
	if len(elf) < 20 || !bytes.Equal(elf[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		t.Fatal("missing ELF signature")
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		path := filepath.Join(t.TempDir(), "program")
		if err := os.WriteFile(path, elf, 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(path).Output()
		if err != nil {
			t.Fatalf("AOT executable failed: %v", err)
		}
		if string(out) != "ok\n" {
			t.Fatalf("unexpected AOT output %q", out)
		}
	}
}

// TestNativeBackendFunctions verifies that the C-based AOT backend now compiles
// real functions, recursion, control flow and structs into runnable native code
// (previously such programs were rejected outright).
func TestNativeBackendFunctions(t *testing.T) {
	src := `struct Point { x: Int, y: Int }
fn fib(n: Int) -> Int {
    if n < 2 { return n }
    return fib(n - 1) + fib(n - 2)
}
fn main() -> Nil {
    let p: Point = Point{ x: 3, y: 4 }
    println(str(fib(10)) + ":" + str(p.x + p.y))
    return nil
}
`
	p, d := Parse(&Source{Name: "main.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	elf, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatalf("functions must be supported by the native backend: %v", err)
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		path := filepath.Join(t.TempDir(), "program")
		if err := os.WriteFile(path, elf, 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(path).Output()
		if err != nil {
			t.Fatalf("AOT executable failed: %v", err)
		}
		if string(out) != "55:7\n" {
			t.Fatalf("unexpected AOT output %q", out)
		}
	}
}

// TestNativeBackendBuiltinsParity builds a program that exercises JSON, crypto
// and filesystem builtins natively and checks the output matches the
// interpreter byte-for-byte.
func TestNativeBackendBuiltinsParity(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native parity test requires linux/amd64")
	}
	src := `fn main() -> Nil {
    match json_parse("{\"b\":2,\"a\":[1,2,3]}") {
        ok(doc) => { println(json_stringify(doc)) }
        err(problem) => { println("err") }
    }
    println(hex_encode(crypto_sha256(string_to_bytes("abc"))))
    println(hex_encode(crypto_hmac_sha256(string_to_bytes("key"), string_to_bytes("msg"))))
    println(fs_join_path("/tmp", ["a", "b"]))
    return nil
}
`
	p, d := Parse(&Source{Name: "main.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	elf, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(path, elf, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("AOT executable failed: %v", err)
	}
	want := "{\"a\":[1,2,3],\"b\":2}\n" +
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n" +
		"2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628\n" +
		"/tmp/a/b\n"
	if string(out) != want {
		t.Fatalf("native builtin output mismatch:\n got %q\nwant %q", out, want)
	}
}

// TestNativeBackendRejectsUnsupported ensures unsupported builtins fail loudly
// instead of silently degrading to a stub.
func TestNativeBackendRejectsUnsupported(t *testing.T) {
	src := "fn main() -> Nil { websocket_connect(\"ws://x\"); return nil }\n"
	p, d := Parse(&Source{Name: "unsupported.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	if _, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf"); err == nil {
		t.Fatal("unsupported program was silently emitted as AOT")
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

func TestFreshInstallBootstrapAndPublishValidation(t *testing.T) {
	project := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(project, "main.kry")
	if err := os.WriteFile(mainPath, []byte("println(\"keep me\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProject(project, "fresh"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(mainPath); err != nil || string(got) != "println(\"keep me\")\n" {
		t.Fatalf("EnsureProject overwrote main.kry: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(project, "kry.toml")); err != nil {
		t.Fatalf("manifest was not bootstrapped: %v", err)
	}

	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "kry.toml"), []byte("[package]\nname = \"discord\"\nversion = \"1.0.0\"\nkryndel = \">=1.3.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.kry"), []byte("pub fn ok() -> Nil { return nil }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "discord.tar.gz")
	if err := PackageArchive(source, archive); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePublishedArchive(data, "discord", "1.0.0"); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
	if err := validatePublishedArchive(data, "other", "1.0.0"); err == nil {
		t.Fatal("archive with mismatched coordinates was accepted")
	}
}

func TestStrictLanguageExtensions(t *testing.T) {
	text := `
const Limit: Int = 2
fn choose(value: Int) -> String { return "int:" + str(value) }
fn choose(value: String) -> String { return "text:" + value }
fn identity[T: Copy](value: T) -> T { return value }
struct Vault { private secret: Int, visible: Int }
fn read() -> Int { let vault: Vault = Vault{secret: Limit, visible: 9}; return vault.secret }
fn worker() -> Int { return 7 }
let actor: Actor[Int] = actor_channel()
actor_send(actor, identity(5))
let received: Int = actor_receive_timeout(actor, 100)
let thread: Thread[Int] = thread_spawn("worker")
let answer: Int = await(thread)
assert_eq(received + answer, 12)
assert_eq(read(), Limit)
println(choose(answer))
actor_close(actor)
`
	p, d := Parse(&Source{Name: "extensions.kry", Text: text}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("check: %s", d.Message)
	}
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("run: %s", d.Message)
	}

	badConst := `const value: Int = int("2")`
	p, d = Parse(&Source{Name: "bad-const.kry", Text: badConst}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, "compile-time") {
		t.Fatalf("non-constant initializer accepted: %#v", d)
	}
}

func TestConstantFoldingFastPath(t *testing.T) {
	p, d := Parse(&Source{Name: "fold.kry", Text: "let answer: Int = 2 * (3 + 4)\n"}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatal(d.Message)
	}
	if p.Statements[0].Init.ConstValue == nil || p.Statements[0].Init.ConstValue.Kind != VInt || p.Statements[0].Init.ConstValue.I != 14 {
		t.Fatalf("constant expression was not folded: %#v", p.Statements[0].Init.ConstValue)
	}
}

func TestControlledSharedMemoryAndDeepConst(t *testing.T) {
	text := `
let shared: Shared[Int] = shared_new(0)
fn worker() -> Int {
    shared_write(shared, 7)
    return shared_read(shared)
}
let thread: Thread[Int] = thread_spawn("worker")
let result: Int = await(thread)
assert_eq(result, 7)
assert_eq(shared_read(shared), 7)
let old: Int = shared_swap(shared, 9)
assert_eq(old, 7)
assert_eq(shared_read(shared), 9)
`
	p, d := Parse(&Source{Name: "shared.kry", Text: text}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("check: %s", d.Message)
	}
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("run: %s", d.Message)
	}

	if TypeConstSafe(SharedOf(TInt)) {
		t.Fatal("mutable Shared handle accepted as deeply immutable")
	}
}

func TestStructuredTaskGroupAndMultipleDispatch(t *testing.T) {
	text := `
fn first() -> Int { return 4 }
fn second() -> Int { return 8 }
let group: TaskGroup = task_group()
let a: Thread[Int] = task_spawn(group, "first")
let b: Thread[Int] = task_spawn(group, "second")
let joined: Result[Nil,String] = task_group_wait(group)
assert_eq(is_ok(joined), true)
assert_eq(await(a) + await(b), 12)
`
	p, d := Parse(&Source{Name: "tasks.kry", Text: text}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("check: %s", d.Message)
	}
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("run: %s", d.Message)
	}

	ambiguous := `
fn choose(value: Int) -> String { return "int" }
fn choose[T: Copy](value: T) -> String { return "generic" }
let value: String = choose(1)
`
	p, d = Parse(&Source{Name: "ambiguous.kry", Text: ambiguous}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, "ambiguous") {
		t.Fatalf("ambiguous overload was accepted: %#v", d)
	}

	cancelText := `
fn slow() -> Int { sleep_ms(500); return 1 }
let cancellable: TaskGroup = task_group()
let pending: Thread[Int] = task_spawn(cancellable, "slow")
task_group_cancel(cancellable)
let cancelled: Result[Nil,String] = task_group_wait(cancellable)
assert_eq(is_ok(cancelled), false)
`
	p, d = Parse(&Source{Name: "cancel.kry", Text: cancelText}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	c, d = Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	r, d = NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("cancel run: %s", d.Message)
	}
}

func TestTailCallOptimization(t *testing.T) {
	text := `
fn count(n: Int, acc: Int) -> Int {
    if n == 0 { return acc }
    return count(n - 1, acc + 1)
}
let result: Int = count(10000, 0)
assert_eq(result, 10000)
`
	p, d := Parse(&Source{Name: "tco.kry", Text: text}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("check: %s", d.Message)
	}
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("tco run: %s", d.Message)
	}
}
