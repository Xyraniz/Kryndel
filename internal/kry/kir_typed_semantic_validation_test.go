package kry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGoAndSelfhostRejectForgedConstantAndBuiltinTypes(t *testing.T) {
	limits := DefaultLimits()
	probeProgram, diagnostic := LoadProgram(filepath.Join("..", "..", "selfhost", "validated_kir_probe.kry"), limits, "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	probeChecker, diagnostic := Check(probeProgram, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	cases := []struct {
		name   string
		source string
		mutate func(*KIRDocument)
		accept bool
	}{
		{"builtin substring standard encoding", `fn main() -> Nil { println(substring("aá🙂z", 1, 2)) }`, func(d *KIRDocument) {}, true},
		{"builtin substring compact encoding", `fn main() -> Nil { println(substring("aá🙂z", 1, 2)) }`, func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Expr.Args[0].Type = "Result[String,String]"
		}, true},
		{"builtin substring wrong result shape", `fn main() -> Nil { println(substring("aá🙂z", 1, 2)) }`, func(d *KIRDocument) {
			expression := findKIRFunction(d, "main").Body[0].Expr.Args[0]
			expression.Type, expression.Const = "Result[Int,String]", nil
		}, false},
		{"constant kind", "fn main() -> Int { return 1 }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Return.Const = &KIRValue{Kind: "string", String: "forged"}
		}, false},
		{"constant UInt width", "fn main() -> UInt8 { return u8(1) }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Return.Const = &KIRValue{Kind: "uint", UInt: 1, UIntBits: 16}
		}, false},
		{"constant array item", "fn main() -> Nil { let values: Array[Int] = [1] }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Init.Const = &KIRValue{Kind: "array", Array: []*KIRValue{{Kind: "string", String: "forged"}}}
		}, false},
		{"constant Option payload", "fn main() -> Option[Int] { return some(1) }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Return.Const = &KIRValue{Kind: "option", Present: true, Inner: &KIRValue{Kind: "string", String: "forged"}}
		}, false},
		{"constant Result payload", "fn main() -> Result[Int, String] { return ok(1) }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Return.Const = &KIRValue{Kind: "result", OK: false, Inner: &KIRValue{Kind: "int", Int: 1}}
		}, false},
		{"builtin println return", "fn main() -> Nil { println(1) }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Expr.Type = "Int"
		}, false},
		{"builtin assert argument", "fn main() -> Nil { assert(true) }", func(d *KIRDocument) {
			e := findKIRFunction(d, "main").Body[0].Expr.Args[0]
			e.Kind, e.Type, e.Int, e.Const = "int", "Int", 1, &KIRValue{Kind: "int", Int: 1}
		}, false},
		{"builtin len argument", `fn main() -> Int { return len("abc") }`, func(d *KIRDocument) {
			e := findKIRFunction(d, "main").Body[0].Return.Args[0]
			e.Kind, e.Type, e.Int, e.Const = "int", "Int", 1, &KIRValue{Kind: "int", Int: 1}
		}, false},
		{"builtin bytes argument", `fn main() -> Bytes { return string_to_bytes("abc") }`, func(d *KIRDocument) {
			e := findKIRFunction(d, "main").Body[0].Return.Args[0]
			e.Kind, e.Type, e.Int, e.Const = "int", "Int", 1, &KIRValue{Kind: "int", Int: 1}
		}, false},
		{"duplicate v6 binding with forged span", "fn main() -> Nil { let value = 1; println(value) }", func(d *KIRDocument) {
			binding := *findKIRFunction(d, "main").Body[1].Expr.Args[0].Binding
			span := *binding.Span
			span.End++
			binding.Span = &span
			findKIRFunction(d, "main").Body[1].Expr.Args[0].Binding = &binding
		}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "typed-contract.kry", test.source)
			var document KIRDocument
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(&document)
			forged, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeMIR(forged, limits); (err == nil) != test.accept {
				t.Errorf("Go validator acceptance=%t, want %t: %v", err == nil, test.accept, err)
			}
			input := filepath.Join(t.TempDir(), "input.kir")
			if err := os.WriteFile(input, forged, 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, diagnostic := NewRuntimeWithArgs(probeProgram, probeChecker, limits, Sandbox{}, []string{input})
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			var output bytes.Buffer
			runtime.output = &output
			if diagnostic := runtime.run(); diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			if accepted := output.String() == "accepted\n"; accepted != test.accept {
				t.Errorf("selfhost acceptance=%t, want %t: %s", accepted, test.accept, output.String())
			}
		})
	}
}

func TestSelfhostTypedArenaColorsCrossPageBoundaries(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxInstructions = 10_000_000
	program, diagnostic := LoadProgram(filepath.Join("..", "..", "selfhost", "kir_typed_graph_test.kry"), limits, "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	t.Logf("paged graph regression used %d instructions", runtime.Ctx.Instructions)
}

func TestSelfhostBindingIdentityIndexPreservesOpaqueIDsAndCollisions(t *testing.T) {
	limits := DefaultLimits()
	encoded := emitValidatorCorpusKIR(t, limits, "opaque-bindings.kry", `fn main() -> Nil {
        let a: Int = 1
        let b: String = "value"
        println(a)
        println(b)
    }`)
	var base KIRDocument
	if err := json.Unmarshal(encoded, &base); err != nil {
		t.Fatal(err)
	}
	probeProgram, diagnostic := LoadProgram(filepath.Join("..", "..", "selfhost", "validated_kir_probe.kry"), limits, "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	probeChecker, diagnostic := Check(probeProgram, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	for _, test := range []struct {
		name, first, second          string
		accepted, mutabilityConflict bool
	}{
		{"opaque Unicode IDs", "λ/opaque-first", "🙂/opaque-second", true, false},
		// Both strings hash to bucket 457; string equality still distinguishes them.
		{"distinct IDs in same bucket", "opaque/90", "opaque/106", true, false},
		{"same ID conflicting type", "opaque/90", "opaque/90", false, false},
		{"same ID conflicting mutability", "opaque/90", "opaque/90", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := cloneKIRDocument(t, base)
			document.Version = 5 // v5 IDs are opaque; v6 requires canonical source IDs.
			body := findKIRFunction(document, "main").Body
			body[0].Binding.ID, body[2].Expr.Args[0].Binding.ID = test.first, test.first
			body[1].Binding.ID, body[3].Expr.Args[0].Binding.ID = test.second, test.second
			if test.mutabilityConflict {
				body[1].Mutable, body[1].Binding.Mutable = true, true
				body[1].Binding.Type, body[1].Annotation = "Int", "Int"
				body[1].Init.Kind, body[1].Init.Type, body[1].Init.Int, body[1].Init.String = "int", "Int", 2, ""
				body[1].Init.Const = &KIRValue{Kind: "int", Int: 2}
				body[3].Expr.Args[0].Type, body[3].Expr.Args[0].Binding.Type = "Int", "Int"
				body[3].Expr.Args[0].Binding.Mutable = true
			}
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			input := filepath.Join(t.TempDir(), "input.kir")
			if err := os.WriteFile(input, data, 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, diagnostic := NewRuntimeWithArgs(probeProgram, probeChecker, limits, Sandbox{}, []string{input})
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			var output bytes.Buffer
			runtime.output = &output
			if diagnostic := runtime.run(); diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			if accepted := output.String() == "accepted\n"; accepted != test.accepted {
				t.Fatalf("validator accepted=%t, want %t: %s", accepted, test.accepted, output.String())
			}
			if !test.accepted && !bytes.Contains(output.Bytes(), []byte("inconsistent type or mutability")) {
				t.Fatalf("expected binding identity conflict, got %s", output.String())
			}
		})
	}
}

// Each mutation keeps source metadata and children well formed. The changed
// checked type must be rejected by admission, before a backend sees the call.
func TestGoAndSelfhostValidateTypedBuiltinContracts(t *testing.T) {
	limits := DefaultLimits()
	program, diagnostic := LoadProgram(filepath.Join("..", "..", "selfhost", "validated_kir_probe.kry"), limits, "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	cases := []struct{ name, expression, forged string }{
		{"some", "some(1)", "Option[String]"},
		{"ok", "ok(1)", "Result[String,Nil]"},
		{"err", `err("failure")`, "Result[Nil,Int]"},
		{"is_some", "is_some(some(1))", "Int"},
		{"is_none", "is_none(some(1))", "Int"},
		{"is_ok", "is_ok(ok(1))", "Int"},
		{"is_err", `is_err(err("failure"))`, "Int"},
		{"unwrap_or", "unwrap_or(some(1), 2)", "String"},
		{"result_unwrap", "result_unwrap(ok(1))", "String"},
		{"result_error", `result_error(err("failure"))`, "Option[Int]"},
		{"array_push", "array_push([1], 2)", "Array[String]"},
		{"array_get", "array_get([1], 0)", "Option[String]"},
		{"array_set", "array_set([1], 0, 2)", "Result[Array[String],String]"},
		{"array_concat", "array_concat([1], [2])", "Array[String]"},
		{"array_indices", "array_indices([1])", "Array[String]"},
		{"array_slice", "array_slice([1], 0, 1)", "Array[String]"},
		{"map_get", `map_get({"key": 1}, "key")`, "Option[String]"},
		{"map_insert", `map_insert({"key": 1}, "key", 2)`, "Map[String,String]"},
		{"map_contains_key", `map_contains_key({"key": 1}, "key")`, "Int"},
		{"map_remove", `map_remove({"key": 1}, "key")`, "Map[String,String]"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "builtin-contract.kry", "fn main() -> Nil { println("+test.expression+") }")
			var base KIRDocument
			if err := json.Unmarshal(encoded, &base); err != nil {
				t.Fatal(err)
			}
			for _, valid := range []bool{true, false} {
				document := cloneKIRDocument(t, base)
				call := findKIRFunction(document, "main").Body[0].Expr.Args[0]
				if !valid {
					call.Type, call.Const = test.forged, nil
				}
				assertGoAndSelfhostKIRAdmission(t, program, checker, limits, document, valid)
			}
		})
	}
	t.Run("none shape", func(t *testing.T) {
		encoded := emitValidatorCorpusKIR(t, limits, "none-contract.kry", "fn optional() -> Option[Int] { return none() } fn main() -> Nil { println(optional()) }")
		var base KIRDocument
		if err := json.Unmarshal(encoded, &base); err != nil {
			t.Fatal(err)
		}
		assertGoAndSelfhostKIRAdmission(t, program, checker, limits, &base, true)
		forged := cloneKIRDocument(t, base)
		fn := findKIRFunction(forged, "optional")
		fn.Return, fn.Body[0].Return.Type, fn.Body[0].Return.Const = "Int", "Int", nil
		findKIRFunction(forged, "main").Body[0].Expr.Args[0].Type = "Int"
		assertGoAndSelfhostKIRAdmission(t, program, checker, limits, forged, false)
	})
	for _, encodedType := range []string{"Missing", "Option[Missing]", "fn() -> Missing", "Array[Int, String]"} {
		t.Run("unknown "+encodedType, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "unknown-expression.kry", "fn main() -> Nil { println(process_args()) }")
			var document KIRDocument
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			expr := findKIRFunction(&document, "main").Body[0].Expr.Args[0]
			expr.Type, expr.Const = encodedType, nil
			assertGoAndSelfhostKIRAdmission(t, program, checker, limits, &document, false)
		})
	}
	for _, test := range []struct {
		name, expression string
		index            int
	}{
		{"some argument", "some(1)", 0}, {"ok argument", "ok(1)", 0}, {"err argument", `err("failure")`, 0},
		{"is_some argument", "is_some(some(1))", 0}, {"is_ok argument", "is_ok(ok(1))", 0},
		{"unwrap_or fallback", "unwrap_or(some(1),2)", 1}, {"result_unwrap argument", "result_unwrap(ok(1))", 0},
		{"result_error argument", `result_error(err("failure"))`, 0},
		{"array_push element", "array_push([1],2)", 1}, {"array_get index", "array_get([1],0)", 1},
		{"array_set element", "array_set([1],0,2)", 2}, {"array_concat operand", "array_concat([1],[2])", 1},
		{"array_slice index", "array_slice([1],0,1)", 1},
		{"map_get key", `map_get({"key":1},"key")`, 1}, {"map_insert value", `map_insert({"key":1},"key",2)`, 2},
		{"map_contains_key key", `map_contains_key({"key":1},"key")`, 1}, {"map_remove key", `map_remove({"key":1},"key")`, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "builtin-argument.kry", "fn main() -> Nil { println("+test.expression+") }")
			var document KIRDocument
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			call := findKIRFunction(&document, "main").Body[0].Expr.Args[0]
			argument := call.Args[test.index]
			call.Args[test.index] = &KIRExpr{Kind: "bool", Type: "Bool", Source: argument.Source, Line: argument.Line, Column: argument.Column, Span: argument.Span}
			call.Const = nil
			assertGoAndSelfhostKIRAdmission(t, program, checker, limits, &document, false)
		})
	}
}

func assertGoAndSelfhostKIRAdmission(t *testing.T, program *Program, checker *Checker, limits Limits, document *KIRDocument, accept bool) string {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeMIR(encoded, limits); (err == nil) != accept {
		t.Errorf("Go acceptance=%t, want %t: %v", err == nil, accept, err)
	}
	input := filepath.Join(t.TempDir(), "input.kir")
	if err := os.WriteFile(input, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, diagnostic := NewRuntimeWithArgs(program, checker, limits, Sandbox{}, []string{input})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	var output bytes.Buffer
	runtime.output = &output
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if accepted := output.String() == "accepted\n"; accepted != accept {
		t.Errorf("selfhost acceptance=%t, want %t: %s", accepted, accept, output.String())
	}
	return output.String()
}

func TestGoAndSelfhostRejectBindingsOutsideLexicalScope(t *testing.T) {
	limits := DefaultLimits()
	program, diagnostic := LoadProgram(filepath.Join("..", "..", "selfhost", "validated_kir_probe.kry"), limits, "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	cases := []struct {
		name, source string
		mutate       func(*KIRDocument)
	}{
		{"declaration removed", "fn main() -> Nil { let value = 1; println(value) }", func(d *KIRDocument) { fn := findKIRFunction(d, "main"); fn.Body = fn.Body[1:] }},
		{"use before declaration", "fn main() -> Nil { let value = 1; println(value) }", func(d *KIRDocument) {
			fn := findKIRFunction(d, "main")
			fn.Body[0], fn.Body[1] = fn.Body[1], fn.Body[0]
		}},
		{"own initializer", "fn main() -> Nil { let value = 1; println(value) }", func(d *KIRDocument) {
			fn := findKIRFunction(d, "main")
			fn.Body[0].Init = fn.Body[1].Expr.Args[0]
			fn.Body[0].Init.Const = nil
		}},
		{"escape from branch", "fn main() -> Nil { if true { let value = 1; println(value) }; println(0) }", func(d *KIRDocument) {
			fn := findKIRFunction(d, "main")
			fn.Body[1].Expr.Args[0] = fn.Body[0].Then[1].Expr.Args[0]
		}},
		{"escape into sibling branch", "fn main() -> Nil { if true { let value = 1; println(value) } else { println(0) } }", func(d *KIRDocument) {
			branch := findKIRFunction(d, "main").Body[0]
			branch.Else[0].Expr.Args[0] = branch.Then[1].Expr.Args[0]
		}},
		{"outer ID under shadow", "fn main() -> Nil { let value = 1; if true { let value = 2; println(value) }; println(value) }", func(d *KIRDocument) {
			fn := findKIRFunction(d, "main")
			fn.Body[1].Then[1].Expr.Args[0].Binding = fn.Body[0].Binding
			fn.Body[1].Then[1].Expr.Args[0].Const = nil
		}},
		{"loop binding escaped", "fn main() -> Nil { for value in [1] { println(value) }; println(0) }", func(d *KIRDocument) {
			fn := findKIRFunction(d, "main")
			fn.Body[1].Expr.Args[0] = fn.Body[0].Body[0].Expr.Args[0]
		}},
		{"parameter from other function", "fn identity(value: Int) -> Int { return value } fn main() -> Nil { println(0) }", func(d *KIRDocument) {
			findKIRFunction(d, "main").Body[0].Expr.Args[0] = findKIRFunction(d, "identity").Body[0].Return
		}},
		{"duplicate parameter", "fn identity(value: Int) -> Int { return value } fn main() -> Nil {}", func(d *KIRDocument) {
			fn := findKIRFunction(d, "identity")
			fn.Params = append(fn.Params, fn.Params[0])
		}},
		{"immutable declaration with mutable target", "fn main() -> Nil { let mut value = 1; value = 2; println(value) }", func(d *KIRDocument) {
			d.Version = 5
			fn := findKIRFunction(d, "main")
			fn.Body[0].Mutable, fn.Body[0].Binding.Mutable = false, false
			fn.Body[0].Binding.ID, fn.Body[1].Target.Binding.ID, fn.Body[2].Expr.Args[0].Binding.ID = "", "", ""
			fn.Body[2].Expr.Args[0].Binding.Mutable = false
		}},
		{"v6 binding span absent", "fn main() -> Nil { let value = 1; println(value) }", func(d *KIRDocument) {
			fn := findKIRFunction(d, "main")
			fn.Body[0].Binding.Span = nil
			fn.Body[1].Expr.Args[0].Binding.Span = nil
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "binding-scope.kry", test.source)
			var base KIRDocument
			if err := json.Unmarshal(encoded, &base); err != nil {
				t.Fatal(err)
			}
			assertGoAndSelfhostKIRAdmission(t, program, checker, limits, &base, true)
			forged := cloneKIRDocument(t, base)
			test.mutate(forged)
			assertGoAndSelfhostKIRAdmission(t, program, checker, limits, forged, false)
		})
	}
	for _, source := range []string{
		"fn main() -> Nil { let value = 1; if true { let value = 2; println(value) }; println(value) }",
		"fn main() -> Nil { let mut value = 1; value = 2; println(value) }",
		"fn identity(value: Int) -> Int { if value == 1 { let value = 2; return value }; return value } fn main() -> Nil { println(identity(1)) }",
		"fn main() -> Nil { let value = 1; for value in [2] { println(value) }; println(value) }",
		"enum State { Ready, Waiting } fn main() -> Nil { let value = 1; match State::Ready { State::Ready => { let value = 2; println(value) } State::Waiting => { println(value) } }; println(value) }",
		"fn first(value: Int, fallback: Int = value) -> Int { return fallback } fn main() -> Nil { println(first(1)) }",
	} {
		t.Run("valid "+source, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "valid-scope.kry", source)
			var document KIRDocument
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			assertGoAndSelfhostKIRAdmission(t, program, checker, limits, &document, true)
		})
	}
}

func TestKIRLexicalScopesPreserveGlobalsClosuresAndGenerics(t *testing.T) {
	limits := DefaultLimits()
	for _, source := range []string{
		"const value: Int = 7; fn main() -> Int { return value }",
		"fn make(value: Int) -> fn() -> Int { return fn() -> Int { return value } } fn main() -> Int { return make(7)() }",
		"fn identity[T](value: T) -> T { return value } fn main() -> Int { return identity(7) }",
		"struct Box[T] { value: T } impl Box[T] { fn get() -> T { return self.value } } fn main() -> Int { let box: Box[Int] = Box[Int]{value: 7}; return box.get() }",
		"let value: Option[Int] = some(1); match value { some(item) => { println(item) } none => {} }",
	} {
		t.Run(source, func(t *testing.T) {
			encoded := emitValidatorCorpusKIR(t, limits, "complete-scope.kry", source)
			if _, err := DecodeMIR(encoded, limits); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGoAndSelfhostRejectAssignmentToDeclaredImmutableBinding(t *testing.T) {
	limits := DefaultLimits()
	program, diagnostic := LoadProgram(filepath.Join("..", "..", "selfhost", "validated_kir_probe.kry"), limits, "")
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	encoded := emitValidatorCorpusKIR(t, limits, "immutable-place.kry", "fn main() -> Nil { let mut value = 1; value = 2; println(value) }")
	var base KIRDocument
	if err := json.Unmarshal(encoded, &base); err != nil {
		t.Fatal(err)
	}
	for _, mutable := range []bool{true, false} {
		t.Run(map[bool]string{true: "mutable", false: "immutable"}[mutable], func(t *testing.T) {
			document := cloneKIRDocument(t, base)
			document.Version = 5
			body := findKIRFunction(document, "main").Body
			body[0].Mutable, body[0].Binding.Mutable = mutable, mutable
			body[1].Target.Binding.Mutable, body[2].Expr.Args[0].Binding.Mutable = mutable, mutable
			output := assertGoAndSelfhostKIRAdmission(t, program, checker, limits, document, mutable)
			if !mutable {
				if !bytes.Contains([]byte(output), []byte("immutable")) {
					t.Fatalf("expected semantic mutability diagnostic, got %s", output)
				}
				encoded, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := DecodeMIR(encoded, limits); err == nil || !bytes.Contains([]byte(err.Error()), []byte("immutable")) {
					t.Fatalf("expected Go semantic mutability diagnostic, got %v", err)
				}
			}
		})
	}
}
