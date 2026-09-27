package kry

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSQLiteCRuntimeDynamicLoaderHarness(t *testing.T) {
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "linux" && runtime.GOOS != "windows") {
		t.Skip("SQLite C harness requires Linux or Windows amd64")
	}
	target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
	source := cRuntimePrelude + cRuntimeDisplay + cRuntimeBuiltins + cRuntimeExtra + cRuntimeCrypto + `
int main(void) {
    if (setjmp(k_jmp)) {
        printf("close error:%s\n", k_errbuf);
        k_sqlite_cleanup(); k_tcp_cleanup();
        return 0;
    }
    k_max_array_elements=16;
    KValue opened=k_sqlite_open(kv_cstr(":memory:"));
    if (!opened.u.res.ok) { k_print(*opened.u.res.inner,1); k_sqlite_cleanup(); k_tcp_cleanup(); return 2; }
    KValue database=*opened.u.res.inner;
    KValue rows=k_sqlite_query(database,kv_cstr("SELECT NULL, 17, 0.1, 'text', X'6100FF'"));
    k_print(rows,1);
    k_max_array_elements=1;
    KValue limited=k_sqlite_query(database,kv_cstr("SELECT 1 UNION ALL SELECT 2"));
    k_print(limited,1);
    KValue wide=k_sqlite_query(database,kv_cstr("SELECT 1, 2"));
    k_print(wide,1);
    k_sqlite_close(database);
    k_sqlite_close(database);
    return 3;
}
`
	binary, err := compileC(source, target)
	if err != nil {
		t.Fatalf("compile SQLite C runtime harness: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sqlite-runtime-harness")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("SQLite C runtime harness failed: %v; output=%q", err, output)
	}
	for _, expected := range []string{
		"ok([[, 17, 0.1, text, a\x00\xff]])",
		"err(SQLite result exceeds configured row limit)",
		"err(SQLite result exceeds configured column limit)",
		"close error:SQLite handle is already closed",
	} {
		if !strings.Contains(string(output), expected) {
			t.Errorf("SQLite C runtime output %q does not contain %q", output, expected)
		}
	}
}

func runInterpreterCaptureWithLimits(t *testing.T, source string, limits Limits) (string, *Diagnostic) {
	t.Helper()
	program, diagnostic := Parse(&Source{Name: "sqlite-limits.kry", Text: source}, limits)
	if diagnostic != nil {
		return "", diagnostic
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		return "", diagnostic
	}
	runtimeValue, diagnostic := NewRuntime(program, checker, limits, Sandbox{})
	if diagnostic != nil {
		return "", diagnostic
	}
	old := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = write
	runErr := runtimeValue.run()
	_ = write.Close()
	os.Stdout = old
	data, _ := io.ReadAll(read)
	_ = read.Close()
	return string(data), runErr
}

func buildAndRunNativeAOTWithLimits(t *testing.T, source string, limits Limits) (string, int, error) {
	t.Helper()
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "linux" && runtime.GOOS != "windows") {
		t.Skip("C AOT differential test requires Linux or Windows amd64")
	}
	program, diagnostic := Parse(&Source{Name: "sqlite-limits.kry", Text: source}, limits)
	if diagnostic != nil {
		return "", 0, diagnostic
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		return "", 0, diagnostic
	}
	target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
	format := "elf"
	extension := ""
	if runtime.GOOS == "windows" {
		format = "exe"
		extension = ".exe"
	}
	binary, err := BuildNative(program, checker, target, format)
	if err != nil {
		return "", 0, err
	}
	path := filepath.Join(t.TempDir(), "sqlite-limits"+extension)
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		return "", 0, err
	}
	output, err := exec.Command(path).CombinedOutput()
	if err == nil {
		return strings.ReplaceAll(string(output), "\r\n", "\n"), 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return strings.ReplaceAll(string(output), "\r\n", "\n"), exit.ExitCode(), nil
	}
	return string(output), -1, err
}

func TestCAOTSQLiteColumnConversionsMatchInterpreter(t *testing.T) {
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => {
            match sqlite_query(db, "SELECT NULL, CAST('-9223372036854775808' AS INTEGER), CAST('9223372036854775807' AS INTEGER), CAST('0.0000001' AS REAL), CAST('10000000.0' AS REAL), CAST('1e20' AS REAL), CAST('-0.0' AS REAL), 'text', X'6100FF'") {
                ok(rows) => { println(str(rows)) }
                err(problem) => { println("query error:" + problem) }
            }
            sqlite_close(db)
        }
        err(problem) => { println("open error:" + problem) }
    }
    return nil
}`
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil || status != 0 {
		t.Fatalf("native SQLite conversion program failed: status=%d err=%v output=%q", status, err, native)
	}
	if native != interpreted {
		t.Fatalf("SQLite conversion differs:\ninterpreter: %q\nC AOT:       %q", interpreted, native)
	}
	for _, expected := range []string{"-9223372036854775808", "9223372036854775807", "1e-07", "1e+07", "1e+20", "-0", "text"} {
		if !strings.Contains(native, expected) {
			t.Errorf("SQLite conversion output %q does not contain %q", native, expected)
		}
	}
}

func TestCAOTSQLiteMultipleStatementSemanticsMatchInterpreter(t *testing.T) {
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => {
            match sqlite_exec(db, "CREATE TABLE samples(value INTEGER); INSERT INTO samples VALUES(1); INSERT INTO samples VALUES(2)") {
                ok(count) => { println("exec:" + str(count)) }
                err(problem) => { println("exec error:" + problem) }
            }
            match sqlite_query(db, "SELECT value FROM samples ORDER BY value; SELECT 999") {
                ok(rows) => { println(str(rows)) }
                err(problem) => { println("query error:" + problem) }
            }
            match sqlite_query(db, "SELECT 42; CREATE TABLE final_table(value INTEGER)") {
                ok(rows) => { println("query without final rows:" + str(rows)) }
                err(problem) => { println("query error:" + problem) }
            }
            sqlite_close(db)
        }
        err(problem) => { println("open error:" + problem) }
    }
    return nil
}`
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil || status != 0 {
		t.Fatalf("native SQLite multi-statement program failed: status=%d err=%v output=%q", status, err, native)
	}
	if native != interpreted {
		t.Fatalf("SQLite multi-statement behavior differs:\ninterpreter: %q\nC AOT:       %q", interpreted, native)
	}
}

func TestCAOTSQLiteMissingRuntimeLibraryReturnsErr(t *testing.T) {
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "linux" && runtime.GOOS != "windows") {
		t.Skip("C AOT SQLite test requires Linux or Windows amd64")
	}
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => { sqlite_close(db); println("unexpected SQLite library") }
        err(problem) => { println(problem) }
    }
    return nil
}`
	program, diagnostic := Parse(&Source{Name: "sqlite-missing-library.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	generated, err := GenerateC(program, checker)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		generated = strings.Replace(generated, `const char *names[]={"winsqlite3.dll","sqlite3.dll",NULL};`, `const char *names[]={"kryndel-missing-sqlite-a.dll","kryndel-missing-sqlite-b.dll",NULL};`, 1)
	} else {
		generated = strings.Replace(generated, `const char *names[]={"libsqlite3.so.0","libsqlite3.so",NULL};`, `const char *names[]={"libkryndel-missing-sqlite-a.so","libkryndel-missing-sqlite-b.so",NULL};`, 1)
	}
	target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
	binary, err := compileC(generated, target)
	if err != nil {
		t.Fatalf("compile missing-library regression: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sqlite-missing-library")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, binary, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("missing-library regression exited with error: %v; output=%q", err, output)
	}
	if !strings.Contains(string(output), "SQLite library unavailable") || !strings.Contains(string(output), "install ") {
		t.Fatalf("missing SQLite runtime did not return a clear Err: %q", output)
	}
}

func TestCAOTSQLiteLeakDiagnosticMatchesInterpreter(t *testing.T) {
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => { println("left open") }
        err(problem) => { println("open error:" + problem) }
    }
    return nil
}`
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic == nil || !strings.Contains(diagnostic.Message, "resource 'SQLite' was not closed") {
		t.Fatalf("interpreter did not report SQLite leak: output=%q diagnostic=%#v", interpreted, diagnostic)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil {
		t.Fatalf("build SQLite leak regression: %v", err)
	}
	want := interpreted + "kryndel: " + diagnostic.Message + "\n"
	if status == 0 || native != want {
		t.Fatalf("SQLite leak diagnostic differs: status=%d\ninterpreter output: %q\nC AOT: %q\nwant: %q", status, interpreted, native, want)
	}
}

func TestCAOTSQLiteArrayLimitsMatchInterpreter(t *testing.T) {
	queries := []struct {
		name, query, expected string
	}{
		{name: "rows", query: "SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3", expected: "SQLite result exceeds configured row limit"},
		{name: "columns", query: "SELECT 1, 2, 3", expected: "SQLite result exceeds configured column limit"},
	}
	for _, test := range queries {
		t.Run(test.name, func(t *testing.T) {
			source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => {
            match sqlite_query(db, "` + test.query + `") {
                ok(rows) => { println("query accepted:" + str(rows)) }
                err(problem) => { println(problem) }
            }
            sqlite_close(db)
        }
        err(problem) => { println("open error:" + problem) }
    }
    return nil
}`
			limits := DefaultLimits()
			limits.MaxArrayElements = 2
			interpreted, diagnostic := runInterpreterCaptureWithLimits(t, source, limits)
			if diagnostic != nil {
				t.Fatalf("interpreter failed: %s", diagnostic.Message)
			}
			native, status, err := buildAndRunNativeAOTWithLimits(t, source, limits)
			if err != nil || status != 0 {
				t.Fatalf("native SQLite limit program failed: status=%d err=%v output=%q", status, err, native)
			}
			if native != interpreted || !strings.Contains(native, test.expected) {
				t.Fatalf("SQLite limit mismatch:\ninterpreter: %q\nC AOT:       %q\nexpected:    %q", interpreted, native, test.expected)
			}
		})
	}
}

func TestCAOTSQLiteMatchesInterpreter(t *testing.T) {
	source := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => {
            match sqlite_exec(db, "CREATE TABLE users (id INTEGER, name TEXT, note TEXT)") {
                ok(count) => { println("create:" + str(count)) }
                err(problem) => { println("create error:" + problem) }
            }
            match sqlite_exec(db, "INSERT INTO users VALUES (1, 'Ada', NULL), (2, 'Grace', 'compiler')") {
                ok(count) => { println("insert:" + str(count)) }
                err(problem) => { println("insert error:" + problem) }
            }
            match sqlite_query(db, "SELECT id, name, note FROM users ORDER BY id") {
                ok(rows) => { println(str(rows)) }
                err(problem) => { println("query error:" + problem) }
            }
            match sqlite_query(db, "SELECT missing FROM users") {
                ok(rows) => { println("bad query accepted") }
                err(problem) => { println("query rejected:" + problem) }
            }
            sqlite_close(db)
            match sqlite_exec(db, "SELECT 1") {
                ok(count) => { println("closed database accepted") }
                err(problem) => { println("closed database:" + problem) }
            }
        }
        err(problem) => { println("open error:" + problem) }
    }
    return nil
}`
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil || status != 0 {
		t.Fatalf("native SQLite program failed: status=%d err=%v output=%q", status, err, native)
	}
	if interpreted != native {
		t.Fatalf("SQLite result differs:\ninterpreter: %q\nC AOT:       %q", interpreted, native)
	}
	for _, expected := range []string{"create:0", "insert:2", "[[1, Ada, ], [2, Grace, compiler]]", "query rejected:", "closed database:SQLite handle is closed"} {
		if !strings.Contains(native, expected) {
			t.Errorf("SQLite output %q does not contain %q", native, expected)
		}
	}
}
