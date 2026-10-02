package kry

import "testing"

func TestSelfhostPEBackendJSONStringDecoderUnicodeFixtures(t *testing.T) {
	fixtures := []struct {
		name string
		json string
		want string
	}{
		{name: "raw ASCII", json: `"ascii"`, want: "ascii"},
		{name: "escaped ASCII", json: `"\u0062"`, want: "b"},
		{name: "BMP", json: `"\u20AC"`, want: "€"},
		{name: "surrogate pair", json: `"\uD83D\uDCA9"`, want: "💩"},
		{name: "lone high", json: `"\uD800"`, want: "�"},
		{name: "lone low", json: `"\uDC00"`, want: "�"},
		{name: "high followed by text", json: `"\uD800x"`, want: "�x"},
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			source := `fn main() -> Nil { println(result_unwrap(json_string(result_unwrap(json_parse("` + kryStringContents(tc.json) + `"))))) }
`
			runSelfhostPEBackendExpectingOutput(t, source, "json-string-decoder.exe", tc.want+"\n")
		})
	}
}
