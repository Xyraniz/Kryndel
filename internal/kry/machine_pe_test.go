package kry

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirectPEWindowsExecutable(t *testing.T) {
	p, c := testProgram(t, `fn main() -> Nil {
    let prefix: String = "Kryndel "
    println(prefix + str(40 + 2))
    return nil
}
`)
	called := false
	previous := nativeExecCommand
	t.Cleanup(func() { nativeExecCommand = previous })
	nativeExecCommand = func(name string, args ...string) *exec.Cmd {
		called = true
		return exec.Command(name, args...)
	}
	data, err := BuildNativeWithPolicyOpts(p, c, NativeTarget{OS: "windows", Arch: "amd64"}, "pe-direct", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("direct PE invoked an external toolchain")
	}
	if len(data) < 512 || string(data[:2]) != "MZ" {
		t.Fatal("direct PE did not emit a DOS/PE header")
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Go PE parser rejected image: %v", err)
	}
	defer image.Close()
	if image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(image.Sections) != 4 {
		t.Fatalf("unexpected machine/sections: machine=%#x sections=%d", image.Machine, len(image.Sections))
	}
	opt, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || opt.Magic != 0x20b || opt.Subsystem != 3 || opt.AddressOfEntryPoint != peTextRVA {
		t.Fatalf("invalid PE32+ console entrypoint: %#v", image.OptionalHeader)
	}
	if opt.DataDirectory[3].VirtualAddress != image.Sections[3].VirtualAddress || opt.DataDirectory[3].Size != 12 {
		t.Fatalf("missing x64 exception directory: %#v", opt.DataDirectory[3])
	}
	unwind, err := image.Sections[3].Data()
	if err != nil || len(unwind) < 24 || !bytes.Equal(unwind[12:24], []byte{1, 11, 3, 0x50, 11, 0x72, 4, 3, 1, 0x50, 0, 0}) {
		t.Fatalf("missing Win64 stack allocation unwind information: err=%v data=%x", err, unwind)
	}
	imports, err := image.ImportedSymbols()
	if err != nil {
		t.Fatalf("could not parse PE imports: %v", err)
	}
	for _, name := range []string{"GetStdHandle:KERNEL32.dll", "WriteFile:KERNEL32.dll", "ExitProcess:KERNEL32.dll"} {
		found := false
		for _, imported := range imports {
			if strings.EqualFold(imported, name) {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing import %q from %v", name, imports)
		}
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "direct-program.exe")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("direct PE failed to run: %v; output: %q", err, out)
	}
	if string(out) != "Kryndel 42\n" {
		t.Fatalf("direct PE output %q", out)
	}
}

func TestDirectPERejectsUnsupportedProgramsAndTargets(t *testing.T) {
	p, c := testProgram(t, "println([1, 2, 3])\n")
	if _, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "direct PE dynamic subset") {
		t.Fatalf("expected explicit dynamic-subset rejection, got %v", err)
	}
	if _, err := BuildDirectPE(p, c, NativeTarget{OS: "linux", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "windows-amd64") {
		t.Fatalf("expected target rejection, got %v", err)
	}
	p, c = testProgram(t, `
let mut value: String = "x"
println(value == "x")
`)
	if _, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "String comparison") {
		t.Fatalf("expected explicit String comparison rejection, got %v", err)
	}
}

func TestDirectPEUnsignedShiftOutOfRangeExitsWithFailure(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("generated PE execution requires native windows-amd64")
	}
	for _, count := range []string{"8", "-1"} {
		t.Run("count_"+strings.ReplaceAll(count, "-", "negative_"), func(t *testing.T) {
			p, c := testProgram(t, fmt.Sprintf("let count: Int = %s\nprintln(u8(1) << count)\n", count))
			data, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "invalid-shift.exe")
			if err := os.WriteFile(path, data, 0o755); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(path).CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || len(out) != 0 {
				t.Fatalf("out-of-range shift result: err=%v output=%q", err, out)
			}
		})
	}
}

func TestDirectPEDynamicFunctionsAndControlFlow(t *testing.T) {
	source := strings.Join([]string{
		"fn add(a: Int, b: Int) -> Int {",
		"    return a + b",
		"}",
		"fn mix(a: Int, b: Int, c: Int, d: Int) -> Int {",
		"    return a + b * 10 + c * 100 + d * 1000",
		"}",
		"fn choose(value: Int) -> Int {",
		"    if value >= 0 {",
		"        return value",
		"    }",
		"    return -value",
		"}",
		"fn calc(value: Int) -> Int {",
		"    return value + add(value, 2)",
		"}",
		"fn large_frame() -> Int {",
		"    let a0: Int = 1",
		"    let a1: Int = 2",
		"    let a2: Int = 3",
		"    let a3: Int = 4",
		"    let a4: Int = 5",
		"    let a5: Int = 6",
		"    let a6: Int = 7",
		"    let a7: Int = 8",
		"    let a8: Int = 9",
		"    let a9: Int = 10",
		"    let a10: Int = 11",
		"    let a11: Int = 12",
		"    return a0",
		"}",
		"fn main() -> Nil {",
		"    let mut i: Int = 0",
		"    let mut message: String = \"runtime string\"",
		"    println(message)",
		"    message = \"updated string\"",
		"    println(message)",
		"    if i == 0 {",
		"        println(\"start\")",
		"    } else {",
		"        println(\"wrong branch\")",
		"    }",
		"    while i < 3 {",
		"        println(mix(i, add(i, 1), add(i, 2), add(i, 3)))",
		"        i = i + 1",
		"    }",
		"    println(choose(-123))",
		"    println(calc(5))",
		"    println(large_frame())",
		"    println(u8(255))",
		"    println(u16(65535))",
		"    println(u32(4294967295))",
		"    let mut wide: UInt64 = u64(9223372036854775807)",
		"    wide = wide + u64(1)",
		"    println(wide)",
		"    println(i < 3)",
		"    let mut j: Int = 0",
		"    while j < 5 {",
		"        j = j + 1",
		"        if j == 2 {",
		"            continue",
		"        }",
		"        if j == 4 {",
		"            break",
		"        }",
		"        println(j)",
		"    }",
		"    return nil",
		"}",
	}, "\n")
	p, c := testProgram(t, source)
	called := false
	previous := nativeExecCommand
	t.Cleanup(func() { nativeExecCommand = previous })
	nativeExecCommand = func(name string, args ...string) *exec.Cmd {
		called = true
		return exec.Command(name, args...)
	}
	data, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("dynamic direct PE invoked an external toolchain")
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Go PE parser rejected dynamic image: %v", err)
	}
	defer image.Close()
	if image.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || len(image.Sections) != 4 {
		t.Fatalf("unexpected dynamic PE machine/sections: %#x/%d", image.Machine, len(image.Sections))
	}
	opt, ok := image.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || opt.AddressOfEntryPoint != peTextRVA || opt.DataDirectory[3].Size != 6*12 {
		t.Fatalf("dynamic PE header or unwind table is incomplete: %#v", image.OptionalHeader)
	}
	for i := 1; i < len(image.Sections); i++ {
		previous := image.Sections[i-1]
		minimumRVA := previous.VirtualAddress + peAlign(previous.VirtualSize, peSectionAlignment)
		if image.Sections[i].VirtualAddress < minimumRVA {
			t.Fatalf("dynamic PE section %s overlaps %s: rva=%#x, minimum=%#x", image.Sections[i].Name, previous.Name, image.Sections[i].VirtualAddress, minimumRVA)
		}
	}
	pdata, err := image.Sections[3].Data()
	if err != nil || len(pdata) < 6*24 {
		t.Fatalf("dynamic PE unwind records are truncated: err=%v bytes=%d", err, len(pdata))
	}
	sawLargeFrame := false
	for i := 0; i < 6; i++ {
		begin := binary.LittleEndian.Uint32(pdata[i*12:])
		end := binary.LittleEndian.Uint32(pdata[i*12+4:])
		unwind := binary.LittleEndian.Uint32(pdata[i*12+8:])
		if begin >= end || unwind < image.Sections[3].VirtualAddress || unwind >= image.Sections[3].VirtualAddress+uint32(len(pdata)) {
			t.Fatalf("invalid dynamic PE RUNTIME_FUNCTION[%d]: begin=%#x end=%#x unwind=%#x", i, begin, end, unwind)
		}
		unwindOffset := int(unwind - image.Sections[3].VirtualAddress)
		info := pdata[unwindOffset:]
		if len(info) < 12 || info[0] != 1 || info[1] != 11 || info[3] != 0x50 || (info[2] != 3 && info[2] != 4) {
			t.Fatalf("invalid dynamic PE UNWIND_INFO[%d]: %x", i, info)
		}
		if info[5]&0x0f == 2 { // UWOP_ALLOC_SMALL
			if info[2] != 3 || info[4] != 11 || info[6] != 4 || info[7] != 3 || info[8] != 1 || info[9] != 0x50 {
				t.Fatalf("invalid small-frame unwind codes[%d]: %x", i, info)
			}
		} else if info[5]&0x0f == 1 { // UWOP_ALLOC_LARGE, OpInfo 0
			if info[2] != 4 || info[5]>>4 != 0 || info[4] != 11 || info[8] != 4 || info[9] != 3 || info[10] != 1 || info[11] != 0x50 {
				t.Fatalf("invalid large-frame unwind codes[%d]: %x", i, info)
			}
			if binary.LittleEndian.Uint16(info[6:8]) <= 16 {
				t.Fatalf("large-frame unwind code[%d] encodes no allocation above 128 bytes: %x", i, info)
			}
			sawLargeFrame = true
		} else {
			t.Fatalf("unsupported Win64 stack allocation opcode in unwind codes[%d]: %x", i, info)
		}
	}
	if !sawLargeFrame {
		t.Fatal("large-frame unwind encoding was not exercised")
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return
	}
	path := filepath.Join(t.TempDir(), "dynamic-program.exe")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("dynamic direct PE failed to run: %v; output: %q", err, out)
	}
	want := "runtime string\nupdated string\nstart\n3210\n4321\n5432\n123\n12\n1\n255\n65535\n4294967295\n9223372036854775808\nfalse\n1\n3\n"
	if string(out) != want {
		t.Fatalf("dynamic direct PE output %q, want %q", out, want)
	}
}

func TestDirectPELargeStaticOutputKeepsSectionsDistinct(t *testing.T) {
	want := strings.Repeat("x", 8192) + "\n"
	p, c := testProgram(t, fmt.Sprintf("println(%q)\n", strings.TrimSuffix(want, "\n")))
	data, err := BuildDirectPE(p, c, NativeTarget{OS: "windows", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if image.Sections[2].VirtualAddress < image.Sections[1].VirtualAddress+peAlign(image.Sections[1].VirtualSize, peSectionAlignment) {
		t.Fatal("import section overlaps output data")
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		path := filepath.Join(t.TempDir(), "large-output.exe")
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(path).Output()
		if err != nil || string(out) != want {
			t.Fatalf("large direct PE output failed: err=%v bytes=%d", err, len(out))
		}
	}
}
