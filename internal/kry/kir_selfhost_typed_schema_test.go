package kry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelfhostTypedKIRArenaMatchesGoRowContract(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(contents)
	}
	selfhost := read(filepath.Join(root, "..", "..", "selfhost", "kir_typed_arena.kry"))
	for _, row := range []string{
		"KIRTypedTarget", "KIRTypedImport", "KIRTypedTypeParam", "KIRTypedBinding",
		"KIRTypedField", "KIRTypedStruct", "KIRTypedEnum", "KIRTypedTraitMethod",
		"KIRTypedTrait", "KIRTypedTraitImplMethod", "KIRTypedTraitImpl", "KIRTypedParameter",
		"KIRTypedCapture", "KIRTypedFunction", "KIRTypedExpression", "KIRTypedStatement",
		"KIRTypedArm", "KIRTypedPattern", "KIRTypedValue", "KIRTypedSourceSpan",
	} {
		if !strings.Contains(selfhost, "pub struct "+row) {
			t.Errorf("selfhost typed arena is missing row %s", row)
		}
	}
	for _, required := range []string{
		"pub enum KIRTypedTable", "KIRTableImports", "KIRTableImportRecords", "KIRTableSources",
		"KIRTableStructs", "KIRTableFields", "KIRTableEnums", "KIRTableTraits", "KIRTableTraitMethods",
		"KIRTableTraitImpls", "KIRTableTraitImplMethods", "KIRTableTypeParams", "KIRTableFunctions",
		"KIRTableParameters", "KIRTableCaptures", "KIRTableBindings", "KIRTableExpressions",
		"KIRTableStatements", "KIRTableArms", "KIRTablePatterns", "KIRTableValues", "KIRTableSpans", "KIRTableEdges",
		"table: KIRTypedTable", "index: Int", "present: Bool",
		"start: Int", "end: Int", "private target: KIRTypedTarget",
		"private bindings: Array[KIRTypedBinding]", "private expressions: Array[KIRTypedExpression]",
		"private statements: Array[KIRTypedStatement]", "private functions: Array[KIRTypedFunction]",
		"private top_functions: KIRIndexRange", "private top_statements: KIRIndexRange",
		"generic_arguments: Array[String]", "call_target: String",
		"uint_bits: UInt8", "source: String", "span: KIRIndexRef",
		"if range.table != KIRTypedTable::KIRTableEdges", "range.count <= edge_count - range.start",
		"reference.table != table || !kir_typed_reference_valid(arena, reference)",
		"kir_typed_reference_valid", "kir_typed_range_valid", "kir_typed_arena_references_valid",
	} {
		if !strings.Contains(selfhost, required) {
			t.Errorf("selfhost typed arena is missing field contract %q", required)
		}
	}
	for _, forbidden := range []string{"KIRArenaCell", "field_lookup", "KIRNodeRef"} {
		if strings.Contains(selfhost, forbidden) {
			t.Errorf("typed arena rows retain generic wire representation %q", forbidden)
		}
	}
	start := strings.Index(selfhost, "pub struct KIRTypedArena {")
	if start < 0 {
		t.Fatal("selfhost typed arena root row is missing")
	}
	end := strings.Index(selfhost[start:], "\n}")
	if end < 0 {
		t.Fatal("selfhost typed arena root row is unterminated")
	}
	if strings.Contains(selfhost[start:start+end], "Json") {
		t.Fatal("typed arena root row retains the recursive wire document")
	}
	if strings.Contains(selfhost, "private table: String") {
		t.Fatal("selfhost typed IR table references accept arbitrary string tags")
	}
	for _, goContract := range []struct {
		file string
		text string
	}{
		{filepath.Join(root, "mir_arena.go"), "type KIRArena struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRExpression struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRStatement struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRFunction struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRParameter struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRPattern struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRArm struct"},
		{filepath.Join(root, "mir_arena.go"), "type MIRValue struct"},
		{filepath.Join(root, "kir.go"), "type KIRBinding struct"},
		{filepath.Join(root, "kir.go"), "type KIRStruct struct"},
		{filepath.Join(root, "kir.go"), "type KIREnum struct"},
		{filepath.Join(root, "kir.go"), "type KIRTrait struct"},
		{filepath.Join(root, "kir.go"), "type KIRImport struct"},
	} {
		if contents := read(goContract.file); !strings.Contains(contents, goContract.text) {
			t.Errorf("Go typed IR contract is missing %q", goContract.text)
		}
	}
	if !strings.Contains(selfhost, "KIRIndexRef") || !strings.Contains(selfhost, "KIRIndexRange") {
		t.Fatal("selfhost typed IR edges are not represented by explicit references and ranges")
	}
}
