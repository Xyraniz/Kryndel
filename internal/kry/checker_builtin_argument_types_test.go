package kry

import (
	"strings"
	"testing"
)

func TestBuiltinExpectedArgumentTypesAreEnforced(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "valid float builtin", source: "tan(1.0)\n"},
		{name: "valid clamp", source: "clamp(1, 0, 2)\n"},
		{name: "tan rejects string", source: "tan(\"not a float\")\n", wantErr: "expected Float, found String"},
		{name: "clamp rejects mismatched bounds", source: "clamp(1, \"not an integer\", true)\n", wantErr: "expected Int, found String"},
		{name: "array slice rejects noninteger index", source: "array_slice([1], 0.5, 1)\n", wantErr: "expected Int, found Float"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			program, diagnostic := Parse(&Source{Name: "builtin-argument-types.kry", Text: test.source}, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			_, diagnostic = Check(program, DefaultLimits())
			if test.wantErr == "" {
				if diagnostic != nil {
					t.Fatalf("valid builtin call was rejected: %s", diagnostic.Message)
				}
				return
			}
			if diagnostic == nil || !strings.Contains(diagnostic.Message, test.wantErr) {
				t.Fatalf("expected diagnostic containing %q, got %#v", test.wantErr, diagnostic)
			}
		})
	}
}
