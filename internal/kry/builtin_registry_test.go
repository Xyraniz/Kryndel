package kry

import "testing"

func TestBuiltinRegistryMatchesFreshPublicRegistry(t *testing.T) {
	public := Builtins()
	if len(public) != len(builtinList) {
		t.Fatalf("public builtin registry has %d entries, want %d", len(public), len(builtinList))
	}
	if len(builtinRegistry) != len(public) {
		t.Fatalf("cached builtin registry has %d entries, want %d", len(builtinRegistry), len(public))
	}
	for _, want := range builtinList {
		got, ok := lookupBuiltin(want.Name)
		if !ok || got != want {
			t.Errorf("lookupBuiltin(%q) = %#v, %t; want %#v, true", want.Name, got, ok, want)
		}
	}

	public["println"] = Builtin{Name: "tampered"}
	delete(public, "len")
	if got, ok := lookupBuiltin("println"); !ok || got.Name != "println" {
		t.Fatalf("mutating Builtins() changed cached println entry: %#v, %t", got, ok)
	}
	if _, ok := Builtins()["len"]; !ok {
		t.Fatal("mutating one Builtins() result changed a later result")
	}
}
