package kry

import (
	"runtime"
	"strings"
	"testing"
)

func TestUtilityBuiltinsInterpreter(t *testing.T) {
	if values, err := dotenvParse("export NAME=Kryndel\nGREETING=hello ${NAME}\nQUOTED=\"a b\" # comment\n"); err != nil {
		t.Fatalf("dotenv fixture parse: %v", err)
	} else if values["GREETING"] != "hello Kryndel" || values["QUOTED"] != "a b" {
		t.Fatalf("dotenv fixture values: %#v", values)
	}
	hostOutput := runInterpUnrestricted(t, `fn main() -> Nil {
    println(platform_os())
    println(platform_arch())
    println(platform_runtime())
    match platform_os_version() {
        ok(value) => { println(str(len(value) > 0)) }
        err(problem) => { println("version err") }
    }
    match platform_hostname() {
        ok(value) => { println(str(len(value) > 0)) }
        err(problem) => { println("hostname err") }
    }
    return nil
}
main()
`)
	hostLines := strings.Split(strings.TrimSuffix(hostOutput, "\n"), "\n")
	if len(hostLines) != 5 || hostLines[0] != runtime.GOOS || hostLines[1] != runtime.GOARCH || !strings.HasPrefix(hostLines[2], "go") || hostLines[3] != "true" || hostLines[4] != "true" {
		t.Fatalf("unexpected host utility output: %q", hostOutput)
	}
	src := `fn main() -> Nil {
    match uuid_v4() {
        ok(value) => { println(str(uuid_is_valid(value))) }
        err(problem) => { println("uuid err") }
    }
    match uuid_v5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "www.example.com") {
        ok(value) => { println(value); println(str(uuid_is_valid(value))) }
        err(problem) => { println("uuid5 err") }
    }
    let first: Random = random_new(42)
    let second: Random = random_new(42)
    match random_int(first, 10, 20) {
        ok(value) => { println(str(value)) }
        err(problem) => { println("random err") }
    }
    match random_int(second, 10, 20) {
        ok(value) => { println(str(value)) }
        err(problem) => { println("random err") }
    }
    println(str(random_float(first) >= 0.0))
    match regex_compile("[a-z]+") {
        ok(rx) => {
            println(str(regex_is_match(rx, "abc 123")))
            match regex_find(rx, "123 abc") {
                some(value) => { println(value) }
                none => { println("find none") }
            }
            println(str(regex_find_all(rx, "one TWO three")))
            println(regex_replace_all(rx, "one two", "X"))
            println(str(regex_split(rx, "one 123 two")))
        }
        err(problem) => { println("regex err") }
    }
    match datetime_parse("2024-01-02", "2006-01-02") {
        ok(value) => {
            match datetime_format(value, "2006-01-02") {
                ok(formatted) => { println(formatted) }
                err(problem) => { println("format err") }
            }
        }
        err(problem) => { println("parse err") }
    }
    match fs_write_text(".env", "export NAME=Kryndel\nGREETING=hello ${NAME}\nQUOTED=\"a b\" # comment\n") {
        ok(done) => {
            match dotenv_load(".env") {
                ok(values) => {
                    println(str(map_get(values, "GREETING")))
                    println(str(map_get(values, "QUOTED")))
                }
                err(problem) => { println("dotenv err") }
            }
        }
        err(problem) => { println("write err") }
    }
    return nil
}
`
	got := runInterp(t, src)
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 14 {
		t.Fatalf("unexpected utility output (%d lines): %q", len(lines), got)
	}
	if lines[0] != "true" || lines[2] != "true" || lines[3] != lines[4] || lines[5] != "true" || lines[7] != "abc" || lines[11] != "2024-01-02" || lines[12] != "some(hello Kryndel)" || lines[13] != "some(a b)" {
		t.Fatalf("unexpected utility output: %q", got)
	}
}

func TestUUIDV5KnownVector(t *testing.T) {
	got, err := uuidV5("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := "2ed6657d-e927-568b-95e1-2665a8aea6a2"
	if got != want {
		t.Fatalf("uuid v5 mismatch: got %q want %q", got, want)
	}
}

func TestDotenvRejectsMalformedInput(t *testing.T) {
	if _, err := dotenvParse("not-a-binding"); err == nil {
		t.Fatal("malformed dotenv input was accepted")
	}
	if _, err := dotenvParse("BAD-NAME=value"); err == nil {
		t.Fatal("invalid dotenv variable name was accepted")
	}
}
