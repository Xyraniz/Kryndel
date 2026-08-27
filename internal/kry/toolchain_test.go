package kry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testProgram(t *testing.T, text string) (*Program, *Checker) {
	t.Helper()
	p, d := Parse(&Source{Name: "<test>", Text: text}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse failed: %s", d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("check failed: %s", d.Message)
	}
	return p, c
}

func TestBuiltinRegistryIsAuthoritative(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range builtinList {
		if b.Name == "" || b.Signature == "" || b.Version == "" || b.ID == "" {
			t.Fatalf("incomplete builtin metadata: %#v", b)
		}
		if seen[b.Name] {
			t.Fatalf("duplicate builtin %q", b.Name)
		}
		seen[b.Name] = true
	}
	if len(Builtins()) != len(builtinList) {
		t.Fatalf("registry map lost entries")
	}
}

func TestRecursiveCopyTerminatesSafely(t *testing.T) {
	node := &Type{Kind: TyStruct, Name: "Node"}
	node.Struct = &StructDecl{Name: "Node"}
	node.Struct.Fields = []FieldDecl{{Name: "next", Type: Opt(node)}}
	start := time.Now()
	if TypeCopyable(node) {
		t.Fatalf("recursive structural type must not be Copy")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("recursive Copy analysis exceeded bound")
	}
}

func TestChannelSendRejectsNonCopyValues(t *testing.T) {
	_, d := func() (*Checker, *Diagnostic) {
		p, parseD := Parse(&Source{Name: "copy.kry", Text: "struct Holder { channel: Channel[Int] }\nlet target: Channel[Holder] = thread_channel()\nlet inner: Channel[Int] = thread_channel()\nlet holder: Holder = Holder{ channel: inner }\nthread_try_send(target, holder)\n"}, DefaultLimits())
		if parseD != nil {
			return nil, parseD
		}
		return Check(p, DefaultLimits())
	}()
	if d == nil || !strings.Contains(d.Message, "Copy") {
		t.Fatalf("expected static Copy rejection, got %#v", d)
	}
}

func TestExhaustiveMatchReturns(t *testing.T) {
	text := "enum Flag { On, Off }\nfn value(flag: Flag) -> Int { match flag { Flag::On => { return 1 } Flag::Off => { return 2 } } }\nprintln(value(Flag::On))\n"
	p, c := testProgram(t, text)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatal(d.Message)
	}
}

func TestArtifactDeterminismAndCorruption(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.kry")
	module := filepath.Join(dir, "mod.kry")
	if err := os.WriteFile(root, []byte("import \"mod\"\nprintln(add(2, 3))\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(module, []byte("pub fn add(a: Int, b: Int) -> Int { return a + b }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, d := LoadProgram(root, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	a, d := BuildArtifact(p, root)
	if d != nil {
		t.Fatal(d.Message)
	}
	b, d := BuildArtifact(p, root)
	if d != nil {
		t.Fatal(d.Message)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("artifact bytes are not deterministic")
	}
	decoded, d := DecodeArtifact(a, DefaultLimits())
	if d != nil || len(decoded.Entries) != 2 || decoded.Entries[0].Path != "<root>" {
		t.Fatalf("invalid decoded artifact: %#v", d)
	}
	if _, d = DecodeArtifact(append(append([]byte{}, a...), 'x'), DefaultLimits()); d == nil || !strings.Contains(d.Message, "trailing") {
		t.Fatalf("trailing bytes were accepted: %#v", d)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(module); err != nil {
		t.Fatal(err)
	}
	replayed, d := ProgramFromArtifact(decoded, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	replayChecker, d := Check(replayed, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	r, d := NewRuntime(replayed, replayChecker, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatal(d.Message)
	}
}

func TestFormatterIdempotent(t *testing.T) {
	src := &Source{Name: "format.kry", Text: "let x: Int = 1   \nif true { println(x) }\n"}
	first, d := FormatSource(src, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	second, d := FormatSource(&Source{Name: "format.kry", Text: first}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if first != second {
		t.Fatalf("formatter is not idempotent:\n%s\n---\n%s", first, second)
	}
}

func TestSandboxRejectsTraversalAndSymlink(t *testing.T) {
	root := t.TempDir()
	sb := Sandbox{Root: root, Restricted: true}
	if err := sb.Write("../escape", []byte("bad")); err == nil {
		t.Fatal("parent traversal was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err == nil {
		if err := sb.Write("link", []byte("bad")); err == nil {
			t.Fatal("final symlink write was accepted")
		}
		got, readErr := os.ReadFile(outside)
		if readErr != nil || string(got) != "safe" {
			t.Fatal("outside target was modified")
		}
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	}
}

func TestWorkerCPUShutdownIsBounded(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxWallTimeMS = 100
	lim.ShutdownMS = 200
	lim.MaxInstructions = 100000
	text := "fn worker() -> Nil { while true { } }\nlet t: Thread[Nil] = thread_spawn(\"worker\")\nthread_join_timeout(t, 5)\n"
	p, d := Parse(&Source{Name: "worker.kry", Text: text}, lim)
	if d != nil {
		t.Fatal(d.Message)
	}
	c, d := Check(p, lim)
	if d != nil {
		t.Fatal(d.Message)
	}
	r, d := NewRuntime(p, c, lim, Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	start := time.Now()
	d = r.run()
	elapsed := time.Since(start)
	if d != nil {
		t.Fatal(d.Message)
	}
	if elapsed > time.Second {
		t.Fatalf("worker shutdown exceeded bound: %s", elapsed)
	}
}

func TestModuleVisibilityAndCycles(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "main.kry")
	mod := filepath.Join(dir, "mod.kry")
	if err := os.WriteFile(root, []byte("import \"mod\"\nsecret()\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mod, []byte("fn secret() -> Int { return 1 }\npub fn add(a: Int, b: Int) -> Int { return a + b }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, d := LoadProgram(root, DefaultLimits(), "")
	if d == nil {
		_, d = Check(p, DefaultLimits())
	}
	if d == nil || !strings.Contains(d.Message, "unknown function") {
		t.Fatalf("private function leaked: %#v", d)
	}
	if err := os.WriteFile(root, []byte("import \"mod\"\nprintln(add(1, 2))\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, d = LoadProgram(root, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatal(d.Message)
	}
	if err := os.WriteFile(mod, []byte("struct Hidden { x: Int }\npub fn leak() -> Hidden { return Hidden{x: 1} }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, d = LoadProgram(root, DefaultLimits(), "")
	if d == nil {
		_, d = Check(p, DefaultLimits())
	}
	if d == nil || !strings.Contains(d.Message, "private") {
		t.Fatalf("private type exposed by public API: %#v", d)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.kry"), []byte("import \"b\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.kry"), []byte("import \"a\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, d := LoadProgram(filepath.Join(dir, "a.kry"), DefaultLimits(), ""); d == nil || !strings.Contains(d.Message, "cycle") {
		t.Fatalf("module cycle was accepted: %#v", d)
	}
}

func TestLimitsAndJSONDiagnostics(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxSourceBytes = 4
	if _, d := Lex(&Source{Name: "large.kry", Text: "12345"}, lim); d == nil || d.Category != CatResource {
		t.Fatalf("source limit was not enforced: %#v", d)
	}
	p, d := Parse(&Source{Name: "bad.kry", Text: "let x: Int = true\n"}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	_, d = Check(p, DefaultLimits())
	if d == nil {
		t.Fatal("type error was accepted")
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(d.Format(true)), &object); err != nil {
		t.Fatalf("invalid JSON diagnostic: %v", err)
	}
	if object["code"] == nil || object["category"] == nil || object["message"] == nil {
		t.Fatalf("incomplete diagnostic JSON: %#v", object)
	}
}

func TestFuzzSmoke(t *testing.T) {
	seeds := []string{"", "{", "let x: Int =", "\\x00", "/* nested /* comment */", "fn f(\\u03bb: Int) -> Int { return \\u03bb }", strings.Repeat("(", 1000), "\\xff"}
	for i, seed := range seeds {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("fuzz input panicked: %v", recovered)
				}
			}()
			lim := DefaultLimits()
			lim.MaxSourceBytes = 1 << 20
			p, d := Parse(&Source{Name: "fuzz.kry", Text: seed}, lim)
			if d == nil {
				_, _ = Check(p, lim)
			}
			for j := 0; j < len(seed); j++ {
				_, _ = DecodeArtifact([]byte(seed[:j]), lim)
			}
		})
	}
}

func TestDocumentation(t *testing.T) {
	paths := []string{"README.md", "CONTRIBUTING.md", "CHANGELOG.md", "docs/native.md", "docs/architecture.md", "docs/design.md", "docs/release.md", "docs/testing.md", "docs/concurrency.md", "docs/memory.md", "docs/modules.md", "docs/system.md"}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "native/kry.c") || strings.Contains(text, "KRYNATIVE2") || strings.Contains(text, "C11 compiler") {
			t.Fatalf("stale implementation reference in %s", path)
		}
	}
}
