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

// The Linux bootstrap job sets this to a PE emitted by its Stage 2 compiler.
// This verifies the cross-job artifact on native Windows; it does not imply
// that the Stage 2 or Stage 3 ELF compiler can run as a native Windows program.
func TestStage2KryndelGeneratedPEExecutesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("the Stage 2 PE artifact is executed by the Windows amd64 CI job")
	}
	path := os.Getenv("KRY_STAGE36_WINDOWS_PE_INPUT")
	if path == "" {
		t.Skip("set KRY_STAGE36_WINDOWS_PE_INPUT to the PE emitted by the Stage 2 bootstrap test")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve Stage 2 PE artifact path: %v", err)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Stage 2 PE artifact %q: %v", path, err)
	}
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Stage 2 artifact is not a valid PE: %v", err)
	}
	if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("Stage 2 artifact machine is %#x, want amd64", file.Machine)
	}
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("Stage 2 artifact is PE32, want PE32+")
	}
	if header.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_CUI {
		t.Fatalf("Stage 2 artifact subsystem is %d, want Windows console", header.Subsystem)
	}

	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("Stage 2 Kryndel compiler's PE failed on native Windows: %v; output: %s", err, output)
	}
	want := []byte("Mode::Ready\n42\n")
	if !bytes.Equal(output, want) {
		t.Fatalf("Stage 2 PE output = %q, want %q", output, want)
	}
	t.Log("Stage 2 Kryndel compiler emitted this PE with PATH pointing to an empty directory; Windows amd64 executed it successfully")
}

func TestStage2KryndelGeneratedFFIPEExecutesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("the Stage 2 FFI PE artifact is executed by the Windows amd64 CI job")
	}
	path := os.Getenv("KRY_STAGE39_FFI_PE_INPUT")
	if path == "" {
		t.Skip("set KRY_STAGE39_FFI_PE_INPUT to the FFI PE emitted by the Stage 2 bootstrap")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve Stage 2 FFI PE artifact path: %v", err)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Stage 2 FFI PE artifact %q: %v", path, err)
	}
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("Stage 2 FFI artifact is not a valid PE: %v", err)
	}
	if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("Stage 2 FFI artifact machine is %#x, want amd64", file.Machine)
	}
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		t.Fatal("Stage 2 FFI artifact is PE32, want PE32+")
	}
	if header.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_CUI {
		t.Fatalf("Stage 2 FFI artifact subsystem is %d, want Windows console", header.Subsystem)
	}
	imports, err := file.ImportedSymbols()
	file.Close()
	if err != nil {
		t.Fatalf("read Stage 2 FFI PE imports: %v", err)
	}
	for _, want := range []string{"LoadLibraryA:KERNEL32.dll", "GetProcAddress:KERNEL32.dll", "FreeLibrary:KERNEL32.dll", "HeapFree:KERNEL32.dll"} {
		if !containsString(imports, want) {
			t.Errorf("Stage 2 FFI PE does not import %q; imports: %v", want, imports)
		}
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("Stage 2 FFI PE failed on native Windows: %v; output: %q", err, output)
	}
	lines := strings.Fields(string(output))
	want := []string{"true", "true", "true"}
	if len(lines) != len(want) {
		t.Fatalf("unexpected Stage 2 FFI PE output %q", output)
	}
	for index, value := range want {
		if lines[index] != value {
			t.Fatalf("Stage 2 FFI PE output line %d is %q, want %q; full output %q", index+1, lines[index], value, output)
		}
	}
	t.Log("Stage 2 generated and ran its FFI PE with an empty PATH; no Go or C compiler was available to the compiler process")
}
