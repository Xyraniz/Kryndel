package kry

import (
	"strings"
	"testing"
)

func TestParserNodeBlocksKeepEarlierASTPointersStable(t *testing.T) {
	const statementCount = parserNodeChunkSize * 4
	source := strings.Repeat("println(1)\n", statementCount)
	program, diagnostic := Parse(&Source{Name: "parser-arena.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if len(program.Statements) != statementCount {
		t.Fatalf("parsed %d statements, want %d", len(program.Statements), statementCount)
	}
	for index, statement := range program.Statements {
		if statement.Kind != StExpr || statement.Expr == nil || statement.Expr.Name != "println" || len(statement.Expr.Args) != 1 || statement.Expr.Args[0].Int != 1 {
			t.Fatalf("AST links changed at statement %d: %#v", index, statement)
		}
	}
	if _, diagnostic := Check(program, DefaultLimits()); diagnostic != nil {
		t.Fatalf("checker rejected the arena-backed AST: %s", diagnostic.Message)
	}
}

func TestParserCountsExpressionStatementsAgainstNodeLimit(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxASTNodes = 2
	_, diagnostic := Parse(&Source{Name: "expression-statement-limit.kry", Text: "println(1)\n"}, limits)
	if diagnostic == nil || !strings.Contains(diagnostic.Message, "AST node limit exceeded (2)") {
		t.Fatalf("expected expression statement to respect AST node limit, got %#v", diagnostic)
	}
}
