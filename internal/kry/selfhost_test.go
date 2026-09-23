package kry

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func assertNativeArtifact(t *testing.T, data []byte, label string) {
	t.Helper()
	if _, err := InspectNative(data); err != nil {
		t.Fatalf("%s is not a valid native artifact: %v", label, err)
	}
}

func assertLinuxAMD64ELF(t *testing.T, data []byte, label string) {
	t.Helper()
	assertNativeArtifact(t, data, label)
	if len(data) < 20 || !bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		t.Fatalf("%s is not an ELF executable", label)
	}
	if data[4] != 2 || data[5] != 1 {
		t.Fatalf("%s is not little-endian ELF64: class=%d data=%d", label, data[4], data[5])
	}
	if machine := binary.LittleEndian.Uint16(data[18:20]); machine != 62 {
		t.Fatalf("%s has ELF machine %#x, want x86-64 (62)", label, machine)
	}
}

func TestStage1KryndelBackendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "static_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "input.kir")
	outputPath := filepath.Join(dir, "stage1")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage1 backend failed: %s", d.Message)
	}
	stage1, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, stage1, "stage1 self-hosted backend output")
	assertNativeArtifact(t, oracle, "stage1 direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		if _, err := InspectNative(stage1); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStage1SourceCompilerMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_stage1.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage1")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("source stage1 compiler failed: %s", d.Message)
	}
	stage1, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, stage1, "source stage1 self-hosted output")
	assertNativeArtifact(t, oracle, "source stage1 direct ELF oracle")
}

func TestStage2SourceCompilerParsesExpressionsAndEscapes(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_stage2.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage2")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage2 source compiler failed: %s", d.Message)
	}
	stage2, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, stage2, "source stage2 self-hosted output")
	assertNativeArtifact(t, oracle, "source stage2 direct ELF oracle")
}

func TestStage2SourceCompilerRejectsUnsupportedSyntax(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_compiler.kry")
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "unsupported.kry")
	outputPath := filepath.Join(dir, "unsupported-output")
	if err := os.WriteFile(inputPath, []byte("while true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{inputPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d == nil {
		t.Fatal("unsupported source syntax was accepted")
	} else if !strings.Contains(d.Message, "unsupported statement") {
		t.Fatalf("unexpected unsupported-syntax diagnostic: %s", d.Message)
	}
}

func TestStage2KryndelDynamicBackendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic.kir")
	outputPath := filepath.Join(dir, "dynamic-stage2")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("dynamic Kryndel backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage2 dynamic self-hosted output")
	assertNativeArtifact(t, want, "stage2 dynamic direct ELF oracle")
}

func TestStage29KryndelDynamicBackendU8Array(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_u8_array_stage29.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic backend ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic-u8-array.kir")
	outputPath := filepath.Join(dir, "dynamic-u8-array")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	backendLimits.MaxWallTimeMS = 60_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage29 dynamic backend failed: %s", d.Message)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, data, "stage29 dynamic u8_array output")
	runnable := filepath.Join(dir, "dynamic-u8-array-runnable")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage29 dynamic u8_array ELF failed to execute: %v", err)
	}
	if string(output) != "3\n65\n67\n3\n68\n70\n" {
		t.Fatalf("unexpected stage29 dynamic u8_array output %q", output)
	}
}

func TestStage30KryndelDynamicBackendMapRuntime(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_map_stage30.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic backend ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic-map.kir")
	outputPath := filepath.Join(dir, "dynamic-map")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	backendLimits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage30 dynamic map backend failed: %s", d.Message)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, data, "stage30 dynamic map output")
	runnable := filepath.Join(dir, "dynamic-map-runnable")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage30 dynamic map ELF failed to execute: %v", err)
	}
	if string(output) != "42\ntrue\nfalse\nfalse\ntrue\n" {
		t.Fatalf("unexpected stage30 dynamic map output %q", output)
	}
}

func TestStage31KryndelDynamicBackendArrayMutation(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_array_mutation_stage31.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic backend ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic-array-mutation.kir")
	outputPath := filepath.Join(dir, "dynamic-array-mutation")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	backendLimits.MaxWallTimeMS = 60_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage31 dynamic backend failed: %s", d.Message)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, data, "stage31 dynamic array mutation output")
	runnable := filepath.Join(dir, "dynamic-array-mutation-runnable")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage31 dynamic array mutation ELF failed to execute: %v", err)
	}
	if string(output) != "20\n99\n2\n99\n30\n" {
		t.Fatalf("unexpected stage31 dynamic array mutation output %q", output)
	}
}

func TestStage32KryndelDynamicBackendJSONParseKind(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_json_stage32.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic backend ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic-json.kir")
	outputPath := filepath.Join(dir, "dynamic-json")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	backendLimits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage32 dynamic backend failed: %s", d.Message)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, data, "stage32 dynamic JSON output")
	runnable := filepath.Join(dir, "dynamic-json-runnable")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage32 dynamic JSON ELF failed to execute: %v", err)
	}
	if string(output) != "true\nobject\ntrue\nnumber\ntrue\ntrue\ntrue\n3\ntrue\nbool\ntrue\n" {
		t.Fatalf("unexpected stage32 dynamic JSON output %q", output)
	}
}

func TestStage33KryndelDynamicBackendJSONScalarAccessors(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "dynamic_json_scalars_stage33.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic backend ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "dynamic-json-scalars.kir")
	outputPath := filepath.Join(dir, "dynamic-json-scalars")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	backendLimits.MaxWallTimeMS = 180_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage33 dynamic JSON scalar backend failed: %s", d.Message)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, data, "stage33 dynamic JSON scalar output")
	runnable := filepath.Join(dir, "dynamic-json-scalars-runnable")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage33 dynamic JSON scalar ELF failed to execute: %v", err)
	}
	if string(output) != "hello\ntrue\n-42\n42\ntrue\ntrue\né🙂\ntrue\ntrue\ntrue\nfalse\n" {
		t.Fatalf("unexpected stage33 dynamic JSON scalar output %q", output)
	}
}

func TestStage34KryndelDynamicBackendUnaryNot(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	source := &Source{Name: "dynamic-unary-not-stage34.kry", Text: "fn negate(value: Bool) -> Bool { return !value }\nfn main() -> Nil { println(negate(false)); println(negate(true)) }\n"}
	program, d := Parse(source, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
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
	kirPath := filepath.Join(dir, "dynamic-unary-not.kir")
	outputPath := filepath.Join(dir, "dynamic-unary-not")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage34 dynamic unary-not backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	oracle, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage34 dynamic unary-not output")
	if !bytes.Equal(got, oracle) {
		t.Fatal("stage34 dynamic unary-not ELF differs from direct backend")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic backend ELF execution requires linux-amd64")
	}
	runnable := filepath.Join(dir, "dynamic-unary-not-runnable")
	if err := os.WriteFile(runnable, got, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).CombinedOutput()
	if err != nil {
		t.Fatalf("stage34 dynamic unary-not ELF failed: %v; output: %s", err, output)
	}
	if string(output) != "true\nfalse\n" {
		t.Fatalf("unexpected stage34 dynamic unary-not output %q", output)
	}
}

func TestStage36KryndelSecondCompilerBootstrap(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	selfhost := filepath.Join(root, "..", "..", "selfhost")
	compilerPath := filepath.Join(selfhost, "source_kir_compiler.kry")
	backendPath := filepath.Join(selfhost, "kir_backend.kry")
	fixturePath := filepath.Join(selfhost, "fixtures", "bootstrap_hello_stage27.kry")
	invalidPath := filepath.Join(t.TempDir(), "invalid-source.kry")

	compilerProgram, d := LoadProgram(compilerPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(compilerProgram, compilerChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Format          string `json:"format"`
		Version         int    `json:"version"`
		LanguageVersion string `json:"language_version"`
		Target          struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
		} `json:"target"`
	}
	if err := json.Unmarshal(kir, &header); err != nil {
		t.Fatalf("source compiler KIR is not valid JSON: %v", err)
	}
	if header.Format != KIRFormat || header.Version != KIRVersion || header.LanguageVersion != LanguageVersion || header.Target.OS != "linux" || header.Target.Arch != "amd64" {
		t.Fatalf("unexpected source compiler KIR header: %#v", header)
	}
	if len(kir) > DefaultLimits().MaxJSONBytes {
		t.Fatalf("source compiler KIR is %d bytes, exceeding MaxJSONBytes=%d", len(kir), DefaultLimits().MaxJSONBytes)
	}

	backendProgram, d := LoadProgram(backendPath, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	kirFile := filepath.Join(dir, "source-compiler.kir")
	generatedCompiler := filepath.Join(dir, "source-kir-compiler")
	if err := os.WriteFile(kirFile, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	backendLimits.MaxWallTimeMS = 12 * 60 * 1000
	backendLimits.MaxInstructions = 100_000_000
	if os.Getenv("KRY_RACE") == "1" {
		// The race instrumented interpreter is substantially slower during
		// the large bootstrap, but it must still exercise the same checks.
		backendLimits.MaxWallTimeMS = 30 * 60 * 1000
		backendLimits.MaxInstructions = 250_000_000
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirFile, generatedCompiler})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage35 dynamic backend failed to compile source compiler KIR: %s", d.Message)
	}
	t.Logf("stage35 generated compiler ELF from %d-byte KIR", len(kir))
	compilerELF, err := os.ReadFile(generatedCompiler)
	if err != nil {
		t.Fatalf("stage35 dynamic backend did not write compiler ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, compilerELF, "stage35 generated source compiler")
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("generated compiler execution requires linux-amd64; KIR parsing and ELF generation passed")
	}
	if err := os.Chmod(generatedCompiler, 0o700); err != nil {
		t.Fatal(err)
	}
	generatedProgram := filepath.Join(dir, "bootstrap-program")
	compilerOutput, err := exec.Command(generatedCompiler, fixturePath, generatedProgram).CombinedOutput()
	if err != nil {
		t.Fatalf("stage35 generated compiler failed to compile fixture: %v; output: %s", err, compilerOutput)
	}
	programELF, err := os.ReadFile(generatedProgram)
	if err != nil {
		t.Fatalf("stage35 generated compiler did not write fixture ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, programELF, "stage35 generated fixture")
	if err := os.Chmod(generatedProgram, 0o700); err != nil {
		t.Fatal(err)
	}
	programOutput, err := exec.Command(generatedProgram).CombinedOutput()
	if err != nil {
		t.Fatalf("stage35 generated fixture ELF failed: %v; output: %s", err, programOutput)
	}
	if string(programOutput) != "hello from bootstrap\n" {
		t.Fatalf("unexpected stage35 generated fixture output %q", programOutput)
	}
	t.Log("stage35 generated compiler compiled and ran the fixture")

	frontendSource, err := os.ReadFile(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	frontendText := strings.ReplaceAll(string(frontendSource), "\r\n", "\n")
	const backendImport = "import \"dynamic_backend\"\n"
	if !strings.HasPrefix(frontendText, backendImport) {
		t.Fatalf("source compiler no longer starts with expected backend import %q", backendImport)
	}
	backendSource, err := os.ReadFile(filepath.Join(selfhost, "dynamic_backend.kry"))
	if err != nil {
		t.Fatal(err)
	}
	backendText := strings.ReplaceAll(string(backendSource), "\r\n", "\n")
	const elfImport = "import \"elf_backend\"\n"
	if !strings.HasPrefix(backendText, elfImport) {
		t.Fatalf("dynamic backend no longer starts with expected ELF backend import %q", elfImport)
	}
	elfSource, err := os.ReadFile(filepath.Join(selfhost, "elf_backend.kry"))
	if err != nil {
		t.Fatal(err)
	}
	elfText := strings.ReplaceAll(string(elfSource), "\r\n", "\n")
	bundledCompilerSource := strings.TrimSuffix(elfText, "\n") + "\n\n" + strings.TrimPrefix(backendText, elfImport) + "\n\n" + strings.TrimPrefix(frontendText, backendImport)
	bundledCompiler := filepath.Join(dir, "source-kir-compiler-bundle.kry")
	if err := os.WriteFile(bundledCompiler, []byte(bundledCompilerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("stage36 compiling %d-byte bundled compiler source", len(bundledCompilerSource))
	secondCompiler := filepath.Join(dir, "second-source-kir-compiler")
	secondCompilerOutput, err := exec.Command(generatedCompiler, bundledCompiler, secondCompiler).CombinedOutput()
	if err != nil {
		t.Fatalf("stage36 generated compiler failed to compile the bundled frontend/backend source: %v; output: %s", err, secondCompilerOutput)
	}
	t.Log("stage36 generated second-level compiler ELF")
	secondCompilerELF, err := os.ReadFile(secondCompiler)
	if err != nil {
		t.Fatalf("stage36 generated compiler did not write second-level compiler ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, secondCompilerELF, "stage36 second-level source compiler")
	if err := os.Chmod(secondCompiler, 0o700); err != nil {
		t.Fatal(err)
	}
	secondProgram := filepath.Join(dir, "second-bootstrap-program")
	secondOutput, err := exec.Command(secondCompiler, fixturePath, secondProgram).CombinedOutput()
	if err != nil {
		t.Fatalf("stage36 second-level compiler failed to compile fixture: %v; output: %s", err, secondOutput)
	}
	secondProgramELF, err := os.ReadFile(secondProgram)
	if err != nil {
		t.Fatalf("stage36 second-level compiler did not write fixture ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, secondProgramELF, "stage36 second-level generated fixture")
	if err := os.Chmod(secondProgram, 0o700); err != nil {
		t.Fatal(err)
	}
	secondProgramOutput, err := exec.Command(secondProgram).CombinedOutput()
	if err != nil {
		t.Fatalf("stage36 second-level generated fixture failed: %v; output: %s", err, secondProgramOutput)
	}
	if string(secondProgramOutput) != "hello from bootstrap\n" {
		t.Fatalf("unexpected stage36 second-level fixture output %q", secondProgramOutput)
	}
	t.Log("stage36 second-level compiler compiled and ran the fixture")

	if err := os.WriteFile(invalidPath, []byte("match value { }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidOutput, invalidErr := exec.Command(secondCompiler, invalidPath, filepath.Join(dir, "invalid-output")).CombinedOutput()
	if invalidErr == nil {
		t.Fatalf("stage36 second-level compiler accepted invalid source: %s", invalidOutput)
	}
	if !strings.Contains(string(invalidOutput), "unsupported statement") {
		t.Fatalf("stage36 second-level compiler returned an unexpected invalid-source diagnostic (exit %v): %s", invalidErr, invalidOutput)
	}
}

func TestStage3SourceKIRCompilerMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_dynamic_stage3.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage3")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage3 source KIR compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage3 source KIR self-hosted output")
	assertNativeArtifact(t, want, "stage3 source KIR direct ELF oracle")
}

func TestStage3SourceKIRCompilerRejectsUnsupportedSyntax(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "unsupported.kry")
	outputPath := filepath.Join(dir, "unsupported-output")
	if err := os.WriteFile(inputPath, []byte("match value { }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{inputPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d == nil {
		t.Fatal("unsupported source syntax was accepted")
	} else if !strings.Contains(d.Message, "unsupported statement") {
		t.Fatalf("unexpected unsupported-syntax diagnostic: %s", d.Message)
	}
}

func TestStage5SourceFunctionFrontendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_scalar_functions_stage5.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-stage5")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage5 source function compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage5 source function self-hosted output")
	assertNativeArtifact(t, want, "stage5 source function direct ELF oracle")
}

func TestStage4KryndelFunctionBackendMatchesDirectELFOracle(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "function_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "functions.kir")
	outputPath := filepath.Join(dir, "functions-stage4")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("function backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage4 function self-hosted output")
	assertNativeArtifact(t, want, "stage4 function direct ELF oracle")
}

func TestStage4ScalarFunctionABIParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "scalar_function_output.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "scalar-functions.kir")
	outputPath := filepath.Join(dir, "scalar-functions-stage4")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("scalar function backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage4 scalar-function self-hosted output")
	assertNativeArtifact(t, want, "stage4 scalar-function direct ELF oracle")
}

func TestStage6ArrayRuntimeParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "array_runtime_stage6.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "arrays.kir")
	outputPath := filepath.Join(dir, "arrays-stage6")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("array runtime backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage6 array self-hosted output")
	assertNativeArtifact(t, want, "stage6 array direct ELF oracle")
}

func TestStage7OptionResultRuntimeParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "option_result_runtime_stage7.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "option-result.kir")
	outputPath := filepath.Join(dir, "option-result-stage7")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("Option/Result runtime backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage7 Option/Result self-hosted output")
	assertNativeArtifact(t, want, "stage7 Option/Result direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "option-result-stage7.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted Option/Result ELF failed to execute: %v", err)
		}
		if string(output) != "true\ntrue\n7\n41\ntrue\ntrue\n42\nbad\n99\n" {
			t.Fatalf("unexpected self-hosted Option/Result output %q", output)
		}
	}
}

func TestStage8SourceOptionResultFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_option_result_stage8.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-option-result-stage8")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage8 source Option/Result compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage8 source Option/Result self-hosted output")
	assertNativeArtifact(t, want, "stage8 source Option/Result direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-option-result-stage8.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source Option/Result ELF failed to execute: %v", err)
		}
		if string(output) != "true\ntrue\n7\n41\ntrue\ntrue\n42\nbad\n" {
			t.Fatalf("unexpected self-hosted source Option/Result output %q", output)
		}
	}
}

func TestStage9SourceArrayFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "array_runtime_stage6.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-array-stage9")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage9 source Array compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage9 source Array self-hosted output")
	assertNativeArtifact(t, want, "stage9 source Array direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-array-stage9.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source Array ELF failed to execute: %v", err)
		}
		if string(output) != "3\n20\n40\n7\n" {
			t.Fatalf("unexpected self-hosted source Array output %q", output)
		}
	}
}

func TestStage10SourceNestedGenericFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_nested_generics_stage10.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-nested-generics-stage10")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage10 source nested generic compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage10 source nested-generic self-hosted output")
	assertNativeArtifact(t, want, "stage10 source nested-generic direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-nested-generics-stage10.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source nested generic ELF failed to execute: %v", err)
		}
		if string(output) != "true\n8\ntrue\ntrue\n5\n" {
			t.Fatalf("unexpected self-hosted source nested generic output %q", output)
		}
	}
}

func TestStage11SourceLoopControlFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_loop_control_stage11.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-loop-control-stage11")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage11 source loop-control compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage11 source loop-control self-hosted output")
	assertNativeArtifact(t, want, "stage11 source loop-control direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-loop-control-stage11.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source loop-control ELF failed to execute: %v", err)
		}
		if string(output) != "1\n3\n" {
			t.Fatalf("unexpected self-hosted source loop-control output %q", output)
		}
	}
}

func TestStage12SourcePublicFunctionFrontendParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_public_function_stage12.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-public-function-stage12")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage12 source public-function compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage12 source public-function self-hosted output")
	assertNativeArtifact(t, want, "stage12 source public-function direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-public-function-stage12.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source public-function ELF failed to execute: %v", err)
		}
		if string(output) != "5\n" {
			t.Fatalf("unexpected self-hosted source public-function output %q", output)
		}
	}
}

func TestStage13SourceOpaqueABIFunctionParities(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "source_opaque_abi_stage13.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-opaque-abi-stage13")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage13 source opaque ABI compiler failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage13 source opaque-ABI self-hosted output")
	assertNativeArtifact(t, want, "stage13 source opaque-ABI direct ELF oracle")
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		runnable := filepath.Join(dir, "source-opaque-abi-stage13.run")
		if err := os.WriteFile(runnable, got, 0o700); err != nil {
			t.Fatal(err)
		}
		output, err := exec.Command(runnable).Output()
		if err != nil {
			t.Fatalf("self-hosted source opaque ABI ELF failed to execute: %v", err)
		}
		if len(output) != 0 {
			t.Fatalf("unexpected self-hosted source opaque ABI output %q", output)
		}
	}
}

func TestStage14DirectStructAndForParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_struct_loop_stage14.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-struct-loop-stage14")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage14 direct ELF failed to execute: %v", err)
	}
	if string(output) != "48\n" {
		t.Fatalf("unexpected stage14 direct ELF output %q", output)
	}
}

func TestStage15DirectHostIO(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_host_io_stage15.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.txt")
	output := filepath.Join(dir, "output.bin")
	rejected := filepath.Join(dir, "missing", "output.bin")
	if err := os.WriteFile(input, []byte("host-io"), 0o600); err != nil {
		t.Fatal(err)
	}
	runnable := filepath.Join(dir, "direct-host-io-stage15")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(runnable, input, output, rejected)
	outputText, err := command.Output()
	if err != nil {
		t.Fatalf("stage15 direct host I/O ELF failed to execute: %v", err)
	}
	if string(outputText) != "3\nhost-io\ntrue\nfalse\n" {
		t.Fatalf("unexpected stage15 direct host I/O output %q", outputText)
	}
	written, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "ABC" {
		t.Fatalf("unexpected stage15 written bytes %q", written)
	}
	if _, err := os.Stat(rejected); !os.IsNotExist(err) {
		t.Fatalf("rejected path was unexpectedly created: %v", err)
	}
}

func TestStage16KryndelBackendHostIOParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_host_io_stage15.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "host-io-stage15.kir")
	outputPath := filepath.Join(dir, "host-io-stage16")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage16 Kryndel backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage16 host I/O self-hosted output")
	assertNativeArtifact(t, want, "stage16 host I/O direct ELF oracle")
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("self-hosted ELF execution requires linux-amd64")
	}
	input := filepath.Join(dir, "input.txt")
	output := filepath.Join(dir, "output.bin")
	rejected := filepath.Join(dir, "missing", "output.bin")
	if err := os.WriteFile(input, []byte("host-io"), 0o600); err != nil {
		t.Fatal(err)
	}
	runnable := filepath.Join(dir, "kryndel-host-io-stage16")
	if err := os.WriteFile(runnable, got, 0o700); err != nil {
		t.Fatal(err)
	}
	outputText, err := exec.Command(runnable, input, output, rejected).Output()
	if err != nil {
		t.Fatalf("stage16 self-hosted ELF failed to execute: %v", err)
	}
	if string(outputText) != "3\nhost-io\ntrue\nfalse\n" {
		t.Fatalf("unexpected stage16 self-hosted ELF output %q", outputText)
	}
	written, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != "ABC" {
		t.Fatalf("unexpected stage16 self-hosted written bytes %q", written)
	}
	if _, err := os.Stat(rejected); !os.IsNotExist(err) {
		t.Fatalf("stage16 rejected path was unexpectedly created: %v", err)
	}
}

func TestStage17KryndelBackendStructForParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_struct_loop_stage14.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "struct-stage14.kir")
	outputPath := filepath.Join(dir, "struct-stage17")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage17 Kryndel backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage17 struct/for self-hosted output")
	assertNativeArtifact(t, want, "stage17 struct/for direct ELF oracle")
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("self-hosted ELF execution requires linux-amd64")
	}
	runnable := filepath.Join(dir, "kryndel-struct-stage17")
	if err := os.WriteFile(runnable, got, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage17 self-hosted ELF failed to execute: %v", err)
	}
	if string(output) != "48\n" {
		t.Fatalf("unexpected stage17 self-hosted ELF output %q", output)
	}
}

func TestStage18SourceFrontendStructForParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_struct_loop_stage14.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "source-frontend-stage18")
	r, d := NewRuntimeWithArgs(compilerProgram, compilerChecker, DefaultLimits(), Sandbox{}, []string{fixture, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage18 source frontend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage18 source frontend self-hosted output")
	assertNativeArtifact(t, want, "stage18 source frontend direct ELF oracle")
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("self-hosted ELF execution requires linux-amd64")
	}
	runnable := filepath.Join(dir, "source-frontend-stage18.run")
	if err := os.WriteFile(runnable, got, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage18 source frontend ELF failed to execute: %v", err)
	}
	if string(output) != "48\n" {
		t.Fatalf("unexpected stage18 source frontend output %q", output)
	}
}

func TestStage19DirectStringPredicates(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_string_predicates_stage19.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-string-predicates-stage19")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage19 direct string predicate ELF failed: %v", err)
	}
	if string(output) != "string-predicates\n" {
		t.Fatalf("unexpected stage19 string predicate output %q", output)
	}
}

func TestStage20DirectStringChars(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_string_chars_stage20.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-string-chars-stage20")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage20 direct string_chars ELF failed: %v", err)
	}
	if string(output) != "3\na\né\n🙂\n" {
		t.Fatalf("unexpected stage20 string_chars output %q", output)
	}
}

func TestStage20DynamicStringCharsArena(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	selfhost := filepath.Join(root, "..", "..", "selfhost")
	fixture := filepath.Join(selfhost, "fixtures", "direct_string_chars_stage20.kry")
	backend := filepath.Join(selfhost, "kir_backend.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	kir, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "string-chars.kir")
	outputPath := filepath.Join(dir, "dynamic-string-chars")
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("dynamic string_chars backend failed: %s", d.Message)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("dynamic string_chars execution requires linux-amd64")
	}
	if err := os.Chmod(outputPath, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("dynamic string_chars ELF failed: %v; output: %s", err, output)
	}
	if string(output) != "3\na\né\n🙂\n" {
		t.Fatalf("unexpected dynamic string_chars output %q", output)
	}
}

func TestStage21DirectMapRuntime(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_map_stage21.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-map-stage21")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage21 direct map ELF failed: %v", err)
	}
	if string(output) != "dos\nfallback\n" {
		t.Fatalf("unexpected stage21 map output %q", output)
	}
}

func TestStage22DirectSubstring(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_substring_stage22.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-substring-stage22")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage22 direct substring ELF failed: %v", err)
	}
	if string(output) != "true\né🙂\n" {
		t.Fatalf("unexpected stage22 substring output %q", output)
	}
}

func TestStage23DirectStr(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_str_stage23.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-str-stage23")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage23 direct str ELF failed: %v", err)
	}
	if string(output) != "-42\n0\n255\ntrue\nfalse\nhé\n" {
		t.Fatalf("unexpected stage23 str output %q", output)
	}
}

func TestStage23KryndelDynamicStrParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_str_stage23.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "direct-str-stage23.kir")
	outputPath := filepath.Join(dir, "direct-str-stage23-selfhost")
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, DefaultLimits(), Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage23 Kryndel dynamic backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage23 dynamic self-hosted output")
	assertNativeArtifact(t, want, "stage23 dynamic direct ELF oracle")
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("self-hosted ELF execution requires linux-amd64")
	}
	runnable := filepath.Join(dir, "direct-str-stage23-selfhost.run")
	if err := os.WriteFile(runnable, got, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage23 self-hosted ELF failed to execute: %v", err)
	}
	if string(output) != "-42\n0\n255\ntrue\nfalse\nhé\n" {
		t.Fatalf("unexpected stage23 self-hosted ELF output %q", output)
	}
}

func TestStage24DirectInt(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_int_stage24.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-int-stage24")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage24 direct int ELF failed to execute: %v", err)
	}
	want := "-42\n17\n0\n9223372036854775807\n-9223372036854775808\n255\n1\n0\n"
	if string(output) != want {
		t.Fatalf("unexpected stage24 direct int output %q", output)
	}
}

func TestStage24KryndelDynamicIntParity(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_int_stage24.kry")
	backend := filepath.Join(root, "..", "..", "selfhost", "kir_backend.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	fixtureChecker, d := Check(fixtureProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	backendProgram, d := LoadProgram(backend, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	backendChecker, d := Check(backendProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	dir := t.TempDir()
	kirPath := filepath.Join(dir, "direct-int-stage24.kir")
	outputPath := filepath.Join(dir, "direct-int-stage24-selfhost")
	kir, err := EmitKIR(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kirPath, kir, 0o600); err != nil {
		t.Fatal(err)
	}
	backendLimits := DefaultLimits()
	// Stage 24 emits a complete decimal parser as Kryndel source. Its
	// interpreter bootstrap is intentionally bounded, but needs more than the
	// ordinary 10-second application budget on slower developer machines.
	backendLimits.MaxWallTimeMS = 60_000
	r, d := NewRuntimeWithArgs(backendProgram, backendChecker, backendLimits, Sandbox{}, []string{kirPath, outputPath})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("stage24 Kryndel dynamic backend failed: %s", d.Message)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BuildDirectELF(fixtureProgram, fixtureChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeArtifact(t, got, "stage24 Int self-hosted output")
	assertNativeArtifact(t, want, "stage24 Int direct ELF oracle")
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("self-hosted ELF execution requires linux-amd64")
	}
	runnable := filepath.Join(dir, "direct-int-stage24-selfhost.run")
	if err := os.WriteFile(runnable, got, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage24 self-hosted ELF failed to execute: %v", err)
	}
	wantOutput := "-42\n17\n0\n9223372036854775807\n-9223372036854775808\n255\n1\n0\n"
	if string(output) != wantOutput {
		t.Fatalf("unexpected stage24 self-hosted ELF output %q", output)
	}
}

func TestStage24IntRejectsInvalidInput(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_int_invalid_stage24.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-int-invalid-stage24")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(runnable).Run(); err == nil {
		t.Fatal("invalid decimal input unexpectedly succeeded")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("invalid decimal input exited incorrectly: %v", err)
	}
}

func TestStage25DirectJSON(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_json_parse_stage25.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-json-stage25")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage25 direct JSON ELF failed to execute: %v", err)
	}
	want := "true\ntrue\ntrue\ntrue\nfalse\nfalse\nobject\narray\nstring\nbool\nnull\nnumber\narray\nobject\ntrue\nfalse\n"
	if string(output) != want {
		t.Fatalf("unexpected stage25 direct JSON output %q", output)
	}
}

func TestStage26DirectJSONString(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_json_string_stage26.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-json-string-stage26")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage26 direct JSON string ELF failed to execute: %v", err)
	}
	want := "hello\n[a\tb]\n[a\\b]\n[a/b]\nfalse\nfalse\nfalse\n"
	if string(output) != want {
		t.Fatalf("unexpected stage26 direct JSON string output %q", output)
	}
}

func TestStage27DirectJSONInt(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_json_int_stage27.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-json-int-stage27")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage27 direct JSON int ELF failed to execute: %v", err)
	}
	want := "0\n0\n42\n-42\n9223372036854775807\n-9223372036854775808\n-7\nfalse\nfalse\nfalse\nfalse\nfalse\nfalse\n"
	if string(output) != want {
		t.Fatalf("unexpected stage27 direct JSON int output %q", output)
	}
}

func TestStage28DirectU8Array(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_u8_array_roundtrip_stage28.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-u8-array-stage28")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage28 direct u8_array ELF failed to execute: %v", err)
	}
	if string(output) != "3\n65\n66\n67\n" {
		t.Fatalf("unexpected stage28 direct u8_array output %q", output)
	}
}

func TestStage28DirectFunctionAndArraySlice(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_json_array_stage28.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-function-array-slice-stage28")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage28 direct function/array ELF failed to execute: %v", err)
	}
	if string(output) != "expr\n2\n2\n3\n" {
		t.Fatalf("unexpected stage28 function/array output %q", output)
	}
}

func TestStage28DirectJSONStringToU8Array(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "direct_json_string_bytes_stage28.kry")
	program, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	data, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("direct ELF execution requires linux-amd64")
	}
	dir := t.TempDir()
	runnable := filepath.Join(dir, "direct-json-string-u8-stage28")
	if err := os.WriteFile(runnable, data, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(runnable).Output()
	if err != nil {
		t.Fatalf("stage28 direct JSON string/u8 ELF failed to execute: %v", err)
	}
	if string(output) != "5\n104\n101\n111\n" {
		t.Fatalf("unexpected stage28 JSON string/u8 output %q", output)
	}
}

func TestStage28SourceCompilerBootstrap(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "bootstrap_hello_stage27.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d := Check(fixtureProgram, DefaultLimits()); d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerELF, err := BuildDirectELF(compilerProgram, compilerChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("source compiler bootstrap execution requires linux-amd64")
	}
	dir := t.TempDir()
	compilerPath := filepath.Join(dir, "source-kir-compiler")
	outputPath := filepath.Join(dir, "bootstrap-output")
	if err := os.WriteFile(compilerPath, compilerELF, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(compilerPath, fixture, outputPath).Run(); err != nil {
		t.Fatalf("stage28 generated source compiler failed: %v", err)
	}
	generated, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectNative(generated); err != nil {
		t.Fatalf("stage28 generated bootstrap is not a valid native artifact: %v", err)
	}
	generatedPath := filepath.Join(dir, "bootstrap-output.run")
	if err := os.WriteFile(generatedPath, generated, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(generatedPath).Output()
	if err != nil {
		t.Fatalf("stage28 generated bootstrap failed to execute: %v", err)
	}
	if string(output) != "hello from bootstrap\n" {
		t.Fatalf("unexpected stage28 bootstrap output %q", output)
	}
}
