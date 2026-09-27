package kry

import (
	"strings"
	"testing"
)

func TestNestedGenericInferenceAndContextualReturnInference(t *testing.T) {
	source := `
fn preserve[T: Copy](value: Option[Array[T]]) -> Option[Array[T]] { return value }
fn preserve_result[T: Copy](value: Result[Array[T], String]) -> Result[Array[T], String] { return value }
fn relay[U: Copy](value: Option[Array[U]]) -> Option[Array[U]] { return preserve(value) }
fn use_default[T: Copy](first: T, second: T = first) -> T { return second }
fn box[T: Copy](value: T) -> Option[Array[T]] {
    let values: Array[T] = [value]
    return some(values)
}
fn nothing[T: Copy]() -> Option[T] { return none() }
let nested: Option[Array[Int]] = relay(some([1, 2]))
let result: Result[Array[Int], String] = preserve_result(ok([3]))
let boxed: Option[Array[Int]] = box(3)
let empty: Option[Int] = nothing()
let defaulted: Int = use_default(4)
`
	p, d := Parse(&Source{Name: "nested-generics.kry", Text: source}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d != nil {
		t.Fatalf("nested generic inference failed: %s", d.Message)
	}
}

func TestGenericHigherOrderInferenceAcrossArgumentOrder(t *testing.T) {
	program, checker := testProgram(t, `
fn transform[T: Copy](callback: fn(T) -> T, value: T) -> T { return callback(value) }
fn transform_value_first[T: Copy](value: T, callback: fn(T) -> T) -> T { return callback(value) }
struct Pipeline { seed: Int }
impl Pipeline {
    fn apply[T: Copy](callback: fn(T) -> T, value: T) -> T { return callback(value) }
}
fn increment(value: Int) -> Int { return value + 1 }
fn increment(value: String) -> String { return value + "!" }
fn identity[T: Copy](value: T) -> T { return value }
fn identity_callback[T: Copy](callback: fn(T) -> T) -> fn(T) -> T { return callback }
fn echo(value: Int) -> Int { return value }
fn echo(value: String) -> String { return value }
let lambda_first: Int = transform(fn(value: Int) -> Int { return value + 2 }, 3)
let lambda_last: Int = transform_value_first(4, fn(value: Int) -> Int { return value + 3 })
let overload_first: Int = transform(increment, 5)
let generic_first: Int = transform(identity, 6)
let callback_from_context: fn(Int) -> Int = identity_callback(echo)
let pipeline: Pipeline = Pipeline{seed: 0}
let generic_method_first: Int = pipeline.apply(fn(value: Int) -> Int { return value + 4 }, 3)
assert_eq(lambda_first, 5)
assert_eq(lambda_last, 7)
assert_eq(overload_first, 6)
assert_eq(generic_first, 6)
assert_eq(callback_from_context(8), 8)
assert_eq(generic_method_first, 7)
`)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if diagnostic = runtime.run(); diagnostic != nil {
		t.Fatalf("generic higher-order execution failed: %s", diagnostic.Message)
	}
}

func TestGenericHigherOrderInferenceRejectsMismatchedLambdaSignature(t *testing.T) {
	program, diagnostic := Parse(&Source{Name: "generic-callback-mismatch.kry", Text: `
fn transform[T: Copy](callback: fn(T) -> T, value: T) -> T { return callback(value) }
transform(fn(value: Int) -> String { return "wrong" }, 3)
`}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil || !strings.Contains(diagnostic.Message, "no overload of 'transform'") {
		t.Fatalf("expected a generic callback mismatch diagnostic, got %#v", diagnostic)
	}
}

func TestGenericStructInstantiationAndMethods(t *testing.T) {
	program, checker := testProgram(t, `
struct Box[T: Copy] { value: T }
impl Box[T] {
    fn get() -> T { return self.value }
    fn map[U: Copy](callback: fn(T) -> U) -> Box[U] {
        return Box[U]{value: callback(self.value)}
    }
}
fn make_box[T: Copy](value: T) -> Box[T] { return Box[T]{value: value} }
let boxed: Box[Int] = make_box(13)
assert_eq(boxed.get(), 13)
let mapped: Box[String] = boxed.map(fn(value: Int) -> String { return "value=13" })
assert_eq(mapped.value, "value=13")
let nested: Box[Array[Int]] = Box[Array[Int]]{value: [2, 5, 8]}
assert_eq(nested.value[1], 5)
`)
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if diagnostic = runtime.run(); diagnostic != nil {
		t.Fatalf("generic struct execution failed: %s", diagnostic.Message)
	}
}

func TestGenericStructRejectsBadArityConstraintAndFields(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "missing arguments", source: "struct Box[T] { value: T }\nlet value = Box{value: 1}"},
		{name: "wrong arity", source: "struct Pair[A, B] { first: A, second: B }\nlet value: Pair[Int] = Pair[Int]{first: 1, second: 2}"},
		{name: "constraint", source: "struct CopyBox[T: Copy] { value: T }\nlet value: CopyBox[SQLite] = CopyBox[SQLite]{value: nil}"},
		{name: "field type", source: "struct Box[T] { value: T }\nlet value: Box[Int] = Box[Int]{value: \"wrong\"}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, diagnostic := Parse(&Source{Name: "generic-struct-error.kry", Text: test.source}, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			if _, diagnostic = Check(program, DefaultLimits()); diagnostic == nil {
				t.Fatal("expected generic struct type error")
			}
		})
	}
}

func TestGenericStructKIRRoundTripPreservesInstantiation(t *testing.T) {
	program, checker := testProgram(t, `
struct Box[T] { value: T }
fn wrap[T: Copy](value: T) -> Box[T] { return Box[T]{value: value} }
let boxed: Box[Int] = wrap(9)
assert_eq(boxed.value, 9)
`)
	encoded, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("emit generic KIR: %v", err)
	}
	document, err := DecodeKIR(encoded, DefaultLimits())
	if err != nil {
		t.Fatalf("decode generic KIR: %v", err)
	}
	if document.Version != KIRVersion || len(document.Structs) != 1 || len(document.Structs[0].TypeParams) != 1 || document.Structs[0].TypeParams[0].Name != "T" {
		t.Fatalf("generic struct metadata was lost: %#v", document.Structs)
	}
	var sawInstance bool
	var walk func(*KIRExpr)
	walk = func(expression *KIRExpr) {
		if expression == nil {
			return
		}
		if expression.Kind == "struct" && expression.StructName == "Box" && expression.StructType == "Box[T]" && expression.Type == "Box[T]" {
			sawInstance = true
		}
		for _, child := range []*KIRExpr{expression.Left, expression.Right, expression.Operand, expression.Callee, expression.Base, expression.Receiver} {
			walk(child)
		}
		for _, list := range [][]*KIRExpr{expression.Args, expression.Items, expression.MapKeys, expression.Values} {
			for _, child := range list {
				walk(child)
			}
		}
	}
	for _, statement := range document.Functions[0].Body {
		walk(statement.Return)
	}
	if !sawInstance {
		t.Fatal("KIR did not preserve the generic struct expression and its instantiated type")
	}
}

func TestNumericConstraintsEnableGenericOperations(t *testing.T) {
	p, c := testProgram(t, `
fn add[T: Numeric](a: T, b: T) -> T { return a + b }
fn divide[T: Numeric](a: T, b: T) -> T { return a / b }
fn positive[T: Numeric](value: T) -> T { return +value }
fn less[T: Numeric](a: T, b: T) -> Bool { return a < b }
fn remainder[T: Integer](a: T, b: T) -> T { return a % b }
assert_eq(add(3, 4), 7)
assert_eq(add(u8(250), u8(10)), u8(4))
assert_eq(add(1.5, 2.25), 3.75)
assert_eq(divide(8.0, 2.0), 4.0)
assert_eq(positive(2.5), 2.5)
assert_eq(less(u16(4), u16(5)), true)
assert_eq(remainder(-7, 3), -1)
assert_eq(remainder(u8(255), u8(16)), u8(15))
`)
	r, d := NewRuntime(p, c, DefaultLimits(), Sandbox{})
	if d != nil {
		t.Fatal(d.Message)
	}
	if d = r.run(); d != nil {
		t.Fatalf("generic numeric execution failed: %s", d.Message)
	}
}

func TestGenericNumericConstraintsRejectUnsafeAndMismatchedOperations(t *testing.T) {
	cases := []struct {
		name, source, diagnostic string
	}{
		{
			name:       "unsigned negation",
			source:     `fn negate[T: Numeric](value: T) -> T { return -value }`,
			diagnostic: "because it may be UInt",
		},
		{
			name:       "remainder needs integer constraint",
			source:     `fn remainder[T: Numeric](a: T, b: T) -> T { return a % b }`,
			diagnostic: "remainder operands must have matching Int or UInt types",
		},
		{
			name:       "distinct type parameters",
			source:     `fn add[T: Numeric, U: Numeric](a: T, b: U) -> T { return a + b }`,
			diagnostic: "arithmetic operands must have matching numeric types",
		},
		{
			name:       "float remainder",
			source:     `let invalid: Float = 7.0 % 2.0`,
			diagnostic: "remainder operands must have matching Int or UInt types",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, d := Parse(&Source{Name: "generic-operation-error.kry", Text: tc.source}, DefaultLimits())
			if d != nil {
				t.Fatalf("parse: %s", d.Message)
			}
			if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, tc.diagnostic) {
				t.Fatalf("expected diagnostic %q, got %#v", tc.diagnostic, d)
			}
		})
	}
}

func TestGenericInferenceRejectsConflictsAndConstraintViolations(t *testing.T) {
	cases := []struct {
		name, source string
	}{
		{
			name:   "conflicting nested arguments",
			source: `fn inspect[T: Copy](a: Array[T], b: Option[T]) -> Nil { return nil }` + "\n" + `inspect([1], some("wrong"))`,
		},
		{
			name:   "incompatible constraint",
			source: `fn add[T: Numeric](value: T) -> T { return value }` + "\n" + `let result: String = add("not numeric")`,
		},
		{
			name:   "unresolved type parameter",
			source: `fn create[T: Copy]() -> Option[T] { return none() }` + "\n" + `create()`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, d := Parse(&Source{Name: "generic-error.kry", Text: tc.source}, DefaultLimits())
			if d != nil {
				t.Fatalf("parse: %s", d.Message)
			}
			if _, d = Check(p, DefaultLimits()); d == nil || !strings.Contains(d.Message, "no overload") {
				t.Fatalf("expected generic rejection, got %#v", d)
			}
		})
	}
}

func TestGenericTypeDepthLimitAppliesDuringInference(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxTypeDepth = 2
	p, d := Parse(&Source{Name: "deep-generic.kry", Text: `fn use[T: Copy](value: Option[Array[Option[T]]]) -> Nil { return nil }`}, lim)
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	if _, d = Check(p, lim); d == nil || !strings.Contains(d.Message, "type depth limit") {
		t.Fatalf("expected type depth resource diagnostic, got %#v", d)
	}
}

func TestOverloadCandidateLimitIsConfigurable(t *testing.T) {
	lim := DefaultLimits()
	lim.MaxOverloadsPerName = 2
	source := `
fn select(value: Int) -> String { return "int" }
fn select(value: String) -> String { return "string" }
fn select(value: Bool) -> String { return "bool" }
`
	p, d := Parse(&Source{Name: "too-many-overloads.kry", Text: source}, lim)
	if d != nil {
		t.Fatal(d.Message)
	}
	if _, d = Check(p, lim); d == nil || d.Category != CatResource || !strings.Contains(d.Message, "overload limit of 2") {
		t.Fatalf("expected configurable overload limit diagnostic, got %#v", d)
	}
}
