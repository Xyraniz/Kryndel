package kry

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"fmt"
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

func runStage37ModuleTypeFixture(t *testing.T, compiler, label string) {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.kry")
	geometryPath := filepath.Join(dir, "geometry.kry")
	modesPath := filepath.Join(dir, "modes.kry")
	outputPath := filepath.Join(dir, "module-types")
	files := map[string]string{
		mainPath: `import "geometry"
import "modes"

fn main() -> Nil {
    let point: Point = translate(Point{x: 40, y: 1})
    println(score(Mode::Ready, point.x + point.y))
}
`,
		geometryPath: `pub struct Point { x: Int, y: Int }

pub fn translate(point: Point) -> Point {
    return Point{x: point.x + 1, y: point.y}
}
`,
		modesPath: `pub enum Mode { Idle, Ready }

pub fn score(mode: Mode, value: Int) -> Int {
    if mode == Mode::Ready { return value }
    return 0
}
`,
	}
	for path, source := range files {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compiledOutput, err := exec.Command(compiler, mainPath, outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("%s compiler rejected imported struct/enum fixture: %v; output: %s", label, err, compiledOutput)
	}
	if err := os.Chmod(outputPath, 0o700); err != nil {
		t.Fatalf("make %s compiler's imported struct/enum program executable: %v", label, err)
	}
	programOutput, err := exec.Command(outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("%s compiler's imported struct/enum program failed: %v; output: %s", label, err, programOutput)
	}
	if string(programOutput) != "42\n" {
		t.Fatalf("%s compiler's imported struct/enum output = %q, want %q", label, programOutput, "42\n")
	}
}

type bootstrapLock struct {
	SchemaVersion             int               `json:"schema_version"`
	SourceRevision            string            `json:"source_revision"`
	Target                    string            `json:"target"`
	HostGo                    string            `json:"host_go"`
	Stage0Build               string            `json:"stage0_build"`
	SourceCompilerKIRBytes    int               `json:"source_compiler_kir_bytes"`
	SourceCompilerKIRMaxBytes int               `json:"source_compiler_kir_max_bytes"`
	Command                   string            `json:"command"`
	SHA256                    map[string]string `json:"sha256"`
}

func loadBootstrapLock(t *testing.T, path string) bootstrapLock {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read bootstrap lock %q: %v", path, err)
	}
	var lock bootstrapLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("decode bootstrap lock %q: %v", path, err)
	}
	if lock.SchemaVersion != 3 || lock.Target != "linux-amd64" || lock.HostGo != "go1.26.0" || lock.SourceRevision == "" || lock.Stage0Build == "" || lock.SourceCompilerKIRBytes <= 0 || lock.SourceCompilerKIRMaxBytes <= lock.SourceCompilerKIRBytes || lock.Command == "" {
		t.Fatalf("invalid bootstrap lock metadata: %#v", lock)
	}
	return lock
}

func verifyBootstrapHash(t *testing.T, lock bootstrapLock, seen map[string]bool, name string, data []byte) {
	t.Helper()
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	expected, ok := lock.SHA256[name]
	if !ok {
		t.Fatalf("bootstrap lock has no SHA-256 for %q", name)
	}
	if actual != expected {
		t.Fatalf("bootstrap lock mismatch for %s: got %s, want %s", name, actual, expected)
	}
	seen[name] = true
	t.Logf("bootstrap sha256 %s=%s", name, actual)
}

func verifyBootstrapSourceRevision(t *testing.T, root string, lock bootstrapLock) {
	t.Helper()
	cmd := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", lock.SourceRevision, "HEAD")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap source revision %q is not an ancestor of the checkout: %v; %s", lock.SourceRevision, err, output)
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
		limit := len(got)
		if len(oracle) < limit {
			limit = len(oracle)
		}
		offset := 0
		for offset < limit && got[offset] == oracle[offset] {
			offset++
		}
		gotByte, oracleByte := "<eof>", "<eof>"
		if offset < len(got) {
			gotByte = fmt.Sprintf("%02x", got[offset])
		}
		if offset < len(oracle) {
			oracleByte = fmt.Sprintf("%02x", oracle[offset])
		}
		t.Fatalf("stage34 dynamic unary-not ELF differs from direct backend: lengths=%d/%d first_difference=%d got=%s oracle=%s", len(got), len(oracle), offset, gotByte, oracleByte)
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
	t.Logf("bootstrap host Go version=%s target=linux-amd64", runtime.Version())
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("Stage 3 bootstrap hashes are target-specific; run the locked bootstrap on linux-amd64")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	selfhost := filepath.Join(root, "..", "..", "selfhost")
	lock := loadBootstrapLock(t, filepath.Join(selfhost, "bootstrap.lock.json"))
	if runtime.Version() != lock.HostGo {
		if os.Getenv("KRY_REQUIRE_LOCKED_BOOTSTRAP") == "1" {
			t.Fatalf("bootstrap lock requires host %s, got %s", lock.HostGo, runtime.Version())
		}
		t.Skipf("locked bootstrap requires %s; this compatibility run uses %s", lock.HostGo, runtime.Version())
	}
	verifiedHashes := make(map[string]bool, len(lock.SHA256))
	verifyBootstrapSourceRevision(t, root, lock)
	const stage0Build = "CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o <tmp>/kry ./cmd/kry"
	if lock.Stage0Build != stage0Build {
		t.Fatalf("bootstrap lock stage0_build=%q, want %q", lock.Stage0Build, stage0Build)
	}
	stage0Path := filepath.Join(t.TempDir(), "kry")
	buildStage0 := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", stage0Path, "./cmd/kry")
	buildStage0.Dir = filepath.Clean(filepath.Join(root, "..", ".."))
	buildStage0.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := buildStage0.CombinedOutput(); err != nil {
		t.Fatalf("build locked Stage 0 host compiler: %v; output: %s", err, output)
	}
	stage0Binary, err := os.ReadFile(stage0Path)
	if err != nil {
		t.Fatalf("read built Stage 0 host compiler: %v", err)
	}
	verifyBootstrapHash(t, lock, verifiedHashes, "stage0-host-kry-linux-amd64", stage0Binary)
	if err := os.Chmod(stage0Path, 0o700); err != nil {
		t.Fatal(err)
	}
	compilerPath := filepath.Join(selfhost, "source_kir_compiler.kry")
	backendPath := filepath.Join(selfhost, "kir_backend.kry")
	fixturePath := filepath.Join(selfhost, "fixtures", "bootstrap_hello_stage27.kry")
	invalidPath := filepath.Join(t.TempDir(), "invalid-source.kry")
	kirBackendSource, err := os.ReadFile(backendPath)
	if err != nil {
		t.Fatal(err)
	}
	kirBackendText := strings.ReplaceAll(string(kirBackendSource), "\r\n", "\n")
	verifyBootstrapHash(t, lock, verifiedHashes, "kir_backend.kry", []byte(kirBackendText))

	dir := t.TempDir()
	kirFile := filepath.Join(dir, "source-compiler.kir")
	emitKIR := exec.Command(stage0Path, "emit", compilerPath, "--target=linux-x64", "--format=kry-ir", "-o", kirFile)
	if output, err := emitKIR.CombinedOutput(); err != nil {
		t.Fatalf("Stage 0 failed to emit source compiler KIR: %v; output: %s", err, output)
	}
	kir, err := os.ReadFile(kirFile)
	if err != nil {
		t.Fatalf("Stage 0 did not write source compiler KIR: %v", err)
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
	if len(kir) > lock.SourceCompilerKIRMaxBytes {
		t.Fatalf("source compiler KIR is %d bytes, exceeding locked bootstrap limit=%d", len(kir), lock.SourceCompilerKIRMaxBytes)
	}
	if len(kir) != lock.SourceCompilerKIRBytes {
		t.Fatalf("source compiler KIR is %d bytes, bootstrap lock records %d", len(kir), lock.SourceCompilerKIRBytes)
	}
	t.Logf("source compiler KIR size=%d bytes; bootstrap JSON limit=%d bytes; headroom=%d bytes", len(kir), lock.SourceCompilerKIRMaxBytes, lock.SourceCompilerKIRMaxBytes-len(kir))
	verifyBootstrapHash(t, lock, verifiedHashes, "source-compiler.kir", kir)

	generatedCompiler := filepath.Join(dir, "source-kir-compiler")
	maxWallMS := "720000"
	maxInstructions := "100000000"
	if os.Getenv("KRY_RACE") == "1" {
		// The race instrumented interpreter is substantially slower during
		// the large bootstrap, but it must still exercise the same checks.
		maxWallMS = "1800000"
		maxInstructions = "250000000"
	}
	bootstrapJSONLimit := fmt.Sprint(lock.SourceCompilerKIRMaxBytes)
	runBackend := exec.Command(stage0Path, "--max-json", bootstrapJSONLimit, "--max-instructions", maxInstructions, "--max-wall-ms", maxWallMS, "run", backendPath, kirFile, generatedCompiler)
	if output, err := runBackend.CombinedOutput(); err != nil {
		t.Fatalf("Stage 0 failed to run kir_backend.kry on source compiler KIR: %v; output: %s", err, output)
	}
	t.Logf("stage35 generated compiler ELF from %d-byte KIR", len(kir))
	compilerELF, err := os.ReadFile(generatedCompiler)
	if err != nil {
		t.Fatalf("stage35 dynamic backend did not write compiler ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, compilerELF, "stage35 generated source compiler")
	verifyBootstrapHash(t, lock, verifiedHashes, "stage1-source-kir-compiler.elf", compilerELF)
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
	runStage37ModuleTypeFixture(t, generatedCompiler, "stage1")

	frontendSource, err := os.ReadFile(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	frontendText := strings.ReplaceAll(string(frontendSource), "\r\n", "\n")
	verifyBootstrapHash(t, lock, verifiedHashes, "source_kir_compiler.kry", []byte(frontendText))
	const backendImport = "import \"dynamic_backend\"\n"
	if !strings.HasPrefix(frontendText, backendImport) {
		t.Fatalf("source compiler no longer starts with expected backend import %q", backendImport)
	}
	backendSource, err := os.ReadFile(filepath.Join(selfhost, "dynamic_backend.kry"))
	if err != nil {
		t.Fatal(err)
	}
	backendText := strings.ReplaceAll(string(backendSource), "\r\n", "\n")
	verifyBootstrapHash(t, lock, verifiedHashes, "dynamic_backend.kry", []byte(backendText))
	const backendImports = "import \"elf_backend\"\nimport \"pe_backend\"\n"
	if !strings.HasPrefix(backendText, backendImports) {
		t.Fatalf("dynamic backend no longer starts with expected backend imports %q", backendImports)
	}
	elfSource, err := os.ReadFile(filepath.Join(selfhost, "elf_backend.kry"))
	if err != nil {
		t.Fatal(err)
	}
	elfText := strings.ReplaceAll(string(elfSource), "\r\n", "\n")
	verifyBootstrapHash(t, lock, verifiedHashes, "elf_backend.kry", []byte(elfText))
	peSource, err := os.ReadFile(filepath.Join(selfhost, "pe_backend.kry"))
	if err != nil {
		t.Fatal(err)
	}
	peText := strings.ReplaceAll(string(peSource), "\r\n", "\n")
	verifyBootstrapHash(t, lock, verifiedHashes, "pe_backend.kry", []byte(peText))
	fixtureSource, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	fixtureText := strings.ReplaceAll(string(fixtureSource), "\r\n", "\n")
	verifyBootstrapHash(t, lock, verifiedHashes, "bootstrap-fixture.kry", []byte(fixtureText))
	t.Log("stage36 compiling the checked-in compiler module graph through its import resolver")
	secondCompiler := filepath.Join(dir, "second-source-kir-compiler")
	secondCompilerOutput, err := exec.Command(generatedCompiler, compilerPath, secondCompiler).CombinedOutput()
	if err != nil {
		t.Fatalf("stage36 generated compiler failed to compile the bundled frontend/backend source: %v; output: %s", err, secondCompilerOutput)
	}
	t.Log("stage36 generated second-level compiler ELF")
	secondCompilerELF, err := os.ReadFile(secondCompiler)
	if err != nil {
		t.Fatalf("stage36 generated compiler did not write second-level compiler ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, secondCompilerELF, "stage36 second-level source compiler")
	verifyBootstrapHash(t, lock, verifiedHashes, "stage2-source-kir-compiler.elf", secondCompilerELF)
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
	runStage37ModuleTypeFixture(t, secondCompiler, "stage2")

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

	geometrySource := filepath.Join(dir, "geometry.kry")
	geometry := `pub struct Point { x: Int, y: Int }
pub fn translate(point: Point, delta: Int) -> Point {
    return Point { x: point.x + delta, y: point.y - delta }
}
`
	if err := os.WriteFile(geometrySource, []byte(geometry), 0o600); err != nil {
		t.Fatal(err)
	}
	windowsSource := filepath.Join(dir, "windows-program.kry")
	program := `import "geometry"
fn main() -> Nil {
    let point = translate(Point { x: 40, y: 2 }, 2)
    println(point.x + point.y)
}
`
	if err := os.WriteFile(windowsSource, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	windowsPEPath := filepath.Join(dir, "windows-program.exe")
	windowsPEOutput, err := exec.Command(secondCompiler, windowsSource, windowsPEPath, "windows-amd64").CombinedOutput()
	if err != nil {
		t.Fatalf("stage36 second-level compiler failed to emit Windows PE: %v; output: %s", err, windowsPEOutput)
	}
	windowsPEBytes, err := os.ReadFile(windowsPEPath)
	if err != nil {
		t.Fatalf("stage36 second-level compiler did not write Windows PE: %v", err)
	}
	windowsPE, err := pe.NewFile(bytes.NewReader(windowsPEBytes))
	if err != nil {
		t.Fatalf("stage36 second-level compiler emitted invalid PE: %v", err)
	}
	if windowsPE.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("stage36 second-level compiler emitted PE machine %#x, want amd64", windowsPE.Machine)
	}
	if _, ok := windowsPE.OptionalHeader.(*pe.OptionalHeader64); !ok {
		t.Fatal("stage36 second-level compiler emitted PE32, want PE32+")
	}
	t.Log("stage36 second-level compiler emitted a valid Windows amd64 PE32+ executable")
	if artifactPath := os.Getenv("KRY_STAGE36_WINDOWS_PE_OUTPUT"); artifactPath != "" {
		if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
			t.Fatalf("create Stage36 Windows PE artifact directory: %v", err)
		}
		if err := os.WriteFile(artifactPath, windowsPEBytes, 0o700); err != nil {
			t.Fatalf("write Stage36 Windows PE artifact: %v", err)
		}
		t.Logf("saved Stage2-generated Windows PE for native execution: %s", artifactPath)
	}

	thirdCompiler := filepath.Join(dir, "third-source-kir-compiler")
	thirdCompilerOutput, err := exec.Command(secondCompiler, compilerPath, thirdCompiler).CombinedOutput()
	if err != nil {
		t.Fatalf("stage3 second-level compiler failed to rebuild itself from the bundled sources: %v; output: %s", err, thirdCompilerOutput)
	}
	thirdCompilerELF, err := os.ReadFile(thirdCompiler)
	if err != nil {
		t.Fatalf("stage3 compiler did not write its rebuilt ELF: %v", err)
	}
	assertLinuxAMD64ELF(t, thirdCompilerELF, "stage3 self-rebuilt source compiler")
	verifyBootstrapHash(t, lock, verifiedHashes, "stage3-source-kir-compiler.elf", thirdCompilerELF)
	if !bytes.Equal(secondCompilerELF, thirdCompilerELF) {
		t.Fatalf("stage3 self-rebuild was not byte-reproducible: Stage 2 sha256=%x Stage 3 sha256=%x", sha256.Sum256(secondCompilerELF), sha256.Sum256(thirdCompilerELF))
	}
	if len(verifiedHashes) != len(lock.SHA256) {
		t.Fatalf("verified %d bootstrap hashes, but lock contains %d", len(verifiedHashes), len(lock.SHA256))
	}
	t.Logf("stage3 second-level compiler rebuilt itself byte-for-byte; sha256=%x", sha256.Sum256(thirdCompilerELF))
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
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("source compiler ELF execution requires linux-amd64")
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "..", "..", "selfhost", "fixtures", "bootstrap_hello_stage27.kry")
	compiler := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	bootstrapLock := loadBootstrapLock(t, filepath.Join(root, "..", "..", "selfhost", "bootstrap.lock.json"))
	compilerLimits := DefaultLimits()
	compilerLimits.MaxArtifactBytes = bootstrapLock.SourceCompilerKIRMaxBytes
	compilerLimits.MaxJSONBytes = bootstrapLock.SourceCompilerKIRMaxBytes
	fixtureProgram, d := LoadProgram(fixture, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d := Check(fixtureProgram, DefaultLimits()); d != nil {
		t.Fatal(d.Message)
	}
	compilerProgram, d := LoadProgram(compiler, compilerLimits, "")
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerChecker, d := Check(compilerProgram, compilerLimits)
	if d != nil {
		t.Fatal(d.Message)
	}
	compilerELF, err := BuildDirectELF(compilerProgram, compilerChecker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
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
