package kry

import (
	"strings"
	"testing"
)

func TestCAOTGenericStructFunctionsAndMethodsMatchInterpreter(t *testing.T) {
	source := `
struct Box[T] { value: T }

impl Box[T] {
    fn get() -> T { return self.value }
    fn rebox[U: Copy](value: U) -> Box[U] {
        return Box[U]{value: value}
    }
}

fn make_box[T: Copy](value: T) -> Box[T] {
    return Box[T]{value: value}
}

fn main() -> Nil {
    let integer_box = make_box(13)
    let string_box = make_box("thirteen")
    let nested_box: Box[Box[Int]] = Box[Box[Int]]{value: integer_box}
    let array_box: Box[Array[Int]] = Box[Array[Int]]{value: [2, 5, 8]}
    println(integer_box)
    println(string_box)
    println(integer_box.get())
    println(string_box.get())
    println(integer_box.rebox("reboxed string"))
    println(string_box.rebox(21))
    println(nested_box)
    println(array_box)
}
`

	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil {
		t.Fatalf("C AOT build failed: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("generic struct results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", 0, interpreted, status, native)
	}
	for _, name := range []string{"Box[Int]", "Box[String]", "Box[Box[Int]]", "Box[Array[Int]]"} {
		if !strings.Contains(native, name) {
			t.Errorf("C AOT output %q is missing concrete type name %q", native, name)
		}
	}
}

func TestCAOTRejectsGenericWorkerWithoutSpecializationArguments(t *testing.T) {
	program, checker := testProgram(t, `
fn generic_worker[T]() -> Nil { println("worker") }
fn main() -> Nil {
    let worker: Thread[Nil] = thread_spawn("generic_worker")
    thread_join(worker)
}
`)
	if _, err := GenerateC(program, checker); err == nil || !strings.Contains(err.Error(), "generic worker function values") {
		t.Fatalf("expected a precise C AOT generic-worker diagnostic, got %v", err)
	}
}

func TestCAOTUninstantiatedGenericFunctionIsNotEmittedAsOpenCode(t *testing.T) {
	program, checker := testProgram(t, `
struct Box[T] { value: T }
fn wrap[T: Copy](value: T) -> Box[T] { return Box[T]{value: value} }
fn main() -> Nil { println("ready") }
`)
	generated, err := GenerateC(program, checker)
	if err != nil {
		t.Fatalf("C AOT rejected an unused generic declaration: %v", err)
	}
	if strings.Contains(generated, "Box[T]") || strings.Contains(generated, "_f[0] = T") {
		t.Fatalf("C AOT emitted an open generic function body:\n%s", generated)
	}
}
