package main

import (
	"bytes"
	"debug/pe"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Xyraniz/Kryndel/internal/kry"
)

func TestDirectProgramInvocation(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "hi.kry")
	if err := os.WriteFile(source, []byte("println(\"direct\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{source}); got != 0 {
		t.Fatalf("direct source invocation returned %d", got)
	}
	if got := run([]string{"run", source}); got != 0 {
		t.Fatalf("explicit run invocation returned %d", got)
	}
	if got := run([]string{source, "unexpected"}); got != 2 {
		t.Fatalf("direct invocation accepted extra arguments with status %d", got)
	}
}

func TestProjectTestDiscoversSortedCasesAndCapturesOutput(t *testing.T) {
	dir := t.TempDir()
	writeTestSource(t, filepath.Join(dir, "tests", "z_test.kry"), "println(\"second case output\")\nassert_eq(2 + 2, 4)\n")
	writeTestSource(t, filepath.Join(dir, "tests", "nested", "a_test.kry"), "println(\"first case output\")\nassert(true)\n")
	writeTestSource(t, filepath.Join(dir, "tests", "ignored.kry"), "println(\"must not run\")\n")
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })

	output, status := captureKryStdout(t, func() int { return run([]string{"test"}) })
	if status != 0 {
		t.Fatalf("kry test returned %d: %s", status, output)
	}
	first, second := strings.Index(output, "PASS nested/a_test.kry"), strings.Index(output, "PASS z_test.kry")
	if first < 0 || second < 0 || first >= second || !strings.Contains(output, "first case output") || !strings.Contains(output, "second case output") || strings.Contains(output, "must not run") {
		t.Fatalf("test discovery/order/output is wrong: %q", output)
	}
	if !strings.Contains(output, "PASS 2 cases: 2 passed, 0 failed") {
		t.Fatalf("missing summary: %q", output)
	}
}

func TestProjectTestRunsExplicitFileAndReportsFailures(t *testing.T) {
	fixture, err := filepath.Abs(filepath.Join("..", "..", "tests", "arithmetic_test.kry"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureOutput, fixtureStatus := captureKryStdout(t, func() int { return run([]string{"test", fixture}) })
	if fixtureStatus != 0 || !strings.Contains(fixtureOutput, "PASS arithmetic_test.kry") || !strings.Contains(fixtureOutput, "arithmetic fixture passed") {
		t.Fatalf("explicit checked-in test fixture = (%d, %q)", fixtureStatus, fixtureOutput)
	}

	file := filepath.Join(t.TempDir(), "single_test.kry")
	writeTestSource(t, file, "println(\"still executed\")\nassert_eq(1, 2)\n")
	output, status := captureKryStdout(t, func() int { return run([]string{"test", file}) })
	if status != 1 || !strings.Contains(output, "FAIL single_test.kry") || !strings.Contains(output, "still executed") || !strings.Contains(output, "1 cases: 0 passed, 1 failed") {
		t.Fatalf("failing explicit test = (%d, %q)", status, output)
	}
}

func TestProjectTestDirectoryContinuesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	writeTestSource(t, filepath.Join(dir, "a_test.kry"), "assert_eq(1, 2)\n")
	writeTestSource(t, filepath.Join(dir, "b_test.kry"), "println(\"later case ran\")\nassert_eq(3, 3)\n")
	output, status := captureKryStdout(t, func() int { return run([]string{"test", dir}) })
	if status != 1 || !strings.Contains(output, "FAIL a_test.kry") || !strings.Contains(output, "PASS b_test.kry") || !strings.Contains(output, "later case ran") || !strings.Contains(output, "2 cases: 1 passed, 1 failed") {
		t.Fatalf("directory test run = (%d, %q)", status, output)
	}
}

func TestProjectTestJSONIsOneResultDocument(t *testing.T) {
	dir := t.TempDir()
	writeTestSource(t, filepath.Join(dir, "a_test.kry"), "println(\"captured\")\nassert(true)\n")
	writeTestSource(t, filepath.Join(dir, "b_test.kry"), "assert_eq(1, 2)\n")
	output, status := captureKryStdout(t, func() int { return run([]string{"--json", "test", dir}) })
	var result struct {
		Status string `json:"status"`
		Passed int    `json:"passed"`
		Failed int    `json:"failed"`
		Cases  []struct {
			Path       string          `json:"path"`
			Status     string          `json:"status"`
			Output     string          `json:"output"`
			Diagnostic *kry.Diagnostic `json:"diagnostic"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("kry test --json did not emit one valid JSON document: %v; output %q", err, output)
	}
	if status != 1 || result.Status != "failed" || result.Passed != 1 || result.Failed != 1 || len(result.Cases) != 2 {
		t.Fatalf("unexpected JSON suite result/status: (%d, %#v)", status, result)
	}
	if result.Cases[0].Path != "a_test.kry" || result.Cases[0].Status != "passed" || result.Cases[0].Output != "captured\n" || result.Cases[1].Path != "b_test.kry" || result.Cases[1].Status != "failed" || result.Cases[1].Diagnostic == nil {
		t.Fatalf("JSON case results are incomplete or unordered: %#v", result.Cases)
	}
}

func TestProjectTestFailsWhenNoCasesExist(t *testing.T) {
	dir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	output, status := captureKryStdout(t, func() int { return run([]string{"--json", "test"}) })
	var result kryTestRun
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("empty test discovery did not emit valid JSON: %v; output %q", err, output)
	}
	if status != 1 || result.Status != "failed" || len(result.Cases) != 0 || len(result.Diagnostics) == 0 {
		t.Fatalf("empty test discovery = (%d, %#v)", status, result)
	}
}

func writeTestSource(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalHelpAndVersionFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"--help"}, want: "usage: kry [global-options]"},
		{args: []string{"--version"}, want: "Kryndel " + version + "\n"},
	} {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStdout := os.Stdout
		os.Stdout = writeEnd
		status := run(tc.args)
		_ = writeEnd.Close()
		os.Stdout = oldStdout
		output, err := io.ReadAll(readEnd)
		_ = readEnd.Close()
		if err != nil {
			t.Fatal(err)
		}
		if status != 0 || !strings.Contains(string(output), tc.want) {
			t.Fatalf("run(%q) = %d, output %q; want status 0 and %q", tc.args, status, output, tc.want)
		}
	}
}

func TestRestrictedRejectsEmptyRoot(t *testing.T) {
	if status := run([]string{"--restricted", "", "run", "unused.kry"}); status != 2 {
		t.Fatalf("empty restricted root returned status %d, want 2", status)
	}
}

func TestLSPCommandRejectsPositionalArguments(t *testing.T) {
	if status := run([]string{"lsp", "unexpected"}); status != 2 {
		t.Fatalf("lsp accepted positional arguments with status %d", status)
	}
}

func TestMaxJSONCLIOverride(t *testing.T) {
	if got := kry.DefaultLimits().MaxJSONBytes; got != 64<<20 {
		t.Fatalf("default MaxJSONBytes = %d; want 64 MiB", got)
	}
	source := filepath.Join(t.TempDir(), "json-limit.kry")
	program := []byte("let parsed: Result[Json, String] = json_parse(\"{}\")\nlet value: Json = result_unwrap(parsed)\nprintln(json_kind(value))\n")
	if err := os.WriteFile(source, program, 0o600); err != nil {
		t.Fatal(err)
	}
	if status := run([]string{"--max-json", "1", "run", source}); status == 0 {
		t.Fatal("--max-json 1 accepted a two-byte JSON value")
	}
	if status := run([]string{"--max-json", "2", "run", source}); status != 0 {
		t.Fatalf("--max-json 2 rejected a two-byte JSON value with status %d", status)
	}
}

func TestNoExternalToolchainCLIRejectsUnsupportedTargetsBeforeSourceIO(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.kry")
	for _, tc := range []struct {
		args   []string
		want   []string
		unwant string
	}{
		{
			args: []string{"build", missing, "--format=elf", "--no-external-toolchain"},
			want: []string{"--no-external-toolchain forbids --format=elf", "external C compiler"},
		},
		{
			args:   []string{"build", missing, "--format=exe", "--target=linux-x64", "--no-external-toolchain"},
			want:   []string{"PE output requires a Windows target", "--target=windows-x64"},
			unwant: "--format=pe-direct",
		},
	} {
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStderr := os.Stderr
		os.Stderr = writeEnd
		status := run(tc.args)
		_ = writeEnd.Close()
		os.Stderr = oldStderr
		message, err := io.ReadAll(readEnd)
		_ = readEnd.Close()
		if err != nil {
			t.Fatal(err)
		}
		if status != 2 {
			t.Fatalf("run(%q) returned %d, want 2", tc.args, status)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(message), want) {
				t.Fatalf("run(%q) output %q does not include %q", tc.args, message, want)
			}
		}
		if tc.unwant != "" && strings.Contains(string(message), tc.unwant) {
			t.Fatalf("run(%q) output %q should not include %q", tc.args, message, tc.unwant)
		}
	}
}

func TestNoExternalToolchainWindowsExeBuildUsesDirectPE(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app.kry")
	output := filepath.Join(dir, "app.exe")
	program := `fn twice(value: Int) -> Int {
    return value * 2
}
fn main() -> Nil {
    println(twice(21))
}
`
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}

	oldCompiler, hadCompiler := os.LookupEnv("KRY_CC")
	if err := os.Setenv("KRY_CC", filepath.Join(dir, "c-compiler-must-not-run.exe")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadCompiler {
			_ = os.Setenv("KRY_CC", oldCompiler)
		} else {
			_ = os.Unsetenv("KRY_CC")
		}
	})

	if status := run([]string{"build", source, "--format=exe", "--target=windows-x64", "--no-external-toolchain", "-o", output}); status != 0 {
		t.Fatalf("C-free Windows EXE build returned status %d", status)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("CLI output is not a PE executable: %v", err)
	}
	defer image.Close()
	if image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("PE machine = %#x, want amd64", image.Machine)
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		got, err := exec.Command(output).CombinedOutput()
		if err != nil {
			t.Fatalf("C-free Windows EXE failed to run: %v; output %q", err, got)
		}
		if string(got) != "42\n" {
			t.Fatalf("C-free Windows EXE output = %q, want %q", got, "42\n")
		}
	}
}

func TestGUIFlagRoutesToDirectPEAndMarksWindowsSubsystem(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "gui.kry")
	output := filepath.Join(dir, "gui.exe")
	if err := os.WriteFile(source, []byte("println(42)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := run([]string{"build", source, "--format=exe", "--target=windows-x64", "--no-external-toolchain", "--gui", "-o", output}); status != 0 {
		t.Fatalf("C-free GUI PE build returned status %d", status)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("CLI output is not a PE executable: %v", err)
	}
	defer image.Close()
	opt, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || opt.Subsystem != 2 {
		t.Fatalf("CLI GUI PE subsystem = %#v, want Windows GUI (2)", image.OptionalHeader)
	}
}

func TestGUIFlagRejectsCBackendBeforeReadingSource(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.kry")
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writeEnd
	status := run([]string{"build", missing, "--format=exe", "--target=windows-x64", "--gui"})
	_ = writeEnd.Close()
	os.Stderr = oldStderr
	message, err := io.ReadAll(readEnd)
	_ = readEnd.Close()
	if err != nil {
		t.Fatal(err)
	}
	if status != 2 || !strings.Contains(string(message), "--gui is not supported by the C AOT backend") || strings.Contains(string(message), "cannot read") {
		t.Fatalf("GUI/C AOT rejection was not clear or happened after source IO: status=%d output=%q", status, message)
	}
}

func TestProgramPathRequiresKnownExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "program.txt")
	if err := os.WriteFile(path, []byte("println(\"not direct\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if isProgramPath(path) {
		t.Fatal("non-Kryndel extension was treated as a program")
	}
}

func TestCheckWarningsAndWerror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warnings.kry")
	source := `fn read() -> Int {
    if true { return 1 } else { return 2 }
    println("unreachable")
}`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := run([]string{"check", path}); status != 0 {
		t.Fatalf("check with warnings returned %d", status)
	}
	if status := run([]string{"check", "-Werror", path}); status != 1 {
		t.Fatalf("check -Werror returned %d, want 1", status)
	}
}

func TestCheckWarningRulesCanBeSelectedIndividually(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warning-rules.kry")
	source := `fn read() -> Int {
    if true { return 1 } else { return 2 }
    println("unreachable")
}`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	capture := func(args []string) (int, string) {
		t.Helper()
		readEnd, writeEnd, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStderr := os.Stderr
		os.Stderr = writeEnd
		status := run(args)
		_ = writeEnd.Close()
		os.Stderr = oldStderr
		output, err := io.ReadAll(readEnd)
		_ = readEnd.Close()
		if err != nil {
			t.Fatal(err)
		}
		return status, string(output)
	}
	if status, output := capture([]string{"check", "-Wno=KRYW002", path}); status != 0 || strings.Contains(output, "KRYW002") || !strings.Contains(output, "KRYW004") {
		t.Fatalf("disabled rule should leave other rules enabled, got status %d and %q", status, output)
	}
	if status, output := capture([]string{"check", "-Werror=KRYW002", path}); status != 1 || !strings.Contains(output, "KRYW002") || !strings.Contains(output, "error[") || !strings.Contains(output, "warning[") || !strings.Contains(output, "KRYW004") {
		t.Fatalf("selected error rule should fail with a stable code, got status %d and %q", status, output)
	}
	if status, output := capture([]string{"check", "-Wno=KRYW999", path}); status != 2 || !strings.Contains(output, "unknown warning code") {
		t.Fatalf("unknown warning code should be a usage error, got status %d and %q", status, output)
	}
}

func TestCPUProfileOptionForRunAndCheck(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "profile.kry")
	var source strings.Builder
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&source, "fn profile_%d(value: Int) -> Int { return value + %d }\n", i, i)
	}
	source.WriteString(`fn main() -> Nil {
    let mut index: Int = 0
    while index < 10000 {
        index = index + 1
    }
    return nil
}
`)
	if err := os.WriteFile(sourcePath, []byte(source.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	assertReadableProfile := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("CPU profile was not closed and written: %v", err)
		}
		if info.Size() == 0 {
			t.Fatal("CPU profile is empty")
		}
		output, err := exec.Command("go", "tool", "pprof", "-top", path).CombinedOutput()
		if err != nil {
			t.Fatalf("go tool pprof rejected CPU profile: %v; output: %s", err, output)
		}
		if !strings.Contains(string(output), "Type: cpu") {
			t.Fatalf("go tool pprof output does not identify a CPU profile: %s", output)
		}
	}

	for _, command := range []string{"check", "run"} {
		profilePath := filepath.Join(dir, command+".pprof")
		if status := run([]string{"--cpuprofile", profilePath, command, sourcePath}); status != 0 {
			t.Fatalf("%s with CPU profile returned status %d", command, status)
		}
		assertReadableProfile(profilePath)
	}

	failingPath := filepath.Join(dir, "failing.kry")
	if err := os.WriteFile(failingPath, []byte(source.String()+"fn invalid() -> Int { return \"wrong\" }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	failureProfile := filepath.Join(dir, "failure.pprof")
	if status := run([]string{"--cpuprofile", failureProfile, "check", failingPath}); status != 1 {
		t.Fatalf("failing check with CPU profile returned status %d, want 1", status)
	}
	assertReadableProfile(failureProfile)
}

func TestEmitAcceptsExplicitKIRTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "target.kry")
	out := filepath.Join(dir, "target.kir")
	if err := os.WriteFile(source, []byte("println(\"target\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run([]string{"emit", source, "--target=linux-x64", "-o", out}); got != 0 {
		t.Fatalf("emit with explicit target returned %d", got)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"os": "linux"`) || !strings.Contains(string(data), `"arch": "amd64"`) {
		t.Fatalf("KIR did not contain requested Linux target: %s", data)
	}
}

func TestCapabilitiesCommandWritesJSONMatrix(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writeEnd
	outputCh := make(chan []byte, 1)
	errorCh := make(chan error, 1)
	go func() {
		output, err := io.ReadAll(readEnd)
		outputCh <- output
		errorCh <- err
	}()
	status := run([]string{"--json", "capabilities"})
	_ = writeEnd.Close()
	os.Stdout = oldStdout
	output := <-outputCh
	err = <-errorCh
	_ = readEnd.Close()
	if err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Fatalf("capabilities command returned %d: %s", status, output)
	}
	if !strings.Contains(string(output), `"format":"elf-direct"`) || !strings.Contains(string(output), `"target":"linux-x64"`) || !strings.Contains(string(output), `"status":"partial"`) {
		t.Fatalf("capabilities JSON omitted the direct ELF target row: %s", output)
	}
}

func TestBuiltinCapabilitiesCommandWritesJSONMatrix(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writeEnd
	outputCh := make(chan []byte, 1)
	errorCh := make(chan error, 1)
	go func() {
		output, err := io.ReadAll(readEnd)
		outputCh <- output
		errorCh <- err
	}()
	status := run([]string{"--json", "capabilities", "--builtins"})
	_ = writeEnd.Close()
	os.Stdout = oldStdout
	output := <-outputCh
	err = <-errorCh
	_ = readEnd.Close()
	if err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Fatalf("builtin capabilities command returned %d", status)
	}
	if !strings.Contains(string(output), `"builtin":"websocket_connect"`) || !strings.Contains(string(output), `"c_aot":"unsupported"`) || !strings.Contains(string(output), `"pe_direct":"unsupported"`) || !strings.Contains(string(output), `"self_hosted":"partial"`) {
		t.Fatalf("builtin capability JSON omitted backend states: %s", output[:min(len(output), 1000)])
	}
}

func TestLanguageCapabilitiesCommandWritesJSONMatrix(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writeEnd
	outputCh := make(chan []byte, 1)
	errorCh := make(chan error, 1)
	go func() {
		output, err := io.ReadAll(readEnd)
		outputCh <- output
		errorCh <- err
	}()
	status := run([]string{"--json", "capabilities", "--features"})
	_ = writeEnd.Close()
	os.Stdout = oldStdout
	output := <-outputCh
	err = <-errorCh
	_ = readEnd.Close()
	if err != nil {
		t.Fatal(err)
	}
	if status != 0 {
		t.Fatalf("language capabilities command returned %d", status)
	}
	for _, fragment := range []string{
		`"category":"expression"`, `"feature":"ExFloat"`,
		`"category":"binary_operator"`, `"feature":"SHL"`,
		`"category":"type"`, `"feature":"TyChannel"`,
		`"category":"builtin"`, `"feature":"websocket_connect"`,
		`"target":"linux-x64"`, `"elf_direct":"partial"`,
	} {
		if !strings.Contains(string(output), fragment) {
			t.Fatalf("language capability JSON omitted %s: %s", fragment, output[:min(len(output), 1500)])
		}
	}
}
