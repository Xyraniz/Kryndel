package kry

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The Windows target must survive the checked KIR boundary on every host.
// In particular, a future Windows ABI backend needs the parameter positions
// and checked types even when its producer runs on Linux.
func TestWindowsX64KIRPreservesMixedAndStackArguments(t *testing.T) {
	program, checker := testProgram(t, `
fn select(a: Int, b: Float, c: UInt8, d: Int, e: Bool, f: Int) -> Int {
    if e { return a + d + f }
    return a
}
let answer: Int = select(1, float(2), u8(3), 4, true, 5)
`)
	windowsTarget, err := ParseNativeTarget("windows-x64")
	if err != nil {
		t.Fatal(err)
	}
	windowsBytes, err := EmitKIR(program, checker, windowsTarget)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EmitKIR(program, checker, windowsTarget)
	if err != nil || !bytes.Equal(windowsBytes, second) {
		t.Fatalf("Windows KIR was not deterministic: %v", err)
	}
	windowsDoc, err := DecodeKIR(windowsBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if windowsDoc.Target != (KIRTarget{OS: "windows", Arch: "amd64"}) {
		t.Fatalf("Windows target was lost: %+v", windowsDoc.Target)
	}
	if len(windowsDoc.Functions) != 1 || len(windowsDoc.Functions[0].Params) != 6 {
		t.Fatalf("six positional parameters were lost: %+v", windowsDoc.Functions)
	}
	wantTypes := []string{"Int", "Float", "UInt8", "Int", "Bool", "Int"}
	for i, want := range wantTypes {
		if got := windowsDoc.Functions[0].Params[i].Type; got != want {
			t.Fatalf("parameter %d type = %q, want %q", i, got, want)
		}
	}
	if len(windowsDoc.Statements) != 1 || windowsDoc.Statements[0].Init == nil ||
		len(windowsDoc.Statements[0].Init.Args) != len(wantTypes) ||
		windowsDoc.Statements[0].Init.CallTarget != "function:select" {
		t.Fatalf("checked Windows call was lost: %+v", windowsDoc.Statements)
	}

	linuxBytes, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	linuxDoc, err := DecodeKIR(linuxBytes, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(windowsBytes, linuxBytes) {
		t.Fatal("distinct target identities produced identical KIR")
	}
	linuxDoc.Target = windowsDoc.Target
	if !reflect.DeepEqual(linuxDoc, windowsDoc) {
		t.Fatal("changing targets changed checked KIR contents beyond target metadata")
	}

	windowsDoc.Target.Arch = "386"
	unsupported, err := json.Marshal(windowsDoc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeKIR(unsupported, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("unsupported Windows target was accepted: %v", err)
	}
}
