package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Xyraniz/Kryndel/internal/kry"
)

func TestDebuggerBreakpointsStepsAndShowsCapturedVariables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.kry")
	source := `fn double(value: Int) -> Int {
    let result = value * 2
    return result
}
fn main() -> Nil {
    let answer = double(21)
    println(answer)
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	input := strings.NewReader("break 3\nstep\nlocals\nnext\nlocals\nstack\nfinish\nlocals\nquit\n")
	var commands, diagnostics bytes.Buffer
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writeEnd
	status := debugCmd(kry.NewEngine(), []string{path}, input, &commands, &diagnostics)
	_ = writeEnd.Close()
	os.Stdout = oldStdout
	programOutput, readErr := io.ReadAll(readEnd)
	_ = readEnd.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if status != 0 {
		t.Fatalf("debug command returned %d; diagnostics: %s", status, diagnostics.String())
	}
	if len(programOutput) != 0 {
		t.Fatalf("program continued after debugger quit: output %q", programOutput)
	}
	for _, want := range []string{
		"Breakpoint 1 at " + path + ":3",
		"Paused at " + path + ":2:",
		"value = 21",
		"Paused at " + path + ":3:",
		"result = 42",
		"#0 double at " + path + ":3:",
		"#1 main at " + path + ":5:",
		"Paused at " + path + ":7:",
		"answer = 42",
		"Program stopped by debugger.",
	} {
		if !strings.Contains(commands.String(), want) {
			t.Errorf("debugger output does not contain %q:\n%s", want, commands.String())
		}
	}
}

func TestDebuggerRechecksLoopBreakpointsWithCurrentBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.kry")
	source := `fn main() -> Nil {
    let mut index: Int = 0
    while index < 3 {
        index = index + 1
    }
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands, diagnostics bytes.Buffer
	status := debugCmd(kry.NewEngine(), []string{path}, strings.NewReader("break 3\ncontinue\ncontinue\nlocals\nquit\n"), &commands, &diagnostics)
	if status != 0 {
		t.Fatalf("debug command returned %d; diagnostics: %s", status, diagnostics.String())
	}
	if got := strings.Count(commands.String(), "Paused at "+path+":3:"); got != 2 {
		t.Fatalf("loop breakpoint paused %d times, want 2:\n%s", got, commands.String())
	}
	if !strings.Contains(commands.String(), "index (mutable) = 1") {
		t.Fatalf("loop pause did not expose the current mutable binding:\n%s", commands.String())
	}
}

func TestDebuggerNextStepsOverTailRecursiveCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tail-next.kry")
	source := `fn countdown(value: Int) -> Int {
    if value > 0 {
        return countdown(value - 1)
    }
    return 0
}
fn main() -> Nil {
    let result = countdown(2)
    println(result)
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands, diagnostics bytes.Buffer
	status := debugCmd(kry.NewEngine(), []string{path}, strings.NewReader("break 3\ncontinue\nclear 1\nnext\nlocals\nquit\n"), &commands, &diagnostics)
	if status != 0 {
		t.Fatalf("debug command returned %d; diagnostics: %s", status, diagnostics.String())
	}
	if got := strings.Count(commands.String(), "Paused at "+path+":3:"); got != 1 {
		t.Fatalf("next paused inside a subsequent tail-recursive call %d times, want one initial breakpoint:\n%s", got, commands.String())
	}
	for _, want := range []string{"Paused at " + path + ":9:", "result = 0", "Program stopped by debugger."} {
		if !strings.Contains(commands.String(), want) {
			t.Errorf("debugger output does not contain %q:\n%s", want, commands.String())
		}
	}
}

func TestDebuggerNextStepsOverOrdinaryCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ordinary-next.kry")
	source := `fn increment(value: Int) -> Int {
    let result = value + 1
    return result
}
fn main() -> Nil {
    let answer = increment(2)
    println(answer)
}
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands, diagnostics bytes.Buffer
	status := debugCmd(kry.NewEngine(), []string{path}, strings.NewReader("next\nlocals\nquit\n"), &commands, &diagnostics)
	if status != 0 {
		t.Fatalf("debug command returned %d; diagnostics: %s", status, diagnostics.String())
	}
	for _, want := range []string{"Paused at " + path + ":7:", "answer = 3", "Program stopped by debugger."} {
		if !strings.Contains(commands.String(), want) {
			t.Errorf("debugger output does not contain %q:\n%s", want, commands.String())
		}
	}
	if strings.Contains(commands.String(), "Paused at "+path+":2:") {
		t.Fatalf("next entered the ordinary called function instead of stepping over it:\n%s", commands.String())
	}
}
