package kry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscordGatewayCloseTransitions(t *testing.T) {
	packageDir := filepath.Join("..", "..", "packages", "discord")
	fixtureDir := t.TempDir()
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("list Discord package modules: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (filepath.Ext(name) != ".kry" && name != "kry.toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatalf("read Discord package %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDir, name), data, 0o600); err != nil {
			t.Fatalf("copy Discord package %s: %v", name, err)
		}
	}
	fixturePath := filepath.Join(fixtureDir, "close_test.kry")
	const source = `import "main"

fn expect_reconnect(code: Int, reset_session: Bool) -> Result[Nil, String] {
    let problem: String = "WebSocket peer closed with code " + str(code) + ": simulated"
    assert_eq(gateway_close_code(problem), code)
    let next: Result[GatewaySessionState, String] = gateway_close_transition(code, "session-1", 42, "wss://gateway.discord.gg", problem)
    match next {
        ok(state) => {
            assert_eq(state.reconnect, true)
            if reset_session {
                assert_eq(state.session_id, "")
                assert_eq(state.sequence, -1)
            } else {
                assert_eq(state.session_id, "session-1")
                assert_eq(state.sequence, 42)
            }
            assert_eq(state.resume_url, "wss://gateway.discord.gg")
            return ok(nil)
        }
        err(problem) => { return err("close code " + str(code) + " should reconnect: " + problem) }
    }
}

fn expect_terminal(code: Int) -> Result[Nil, String] {
    let problem: String = "WebSocket peer closed with code " + str(code) + ": simulated"
    assert_eq(gateway_close_code(problem), code)
    assert_eq(gateway_close_is_terminal(code), true)
    assert_eq(is_err(gateway_close_transition(code, "session-1", 42, "wss://gateway.discord.gg", problem)), true)
    return ok(nil)
}

fn main() -> Result[Nil, String] {
    expect_reconnect(4001, false)?
    expect_reconnect(4002, false)?
    expect_reconnect(4003, true)?
    expect_reconnect(4005, false)?
    expect_reconnect(4007, true)?
    expect_reconnect(4009, true)?
    expect_terminal(4004)?
    expect_terminal(4010)?
    expect_terminal(4011)?
    expect_terminal(4012)?
    expect_terminal(4013)?
    expect_terminal(4014)?
    assert_eq(gateway_close_code("not a websocket close"), 0)
    return ok(nil)
}
`
	if err := os.WriteFile(fixturePath, []byte(source), 0o600); err != nil {
		t.Fatalf("write Discord Gateway test fixture: %v", err)
	}
	program, diagnostic := LoadProgram(fixturePath, DefaultLimits(), "")
	if diagnostic != nil {
		t.Fatalf("load Discord Gateway close fixture: %s", diagnostic.Message)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatalf("type-check Discord Gateway close fixture: %s", diagnostic.Message)
	}
	runtime, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		t.Fatalf("create Discord Gateway close runtime: %s", diagnostic.Message)
	}
	if diagnostic := runtime.run(); diagnostic != nil {
		t.Fatalf("run Discord Gateway close fixture: %s", diagnostic.Message)
	}
}
