package kry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStage38SelfHostedEnumMatch(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("the self-hosted ELF fixture runs on Linux amd64")
	}

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "..", "..")
	compiler := filepath.Join(base, "selfhost", "source_kir_compiler.kry")
	fixture := filepath.Join(base, "selfhost", "fixtures", "source_enum_match_stage38.kry")

	program, d := LoadProgram(compiler, DefaultLimits(), "")
	if d != nil {
		t.Fatal(d.Message)
	}
	checker, d := Check(program, DefaultLimits())
	if d != nil {
		t.Fatal(d.Message)
	}

	t.Run("compiles-and-runs", func(t *testing.T) {
		output := filepath.Join(t.TempDir(), "stage38-enum-match")
		r, d := NewRuntimeWithArgs(program, checker, DefaultLimits(), Sandbox{}, []string{fixture, output})
		if d != nil {
			t.Fatal(d.Message)
		}
		if d = r.run(); d != nil {
			t.Fatalf("Stage 38 source compiler failed: %s", d.Message)
		}

		artifact, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		assertLinuxAMD64ELF(t, artifact, "stage38 self-hosted enum match output")
		if err := os.Chmod(output, 0o700); err != nil {
			t.Fatal(err)
		}
		got, err := exec.Command(output).CombinedOutput()
		const want = "red\nyellow\ngreen\nwildcard\n"
		if err != nil || string(got) != want {
			t.Fatalf("stage38 enum match output = %q, error = %v; want %q", got, err, want)
		}
	})

	t.Run("rejects-invalid-matches", func(t *testing.T) {
		invalidMatches := []struct {
			name       string
			source     string
			diagnostic string
		}{
			{
				name: "non-exhaustive",
				source: `enum TrafficLight { Red, Yellow, Green }
fn main() -> Nil {
    match TrafficLight::Red {
        TrafficLight::Red => { println("red") }
        TrafficLight::Yellow => { println("yellow") }
    }
}
`,
				diagnostic: "non-exhaustive match",
			},
			{
				name: "duplicate-variant",
				source: `enum TrafficLight { Red, Yellow }
fn main() -> Nil {
    match TrafficLight::Red {
        TrafficLight::Red => { println("first") }
        TrafficLight::Red => { println("duplicate") }
    }
}
`,
				diagnostic: "duplicate enum match pattern",
			},
			{
				name: "foreign-variant",
				source: `enum TrafficLight { Red, Yellow }
enum Weather { Sunny }
fn main() -> Nil {
    match TrafficLight::Red {
        Weather::Sunny => { println("foreign") }
        _ => { println("fallback") }
    }
}
`,
				diagnostic: "match pattern enum type does not match the scrutinee type",
			},
			{
				name: "wildcard-not-final",
				source: `enum TrafficLight { Red, Yellow }
fn main() -> Nil {
    match TrafficLight::Red {
        TrafficLight::Red => { println("red") }
        _ => { println("fallback") }
        TrafficLight::Yellow => { println("yellow") }
    }
}
`,
				diagnostic: "wildcard pattern must be the final match arm",
			},
		}
		for _, tc := range invalidMatches {
			t.Run(tc.name, func(t *testing.T) {
				input := filepath.Join(t.TempDir(), "invalid.kry")
				if err := os.WriteFile(input, []byte(tc.source), 0o600); err != nil {
					t.Fatal(err)
				}
				invalidOutput := filepath.Join(t.TempDir(), "invalid-output")
				r, d := NewRuntimeWithArgs(program, checker, DefaultLimits(), Sandbox{}, []string{input, invalidOutput})
				if d != nil {
					t.Fatal(d.Message)
				}
				if d = r.run(); d == nil {
					t.Fatal("source compiler accepted invalid enum match")
				} else if !strings.Contains(d.Message, tc.diagnostic) {
					t.Fatalf("source compiler diagnostic = %q, want substring %q", d.Message, tc.diagnostic)
				}
			})
		}
	})
}
