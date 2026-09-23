package kry

import (
	"strings"
	"testing"
)

func TestIntFloatUsesHalfOpenSignedRangeAndTruncatesTowardZero(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		{name: "minimum signed integer", value: "-9223372036854775808", want: -9223372036854775808},
		{name: "largest representable float below upper bound", value: "9223372036854774784", want: 9223372036854774784},
		{name: "positive fraction truncates toward zero", value: "1.9", want: 1},
		{name: "negative fraction truncates toward zero", value: "-1.9", want: -1},
		{name: "positive upper bound is exclusive", value: "9223372036854775808", wantErr: true},
		{name: "below negative lower bound", value: "-9223372036854777856", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, c := testProgram(t, "let converted: Int = int(float(\""+tc.value+"\"))\n")
			r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
			if d != nil {
				t.Fatal(d)
			}
			d = r.run()
			if tc.wantErr {
				if d == nil || d.Category != CatRuntime || !strings.Contains(d.Message, "float is outside Int range") {
					t.Fatalf("expected float range error, got %v", d)
				}
				return
			}
			if d != nil {
				t.Fatalf("conversion failed: %v", d)
			}
			got, ok := r.Global.get("converted")
			if !ok || got.Value.Kind != VInt || got.Value.I != tc.want {
				t.Fatalf("converted value = %#v, want Int %d", got, tc.want)
			}
		})
	}
}

func TestMapLiteralRejectsDuplicateKeysAndMapInsertRetainsPosition(t *testing.T) {
	p, c := testProgram(t, `let values: Map[String, Int] = {"a": 1, "a": 2}`)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d)
	}
	if d = r.run(); d == nil || !strings.Contains(d.Message, "duplicate map key") {
		t.Fatalf("duplicate map literal key should fail, got %v", d)
	}

	output := runInterp(t, `let values: Map[String, Int] = {"a": 1, "b": 2}
let updated: Map[String, Int] = map_insert(values, "a", 3)
println(map_keys(updated))
println(map_values(updated))
`)
	if output != `[a, b]
[3, 2]
` {
		t.Fatalf("map_insert changed order or failed to replace the value: %q", output)
	}
}
