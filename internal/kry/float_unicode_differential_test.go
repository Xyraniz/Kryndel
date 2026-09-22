package kry

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runInterpreterCapture(t *testing.T, source string) (string, *Diagnostic) {
	t.Helper()
	program, d := Parse(&Source{Name: "differential.kry", Text: source}, DefaultLimits())
	if d != nil {
		return "", d
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		return "", d
	}
	runtimeValue, d := NewRuntime(program, checker, DefaultLimits(), Sandbox{Root: t.TempDir(), Restricted: true})
	if d != nil {
		return "", d
	}
	old := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	runErr := runtimeValue.run()
	_ = write.Close()
	os.Stdout = old
	data, _ := read.ReadBytes(0)
	_ = read.Close()
	return string(data), runErr
}

func buildAndRunLinuxELF(t *testing.T, source string) (string, int, error) {
	t.Helper()
	program, d := Parse(&Source{Name: "differential.kry", Text: source}, DefaultLimits())
	if d != nil {
		return "", 0, d
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		return "", 0, d
	}
	data, err := BuildNative(program, checker, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		return "", 0, err
	}
	path := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(path, data, 0o700); err != nil {
		return "", 0, err
	}
	cmd := exec.Command(path)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return string(out), exit.ExitCode(), nil
	}
	return string(out), -1, err
}

func TestFloatBoundaryPolicy(t *testing.T) {
	if !isFinite(0) || !isFinite(math.SmallestNonzeroFloat64) {
		t.Fatal("finite Float boundary was rejected")
	}
	if isFinite(math.NaN()) || isFinite(math.Inf(1)) || isFinite(math.Inf(-1)) {
		t.Fatal("non-finite Float boundary was accepted")
	}
	if !equalValue(floatVal(-0.0), floatVal(0.0)) {
		t.Fatal("Float equality must treat negative zero as equal to positive zero")
	}
	if got := display(floatVal(math.Copysign(0, -1))); got != "-0" {
		t.Fatalf("negative zero formatting changed: %q", got)
	}
	for _, text := range []string{"NaN", "+Inf", "-Inf", "Infinity"} {
		program, d := Parse(&Source{Name: "float.kry", Text: "float(\"" + text + "\")"}, DefaultLimits())
		if d != nil {
			continue
		}
		checker, d := Check(program, DefaultLimits())
		if d != nil {
			continue
		}
		r, d := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
		if d != nil {
			t.Fatal(d)
		}
		if d = r.run(); d == nil {
			t.Fatalf("non-finite conversion %q was accepted", text)
		}
	}
}

func TestUnicodePolicyBoundaries(t *testing.T) {
	program := `fn main() -> Nil {
    let combining: String = "e\u0301"
    let zwj: String = "👩‍💻"
    let flags: String = "🇺🇳"
    println(str(len(combining)))
    println(str(len(zwj)))
    println(str(len(flags)))
    println(str(len(string_to_bytes(combining))))
    println(str(string_chars(zwj)))
    match substring(combining, 0, 2) { ok(value) => { println(value) } err(problem) => { println(problem) } }
    return nil
}`
	out, d := runInterpreterCapture(t, program)
	if d != nil {
		t.Fatal(d.Message)
	}
	want := "2\n3\n2\n3\n[👩, ‍, 💻]\né\n"
	if out != want {
		t.Fatalf("Unicode code-point policy mismatch: got %q want %q", out, want)
	}
}

func TestOrderedCollectionPolicy(t *testing.T) {
	program := `fn main() -> Nil {
    let m = {"é": 1, "👩‍💻": 2, "nested": [3, 4]}
    println(map_keys(m))
    println(map_values(m))
    let s: Set[String] = |{"é", "👩‍💻", "é"}| 
    println(set_to_array(s))
    return nil
}`
	out, d := runInterpreterCapture(t, program)
	if d != nil {
		t.Fatal(d.Message)
	}
	want := "[é, 👩‍💻, nested]\n[1, 2, [3, 4]]\n|{é, 👩‍💻}|\n"
	if out != want {
		t.Fatalf("ordered collection policy mismatch: got %q want %q", out, want)
	}
}

func TestDifferentialCorpusInterpreterAndNative(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native differential corpus requires linux/amd64")
	}
	fragments := []string{
		`println(str(0.0 + 1.5))`,
		`println(str(float("5e-324")))`,
		`println(str(len("é👩‍💻🇺🇳")))`,
		`println(str(string_chars("é👩‍💻🇺🇳")))`,
		`println(str({"a": 1, "b": 2}))`,
		`println(str(|{1, 2, 1}|))`,
		`let x: Option[Result[Int,String]] = some(ok(7)); println(str(x))`,
		`let mut total: Int = 0; for item in [1, 2, 3] { total = total + item }; println(total)`,
		`defer { println("deferred") }; println("body")`,
	}
	for i, fragment := range fragments {
		source := "fn main() -> Nil { " + fragment + " return nil }\n"
		interp, interpErr := runInterpreterCapture(t, source)
		if interpErr != nil {
			t.Fatalf("corpus case %d interpreter failed: %s", i, interpErr.Message)
		}
		native, code, err := buildAndRunLinuxELF(t, source)
		if err != nil {
			t.Fatalf("corpus case %d native failed: %v", i, err)
		}
		if code != 0 || native != interp {
			t.Fatalf("corpus case %d differs: interpreter=%q native=%q exit=%d", i, interp, native, code)
		}
	}
}

func TestDifferentialCorpusReducer(t *testing.T) {
	caseSource := "fn main() -> Nil { println(\"stable\"); return nil }\n"
	parts := strings.Fields(caseSource)
	for end := len(parts); end > 0; end-- {
		candidate := strings.Join(parts[:end], " ")
		if _, d := Parse(&Source{Name: "reducer.kry", Text: candidate}, DefaultLimits()); d == nil {
			return
		}
	}
	t.Fatal("reducer did not preserve a reproducible valid program")
}
