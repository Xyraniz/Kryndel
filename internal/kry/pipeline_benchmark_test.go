package kry

import (
	"io"
	"os"
	"runtime"
	"testing"
)

type phaseBenchmarkCorpus struct {
	source  *Source
	text    []byte
	tokens  []Token
	program *Program
	checker *Checker
	limits  Limits
}

func loadPhaseBenchmarkCorpus(b *testing.B) phaseBenchmarkCorpus {
	b.Helper()
	text, err := os.ReadFile("../../examples/native_features.kry")
	if err != nil {
		b.Fatal(err)
	}
	source := &Source{Name: "examples/native_features.kry", Text: string(text)}
	limits := DefaultLimits()
	tokens, diagnostic := Lex(source, limits)
	if diagnostic != nil {
		b.Fatalf("lex benchmark corpus: %s", diagnostic.Message)
	}
	program, diagnostic := parseBenchmarkTokens(source, tokens, limits)
	if diagnostic != nil {
		b.Fatalf("parse benchmark corpus: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		b.Fatalf("check benchmark corpus: %s", diagnostic.Message)
	}
	return phaseBenchmarkCorpus{
		source: source, text: text, tokens: tokens,
		program: program, checker: checker, limits: limits,
	}
}

// parseBenchmarkTokens mirrors Parse's top-level dispatch while accepting
// pre-lexed tokens, so the parser benchmark excludes lexer work.
func parseBenchmarkTokens(source *Source, tokens []Token, limits Limits) (*Program, *Diagnostic) {
	p := &Parser{Tokens: tokens, Lim: limits}
	program := &Program{Source: source, Module: source.Name, VisibilityScope: sourceVisibilityScope(source), Sources: []*Source{source}}
	for !p.check(EOF) && p.Err == nil {
		pub := p.match(PUB)
		private := p.match(PRIVATE)
		if private && pub {
			p.fail(p.prev(), "a declaration cannot be both pub and private")
		}
		switch {
		case p.check(FN):
			program.Functions = append(program.Functions, p.function(pub))
		case p.check(STRUCT):
			program.Structs = append(program.Structs, p.structDecl(pub))
		case p.check(ENUM):
			program.Enums = append(program.Enums, p.enumDecl(pub))
		case p.match(IMPL):
			p.Pos--
			program.Functions = append(program.Functions, p.implDecl()...)
		case p.match(IMPORT):
			token := p.expect(STRING, "import expects a quoted module path")
			if p.Err != nil {
				break
			}
			path, err := DecodeString(token)
			if err != nil {
				p.fail(token, "invalid import string")
			}
			program.Imports = append(program.Imports, ImportDecl{Path: path, Tok: token})
			p.end()
		case p.match(CONST):
			if pub || private {
				p.fail(p.peek(), "const declarations cannot be public or private")
				break
			}
			statement := p.constStmt()
			statement.EndToken = p.prev()
			program.Statements = append(program.Statements, statement)
			p.end()
		default:
			if pub || private {
				p.fail(p.peek(), "'pub' must be followed by a function, struct, or enum")
				break
			}
			statement := p.statement()
			if statement != nil {
				statement.EndToken = p.prev()
				program.Statements = append(program.Statements, statement)
			}
			p.end()
		}
	}
	if p.Err != nil {
		return nil, p.Err
	}
	return program, nil
}

func BenchmarkInterpreterPhases(b *testing.B) {
	corpus := loadPhaseBenchmarkCorpus(b)
	b.Run("Lexer", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(corpus.text)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tokens, diagnostic := Lex(corpus.source, corpus.limits)
			if diagnostic != nil {
				b.Fatal(diagnostic.Message)
			}
			runtime.KeepAlive(tokens)
		}
	})
	b.Run("ParserPrelexed", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(corpus.text)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			program, diagnostic := parseBenchmarkTokens(corpus.source, corpus.tokens, corpus.limits)
			if diagnostic != nil {
				b.Fatal(diagnostic.Message)
			}
			runtime.KeepAlive(program)
		}
	})
	b.Run("Checker", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(corpus.text)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			checker, diagnostic := Check(corpus.program, corpus.limits)
			if diagnostic != nil {
				b.Fatal(diagnostic.Message)
			}
			runtime.KeepAlive(checker)
		}
	})
	b.Run("Runtime", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(corpus.text)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			runtime, diagnostic := NewRuntime(corpus.program, corpus.checker, corpus.limits, Sandbox{})
			if diagnostic != nil {
				b.Fatal(diagnostic.Message)
			}
			runtime.output = io.Discard
			if diagnostic := runtime.run(); diagnostic != nil {
				b.Fatal(diagnostic.Message)
			}
		}
	})
}
