package kry

import (
	"strings"
	"testing"
)

const fuzzInputLimit = 1 << 20

func FuzzKryndelLex(f *testing.F) {
	for _, seed := range []string{
		"",
		"let value: Int = 42\n",
		"/* nested /* comment */ comment */",
		"fn greet(name: String) -> String { return \"hi, \" + name }",
		"\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > fuzzInputLimit {
			t.Skip()
		}
		limits := DefaultLimits()
		limits.MaxSourceBytes = fuzzInputLimit
		limits.MaxTokens = 100_000
		_, _ = Lex(&Source{Name: "fuzz.kry", Text: input}, limits)
	})
}

func FuzzKryndelParseAndCheck(f *testing.F) {
	for _, seed := range []string{
		"",
		"let value: Int = 42\n",
		"fn add(a: Int, b: Int) -> Int { return a + b }",
		"let value: Int = true\n",
		"match value { }",
		strings.Repeat("(", 256),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > fuzzInputLimit {
			t.Skip()
		}
		limits := DefaultLimits()
		limits.MaxSourceBytes = fuzzInputLimit
		limits.MaxTokens = 100_000
		limits.MaxASTNodes = 100_000
		program, diagnostic := Parse(&Source{Name: "fuzz.kry", Text: input}, limits)
		if diagnostic == nil {
			_, _ = Check(program, limits)
		}
	})
}

func FuzzKryndelArtifactDecoder(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte("KRYNATIVE4\x00"),
		[]byte("KRYNATIVE3\x00\x00\x00\x00\x00"),
		[]byte(strings.Repeat("x", 256)),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > fuzzInputLimit {
			t.Skip()
		}
		limits := DefaultLimits()
		limits.MaxArtifactBytes = fuzzInputLimit
		limits.MaxSourceBytes = fuzzInputLimit / 2
		limits.MaxImports = 128
		_, _ = DecodeArtifact(input, limits)
	})
}

func FuzzKryndelKIRDecoder(f *testing.F) {
	for _, seed := range [][]byte{
		{},
		[]byte(`{"format":"kry-ir","version":2}`),
		[]byte(strings.Repeat("{", 128)),
		[]byte("null"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > fuzzInputLimit {
			t.Skip()
		}
		limits := DefaultLimits()
		limits.MaxArtifactBytes = fuzzInputLimit
		limits.MaxJSONBytes = fuzzInputLimit
		limits.MaxASTNodes = 100_000
		limits.MaxNesting = 256
		_, _ = DecodeKIR(input, limits)
	})
}
