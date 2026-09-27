package kry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSelfhostSourceFrontendSerializesOptionResultPropagation(t *testing.T) {
	frontend := loadSelfhostFFIFrontend(t)
	const source = `
fn result_value() -> Result[Int, String] { return ok(7) }
fn option_value() -> Option[Int] { return some(9) }

fn use_result() -> Result[String, String] {
    let value: Int = result_value()?
    return ok(str(value))
}

fn use_option() -> Option[String] {
    let value: Int = option_value()?
    return some(str(value))
}

fn main() -> Nil { return nil }
`
	serializedKIR, diagnostic := frontend.compile(t, source)
	if diagnostic != nil {
		t.Fatalf("compile valid Option/Result propagation to KIR: %s", diagnostic.Message)
	}
	var document map[string]any
	if err := json.Unmarshal(serializedKIR, &document); err != nil {
		t.Fatalf("decode serialized KIR: %v\n%s", err, serializedKIR)
	}

	propagations := map[string]map[string]any{}
	walkSelfhostKIR(document, func(node map[string]any) {
		if node["kind"] != "propagate" {
			return
		}
		operand, ok := node["operand"].(map[string]any)
		if !ok {
			t.Errorf("propagate operand is not a KIR expression: %#v", node["operand"])
			return
		}
		operandType, _ := operand["type"].(string)
		propagations[operandType] = node
	})
	for operandType, wantNodeType := range map[string]string{
		"Option[Int]":        "Int",
		"Result[Int,String]": "Int",
	} {
		node := propagations[operandType]
		if node == nil {
			t.Errorf("serialized KIR has no propagate node with operand type %q", operandType)
			continue
		}
		if got, _ := node["type"].(string); got != wantNodeType {
			t.Errorf("propagate node for %s has type %q, want %q", operandType, got, wantNodeType)
		}
		operand, _ := node["operand"].(map[string]any)
		if got, _ := operand["type"].(string); got != operandType {
			t.Errorf("propagate operand type = %q, want original wrapper %q", got, operandType)
		}
	}
	if len(propagations) != 2 {
		t.Errorf("serialized KIR contains %d distinct propagation operands, want Option and Result", len(propagations))
	}
}

func TestSelfhostSourceFrontendRejectsInvalidPropagation(t *testing.T) {
	frontend := loadSelfhostFFIFrontend(t)
	for _, test := range []struct {
		name        string
		source      string
		wantMessage string
	}{
		{
			name: "Int operand",
			source: `fn bad() -> Result[Int, String] {
    let value: Int = 1?
    return ok(value)
}
fn main() -> Nil { return nil }
`,
			wantMessage: "'?' requires an Option or Result",
		},
		{
			name: "incompatible Result error type",
			source: `fn source_value() -> Result[Int, String] { return ok(1) }
fn bad() -> Result[Int, Int] {
    let value: Int = source_value()?
    return ok(value)
}
fn main() -> Nil { return nil }
`,
			wantMessage: "'?' requires an Option or Result matching the enclosing function return type",
		},
		{
			name: "Option into Result return",
			source: `fn source_value() -> Option[Int] { return some(1) }
fn bad() -> Result[Int, String] {
    let value: Int = source_value()?
    return ok(value)
}
fn main() -> Nil { return nil }
`,
			wantMessage: "'?' requires an Option or Result matching the enclosing function return type",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostic := frontend.compile(t, test.source)
			if diagnostic == nil || !strings.Contains(diagnostic.Message, test.wantMessage) {
				t.Fatalf("invalid propagation diagnostic = %v, want message containing %q", diagnostic, test.wantMessage)
			}
		})
	}
}
