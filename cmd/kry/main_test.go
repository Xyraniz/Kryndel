package main

import (
	"io"
	"os"
	"path/filepath"
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

func TestNoExternalToolchainCLIRejectsCBackendBeforeSourceIO(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = writeEnd
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})

	missing := filepath.Join(t.TempDir(), "does-not-exist.kry")
	status := run([]string{"build", missing, "--format=elf", "--no-external-toolchain"})
	_ = writeEnd.Close()
	os.Stderr = oldStderr
	message, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatal(err)
	}
	if status != 2 {
		t.Fatalf("no-external-toolchain build returned %d, want 2", status)
	}
	if !strings.Contains(string(message), "--no-external-toolchain forbids --format=elf") || !strings.Contains(string(message), "external C compiler") {
		t.Fatalf("build did not explain the forbidden backend dependency: %s", message)
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
	if !strings.Contains(string(output), `"builtin":"websocket_connect"`) || !strings.Contains(string(output), `"c_aot":"unsupported"`) || !strings.Contains(string(output), `"self_hosted":"partial"`) {
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
