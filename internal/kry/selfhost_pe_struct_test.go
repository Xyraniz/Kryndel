package kry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSelfhostPEBackendRunsLocalScalarStructs(t *testing.T) {
	image, diagnostic := runSelfhostPEBackend(t, `
enum State { Ready, Busy }
struct Point { x: Int, y: Int }
struct Tagged { value: Int, state: State }

fn build_and_read() -> Int {
    let point = Point { y: 23, x: 19 }
    let tagged = Tagged { value: point.x + point.y, state: State::Ready }
    if tagged.state == State::Ready { return tagged.value }
    return 0
}

fn main() -> Nil {
    assert_eq(build_and_read(), 42)
}
`)
	if diagnostic != nil {
		t.Fatalf("selfhost PE backend rejected local scalar-field structs: %v", diagnostic)
	}
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("native struct PE execution requires Windows amd64")
	}
	executable := filepath.Join(t.TempDir(), "scalar-struct.exe")
	if err := os.WriteFile(executable, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(executable).CombinedOutput()
	if err != nil {
		t.Fatalf("selfhost-generated struct PE failed: %v; output: %s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("struct PE wrote unexpected output %q", output)
	}
}

func TestSelfhostPEBackendRejectsUnsupportedStructShapes(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{
			name: "array field",
			source: `struct Batch { values: Array[Int] }
fn main() -> Nil { let batch = Batch { values: [1, 2] } }`,
			want: "must have a scalar or enum type",
		},
		{
			name: "nested struct field",
			source: `struct Inner { value: Int }
struct Outer { inner: Inner }
fn main() -> Nil { let outer = Outer { inner: Inner { value: 1 } } }`,
			want: "must have a scalar or enum type",
		},
		{
			name: "struct parameter",
			source: `struct Point { x: Int }
fn read(point: Point) -> Int { return point.x }
fn main() -> Nil { let point = Point { x: 1 }; assert_eq(read(point), 1) }`,
			want: "function parameters require scalar or enum types",
		},
		{
			name: "struct return",
			source: `struct Point { x: Int }
fn make() -> Point { return Point { x: 1 } }
fn main() -> Nil { let point = make() }`,
			want: "struct function returns are not supported",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image, diagnostic := runSelfhostPEBackend(t, test.source)
			if diagnostic == nil {
				t.Fatalf("unsupported struct shape unexpectedly emitted %d bytes", len(image))
			}
			if !strings.Contains(diagnostic.Error(), test.want) {
				t.Fatalf("unexpected unsupported-struct diagnostic: %v; want substring %q", diagnostic, test.want)
			}
		})
	}
}
