package kry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCCodegenLowersValidatedKIRWithImportsTraitsAndGenerics(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "library.kry"), []byte(`
pub trait Show { fn show() -> String }
pub struct Box[T] { value: T }
impl Show for Box[Int] {
    fn show() -> String { return str(self.value) }
}
pub fn show[T: Show](value: T) -> String { return value.show() }
pub fn identity[T: Copy](value: T) -> T { return value }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.kry")
	if err := os.WriteFile(mainPath, []byte(`
import "library"
enum State { Ready, Waiting }
let value: Box[Int] = identity(Box[Int]{value: 41})
println(show(value))
println(State::Ready)
`), 0o600); err != nil {
		t.Fatal(err)
	}
	program, diagnostic := LoadProgram(mainPath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load failed: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("check failed: %s", diagnostic.Message)
	}
	mir, err := CompileMIR(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatalf("MIR compilation failed: %v", err)
	}
	program, checker = nil, nil
	generated, err := generateCFromValidatedKIR(mir, DefaultLimits(), false)
	if err != nil {
		t.Fatalf("MIR C lowering failed: %v", err)
	}
	for _, want := range []string{"Box[Int]", "State", "kfn_identity_g", "kfn_show_", "kv_enum("} {
		if !strings.Contains(generated, want) {
			t.Errorf("generated C does not contain %q", want)
		}
	}
}
