package kry

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func BenchmarkNativeBackendCorpus(b *testing.B) {
	for _, name := range []string{"scalar_loop", "array_sum"} {
		b.Run(name, func(b *testing.B) {
			benchmarkNativeProgram(b, name)
		})
	}
}

func benchmarkNativeProgram(b *testing.B, name string) {
	b.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		b.Fatal(err)
	}
	base := filepath.Join(root, "benchmarks", "native", name)
	sourcePath, rustPath := base+".kry", base+".rs"
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		b.Fatal(err)
	}
	program, diagnostic := Parse(&Source{Name: sourcePath, Text: string(source)}, DefaultLimits())
	if diagnostic != nil {
		b.Fatalf("parse %s: %s", name, diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		b.Fatalf("check %s: %s", name, diagnostic.Message)
	}
	products := make([]benchmarkProduct, 0, 5)
	cli := buildBenchmarkCLI(b, root)
	products = append(products, benchmarkProduct{name: "interpreter", path: cli, args: []string{"run", sourcePath}})
	b.Logf("go=%s target=%s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
	format := map[string]string{"linux": "elf", "windows": "exe", "darwin": "macho"}[runtime.GOOS]
	if format != "" {
		if !optionResultCompilerAvailable(target) {
			b.Logf("C AOT comparison skipped: no compiler for %s/%s", target.OS, target.Arch)
		} else if native, err := BuildNative(program, checker, target, format); err == nil {
			products = append(products, writeBenchmarkProduct(b, name+"-c-aot", native, target.OS == "windows"))
		} else {
			b.Fatalf("build C AOT benchmark %s: %v", name, err)
		}
	}
	if target.Arch == "amd64" {
		var native []byte
		switch target.OS {
		case "linux":
			native, err = BuildDirectELF(program, checker, target)
		case "windows":
			native, err = BuildDirectPE(program, checker, target)
		}
		if err == nil && len(native) != 0 {
			products = append(products, writeBenchmarkProduct(b, name+"-direct", native, target.OS == "windows"))
		} else if target.OS == "linux" || target.OS == "windows" {
			b.Logf("direct backend skips %s: %v", name, err)
		}
	}
	if rustc, err := exec.LookPath("rustc"); err == nil {
		if version, err := exec.Command(rustc, "--version").Output(); err == nil {
			b.Logf("rustc=%s", strings.TrimSpace(string(version)))
		}
		extension := ""
		if runtime.GOOS == "windows" {
			extension = ".exe"
		}
		rustBinary := filepath.Join(b.TempDir(), "rust"+extension)
		compile := exec.Command(rustc, "-C", "opt-level=3", "-o", rustBinary, rustPath)
		if output, err := compile.CombinedOutput(); err != nil {
			b.Fatalf("compile Rust benchmark %s: %v; %s", name, err, output)
		}
		products = append(products, benchmarkProduct{name: "rust", path: rustBinary})
	} else {
		b.Logf("Rust comparison skipped: rustc unavailable: %v", err)
	}
	if len(products) < 2 {
		b.Fatal("benchmark requires the interpreter and at least one comparison backend")
	}

	var expected string
	for i, product := range products {
		output, status, err := runBenchmarkProduct(product)
		if err != nil || status != 0 {
			b.Fatalf("%s benchmark setup failed: status=%d err=%v output=%q", product.name, status, err, output)
		}
		if i == 0 {
			expected = output
		} else if output != expected {
			b.Fatalf("%s benchmark output differs: interpreter=%q backend=%q", name, expected, output)
		}
	}
	b.Logf("verified output=%q; target=%s/%s", expected, target.OS, target.Arch)

	for _, product := range products {
		b.Run(product.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				command := exec.Command(product.path, product.args...)
				command.Stdout, command.Stderr = io.Discard, io.Discard
				if err := command.Run(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type benchmarkProduct struct {
	name string
	path string
	args []string
}

func buildBenchmarkCLI(b *testing.B, root string) string {
	b.Helper()
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	path := filepath.Join(b.TempDir(), "kry"+extension)
	command := exec.Command("go", "build", "-o", path, "./cmd/kry")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		b.Fatalf("build interpreter CLI for benchmark: %v; %s", err, output)
	}
	return path
}

func writeBenchmarkProduct(b *testing.B, name string, native []byte, windows bool) benchmarkProduct {
	b.Helper()
	extension := ""
	if windows {
		extension = ".exe"
	}
	path := filepath.Join(b.TempDir(), name+extension)
	if err := os.WriteFile(path, native, 0o700); err != nil {
		b.Fatalf("write %s benchmark executable: %v", name, err)
	}
	return benchmarkProduct{name: name, path: path}
}

func runBenchmarkProduct(product benchmarkProduct) (string, int, error) {
	command := exec.Command(product.path, product.args...)
	output, err := command.CombinedOutput()
	if err == nil {
		return strings.ReplaceAll(string(output), "\r\n", "\n"), 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return strings.ReplaceAll(string(output), "\r\n", "\n"), exit.ExitCode(), nil
	}
	return string(output), -1, err
}
