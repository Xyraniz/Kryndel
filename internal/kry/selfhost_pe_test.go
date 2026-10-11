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
	return runSelfhostPEBackendWithBudget(t, source, DefaultLimits().MaxInstructions)
}

func runSelfhostPEBackendWithBudget(t *testing.T, source string, maxInstructions uint64) ([]byte, error) {
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
	limits.MaxInstructions = maxInstructions
	limits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, limits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Logf("selfhost portable PE backend stopped after %d/%d instructions", r.Ctx.Instructions, limits.MaxInstructions)
		return nil, d
	}
	t.Logf("selfhost portable PE backend used %d instructions", r.Ctx.Instructions)
	image, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return image, nil
}

func TestSelfhostDynamicBackendCompilesFunctionLocalDerivedFromParameter(t *testing.T) {
	const source = `
fn add_one(value: Int) -> Int {
    let result: Int = value + 1
    return result
}

fn main() -> Nil {
    println(str(add_one(41)))
}
`
	image, err := runSelfhostPEBackend(t, source)
	if err != nil {
		t.Fatalf("selfhost dynamic backend failed on a local derived from a parameter: %v", err)
	}
	if _, err := pe.NewFile(bytes.NewReader(image)); err != nil {
		t.Fatalf("Go PE parser rejected generated image: %v", err)
	}
}

func TestSelfhostPEBackendProcessArgsWindows(t *testing.T) {
	source := `
fn main() -> Nil {
    let args: Array[String] = process_args()
    println(len(args))
    let mut index: Int = 0
    while index < len(args) {
        println(args[index])
        index = index + 1
    }
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected process_args: %v", d)
	}
	peImage, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the process_args image: %v", err)
	}
	imports, err := peImage.ImportedSymbols()
	peImage.Close()
	if err != nil {
		t.Fatalf("could not read process_args PE imports: %v", err)
	}
	for _, required := range []string{
		"GetCommandLineW:KERNEL32.dll",
		"CommandLineToArgvW:SHELL32.dll",
		"WideCharToMultiByte:KERNEL32.dll",
		"LocalFree:KERNEL32.dll",
	} {
		found := false
		for _, symbol := range imports {
			if symbol == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("process_args PE is missing import %q (imports: %v)", required, imports)
		}
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "process-args.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	emptyOutput, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated process_args PE failed with no user arguments: %v; output: %s", err, emptyOutput)
	}
	if string(emptyOutput) != "0\n" {
		t.Fatalf("process_args included argv[0] for an empty argument list: got %q, want %q", emptyOutput, "0\n")
	}
	args := []string{"plain", "with spaces", "", `quote"inside`, `slash\end`, "é🙂"}
	command := exec.Command(executable, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated process_args PE failed: %v; output: %s", err, output)
	}
	want := "6\nplain\nwith spaces\n\nquote\"inside\nslash\\end\né🙂\n"
	if string(output) != want {
		t.Fatalf("process_args did not preserve decoded UTF-8 arguments: got %q, want %q", output, want)
	}
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
    println(u64(0) - u64(1))
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

func TestSelfhostPEBackendArrayPush(t *testing.T) {
	source := `
enum Mode { Idle, Active }

fn append_one(values: Array[Int], value: Int) -> Array[Int] {
    return array_push(values, value)
}

fn main() -> Nil {
    let original: Array[Int] = [10, 20]
    let appended: Array[Int] = append_one(original, 30)
    let appended_again: Array[Int] = array_push(appended, 40)
    println(len(original))
    println(original[0])
    println(len(appended))
    println(appended[2])
    println(len(appended_again))
    println(appended_again[3])

    let empty: Array[Int] = []
    let from_empty: Array[Int] = array_push(empty, -7)
    println(len(empty))
    println(from_empty[0])

    let flags: Array[Bool] = [false]
    let flags_with_true: Array[Bool] = array_push(flags, true)
    println(flags_with_true[1])

    let ids: Array[UInt64] = [u64(41)]
    let ids_with_42: Array[UInt64] = array_push(ids, u64(42))
    println(str(int(ids_with_42[1])))

    let modes: Array[Mode] = [Mode::Idle]
    let modes_with_active: Array[Mode] = array_push(modes, Mode::Active)
    println(modes_with_active[1])

    let words: Array[String] = ["first"]
    let words_with_second: Array[String] = array_push(words, "second")
    println(words_with_second[1])
}
`
	// Portable decoding and typed semantic validation precede native lowering.
	// This fixture used 5,704,824 instructions; allow less than 10% headroom.
	image, d := runSelfhostPEBackendWithBudget(t, source, 6_250_000)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected array_push: %v", d)
	}
	if len(image) < 0x100 || string(image[:2]) != "MZ" || string(image[0x80:0x84]) != "PE\x00\x00" {
		t.Fatalf("selfhost backend did not emit a PE32+ image (size %d)", len(image))
	}
	parsedPE, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the array_push image: %v", err)
	}
	parsedPE.Close()
	want := "2\n10\n3\n30\n4\n40\n0\n-7\ntrue\n42\nMode::Active\nsecond\n"
	if interpreted := runInterp(t, source); interpreted != want {
		t.Fatalf("unexpected interpreter output %q; want %q", interpreted, want)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native array_push execution requires Windows amd64; PE structure was validated")
	}
	executable := filepath.Join(t.TempDir(), "array-push.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated array_push PE failed: %v; output: %s", err, output)
	}
	if string(output) != want {
		t.Fatalf("unexpected selfhost-generated array_push PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendArrayConcat(t *testing.T) {
	source := `
enum Mode { Idle, Active }

fn concat_ints(left: Array[Int], right: Array[Int]) -> Array[Int] {
    return left + right
}

fn main() -> Nil {
    let left: Array[Int] = [10, 20]
    let right: Array[Int] = [30]
    let joined: Array[Int] = concat_ints(left, right)
    let operator_joined: Array[Int] = left + right
    println(len(joined))
    println(joined[0])
    println(joined[2])
    println(operator_joined[1])
    println(len(left))
    println(len(right))

    let empty: Array[Int] = []
    let empty_left: Array[Int] = empty + left
    let empty_right: Array[Int] = array_concat(left, empty)
    let both_empty: Array[Int] = empty + empty
    println(len(empty_left))
    println(empty_left[1])
    println(len(empty_right))
    println(empty_right[0])
    println(len(both_empty))

    let flags: Array[Bool] = [false] + [true]
    println(flags[0])
    println(flags[1])

    let ids: Array[UInt64] = [u64(41)] + [u64(42)]
    println(str(int(ids[1])))

    let words: Array[String] = ["first"] + ["second"]
    println(words[1])

    let modes: Array[Mode] = [Mode::Idle] + [Mode::Active]
    println(modes[1])
}
`
	// Portable decoding and typed semantic validation precede native lowering.
	// This fixture used 6,632,291 instructions; allow less than 10% headroom.
	image, d := runSelfhostPEBackendWithBudget(t, source, 7_250_000)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected array_concat: %v", d)
	}
	if len(image) < 0x100 || string(image[:2]) != "MZ" || string(image[0x80:0x84]) != "PE\x00\x00" {
		t.Fatalf("selfhost backend did not emit a PE32+ image (size %d)", len(image))
	}
	parsedPE, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the array_concat image: %v", err)
	}
	parsedPE.Close()
	want := "3\n10\n30\n20\n2\n1\n2\n20\n2\n10\n0\nfalse\ntrue\n42\nsecond\nMode::Active\n"
	if interpreted := runInterp(t, source); interpreted != want {
		t.Fatalf("unexpected interpreter output %q; want %q", interpreted, want)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native array concatenation execution requires Windows amd64; PE structure was validated")
	}
	executable := filepath.Join(t.TempDir(), "array-concat.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated array concatenation PE failed: %v; output: %s", err, output)
	}
	if string(output) != want {
		t.Fatalf("unexpected selfhost-generated array concatenation PE output %q; want %q", output, want)
	}
}

func TestSelfhostPEBackendArrayIndices(t *testing.T) {
	source := `
enum Mode { Idle, Active }

fn indices_of_words(words: Array[String]) -> Array[Int] {
    return array_indices(words)
}

fn main() -> Nil {
    let values: Array[Int] = [4, 8, 15]
    let indexes: Array[Int] = array_indices(values)
    println(len(indexes))
    println(indexes[0])
    println(indexes[2])
    println(len(values))
    println(values[1])

    let words: Array[String] = ["first", "second"]
    let word_indexes: Array[Int] = indices_of_words(words)
    println(len(word_indexes))
    println(word_indexes[0])
    println(word_indexes[1])

    let modes: Array[Mode] = [Mode::Idle, Mode::Active]
    let mode_indexes: Array[Int] = array_indices(modes)
    println(mode_indexes[1])

    let empty: Array[String] = []
    let empty_indexes: Array[Int] = array_indices(empty)
    println(len(empty_indexes))
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected array_indices: %v", d)
	}
	if len(image) < 0x100 || string(image[:2]) != "MZ" || string(image[0x80:0x84]) != "PE\x00\x00" {
		t.Fatalf("selfhost backend did not emit a PE32+ image (size %d)", len(image))
	}
	parsedPE, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected the array_indices image: %v", err)
	}
	parsedPE.Close()
	want := "3\n0\n2\n3\n8\n2\n0\n1\n1\n0\n"
	if interpreted := runInterp(t, source); interpreted != want {
		t.Fatalf("unexpected interpreter output %q; want %q", interpreted, want)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native array_indices execution requires Windows amd64; PE structure was validated")
	}
	executable := filepath.Join(t.TempDir(), "array-indices.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated array_indices PE failed: %v; output: %s", err, output)
	}
	if string(output) != want {
		t.Fatalf("unexpected selfhost-generated array_indices PE output %q; want %q", output, want)
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

func TestSelfhostPEBackendCollectionFunctionArgumentsAndReturns(t *testing.T) {
	source := `
fn identity_words(values: Array[String]) -> Array[String] { return values }
fn make_words() -> Array[String] { return ["from-return"] }
fn first_word(values: Array[String]) -> String { return values[0] }

fn identity_flags(flags: Map[String, Bool]) -> Map[String, Bool] { return flags }
fn make_flags() -> Map[String, Bool] { return {"ready": true} }
fn is_ready(flags: Map[String, Bool]) -> Bool { return unwrap_or(map_get(flags, "ready"), false) }

fn main() -> Nil {
    let words: Array[String] = identity_words(make_words())
    println(first_word(words))
    let flags: Map[String, Bool] = identity_flags(make_flags())
    println(is_ready(flags))
    println(is_ready({"ready": false}))
}
`
	image, d := runSelfhostPEBackend(t, source)
	if d != nil {
		t.Fatalf("selfhost PE backend rejected collection function arguments or returns: %v", d)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "collection-functions.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated collection-function PE failed: %v; output: %s", err, output)
	}
	want := "from-return\ntrue\nfalse\n"
	if string(output) != want {
		t.Fatalf("unexpected collection-function PE output %q; want %q", output, want)
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
			name: "map with unsupported function parameter type",
			source: `fn lookup(values: Map[Int, Int]) -> Int { return 0 }
fn main() -> Nil {}`,
			diagnostic: "PE backend: function parameters require supported scalar, enum, struct, or collection types and no defaults",
		},
		{
			name: "map with unsupported function return type",
			source: `fn make() -> Map[Int, Int] { return {} }
fn main() -> Nil {}`,
			diagnostic: "PE backend: function 'make' has an unsupported return type",
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
			name: "array equality",
			source: `fn main() -> Nil {
    let left: Array[Int] = [1]
    let right: Array[Int] = [2]
    println(left == right)
}`,
			diagnostic: "PE backend: array concatenation requires '+' and matching supported Array[T] values",
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
			source: `fn read(values: Array[Array[Int]]) -> Int { return 0 }
fn main() -> Nil {}`,
			diagnostic: "PE backend: function parameters require supported scalar, enum, struct, or collection types and no defaults",
		},
		{
			name: "array function return",
			source: `fn make() -> Array[Array[Int]] { return [[1]] }
fn main() -> Nil {}`,
			diagnostic: "PE backend: function 'make' has an unsupported return type",
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

func runSelfhostPEBackendExpectingOutput(t *testing.T, source, filename, want string) {
	t.Helper()
	image, err := runSelfhostPEBackend(t, source)
	if err != nil {
		t.Fatalf("selfhost PE backend failed to compile fixture: %v", err)
	}
	peImage, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Go PE parser rejected generated image: %v", err)
	}
	if peImage.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		peImage.Close()
		t.Fatalf("generated PE has machine %#x, want amd64", peImage.Machine)
	}
	if _, err := peImage.ImportedSymbols(); err != nil {
		peImage.Close()
		t.Fatalf("could not read generated PE imports: %v", err)
	}
	peImage.Close()
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated PE failed: %v; output: %s", err, output)
	}
	if string(output) != want {
		t.Fatalf("unexpected PE output %q; want %q", output, want)
	}
}

func kryStringContents(value string) string {
	const hex = "0123456789abcdef"
	var escaped strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		switch {
		case b == '\\':
			escaped.WriteString(`\\`)
		case b == '"':
			escaped.WriteString(`\"`)
		case b < 0x20:
			escaped.WriteString(`\x`)
			escaped.WriteByte(hex[b>>4])
			escaped.WriteByte(hex[b&0x0f])
		default:
			escaped.WriteByte(b)
		}
	}
	return escaped.String()
}

func TestSelfhostPEBackendJSONObjectKeysSortedUniqueAndLastWins(t *testing.T) {
	source := `
fn read_keys() -> Array[String] {
    return result_unwrap(json_object_keys(result_unwrap(json_parse("{\"z\":1,\"\\u0062\":2,\"é\":3,\"💩\":4,\"\\uD83D\\uDCA9\":5,\"\\uD800\":6,\"quote\\\"key\":7,\"slash\\\\key\":8,\"z\":9}"))))
}

fn last_duplicate() -> Int {
    return result_unwrap(json_int(result_unwrap(json_object_get(result_unwrap(json_parse("{\"z\":1,\"z\":7}")), "z"))))
}
fn escaped_duplicate() -> Int {
    return result_unwrap(json_int(result_unwrap(json_object_get(result_unwrap(json_parse("{\"z\":1,\"\\u007a\":7}")), "z"))))
}
fn main() -> Nil {
    let keys: Array[String] = read_keys()
    println(len(keys))
    println(unwrap_or(array_get(keys, 0), "missing"))
    println(unwrap_or(array_get(keys, 1), "missing"))
    println(unwrap_or(array_get(keys, 2), "missing"))
    println(unwrap_or(array_get(keys, 3), "missing"))
    println(unwrap_or(array_get(keys, 4), "missing"))
    println(unwrap_or(array_get(keys, 5), "missing"))
    println(unwrap_or(array_get(keys, 6), "missing"))
    println(last_duplicate())
    println(escaped_duplicate())
}
`
	want := "7\nb\nquote\"key\nslash\\key\nz\né\n�\n💩\n7\n7\n"
	runSelfhostPEBackendExpectingOutput(t, source, "json-object-keys.exe", want)
}

func TestSelfhostPEBackendJSONObjectKeysSortOrder(t *testing.T) {
	source := `
fn get_keys() -> Array[String] {
    return result_unwrap(json_object_keys(result_unwrap(json_parse("{\"a\":1,\"b\":2,\"z\":3}"))))
}
fn main() -> Nil {
    let keys: Array[String] = get_keys()
    println(unwrap_or(array_get(keys, 0), "missing"))
    println(unwrap_or(array_get(keys, 1), "missing"))
    println(unwrap_or(array_get(keys, 2), "missing"))
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-keys-sort.exe", "a\nb\nz\n")
}

func TestSelfhostPEBackendJSONObjectKeysSortUTF8(t *testing.T) {
	json := `{"💩":1,"é":2,"�":3,"a":4}`
	source := `
fn get_keys() -> Array[String] {
    return result_unwrap(json_object_keys(result_unwrap(json_parse("` + kryStringContents(json) + `"))))
}

fn main() -> Nil {
    let keys: Array[String] = get_keys()
    println(unwrap_or(array_get(keys, 0), "missing"))
    println(unwrap_or(array_get(keys, 1), "missing"))
    println(unwrap_or(array_get(keys, 2), "missing"))
    println(unwrap_or(array_get(keys, 3), "missing"))
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-keys-sort-utf8.exe", "a\né\n�\n💩\n")
}

func TestSelfhostPEBackendJSONObjectKeysSortUTF8Pair(t *testing.T) {
	first := `{"é":1,"�":2}`
	second := `{"�":1,"é":2}`
	source := `
fn keys_first() -> Array[String] {
    return result_unwrap(json_object_keys(result_unwrap(json_parse("` + kryStringContents(first) + `"))))
}
fn keys_second() -> Array[String] {
    return result_unwrap(json_object_keys(result_unwrap(json_parse("` + kryStringContents(second) + `"))))
}

fn main() -> Nil {
    let a: Array[String] = keys_first()
    let b: Array[String] = keys_second()
    println(unwrap_or(array_get(a, 0), "missing"))
    println(unwrap_or(array_get(a, 1), "missing"))
    println(unwrap_or(array_get(b, 0), "missing"))
    println(unwrap_or(array_get(b, 1), "missing"))
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-keys-sort-utf8-pair.exe", "é\n�\né\n�\n")
}

func TestSelfhostPEBackendJSONObjectKeysDeduplicateEscapedAndRaw(t *testing.T) {
	json := `{"b":1,"\u0062":2,"💩":3,"\uD83D\uDCA9":4}`
	source := `
fn get_keys() -> Array[String] {
    return result_unwrap(json_object_keys(result_unwrap(json_parse("` + kryStringContents(json) + `"))))
}
fn main() -> Nil {
    let keys: Array[String] = get_keys()
    println(len(keys))
    println(unwrap_or(array_get(keys, 0), "missing"))
    println(unwrap_or(array_get(keys, 1), "missing"))
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-keys-dedup-escaped.exe", "2\nb\n💩\n")
}

func TestSelfhostPEBackendJSONArrayLengthAndGet(t *testing.T) {
	source := `
fn inspect_array(raw: String) -> Nil {
    let values: Json = result_unwrap(json_parse(raw))
    println(result_unwrap(json_array_len(values)))
    println(result_unwrap(json_string(result_unwrap(json_array_get(values, 0)))))
    println(result_unwrap(json_string(result_unwrap(json_array_get(values, 1)))))
    return nil
}

fn inspect_kir_sources() -> Nil {
    let document: Json = result_unwrap(json_parse("{\"sources\":[\"main.kry\",\"lib.kry\"]}"))
    let sources: Json = result_unwrap(json_object_get(document, "sources"))
    println(result_unwrap(json_array_len(sources)))
    println(result_unwrap(json_string(result_unwrap(json_array_get(sources, 0)))))
    println(result_unwrap(json_string(result_unwrap(json_array_get(sources, 1)))))
    return nil
}

fn main() -> Nil {
    inspect_array("[\"alpha\",\"beta\"]")
    inspect_array(" [ \"spaced-a\" , \"spaced-b\" ] ")
    println(result_unwrap(json_array_len(result_unwrap(json_parse("[]")))))
    inspect_kir_sources()
    return nil
}
`
	want := "2\nalpha\nbeta\n2\nspaced-a\nspaced-b\n0\n2\nmain.kry\nlib.kry\n"
	runSelfhostPEBackendExpectingOutput(t, source, "json-array-access.exe", want)
}

func TestSelfhostPEBackendJSONArrayGetNestedObjectFields(t *testing.T) {
	source := `
fn main() -> Nil {
    let values: Json = result_unwrap(json_parse("[{\"name\":\"abc\",\"source\":\"f\",\"line\":1}]"))
    let item: Json = result_unwrap(json_array_get(values, 0))
    println(result_unwrap(json_string(result_unwrap(json_object_get(item, "name")))))
    println(result_unwrap(json_string(result_unwrap(json_object_get(item, "source")))))
    println(result_unwrap(json_int(result_unwrap(json_object_get(item, "line")))))
    return nil
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-array-nested-object.exe", "abc\nf\n1\n")
}

func TestSelfhostPEBackendJSONChildFieldValidation(t *testing.T) {
	source := `
fn check(raw: String, candidates: String, allowed: String, empty_arrays: String) -> Bool {
    return json_object_fields_empty_except(result_unwrap(json_parse(raw)), candidates, allowed, empty_arrays)
}
fn main() -> Nil {
    println(check("{}", "|args|items|", "|kind|", "|args|items|"))
    println(check("[]", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"other\":null}", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"args\":[]}", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"items\":[1]}", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"args\":1,\"args\":null}", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"args\":null,\"args\":1}", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"\\u0061rgs\":1}", "|args|items|", "|kind|", "|args|items|"))
    println(check("{\"args\":[]}", "|args|items|", "kind", "|args|items|"))
    return nil
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-child-fields.exe", "true\nfalse\ntrue\ntrue\nfalse\ntrue\nfalse\nfalse\nfalse\n")
}

func TestSelfhostPEBackendJSONObjectKeysAllowed(t *testing.T) {
	source := `
fn main() -> Nil {
    println(json_object_keys_allowed(result_unwrap(json_parse("{\"kind\":\"call\",\"args\":[],\"line\":1}")), "|kind|args|line|"))
    println(json_object_keys_allowed(result_unwrap(json_parse("{\"kind\":\"call\",\"extra\":true}")), "|kind|args|line|"))
    println(json_object_keys_allowed(result_unwrap(json_parse("{\"args\":[],\"extra\":true}")), "|args|kind|line|"))
    println(json_object_keys_allowed(result_unwrap(json_parse("{}")), ""))
    println(json_object_keys_allowed(result_unwrap(json_parse("{}")), "malformed"))
    println(json_object_keys_allowed(result_unwrap(json_parse("{}")), "||"))
    println(json_object_keys_allowed(result_unwrap(json_parse("{\"kind\":1}")), "|kind||args|"))
    println(json_object_keys_allowed(result_unwrap(json_parse("[]")), "|kind|"))
    println(json_object_keys_allowed(result_unwrap(json_parse("{\"é\":1}")), "|é|"))
    return nil
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-object-keys-allowed.exe", "true\nfalse\nfalse\ntrue\nfalse\nfalse\nfalse\nfalse\ntrue\n")
}

func TestSelfhostPEBackendJSONObjectKeysAllowedEmptyObject(t *testing.T) {
	source := `fn main() -> Nil { println(json_object_keys_allowed(result_unwrap(json_parse("{}")), "bad")); return nil }`
	runSelfhostPEBackendExpectingOutput(t, source, "json-object-keys-allowed-empty.exe", "false\n")
}

func TestSelfhostPEBackendJSONParserRejectsMalformedDocuments(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{name: "missing value", input: `{"a":}`},
		{name: "invalid escape", input: `{"a":"\q"}`},
		{name: "raw control character", input: "{\"a\":\"\x01\"}"},
		{name: "object trailing comma", input: `{"a":1,}`},
		{name: "array trailing comma", input: `{"a":[1,]}`},
		{name: "key without colon", input: `{"a" 1}`},
		{name: "leading zero", input: `{"a":01}`},
		{name: "incomplete literal", input: `{"a":tru}`},
		{name: "extra data after document", input: `{} false`},
		{name: "object keys on non-object", input: `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "object keys on non-object" {
				source := `fn main() -> Nil { println(is_err(json_object_keys(result_unwrap(json_parse("` + kryStringContents(tc.input) + `"))))) }
`
				runSelfhostPEBackendExpectingOutput(t, source, "json-non-object-keys.exe", "true\n")
				return
			}
			source := `fn main() -> Nil { println(is_err(json_parse("` + kryStringContents(tc.input) + `"))) }
`
			runSelfhostPEBackendExpectingOutput(t, source, "json-invalid-input.exe", "true\n")
		})
	}
}

func TestSelfhostPEBackendJSONParserAcceptsFractionAndExponent(t *testing.T) {
	source := `
fn main() -> Nil {
    println(is_ok(json_parse("1.25")))
    println(is_ok(json_parse("1.25e2")))
    println(is_ok(json_parse("{\"f\":1.25E-2}")))
}
`
	runSelfhostPEBackendExpectingOutput(t, source, "json-valid-numbers.exe", "true\ntrue\ntrue\n")
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

fn identity_words(values: Array[String]) -> Array[String] {
    return values
}

fn make_words() -> Array[String] {
    return ["from-function"]
}

fn first_word(values: Array[String]) -> String {
    return values[0]
}

fn identity_flags(flags: Map[String, Bool]) -> Map[String, Bool] {
    return flags
}

fn make_flags() -> Map[String, Bool] {
    return {"ready": true}
}

fn is_ready(flags: Map[String, Bool]) -> Bool {
    return unwrap_or(map_get(flags, "ready"), false)
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
    let returned_words: Array[String] = identity_words(make_words())
    println(first_word(returned_words))
    let returned_flags: Map[String, Bool] = identity_flags(make_flags())
    println(is_ready(returned_flags))
    println("compiled from Kryndel source")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	// The source compiler now validates and traverses the schema-shaped typed
	// KIR tables as well as parsing the compiler module graph. This fixture used
	// 8,529,362 instructions; retain about 5% headroom without changing CLI limits.
	limits.MaxInstructions = 9_000_000
	limits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, limits, Sandbox{}, []string{sourcePath, outputPath, "windows-amd64"})
	if d != nil {
		t.Fatal(d.Message)
	}
	if diagnostic := r.run(); diagnostic != nil {
		instructions := uint64(0)
		if r.Ctx != nil {
			instructions = r.Ctx.Instructions
		}
		t.Fatalf("selfhost source compiler failed at %s:%d:%d after %d/%d instructions: %s", diagnostic.Source, diagnostic.Line, diagnostic.Column, instructions, limits.MaxInstructions, diagnostic.Message)
	}
	t.Logf("selfhost source PE compilation used %d instructions", r.Ctx.Instructions)
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
	want := "2\n4\n6\ncontinued condition\n17\n42\n-42\n-42\n0\ntrue\n17\n2\n4\ntrue\n-8\ntrue\nKryndel\nenum-map-ok\nfalse\ntrue\nfrom-array\nenum-array-ok\n0\nfrom-function\ntrue\ncompiled from Kryndel source\n"
	if string(output) != want {
		t.Fatalf("unexpected source-compiled PE output %q", output)
	}
}
