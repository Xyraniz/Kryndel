package kry

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestGoValidatorRejectsContextualBuiltinViolations(t *testing.T) {
	const workerSource = `fn worker() -> Int { return 1 }
fn takes_argument(value: Int) -> Int { return value }
fn main() -> Nil {
    thread_spawn("worker")
}`
	const handlerSource = `fn prefix(value: String) -> String { return "prefix:" + value }
fn suffix(value: String) -> String { return value + ":suffix" }
fn takes_integer(value: Int) -> String { return str(value) }
fn main() -> Nil {
    poly_register("format", "prefix", 10)
    poly_reorder("format", "prefix", "suffix")
}`
	renameStringArgument := func(call *KIRExpr, index int, name string) *KIRExpr {
		argument := call.Args[index]
		argument.String = name
		if argument.Const != nil {
			argument.Const.String = name
		}
		return call
	}
	cases := []struct {
		name, source, builtin string
		mutate                func(*KIRFunction) *KIRExpr
	}{
		{
			name: "array membership of function values",
			source: `fn identity(value: Int) -> Int { return value }
fn main() -> Nil {
    let callbacks: Array[fn(Int) -> Int] = [identity]
    println(len(callbacks))
}`,
			builtin: "array_contains",
			mutate: func(main *KIRFunction) *KIRExpr {
				call := main.Body[1].Expr.Args[0]
				call.Args = append(call.Args, main.Body[0].Init.Items[0])
				call.Type = "Bool"
				return call
			},
		},
		{
			name: "array membership of struct containing a function",
			source: `struct Holder { callback: fn(Int) -> Int }
fn identity(value: Int) -> Int { return value }
fn main() -> Nil {
    let holder: Holder = Holder{ callback: identity }
    let callbacks: Array[Holder] = [holder]
    println(len(callbacks))
}`,
			builtin: "array_contains",
			mutate: func(main *KIRFunction) *KIRExpr {
				call := main.Body[2].Expr.Args[0]
				call.Args = append(call.Args, main.Body[1].Init.Items[0])
				call.Type = "Bool"
				return call
			},
		},
		{
			name: "thread send of non-Copy function",
			source: `fn identity(value: Int) -> Int { return value }
fn main() -> Nil {
    let channel: Channel[fn(Int) -> Int] = thread_channel()
    let callback: fn(Int) -> Int = identity
    thread_close(channel)
}`,
			builtin: "thread_send",
			mutate: func(main *KIRFunction) *KIRExpr {
				call := main.Body[2].Expr
				call.Args = append(call.Args, main.Body[1].Init)
				return call
			},
		},
		{
			name:    "thread spawn of missing worker",
			source:  workerSource,
			builtin: "thread_spawn",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[0].Expr, 0, "missing_worker")
			},
		},
		{
			name:    "thread spawn of worker requiring an argument",
			source:  workerSource,
			builtin: "thread_spawn",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[0].Expr, 0, "takes_argument")
			},
		},
		{
			name:    "thread spawn result disagrees with worker return",
			source:  workerSource,
			builtin: "thread_spawn",
			mutate: func(main *KIRFunction) *KIRExpr {
				call := main.Body[0].Expr
				call.Type = "Thread[String]"
				return call
			},
		},
		{
			name:    "poly register of missing handler",
			source:  handlerSource,
			builtin: "poly_register",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[0].Expr, 1, "missing_handler")
			},
		},
		{
			name:    "poly register of incompatible handler",
			source:  handlerSource,
			builtin: "poly_register",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[0].Expr, 1, "takes_integer")
			},
		},
		{
			name:    "poly reorder of missing first handler",
			source:  handlerSource,
			builtin: "poly_reorder",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[1].Expr, 1, "missing_handler")
			},
		},
		{
			name:    "poly reorder of incompatible first handler",
			source:  handlerSource,
			builtin: "poly_reorder",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[1].Expr, 1, "takes_integer")
			},
		},
		{
			name:    "poly reorder of missing second handler",
			source:  handlerSource,
			builtin: "poly_reorder",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[1].Expr, 2, "missing_handler")
			},
		},
		{
			name:    "poly reorder of incompatible second handler",
			source:  handlerSource,
			builtin: "poly_reorder",
			mutate: func(main *KIRFunction) *KIRExpr {
				return renameStringArgument(main.Body[1].Expr, 2, "takes_integer")
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			limits := DefaultLimits()
			encoded := emitValidatorCorpusKIR(t, limits, "builtin-context.kry", test.source)
			if _, err := DecodeMIR(encoded, limits); err != nil {
				t.Fatalf("valid base KIR rejected: %v", err)
			}
			var document KIRDocument
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			main := findKIRFunction(&document, "main")
			if main == nil {
				t.Fatal("valid base KIR has no main function")
			}
			call := test.mutate(main)
			builtin, ok := lookupBuiltin(test.builtin)
			if !ok {
				t.Fatalf("missing builtin %q", test.builtin)
			}
			call.Name, call.CallTarget, call.BuiltinID = builtin.Name, "builtin:"+builtin.Name, builtin.ID
			call.Const = nil

			// The declared type relation still holds. Only the contextual rule
			// rejects this value, worker metadata, or handler declaration.
			argumentTypes := make([]string, len(call.Args))
			for index, argument := range call.Args {
				argumentTypes[index] = argument.Type
			}
			if err := validateBuiltinSignature(builtin, argumentTypes, call.Type); err != nil {
				t.Fatalf("fixture violates the declared signature before its contextual rule: %v", err)
			}
			forged, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeMIR(forged, limits); err == nil {
				t.Fatalf("builtin %q passed admission despite its contextual violation", builtin.Name)
			} else if !strings.Contains(err.Error(), fmt.Sprintf("builtin %q", builtin.Name)) || !strings.Contains(err.Error(), "checked argument or result types") {
				t.Fatalf("contextual fixture was rejected for an unrelated invariant: %v", err)
			}
		})
	}
}
