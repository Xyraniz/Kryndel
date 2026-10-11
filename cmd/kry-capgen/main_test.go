package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSelfHostedKindInventoriesReadTypedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontend.kry")
	// Portable serializer text must not invent source parser capabilities.
	const source = `
fn source_is_scalar_type(type_name: String) -> Bool { return type_name == "Int" }
fn row() -> KIRTypedExpression {
    return KIRTypedExpression{
        kind: "int", type_name: "Int"
    }
}
fn stmt() -> KIRTypedStatement {
    return KIRTypedStatement{ kind: "return", return_value: ref }
}
fn binding() -> KIRTypedStatement {
    if token.text == "let" || keyword == "const" { }
    return KIRTypedStatement{ kind: keyword, is_const: keyword == "const" }
}
fn portable_text() -> String { return "{\"kind\":\"float\"}" }
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	inventories, err := selfHostedKindInventories(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"generatedSelfHostedExprKinds": {"ExInt"},
		"generatedSelfHostedStmtKinds": {"StConst", "StLet", "StReturn"},
		"generatedSelfHostedTypes":     {"TyInt"},
	} {
		if got := inventories[name]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

func TestSelfHostedKindInventoriesCoverCurrentSourceArena(t *testing.T) {
	inventories, err := selfHostedKindInventories(filepath.Join("..", "..", "selfhost", "source_kir_compiler.kry"))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"generatedSelfHostedExprKinds": {"ExArray", "ExBinary", "ExBool", "ExCall", "ExEnum", "ExField", "ExIndex", "ExInt", "ExMap", "ExNil", "ExPropagate", "ExString", "ExStruct", "ExUnary", "ExVar"},
		"generatedSelfHostedStmtKinds": {"StAssign", "StBreak", "StConst", "StContinue", "StExpr", "StFor", "StIf", "StLet", "StMatch", "StReturn", "StWhile"},
	} {
		if got := inventories[name]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}
