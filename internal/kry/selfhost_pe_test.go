package kry

import (
	"bytes"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runSelfhostPEBackend(t *testing.T, source string) ([]byte, error) {
	t.Helper()
	program, d := Parse(&Source{Name: "selfhost-pe-stage38.kry", Text: source}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	backendPath := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	backendProgram, d := LoadProgram(backendPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "program.kir")
	outputPath := filepath.Join(dir, "program.exe")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, limits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		return nil, d
	}
	image, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return image, nil
}

func TestSelfhostPEBackendFunctionsControlFlowAndOutput(t *testing.T) {
	source := `
fn weighted(a: Int, b: Int, c: Int, d: Int) -> Int {
    return a + b * 2 + c * 3 + d * 4
}

fn noisy() -> Bool {
    println("should not appear")
    return true
}

fn main() -> Nil {
    let mut n: Int = 0
    while n < 4 {
        n = n + 1
        if n == 2 { continue }
        if n == 4 { break }
        println(weighted(n, 2, 3, 4))
    }
    if false && noisy() { println("bad and") }
    if true || noisy() { println("short circuit ok") }
    println(weighted(1, 1, 1, 1))
    println(u64(17) / u64(5))
    println(u64(17) % u64(5))
    println(-17 / 5)
    println(u64(-1))
    println("selfhost pe ok")
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend failed: %v", d)
	}
	if len(image) < 0x100 || string(image[:2]) != "MZ" || string(image[0x80:0x84]) != "PE\x00\x00" {
		t.Fatalf("selfhost backend did not emit a PE32+ image (size %d)", len(image))
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "selfhost-pe.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated PE failed: %v; output: %s", err, output)
	}
	program, diagnostic := Parse(&Source{Name: "selfhost-pe-oracle.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	oracle, err := BuildDirectPE(program, checker, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatalf("direct Go PE oracle rejected the selfhost fixture: %v", err)
	}
	oraclePath := filepath.Join(t.TempDir(), "direct-pe.exe")
	if err := os.WriteFile(oraclePath, oracle, 0o700); err != nil {
		t.Fatal(err)
	}
	oracleOutput, err := exec.Command(oraclePath).CombinedOutput()
	if err != nil {
		t.Fatalf("direct Go PE oracle failed: %v; output: %s", err, oracleOutput)
	}
	if !bytes.Equal(output, oracleOutput) {
		t.Fatalf("selfhost PE disagrees with direct Go PE: got %q, oracle %q", output, oracleOutput)
	}
	want := "30\n32\nshort circuit ok\n10\n3\n2\n-3\n18446744073709551615\nselfhost pe ok\n"
	if string(output) != want {
		t.Fatalf("unexpected selfhost-generated PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendRejectsOutsideSubset(t *testing.T) {
	image, d := runSelfhostPEBackend(t, `
fn main() -> Nil {
    let values: Array[Int] = [1, 2]
    println(len(values))
}
`)
	if d == nil {
		t.Fatalf("unsupported array program unexpectedly emitted %d bytes", len(image))
	}
	if !strings.Contains(d.Error(), "PE backend: binding 'values' has an unsupported type") {
		t.Fatalf("unexpected unsupported-feature diagnostic: %v", d)
	}
	image, d = runSelfhostPEBackend(t, `fn main() -> Nil { println(str("already a String")) }`)
	if d == nil {
		t.Fatalf("PE backend unexpectedly accepted a non-integer str conversion and emitted %d bytes", len(image))
	}
	if !strings.Contains(d.Error(), "PE backend: str supports Int, Bool, and UInt values") {
		t.Fatalf("unexpected str-subset diagnostic: %v", d)
	}
}

func TestSelfhostPEBackendTrapsOutOfRangeShift(t *testing.T) {
	source := `fn main() -> Nil { println(u64(1) << 64) }`
	image, err := runSelfhostPEBackend(t, source)
	if err != nil {
		t.Fatalf("selfhost PE backend failed: %v", err)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "bad-shift.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err == nil {
		t.Fatalf("out-of-range shift unexpectedly succeeded with output %q", output)
	}
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("out-of-range shift exited with %v and output %q; want ExitProcess(1)", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("out-of-range shift wrote output before trapping: %q", output)
	}
}

func TestSelfhostSourceCompilerBuildsWindowsPE(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compilerPath := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	compilerProgram, d := LoadProgram(compilerPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "app.kry")
	modePath := filepath.Join(dir, "modes.kry")
	outputPath := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(modePath, []byte(`pub enum Mode { Idle, Active }

pub fn score(mode: Mode, first: Int, second: Int, third: Int, fourth: Int, fifth: Int) -> Int {
    if mode == Mode::Active { return fifth }
    return 0
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `
import "modes"

fn twice(value: Int) -> Int {
    return value * 2
}

fn negative_value() -> Int {
    return -42
}

fn zero_value() -> Int {
    return 0
}

fn truth_value() -> Bool {
    return true
}

fn main() -> Nil {
    let mut index: Int = 0
    while index < 3 {
        println(twice(index + 1))
        index = index + 1
    }
    if index == 3 ||
        index == 4 { println("continued condition") }
	println(int(u64(17)))
    println(score(Mode::Active, 1, 2, 3, 4, 42))
    println(str(-42))
    println(str(negative_value()))
    println(str(zero_value()))
    println(str(truth_value()))
    println(str(u64(17)))
    println("compiled from Kryndel source")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, limits, Sandbox{}, []string{sourcePath, outputPath, "windows-amd64"})
	if d != nil {
		t.Fatal(d.Message)
	}
	if err := r.run(); err != nil {
		t.Fatalf("selfhost source compiler failed: %v", err)
	}
	image, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) < 0x100 || string(image[:2]) != "MZ" || string(image[0x80:0x84]) != "PE\x00\x00" {
		t.Fatalf("selfhost source compiler did not emit a PE32+ image (size %d)", len(image))
	}
	peImage, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected source-compiled image: %v", err)
	}
	imports, err := peImage.ImportedSymbols()
	peImage.Close()
	if err != nil {
		t.Fatalf("could not read source-compiled PE imports: %v", err)
	}
	processHeapImports, heapAllocImports := 0, 0
	for _, symbol := range imports {
		if strings.HasPrefix(symbol, "GetProcessHeap:") {
			processHeapImports++
		}
		if strings.HasPrefix(symbol, "HeapAlloc:") {
			heapAllocImports++
		}
	}
	if processHeapImports != 1 || heapAllocImports != 1 {
		t.Fatalf("expected one GetProcessHeap and one HeapAlloc PE import, got %d and %d (%v)", processHeapImports, heapAllocImports, imports)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(dir, "app.exe")
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-source-generated PE failed: %v; output: %s", err, output)
	}
	want := "2\n4\n6\ncontinued condition\n17\n42\n-42\n-42\n0\ntrue\n17\ncompiled from Kryndel source\n"
	if string(output) != want {
		t.Fatalf("unexpected source-compiled PE output %q", output)
	}
}
