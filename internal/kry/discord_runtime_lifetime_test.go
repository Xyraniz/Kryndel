package kry

import (
	"testing"
	"time"
)

func TestDiscordBotCanOptOutOfRuntimeWallClockLimit(t *testing.T) {
	program, checker := loadDiscordTestProgram(t, `fn main() -> Result[Nil, String] {
    sleep_ms(40)?
    return ok(nil)
}
`)
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 0
	runtime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if _, ok := runtime.Ctx.Ctx.Deadline(); ok {
		t.Fatal("zero MaxWallTimeMS must not set a deadline")
	}

	started := time.Now()
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("long-running bot fixture failed with wall limit disabled: %s", diagnostic.Message)
	}
	if elapsed := time.Since(started); elapsed < 35*time.Millisecond {
		t.Fatalf("bot fixture returned after %s; expected its 40 ms wait to finish", elapsed)
	}
}

func TestDiscordBotRetainsConfiguredRuntimeWallClockLimit(t *testing.T) {
	program, checker := loadDiscordTestProgram(t, `fn main() -> Result[Nil, String] {
    sleep_ms(30)?
    return ok(nil)
}
`)
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 2
	runtime, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		t.Fatal(diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic == nil || diagnostic.Message != "wall-clock execution limit exceeded" {
		t.Fatalf("configured wall-clock limit diagnostic = %v, want limit exceeded", diagnostic)
	}
}

func TestNetworkOperationsStayBoundedWhenRuntimeWallLimitIsDisabled(t *testing.T) {
	if got, want := networkTimeout(0), 10*time.Second; got != want {
		t.Fatalf("network timeout with disabled wall limit = %s, want %s", got, want)
	}
	if got, want := networkTimeout(250), 250*time.Millisecond; got != want {
		t.Fatalf("network timeout with configured wall limit = %s, want %s", got, want)
	}
	if deadline := socketDeadline(0); time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Second {
		t.Fatalf("socket deadline with disabled wall limit is not bounded: %s", deadline)
	}
}
