package kry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTCPServerBuiltinsInterpreterLoopback(t *testing.T) {
	port := reserveNetworkTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	peerDone := make(chan error, 1)
	go func() {
		var conn net.Conn
		var err error
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		for {
			conn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err == nil || ctx.Err() != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			peerDone <- fmt.Errorf("connect to Kryndel TCP listener: %w", err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		for _, b := range []byte("ping") {
			if _, err := conn.Write([]byte{b}); err != nil {
				peerDone <- err
				return
			}
		}
		response := make([]byte, 4)
		if _, err := io.ReadFull(conn, response); err != nil {
			peerDone <- err
			return
		}
		if string(response) != "pong" {
			peerDone <- fmt.Errorf("TCP peer received %q, want pong", response)
			return
		}
		var extra [1]byte
		n, err := conn.Read(extra[:])
		if err != io.EOF || n != 0 {
			peerDone <- fmt.Errorf("TCP peer read after tcp_close = (%d, %v), want (0, EOF)", n, err)
			return
		}
		peerDone <- nil
	}()

	source := fmt.Sprintf(`fn main() -> Nil {
    match tcp_listen("127.0.0.1", %d) {
        ok(listener) => {
            match tcp_local_port(listener) {
                ok(bound) => { println("local=" + str(bound)) }
                err(problem) => { println("local_error=" + problem) }
            }
            match tcp_accept(listener) {
                ok(socket) => {
                    let mut received: String = ""
                    let mut index: Int = 0
                    while index < 4 {
                        match tcp_receive(socket, 1) {
                            ok(part) => { received = received + bytes_to_string(part) }
                            err(problem) => { received = received + "read_error:" + problem }
                        }
                        index = index + 1
                    }
                    println("accepted=" + received)
                    match tcp_send(socket, string_to_bytes("pong")) {
                        ok(written) => { println("sent=" + str(written)) }
                        err(problem) => { println("send_error=" + problem) }
                    }
                    tcp_close(socket)
                    match tcp_send(socket, string_to_bytes("x")) {
                        ok(written) => { println("send_closed=unexpected success") }
                        err(problem) => { println("send_closed=" + problem) }
                    }
                    match tcp_receive(socket, 1) {
                        ok(data) => { println("receive_closed=unexpected success") }
                        err(problem) => { println("receive_closed=" + problem) }
                    }
                }
                err(problem) => { println("accept_error=" + problem) }
            }
            tcp_listener_close(listener)
            match tcp_local_port(listener) {
                ok(bound) => { println("local_closed=unexpected success") }
                err(problem) => { println("local_closed=" + problem) }
            }
            match tcp_accept(listener) {
                ok(socket) => { println("accept_closed=unexpected success") }
                err(problem) => { println("accept_closed=" + problem) }
            }
        }
        err(problem) => { println("listen_error=" + problem) }
    }
    match tcp_listen("127.0.0.1", 70000) {
        ok(listener) => { println("invalid_port=unexpected success"); tcp_listener_close(listener) }
        err(problem) => { println("invalid_port=" + problem) }
    }
    return nil
}`, port)
	output, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("TCP server interpreter program failed: %s; output=%q", diagnostic.Message, output)
	}
	want := fmt.Sprintf("local=%d\naccepted=ping\nsent=4\nsend_closed=TcpSocket handle is closed\nreceive_closed=TcpSocket handle is closed\nlocal_closed=TcpListener handle is closed\naccept_closed=TcpListener handle is closed\ninvalid_port=port must be between 0 and 65535\n", port)
	if output != want {
		t.Fatalf("TCP server output mismatch:\n got %q\nwant %q", output, want)
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatalf("TCP loopback peer: %v", err)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("TCP loopback peer did not finish")
	}
}

func TestTCPListenerDuplicateCloseDiagnosticMatchesRuntime(t *testing.T) {
	source := `fn main() -> Nil {
    match tcp_listen("127.0.0.1", 0) {
        ok(listener) => { tcp_listener_close(listener); tcp_listener_close(listener) }
        err(problem) => { println(problem) }
    }
    return nil
}`
	output, diagnostic := runInterpreterCapture(t, source)
	if output != "" || diagnostic == nil || diagnostic.Message != "TcpListener handle is already closed" {
		t.Fatalf("duplicate TCP listener close = output %q diagnostic %#v", output, diagnostic)
	}
}

func TestUDPBuiltinsInterpreterLoopback(t *testing.T) {
	peer, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peerPort := peer.LocalAddr().(*net.UDPAddr).Port
	peerDone := make(chan error, 1)
	go func() {
		_ = peer.SetReadDeadline(time.Now().Add(8 * time.Second))
		buffer := make([]byte, 32)
		n, client, err := peer.ReadFromUDP(buffer)
		if err != nil {
			peerDone <- err
			return
		}
		if string(buffer[:n]) != "ping" {
			peerDone <- fmt.Errorf("UDP peer received %q, want ping", buffer[:n])
			return
		}
		for _, response := range []string{"pong", "meta"} {
			if _, err := peer.WriteToUDP([]byte(response), client); err != nil {
				peerDone <- err
				return
			}
		}
		peerDone <- nil
	}()

	maxSourceBytes := DefaultLimits().MaxSourceBytes
	source := fmt.Sprintf(`fn main() -> Nil {
    match udp_bind("127.0.0.1", 0) {
        ok(socket) => {
            match udp_send(socket, "127.0.0.1", 70000, string_to_bytes("bad")) {
                ok(written) => { println("invalid_send=unexpected success") }
                err(problem) => { println("invalid_send=" + problem) }
            }
            match udp_receive(socket, 0) {
                ok(data) => { println("zero_receive=unexpected success") }
                err(problem) => { println("zero_receive=" + problem) }
            }
            match udp_receive_from(socket, %d) {
                ok(packet) => { println("large_receive=unexpected success") }
                err(problem) => { println("large_receive=" + problem) }
            }
            match udp_send(socket, "127.0.0.1", %d, string_to_bytes("ping")) {
                ok(written) => { println("sent=" + str(written)) }
                err(problem) => { println("send_error=" + problem) }
            }
            match udp_receive(socket, 4) {
                ok(data) => { println("received=" + bytes_to_string(data)) }
                err(problem) => { println("receive_error=" + problem) }
            }
            match udp_receive_from(socket, 32) {
                ok(packet) => { println("from=" + json_stringify(packet)) }
                err(problem) => { println("from_error=" + problem) }
            }
            udp_close(socket)
            match udp_send(socket, "127.0.0.1", %d, string_to_bytes("x")) {
                ok(written) => { println("send_closed=unexpected success") }
                err(problem) => { println("send_closed=" + problem) }
            }
            match udp_receive(socket, 1) {
                ok(data) => { println("receive_closed=unexpected success") }
                err(problem) => { println("receive_closed=" + problem) }
            }
            match udp_receive_from(socket, 1) {
                ok(packet) => { println("receive_from_closed=unexpected success") }
                err(problem) => { println("receive_from_closed=" + problem) }
            }
        }
        err(problem) => { println("bind_error=" + problem) }
    }
    match udp_bind("127.0.0.1", 70000) {
        ok(socket) => { println("invalid_bind=unexpected success"); udp_close(socket) }
        err(problem) => { println("invalid_bind=" + problem) }
    }
    return nil
}`, maxSourceBytes+1, peerPort, peerPort)
	output, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("UDP interpreter program failed: %s; output=%q", diagnostic.Message, output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	wantPrefix := []string{
		"invalid_send=port must be between 1 and 65535",
		"zero_receive=receive size is outside configured limits",
		"large_receive=receive size is outside configured limits",
		"sent=4",
		"received=pong",
	}
	if len(lines) != len(wantPrefix)+5 {
		t.Fatalf("UDP output has %d lines, want %d: %q", len(lines), len(wantPrefix)+5, output)
	}
	for i, want := range wantPrefix {
		if lines[i] != want {
			t.Errorf("UDP output line %d = %q, want %q", i, lines[i], want)
		}
	}
	var sender struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
		Data    string `json:"data"`
	}
	if !strings.HasPrefix(lines[5], "from=") {
		t.Fatalf("udp_receive_from output = %q", lines[5])
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[5], "from=")), &sender); err != nil {
		t.Fatalf("decode udp_receive_from JSON: %v; line=%q", err, lines[5])
	}
	decoded, err := base64.StdEncoding.DecodeString(sender.Data)
	if err != nil {
		t.Fatalf("decode udp_receive_from payload: %v", err)
	}
	if sender.Address != "127.0.0.1" || sender.Port != peerPort || string(decoded) != "meta" {
		t.Fatalf("udp_receive_from sender=%+v payload=%q, want 127.0.0.1:%d payload meta", sender, decoded, peerPort)
	}
	wantSuffix := []string{
		"send_closed=UdpSocket handle is closed",
		"receive_closed=UdpSocket handle is closed",
		"receive_from_closed=UdpSocket handle is closed",
		"invalid_bind=port must be between 0 and 65535",
	}
	for i, want := range wantSuffix {
		if lines[i+6] != want {
			t.Errorf("UDP output line %d = %q, want %q", i+6, lines[i+6], want)
		}
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatalf("UDP loopback peer: %v", err)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("UDP loopback peer did not finish")
	}
}

func TestUDPSocketDuplicateCloseDiagnosticMatchesRuntime(t *testing.T) {
	source := `fn main() -> Nil {
    match udp_bind("127.0.0.1", 0) {
        ok(socket) => { udp_close(socket); udp_close(socket) }
        err(problem) => { println(problem) }
    }
    return nil
}`
	output, diagnostic := runInterpreterCapture(t, source)
	if output != "" || diagnostic == nil || diagnostic.Message != "UdpSocket handle is already closed" {
		t.Fatalf("duplicate UDP socket close = output %q diagnostic %#v", output, diagnostic)
	}
}

func TestNativeBackendsRejectTCPServerAndUDPBuiltinsWithExplicitDiagnostics(t *testing.T) {
	fixtures := map[string]string{
		"tcp_listen":         `fn subject() -> Result[TcpListener,String] { return tcp_listen("127.0.0.1", 0) }`,
		"tcp_accept":         `fn subject(listener: TcpListener) -> Result[TcpSocket,String] { return tcp_accept(listener) }`,
		"tcp_local_port":     `fn subject(listener: TcpListener) -> Result[Int,String] { return tcp_local_port(listener) }`,
		"tcp_listener_close": `fn subject(listener: TcpListener) -> Nil { tcp_listener_close(listener); return nil }`,
		"udp_bind":           `fn subject() -> Result[UdpSocket,String] { return udp_bind("127.0.0.1", 0) }`,
		"udp_send":           `fn subject(socket: UdpSocket, data: Bytes) -> Result[Int,String] { return udp_send(socket, "127.0.0.1", 1, data) }`,
		"udp_receive":        `fn subject(socket: UdpSocket) -> Result[Bytes,String] { return udp_receive(socket, 1) }`,
		"udp_receive_from":   `fn subject(socket: UdpSocket) -> Result[Json,String] { return udp_receive_from(socket, 1) }`,
		"udp_close":          `fn subject(socket: UdpSocket) -> Nil { udp_close(socket); return nil }`,
	}
	backends := []struct {
		name   string
		format string
		target NativeTarget
	}{
		{name: "C AOT Linux ELF", format: "elf", target: NativeTarget{OS: "linux", Arch: "amd64"}},
		{name: "C AOT Windows PE", format: "exe", target: NativeTarget{OS: "windows", Arch: "amd64"}},
		{name: "Linux ELF-direct", format: "elf-direct", target: NativeTarget{OS: "linux", Arch: "amd64"}},
		{name: "Windows PE-direct", format: "pe-direct", target: NativeTarget{OS: "windows", Arch: "amd64"}},
	}
	for builtin, subject := range fixtures {
		source := subject + `
fn main() -> Nil {
    let mut iteration: Int = 0
    while iteration < 1 {
        println(iteration)
        iteration = iteration + 1
    }
    return nil
}`
		program, diagnostic := Parse(&Source{Name: "native-network-unsupported.kry", Text: source}, DefaultLimits())
		if diagnostic != nil {
			t.Fatalf("parse %s fixture: %s", builtin, diagnostic.Message)
		}
		checker, diagnostic := Check(program, DefaultLimits())
		if diagnostic != nil {
			t.Fatalf("check %s fixture: %s", builtin, diagnostic.Message)
		}
		for _, backend := range backends {
			t.Run(builtin+"/"+backend.name, func(t *testing.T) {
				_, err := BuildNative(program, checker, backend.target, backend.format)
				want := fmt.Sprintf("builtin %q is not listed as supported by the %s backend for %s-%s; use the interpreter for this feature", builtin, backend.format, backend.target.OS, backend.target.Arch)
				if err == nil || err.Error() != want {
					t.Fatalf("backend rejection diagnostic = %v, want %q", err, want)
				}
			})
		}
	}
}

func reserveNetworkTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
