package kry

import (
	"strings"
	"testing"
)

func TestRuntimeReportsAndClosesLeakedSQLiteHandle(t *testing.T) {
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => { }
        err(problem) => { }
    }
    return nil
}`
	_, d := runInterpreterCapture(t, source)
	if d == nil || d.Category != CatResource || !strings.Contains(d.Message, "SQLite") || !strings.Contains(d.Message, "not closed") {
		t.Fatalf("expected resource leak diagnostic, got %#v", d)
	}
	if d.Line != 2 {
		t.Fatalf("leak diagnostic should point to acquisition, got line %d", d.Line)
	}
}

func TestRuntimeLeakCleanupClosesSQLiteHandle(t *testing.T) {
	db, err := sqliteOpen(Sandbox{}, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{}
	r.trackResource(&Expr{Tok: Token{Source: &Source{Name: "lifecycle.kry"}, Line: 7, Column: 3}}, "SQLite", db.isClosed, func() error { return sqliteClose(db) })
	if d := r.closeResources(nil); d == nil || d.Category != CatResource {
		t.Fatalf("expected resource diagnostic, got %#v", d)
	}
	if _, err := sqliteExec(db, "SELECT 1"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("leaked handle was not closed during cleanup: %v", err)
	}
}

func TestUnjoinedWorkerResourceLeakReachesInvocation(t *testing.T) {
	source := `let started: Channel[Nil] = thread_channel()
fn worker() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => { }
        err(problem) => { }
    }
    thread_send(started, nil)
    while true { }
    return nil
}
let worker_thread: Thread[Nil] = thread_spawn("worker")
thread_receive(started)
thread_join_timeout(worker_thread, 5)`
	_, d := runInterpreterCapture(t, source)
	if d == nil || d.Category != CatResource || !strings.Contains(d.Message, "SQLite") {
		t.Fatalf("expected unjoined worker leak diagnostic, got %#v", d)
	}
}

func TestExplicitDoubleCloseReportsRuntimeDiagnostic(t *testing.T) {
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => { sqlite_close(db); sqlite_close(db) }
        err(problem) => { }
    }
    return nil
}`
	_, d := runInterpreterCapture(t, source)
	if d == nil || d.Category != CatRuntime || !strings.Contains(d.Message, "already closed") {
		t.Fatalf("expected double-close runtime diagnostic, got %#v", d)
	}
}
