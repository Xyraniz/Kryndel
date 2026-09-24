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

func TestSelfhostPEBackendIntArrayLocalsLiteralLenAndIndex(t *testing.T) {
	source := `
fn inspect(seed: Int) -> Int {
    let values: Array[Int] = [seed, seed + 2]
    return len(values) + values[1]
}

fn main() -> Nil {
    let values: Array[Int] = [17, -42, 5]
    let alias: Array[Int] = values
    let empty: Array[Int] = []
    println(len(alias))
    println(alias[0])
    println(alias[1])
    println(alias[2])
    println(len(empty))
    println(inspect(5))
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected the supported Array[Int] subset: %v", d)
	}
	if len(image) < 0x100 || string(image[:2]) != "MZ" || string(image[0x80:0x84]) != "PE\x00\x00" {
		t.Fatalf("selfhost backend did not emit a PE32+ image (size %d)", len(image))
	}
	peImage, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the Array[Int] image: %v", err)
	}
	imports, err := peImage.ImportedSymbols()
	peImage.Close()
	if err != nil {
		t.Fatalf("could not read Array[Int] PE imports: %v", err)
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
	executable := filepath.Join(t.TempDir(), "int-arrays.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated Array[Int] PE failed: %v; output: %s", err, output)
	}
	want := "3\n17\n-42\n5\n0\n9\n"
	if string(output) != want {
		t.Fatalf("unexpected Array[Int] PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendScalarArrayElements(t *testing.T) {
	source := `
enum Mode { Idle, Active }
fn main() -> Nil {
    let flags: Array[Bool] = [false, true]
    let flag_alias: Array[Bool] = flags
    println(len(flag_alias))
    println(flag_alias[0])
    println(flag_alias[1])

    let labels: Array[String] = ["first", "second"]
    println(labels[1])

    let ids: Array[UInt64] = [u64(42)]
    println(str(int(ids[0])))

    let modes: Array[Mode] = [Mode::Idle, Mode::Active]
    if modes[1] == Mode::Active {
        println("enum-array-ok")
    }

    let empty: Array[String] = []
    println(len(empty))
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected scalar-element arrays: %v", d)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "scalar-arrays.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated scalar-array PE failed: %v; output: %s", err, output)
	}
	want := "2\nfalse\ntrue\nsecond\n42\nenum-array-ok\n0\n"
	if string(output) != want {
		t.Fatalf("unexpected scalar-array PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendIntArrayBoundsChecks(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "negative index",
			source: `fn main() -> Nil {
    let values: Array[Int] = [10, 20]
    let index: Int = -1
    println(values[index])
}`,
		},
		{
			name: "index at length",
			source: `fn main() -> Nil {
    let values: Array[Int] = [10, 20]
    let index: Int = len(values)
    println(values[index])
}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			image, err := runSelfhostPEBackend(t, tc.source)
			if err != nil {
				t.Fatalf("selfhost PE backend failed to compile bounds-check fixture: %v", err)
			}
			if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
				t.Skip("native PE execution requires Windows amd64")
			}
			executable := filepath.Join(t.TempDir(), "bad-array-index.exe")
			if err := os.WriteFile(executable, image, 0o700); err != nil {
				t.Fatal(err)
			}
			output, err := exec.Command(executable).CombinedOutput()
			if err == nil {
				t.Fatalf("out-of-bounds Array[Int] access unexpectedly succeeded with output %q", output)
			}
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 {
				t.Fatalf("out-of-bounds Array[Int] access exited with %v and output %q; want ExitProcess(1)", err, output)
			}
			if len(output) != 0 {
				t.Fatalf("out-of-bounds Array[Int] access wrote output before trapping: %q", output)
			}
		})
	}
}

func TestSelfhostPEBackendStringIntMapLiteralAndLookup(t *testing.T) {
	source := `
fn main() -> Nil {
    let scores: Map[String, Int] = {"alpha": 17, "beta": -42, "gamma": 5}
    let alias: Map[String, Int] = scores
    println(map_contains_key(alias, "beta"))
    println(map_contains_key(alias, "missing"))
    println(unwrap_or(map_get(alias, "alpha"), -1))
    println(unwrap_or(map_get(alias, "missing"), -1))
    println(unwrap_or(map_get({"inline": 23}, "inline"), -2))
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected supported Map[String,Int] lookup: %v", d)
	}
	peImage, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the Map[String,Int] image: %v", err)
	}
	imports, err := peImage.ImportedSymbols()
	peImage.Close()
	if err != nil {
		t.Fatalf("could not read Map[String,Int] PE imports: %v", err)
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
	executable := filepath.Join(t.TempDir(), "string-int-map.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated Map[String,Int] PE failed: %v; output: %s", err, output)
	}
	want := "true\nfalse\n17\n-1\n23\n"
	if string(output) != want {
		t.Fatalf("unexpected Map[String,Int] PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendStringMapScalarValues(t *testing.T) {
	source := `
enum Mode { Idle, Active }
fn main() -> Nil {
    let flags: Map[String, Bool] = {"enabled": true, "disabled": false}
    let found: Bool = map_contains_key(flags, "enabled")
    println(found)
    println(unwrap_or(map_get(flags, "disabled"), true))

    let labels: Map[String, String] = {"name": "Kryndel"}
    println(unwrap_or(map_get(labels, "name"), "missing"))
    println(unwrap_or(map_get(labels, "absent"), "fallback"))

    let ids: Map[String, UInt64] = {"answer": u64(42)}
    println(str(int(unwrap_or(map_get(ids, "answer"), u64(0)))))

    let modes: Map[String, Mode] = {"current": Mode::Active}
    if unwrap_or(map_get(modes, "current"), Mode::Idle) == Mode::Active {
        println("enum-map-ok")
    }

    let empty: Map[String, Bool] = {}
    println(map_contains_key(empty, "missing"))
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected scalar-valued String maps: %v", d)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "string-map-values.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated String map PE failed: %v; output: %s", err, output)
	}
	want := "true\nfalse\nKryndel\nfallback\n42\nenum-map-ok\nfalse\n"
	if string(output) != want {
		t.Fatalf("unexpected String map PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendRejectsUnsupportedCollectionShapes(t *testing.T) {
	cases := []struct {
		name, source, diagnostic string
	}{
		{
			name: "map with unsupported key type",
			source: `fn main() -> Nil {
    let values: Map[Int, Int] = {1: 2}
}`,
			diagnostic: "PE backend: maps require String keys and scalar or fieldless-enum values",
		},
		{
			name:       "map builtin",
			source:     `fn main() -> Nil { map_remove({"key": 1}, "key") }`,
			diagnostic: "PE backend: map builtins are not supported",
		},
		{
			name: "map insertion",
			source: `fn main() -> Nil {
    let values: Map[String, Int] = {"key": 1}
    map_insert(values, "new", 2)
}`,
			diagnostic: "PE backend: map builtins are not supported",
		},
		{
			name: "map reassignment",
			source: `fn main() -> Nil {
    let mut values: Map[String, Int] = {"key": 1}
    values = {"new": 2}
}`,
			diagnostic: "PE backend: map reassignment is not supported",
		},
		{
			name: "map values cannot be collections",
			source: `fn main() -> Nil {
    let values: Map[String, Array[Int]] = {"key": [1]}
}`,
			diagnostic: "PE backend: maps require String keys and scalar or fieldless-enum values",
		},
		{
			name: "map keys must be literals",
			source: `fn main() -> Nil {
    let key: String = "key"
    let values: Map[String, Int] = {key: 1}
}`,
			diagnostic: "PE backend: map literal keys must be String literals",
		},
		{
			name: "map function parameter",
			source: `fn lookup(values: Map[String, Int]) -> Bool { return map_contains_key(values, "key") }
fn main() -> Nil {}`,
			diagnostic: "PE backend: map function parameters are not supported",
		},
		{
			name: "map function return",
			source: `fn make() -> Map[String, Int] { return {"key": 1} }
fn main() -> Nil {}`,
			diagnostic: "PE backend: map function returns are not supported",
		},
		{
			name: "nested array elements",
			source: `fn main() -> Nil {
	let values: Array[Array[Int]] = [[1]]
	println(len(values))
}`,
			diagnostic: "PE backend: arrays require scalar or fieldless-enum element types",
		},
		{
			name: "array push",
			source: `fn main() -> Nil {
    let values: Array[Int] = [1]
    let pushed: Array[Int] = array_push(values, 2)
    println(len(pushed))
}`,
			diagnostic: "PE backend: array builtins are not supported",
		},
		{
			name: "array concatenation",
			source: `fn main() -> Nil {
    let values: Array[Int] = [1] + [2]
    println(len(values))
}`,
			diagnostic: "PE backend: array binary operations are not supported",
		},
		{
			name: "array reassignment",
			source: `fn main() -> Nil {
    let mut values: Array[Int] = [1]
    values = [2]
}`,
			diagnostic: "PE backend: array mutation and reassignment are not supported",
		},
		{
			name: "array set mutation builtin",
			source: `fn main() -> Nil {
	let values: Array[Int] = [1]
	array_set(values, 0, 2)
}`,
			diagnostic: "PE backend: array builtins are not supported",
		},
		{
			name:       "temporary literal indexing",
			source:     `fn main() -> Nil { println([1, 2][0]) }`,
			diagnostic: "PE backend: indexing is supported only through a local supported array variable",
		},
		{
			name: "array function parameter",
			source: `fn read(values: Array[Int]) -> Int { return len(values) }
fn main() -> Nil { println(read([1])) }`,
			diagnostic: "PE backend: array function parameters are not supported",
		},
		{
			name: "array function return",
			source: `fn make() -> Array[Int] { return [1] }
fn main() -> Nil { println(len(make())) }`,
			diagnostic: "PE backend: array function returns are not supported",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			image, d := runSelfhostPEBackend(t, tc.source)
			if d == nil {
				t.Fatalf("unsupported collection shape unexpectedly emitted %d bytes", len(image))
			}
			if !strings.Contains(d.Error(), tc.diagnostic) {
				t.Fatalf("unexpected unsupported-feature diagnostic: got %v, want %q", d, tc.diagnostic)
			}
		})
	}
	program, d := Parse(&Source{Name: "ambiguous-empty-array.kry", Text: `fn main() -> Nil { let values = [] }`}, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(program, DefaultLimits()); d == nil || !strings.Contains(d.Message, "empty array requires Array[T] context") {
		t.Fatalf("untyped empty array should be rejected by the frontend, got %v", d)
	}
}

func TestSelfhostPEBackendRejectsOutsideSubset(t *testing.T) {
	image, d := runSelfhostPEBackend(t, `fn main() -> Nil { println(str("already a String")) }`)
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
    let values: Array[Int] = [8, 4]
    println(len(values))
    println(values[1])
    let scores: Map[String, Int] = {"source": 17, "compiled": -8}
    let score_alias: Map[String, Int] = scores
    println(map_contains_key(score_alias, "source"))
    println(unwrap_or(map_get(score_alias, "compiled"), 0))
    let flags: Map[String, Bool] = {"ready": true}
    println(unwrap_or(map_get(flags, "ready"), false))
    let labels: Map[String, String] = {"name": "Kryndel"}
    println(unwrap_or(map_get(labels, "name"), "missing"))
    let modes: Map[String, Mode] = {"current": Mode::Active}
    if unwrap_or(map_get(modes, "current"), Mode::Idle) == Mode::Active {
        println("enum-map-ok")
    }
    let empty_flags: Map[String, Bool] = {}
    println(map_contains_key(empty_flags, "missing"))
    let ready: Array[Bool] = [true, false]
    println(ready[0])
    let labels_array: Array[String] = ["from-array"]
    println(labels_array[0])
    let modes_array: Array[Mode] = [Mode::Idle, Mode::Active]
    if modes_array[1] == Mode::Active {
        println("enum-array-ok")
    }
    let empty_labels: Array[String] = []
    println(len(empty_labels))
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
	want := "2\n4\n6\ncontinued condition\n17\n42\n-42\n-42\n0\ntrue\n17\n2\n4\ntrue\n-8\ntrue\nKryndel\nenum-map-ok\nfalse\ntrue\nfrom-array\nenum-array-ok\n0\ncompiled from Kryndel source\n"
	if string(output) != want {
		t.Fatalf("unexpected source-compiled PE output %q", output)
	}
}
