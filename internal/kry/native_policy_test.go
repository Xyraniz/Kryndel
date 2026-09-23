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

func TestNativeCapabilityMatrixMatchesTargetPolicy(t *testing.T) {
	rows := NativeCapabilityMatrix()
	if len(rows) != len(nativeCapabilityTargets)*5+1 {
		t.Fatalf("capability matrix has %d rows, want %d", len(rows), len(nativeCapabilityTargets)*5+1)
	}
	targets := make(map[string]NativeTarget, len(nativeCapabilityTargets))
	for _, target := range nativeCapabilityTargets {
		targets[target.name] = target.target
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		key := row.Format + "/" + row.Target
		if seen[key] {
			t.Fatalf("duplicate capability row %s", key)
		}
		seen[key] = true
		if row.Format == "c" {
			if row.Target != "any" || row.Status != "source-only" {
				t.Fatalf("unexpected C source capability: %#v", row)
			}
			continue
		}
		target, ok := targets[row.Target]
		if !ok {
			t.Fatalf("capability row has unknown target %q", row.Target)
		}
		if row.Format == "macho" {
			if row.Status != "unsupported" {
				t.Fatalf("Mach-O capability should be unsupported: %#v", row)
			}
			continue
		}
		reason := nativeOutputTargetReason(row.Format, target)
		want := "supported"
		if row.Format == "elf-direct" && reason == "" {
			want = "partial"
		} else if reason != "" {
			want = "unsupported"
		}
		if row.Status != want {
			t.Fatalf("%s status is %q, want %q (policy reason %q)", key, row.Status, want, reason)
		}
	}
	if len(seen) != len(rows) {
		t.Fatalf("matrix has %d rows but only %d unique rows", len(rows), len(seen))
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

func TestNativeTargetIsRejectedBeforeFeatureLowering(t *testing.T) {
	p, c := testProgram(t, `let result: Result[FFILibrary, String] = ffi_library_open("missing.dll")`)
	_, err := BuildNative(p, c, NativeTarget{OS: "windows", Arch: "amd64"}, "elf")
	if err == nil || !strings.Contains(err.Error(), "ELF output requires a Linux target") {
		t.Fatalf("expected target validation before C feature lowering, got %v", err)
	}
}
