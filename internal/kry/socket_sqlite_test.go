package kry

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestSQLiteBuiltinsInterpreter(t *testing.T) {
	src := `fn main() -> Nil {
    match sqlite_open(":memory:") {
        ok(db) => {
            match sqlite_exec(db, "CREATE TABLE users (id INTEGER, name TEXT)") {
                ok(count) => {
                    match sqlite_exec(db, "INSERT INTO users VALUES (1, 'Ada'), (2, 'Grace')") {
                        ok(inserted) => {
                            match sqlite_query(db, "SELECT id, name FROM users ORDER BY id") {
                                ok(rows) => { println(str(rows)) }
                                err(problem) => { println("query err") }
                            }
                        }
                        err(problem) => { println("insert err") }
                    }
                }
                err(problem) => { println("create err") }
            }
            match sqlite_query(db, "SELECT missing FROM users") {
                ok(rows) => { println("bad query accepted") }
                err(problem) => { println("query rejected") }
            }
            sqlite_close(db)
        }
        err(problem) => { println("open err") }
    }
    return nil
}
`
	got := runInterpUnrestricted(t, src)
	want := "[[1, Ada], [2, Grace]]\nquery rejected\n"
	if got != want {
		t.Fatalf("unexpected SQLite output: got %q want %q", got, want)
	}
}

func TestTCPBuiltinsInterpreter(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		request := make([]byte, 4)
		if _, err := io.ReadFull(conn, request); err != nil {
			done <- err
			return
		}
		if string(request) != "ping" {
			done <- fmt.Errorf("server received %q", request)
			return
		}
		_, err = conn.Write([]byte("pong"))
		done <- err
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	src := fmt.Sprintf(`fn main() -> Nil {
    match tcp_connect("127.0.0.1", %d) {
        ok(socket) => {
            match tcp_send(socket, string_to_bytes("ping")) {
                ok(written) => {
                    match tcp_receive(socket, 4) {
                        ok(data) => { println(bytes_to_string(data)) }
                        err(problem) => { println("receive err") }
                    }
                }
                err(problem) => { println("send err") }
            }
            tcp_close(socket)
        }
        err(problem) => { println("connect err") }
    }
    match tcp_connect("127.0.0.1", 70000) {
        ok(socket) => { println("invalid port accepted") }
        err(problem) => { println("invalid port rejected") }
    }
    return nil
}
`, port)
	got := runInterpUnrestricted(t, src)
	if got != "pong\ninvalid port rejected\n" {
		t.Fatalf("unexpected TCP output: %q", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUDPBuiltinsInterpreter(t *testing.T) {
	receiver, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	done := make(chan error, 1)
	go func() {
		data := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(data)
		if err != nil {
			done <- err
			return
		}
		if string(data[:n]) != "ping" {
			done <- fmt.Errorf("receiver got %q", data[:n])
			return
		}
		done <- nil
	}()
	port := receiver.LocalAddr().(*net.UDPAddr).Port
	src := fmt.Sprintf(`fn main() -> Nil {
    match udp_bind("", 0) {
        ok(socket) => {
            match udp_send(socket, "127.0.0.1", %d, string_to_bytes("ping")) {
                ok(written) => { println(str(written)) }
                err(problem) => { println("send err") }
            }
            udp_close(socket)
        }
        err(problem) => { println("bind err") }
    }
    return nil
}
`, port)
	got := runInterpUnrestricted(t, src)
	if !strings.HasSuffix(got, "\n") || strings.TrimSpace(got) != "4" {
		t.Fatalf("unexpected UDP output: %q", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
