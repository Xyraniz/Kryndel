package kry

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNativeBackendDescriptions(t *testing.T) {
	cases := []struct {
		format       string
		name         string
		toolchain    string
		requiresTool bool
	}{
		{format: "elf-direct", name: "direct ELF", toolchain: "none"},
		{format: "c", name: "C source", toolchain: "none (source only)"},
		{format: "elf", name: "C AOT", toolchain: "external C compiler", requiresTool: true},
		{format: "exe", name: "C AOT", toolchain: "external C compiler", requiresTool: true},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			got, err := DescribeNativeBackend(tc.format)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tc.name || got.ExternalToolchain != tc.toolchain || got.RequiresExternalToolchain != tc.requiresTool {
				t.Fatalf("unexpected backend description: %#v", got)
			}
		})
	}
}

func TestNoExternalToolchainNeverLaunchesCCompiler(t *testing.T) {
	old := nativeExecCommand
	t.Cleanup(func() { nativeExecCommand = old })
	called := false
	nativeExecCommand = func(name string, args ...string) *exec.Cmd {
		called = true
		return exec.Command(name, args...)
	}

	p, c := testProgram(t, "println(\"must not invoke C\")\n")
	_, err := BuildNativeWithPolicyOpts(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf", false, true)
	if err == nil || !strings.Contains(err.Error(), "--no-external-toolchain") || !strings.Contains(err.Error(), "external C compiler") {
		t.Fatalf("expected explicit external-toolchain rejection, got %v", err)
	}
	if called {
		t.Fatal("no-external-toolchain build attempted to execute an external command")
	}
}
