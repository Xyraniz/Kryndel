package kry

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const traitDispatchFixture = `
trait Render {
    fn render() -> String
}

struct North { value: String }
struct South { value: String }

impl Render for North {
    fn render() -> String { return "north:" + self.value }
}

impl Render for South {
    fn render() -> String { return "south:" + self.value }
}

fn render_value[T: Render](value: T) -> String {
    return value.render()
}

fn main() -> Nil {
    let north: North = North{value: "one"}
    let south: South = South{value: "two"}
    println(render_value(north))
    println(render_value(south))
    println(north.render())
    println(south.render())
}
`

func TestTraitBoundDispatchMatchesInterpreterAndCAOT(t *testing.T) {
	interpreted, diagnostic := runInterpreterCapture(t, traitDispatchFixture)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, traitDispatchFixture)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("trait dispatch differs: interpreter %q, C AOT (%d) %q", interpreted, status, native)
	}
	want := "north:one\nsouth:two\nnorth:one\nsouth:two\n"
	if interpreted != want {
		t.Fatalf("unexpected trait output: got %q, want %q", interpreted, want)
	}
}

const traitGenericInstantiationFixture = `
trait Describe {
    fn describe() -> String
}

struct Box[T] { value: T }

impl Describe for Box[Int] {
    fn describe() -> String { return "int:" + str(self.value) }
}

impl Describe for Box[String] {
    fn describe() -> String { return "string:" + self.value }
}

fn describe[T: Describe](value: T) -> String {
    return value.describe()
}

fn main() -> Nil {
    let number: Box[Int] = Box[Int]{value: 7}
    let text: Box[String] = Box[String]{value: "ok"}
    println(describe(number))
    println(describe(text))
}
`

func TestTraitBoundDispatchMonomorphizesGenericStructReceivers(t *testing.T) {
	interpreted, diagnostic := runInterpreterCapture(t, traitGenericInstantiationFixture)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, traitGenericInstantiationFixture)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("generic trait dispatch differs: interpreter %q, C AOT (%d) %q", interpreted, status, native)
	}
	want := "int:7\nstring:ok\n"
	if interpreted != want {
		t.Fatalf("unexpected generic trait output: got %q, want %q", interpreted, want)
	}
}

func TestTraitKIRV5RoundTripRetainsStaticDispatchAndFunctionLocations(t *testing.T) {
	program, checker := testProgram(t, traitDispatchFixture)
	first, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("trait KIR emission is not deterministic")
	}
	if !strings.Contains(string(first), `"call_target": "trait:Render::render"`) {
		t.Fatalf("KIR omitted the symbolic trait method target: %s", first)
	}
	document, err := DecodeKIR(first, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if document.Version != 5 || len(document.Traits) != 1 || len(document.TraitImpls) != 2 {
		t.Fatalf("unexpected trait KIR declarations: version=%d traits=%d impls=%d", document.Version, len(document.Traits), len(document.TraitImpls))
	}
	for _, function := range document.Functions {
		if function.Source == "" || function.Line < 1 || function.Column < 1 {
			t.Fatalf("KIR lost source position for %q: %#v", function.Name, function)
		}
	}
}

func TestTraitCheckerRejectsIncompleteOrUnsupportedImplementations(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "missing method",
			src: `trait Render { fn render() -> String }
struct North { value: String }
impl Render for North { fn other() -> String { return "x" } }`,
			want: "missing method 'render'",
		},
		{
			name: "extra method",
			src: `trait Render { fn render() -> String }
struct North { value: String }
impl Render for North {
    fn render() -> String { return "x" }
    fn other() -> String { return "y" }
}`,
			want: "does not declare method 'other'",
		},
		{
			name: "wrong signature",
			src: `trait Render { fn render() -> String }
struct North { value: String }
impl Render for North { fn render() -> Int { return 1 } }`,
			want: "does not match trait 'Render' signature",
		},
		{
			name: "duplicate impl",
			src: `trait Render { fn render() -> String }
struct North { value: String }
impl Render for North { fn render() -> String { return "x" } }
impl Render for North { fn render() -> String { return "y" } }`,
			want: "already implemented for North",
		},
		{
			name: "blanket impl",
			src: `trait Render { fn render() -> String }
impl Render for T { fn render() -> String { return "x" } }`,
			want: "generic and blanket implementations are not supported",
		},
		{
			name: "unknown bound",
			src:  `fn render_value[T: Missing](value: T) -> String { return "x" }`,
			want: "unknown type constraint 'Missing'",
		},
		{
			name: "multiple bounds",
			src:  `fn render_value[T: Render + Copy](value: T) -> String { return "x" }`,
			want: "multiple trait bounds are not supported",
		},
		{
			name: "generic trait",
			src:  `trait Render[T] { fn render() -> String }`,
			want: "generic trait declarations are not supported",
		},
		{
			name: "default method",
			src:  `trait Render { fn render() -> String { return "x" } }`,
			want: "default trait method bodies are not supported",
		},
		{
			name: "dynamic trait object",
			src: `trait Render { fn render() -> String }
fn take(value: dyn Render) -> Nil {}`,
			want: "dynamic trait objects using 'dyn Trait' are not supported",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program, diagnostic := Parse(&Source{Name: "trait-negative.kry", Text: test.src}, DefaultLimits())
			if diagnostic == nil {
				_, diagnostic = Check(program, DefaultLimits())
			}
			if diagnostic == nil || !strings.Contains(diagnostic.Message, test.want) {
				t.Fatalf("expected diagnostic containing %q, got %#v", test.want, diagnostic)
			}
		})
	}
}

func TestKIRRejectsMalformedTraitImplementation(t *testing.T) {
	program, checker := testProgram(t, traitDispatchFixture)
	encoded, err := EmitKIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var document KIRDocument
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	document.TraitImpls[0].Methods = nil
	malformed, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeKIR(malformed, DefaultLimits()); err == nil || !strings.Contains(err.Error(), "missing or extra methods") {
		t.Fatalf("expected malformed trait implementation to be rejected, got %v", err)
	}
}
