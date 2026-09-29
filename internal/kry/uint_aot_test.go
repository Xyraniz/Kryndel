package kry

import (
	"strings"
	"testing"
)

func TestCAOTUnsignedConversionsMatchInterpreter(t *testing.T) {
	source := `fn main() -> Nil {
    println(u8(255))
    println(u16(65535))
    println(u32(4294967295))
    println(u64(9223372036854775807))
    let max: UInt64 = u64(0) - u64(1)
    println(u64(max))
    println(str(max))
}
`
	wantOutput, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter rejected unsigned conversion fixture: %s", diagnostic.Message)
	}
	nativeOutput, status, err := buildAndRunNativeAOT(t, source)
	if err != nil || status != 0 || nativeOutput != wantOutput {
		t.Fatalf("C AOT unsigned conversions differ: interpreter=%q; native output=%q status=%d err=%v", wantOutput, nativeOutput, status, err)
	}

	for _, test := range []struct {
		name, source, wantMessage string
	}{
		{name: "negative Int", source: "println(u64(-1))", wantMessage: "Int is outside unsigned range"},
		{name: "u8 overflow", source: "println(u8(256))", wantMessage: "value is outside unsigned range"},
		{name: "u16 overflow", source: "println(u16(65536))", wantMessage: "value is outside unsigned range"},
		{name: "u32 overflow", source: "println(u32(4294967296))", wantMessage: "value is outside unsigned range"},
		{name: "narrow UInt", source: "println(u8(u16(256)))", wantMessage: "value is outside unsigned range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostic := runInterpreterCapture(t, test.source)
			if diagnostic == nil || !strings.Contains(diagnostic.Message, test.wantMessage) {
				t.Fatalf("interpreter error = %#v, want %q", diagnostic, test.wantMessage)
			}
			nativeOutput, status, err := buildAndRunNativeAOT(t, test.source)
			if err != nil || status != 1 || !strings.Contains(nativeOutput, "kryndel: "+diagnostic.Message) {
				t.Fatalf("C AOT error differs: interpreter=%q; native output=%q status=%d err=%v", diagnostic.Message, nativeOutput, status, err)
			}
		})
	}
}
