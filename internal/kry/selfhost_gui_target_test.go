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

func TestSelfhostSourceCompilerPropagatesWindowsGUIPETarget(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	compilerPath := filepath.Join(root, "..", "..", "selfhost", "source_kir_compiler.kry")
	compiler, diagnostic := LoadProgram(compilerPath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load source compiler: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(compiler, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("check source compiler: %s", diagnostic.Message)
	}
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "main.kry")
	if err := os.WriteFile(sourcePath, []byte("fn main() -> Nil {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	limits := DefaultLimits()
	limits.MaxWallTimeMS = 180_000
	for _, target := range []struct {
		name      string
		arg       string
		subsystem uint16
	}{
		{name: "GUI", arg: "windows-amd64-gui", subsystem: pe.IMAGE_SUBSYSTEM_WINDOWS_GUI},
		{name: "console", arg: "windows-amd64", subsystem: pe.IMAGE_SUBSYSTEM_WINDOWS_CUI},
	} {
		t.Run(target.name, func(t *testing.T) {
			outputPath := filepath.Join(dir, strings.ToLower(target.name)+".exe")
			r, diagnostic := NewRuntimeWithArgs(compiler, checker, limits, Sandbox{}, []string{sourcePath, outputPath, target.arg})
			if diagnostic != nil {
				t.Fatalf("create source compiler runtime: %s", diagnostic.Message)
			}
			if diagnostic := r.run(); diagnostic != nil {
				t.Fatalf("compile source for %s: %v", target.arg, diagnostic)
			}
			image, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			file, err := pe.NewFile(bytes.NewReader(image))
			if err != nil {
				t.Fatalf("parse generated PE: %v", err)
			}
			defer file.Close()
			optional, ok := file.OptionalHeader.(*pe.OptionalHeader64)
			if !ok {
				t.Fatalf("optional header has type %T, want PE32+", file.OptionalHeader)
			}
			if optional.Subsystem != target.subsystem {
				t.Fatalf("PE subsystem = %d, want %d", optional.Subsystem, target.subsystem)
			}
			text := file.Section(".text")
			if text == nil || optional.AddressOfEntryPoint < text.VirtualAddress || optional.AddressOfEntryPoint >= text.VirtualAddress+text.VirtualSize {
				t.Fatalf("entrypoint RVA %#x does not resolve inside .text", optional.AddressOfEntryPoint)
			}
			if text.Characteristics&(pe.IMAGE_SCN_CNT_CODE|pe.IMAGE_SCN_MEM_EXECUTE) != pe.IMAGE_SCN_CNT_CODE|pe.IMAGE_SCN_MEM_EXECUTE {
				t.Fatalf(".text characteristics %#x do not mark executable code", text.Characteristics)
			}
			imports, err := file.ImportedSymbols()
			if err != nil {
				t.Fatalf("read generated PE imports: %v", err)
			}
			foundExitProcess := false
			for _, symbol := range imports {
				if symbol == "ExitProcess:KERNEL32.dll" {
					foundExitProcess = true
					break
				}
			}
			if !foundExitProcess {
				t.Fatalf("generated PE does not import ExitProcess: %v", imports)
			}
			if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
				if output, err := exec.Command(outputPath).CombinedOutput(); err != nil {
					t.Fatalf("run generated %s PE: %v; output: %s", target.name, err, output)
				}
			}
		})
	}
}
