package kry

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWebSocketBuiltinsInterpreterAgainstLoopbackServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(8 * time.Second))
	port := listener.Addr().(*net.TCPAddr).Port
	peerDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			peerDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
		request, reader, err := acceptTestWebSocket(conn)
		if err != nil {
			peerDone <- err
			return
		}
		if request.URL.RequestURI() != "/chat?room=loopback" {
			peerDone <- fmt.Errorf("WebSocket request target = %q", request.URL.RequestURI())
			return
		}

		for index, want := range []string{"client-one", "client-two"} {
			frame, err := readTestClientWebSocketFrame(reader)
			if err != nil {
				peerDone <- fmt.Errorf("read client frame %d: %w", index+1, err)
				return
			}
			if !frame.fin || frame.opcode != 0x1 || string(frame.payload) != want {
				peerDone <- fmt.Errorf("client frame %d = %#v, want final text %q", index+1, frame, want)
				return
			}
		}

		if err := writeTestServerWebSocketFrame(conn, true, 0x1, []byte("server-one")); err != nil {
			peerDone <- err
			return
		}
		if err := writeTestServerWebSocketFrame(conn, false, 0x1, []byte("server-")); err != nil {
			peerDone <- err
			return
		}
		if err := writeTestServerWebSocketFrame(conn, true, 0x9, []byte("probe")); err != nil {
			peerDone <- err
			return
		}
		pong, err := readTestClientWebSocketFrame(reader)
		if err != nil {
			peerDone <- fmt.Errorf("read automatic pong: %w", err)
			return
		}
		if !pong.fin || pong.opcode != 0xA || string(pong.payload) != "probe" {
			peerDone <- fmt.Errorf("automatic pong = %#v, want final pong with probe", pong)
			return
		}
		if err := writeTestServerWebSocketFrame(conn, true, 0x0, []byte("two ✓")); err != nil {
			peerDone <- err
			return
		}
		closeFrame, err := readTestClientWebSocketFrame(reader)
		if err != nil {
			peerDone <- fmt.Errorf("read client close frame: %w", err)
			return
		}
		if !closeFrame.fin || closeFrame.opcode != 0x8 || len(closeFrame.payload) != 2 || binary.BigEndian.Uint16(closeFrame.payload) != 1000 {
			peerDone <- fmt.Errorf("client close frame = %#v, want code 1000", closeFrame)
			return
		}
		if _, err := reader.ReadByte(); err != io.EOF {
			peerDone <- fmt.Errorf("bytes after client close frame: %v, want EOF", err)
			return
		}
		peerDone <- nil
	}()

	source := fmt.Sprintf(`fn main() -> Nil {
    match websocket_connect(%q) {
        ok(socket) => {
            println("connected")
            match websocket_send(socket, "client-one") {
                ok(value) => { println("sent:first") }
                err(problem) => { println("send_first_error=" + problem) }
            }
            match websocket_send(socket, "client-two") {
                ok(value) => { println("sent:second") }
                err(problem) => { println("send_second_error=" + problem) }
            }
            match websocket_receive(socket) {
                ok(message) => { println("received:first=" + message) }
                err(problem) => { println("receive_first_error=" + problem) }
            }
            match websocket_receive(socket) {
                ok(message) => { println("received:second=" + message) }
                err(problem) => { println("receive_second_error=" + problem) }
            }
            websocket_close(socket)
            println("closed")
            websocket_close(socket)
            println("closed_again")
        }
        err(problem) => { println("connect_error=" + problem) }
    }
    return nil
}`, "ws://127.0.0.1:"+strconv.Itoa(port)+"/chat?room=loopback")
	output, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("WebSocket interpreter program failed: %s; output=%q", diagnostic.Message, output)
	}
	want := "connected\nsent:first\nsent:second\nreceived:first=server-one\nreceived:second=server-two ✓\nclosed\nclosed_again\n"
	if output != want {
		t.Fatalf("WebSocket output mismatch:\n got %q\nwant %q", output, want)
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatalf("WebSocket loopback peer: %v", err)
		}
	case <-time.After(9 * time.Second):
		t.Fatal("WebSocket loopback peer did not finish")
	}
}

func TestWebSocketInterpreterReportsHandshakeAndPeerCloseErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(6 * time.Second))
	port := listener.Addr().(*net.TCPAddr).Port
	peerDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			peerDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
		request, _, err := readTestWebSocketRequest(conn)
		if err != nil {
			peerDone <- err
			return
		}
		if request.URL.Path != "/reject" {
			peerDone <- fmt.Errorf("rejected WebSocket path = %q", request.URL.Path)
			return
		}
		_, err = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		peerDone <- err
	}()

	source := fmt.Sprintf(`fn main() -> Nil {
    match websocket_connect("http://127.0.0.1/") {
        ok(socket) => { println("invalid_scheme=unexpected success"); websocket_close(socket) }
        err(problem) => { println("invalid_scheme=" + problem) }
    }
    match websocket_connect(%q) {
        ok(socket) => { println("rejected_handshake=unexpected success"); websocket_close(socket) }
        err(problem) => { println("rejected_handshake=" + problem) }
    }
    return nil
}`, "ws://127.0.0.1:"+strconv.Itoa(port)+"/reject")
	output, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("WebSocket handshake error program failed: %s; output=%q", diagnostic.Message, output)
	}
	want := "invalid_scheme=WebSocket URL must use ws or wss\nrejected_handshake=WebSocket handshake rejected: HTTP/1.1 400 Bad Request\n"
	if output != want {
		t.Fatalf("WebSocket handshake error output = %q, want %q", output, want)
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatalf("rejected handshake peer: %v", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("rejected handshake peer did not finish")
	}

	closeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer closeListener.Close()
	_ = closeListener.(*net.TCPListener).SetDeadline(time.Now().Add(6 * time.Second))
	closePort := closeListener.Addr().(*net.TCPAddr).Port
	closeDone := make(chan error, 1)
	go func() {
		conn, err := closeListener.Accept()
		if err != nil {
			closeDone <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(4 * time.Second))
		_, reader, err := acceptTestWebSocket(conn)
		if err != nil {
			closeDone <- err
			return
		}
		closePayload := make([]byte, 2+len("policy"))
		binary.BigEndian.PutUint16(closePayload[:2], 1008)
		copy(closePayload[2:], "policy")
		if err := writeTestServerWebSocketFrame(conn, true, 0x8, closePayload); err != nil {
			closeDone <- err
			return
		}
		if _, err := reader.ReadByte(); err != io.EOF {
			closeDone <- fmt.Errorf("WebSocket connection after peer close: %v, want EOF", err)
			return
		}
		closeDone <- nil
	}()
	closeSource := fmt.Sprintf(`fn main() -> Nil {
    match websocket_connect(%q) {
        ok(socket) => {
            match websocket_receive(socket) {
                ok(message) => { println("receive=unexpected success") }
                err(problem) => { println("receive=" + problem) }
            }
            websocket_close(socket)
        }
        err(problem) => { println("connect=" + problem) }
    }
    return nil
}`, "ws://127.0.0.1:"+strconv.Itoa(closePort)+"/peer-close")
	closeOutput, closeDiagnostic := runInterpreterCapture(t, closeSource)
	if closeDiagnostic != nil {
		t.Fatalf("WebSocket peer-close program failed: %s; output=%q", closeDiagnostic.Message, closeOutput)
	}
	if closeOutput != "receive=WebSocket peer closed with code 1008: policy\n" {
		t.Fatalf("WebSocket peer-close output = %q", closeOutput)
	}
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("peer-initiated close server: %v", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("peer-initiated close server did not finish")
	}
}

func TestNativeBackendsRejectWebSocketBuiltinsWithExplicitDiagnostics(t *testing.T) {
	fixtures := map[string]string{
		"websocket_connect": `fn subject() -> Result[WebSocket,String] { return websocket_connect("ws://127.0.0.1/") }`,
		"websocket_send":    `fn subject(socket: WebSocket) -> Result[Nil,String] { return websocket_send(socket, "message") }`,
		"websocket_receive": `fn subject(socket: WebSocket) -> Result[String,String] { return websocket_receive(socket) }`,
		"websocket_close":   `fn subject(socket: WebSocket) -> Nil { websocket_close(socket); return nil }`,
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
		program, diagnostic := Parse(&Source{Name: "native-websocket-unsupported.kry", Text: source}, DefaultLimits())
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

type testWebSocketFrame struct {
	fin     bool
	opcode  byte
	payload []byte
}

func acceptTestWebSocket(conn net.Conn) (*http.Request, *bufio.Reader, error) {
	request, reader, err := readTestWebSocketRequest(conn)
	if err != nil {
		return nil, nil, err
	}
	key := request.Header.Get("Sec-WebSocket-Key")
	decoded, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(decoded) != 16 {
		return nil, nil, fmt.Errorf("invalid WebSocket key %q", key)
	}
	if !strings.EqualFold(request.Header.Get("Upgrade"), "websocket") || request.Header.Get("Sec-WebSocket-Version") != "13" || !headerHasToken(request.Header.Get("Connection"), "upgrade") {
		return nil, nil, fmt.Errorf("invalid WebSocket upgrade headers: %#v", request.Header)
	}
	digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(digest[:])
	if _, err := fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept); err != nil {
		return nil, nil, err
	}
	return request, reader, nil
}

func readTestWebSocketRequest(conn net.Conn) (*http.Request, *bufio.Reader, error) {
	reader := bufio.NewReader(conn)
	request, err := http.ReadRequest(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("read WebSocket HTTP request: %w", err)
	}
	if request.Method != http.MethodGet || request.ProtoMajor != 1 || request.ProtoMinor != 1 {
		_ = request.Body.Close()
		return nil, nil, fmt.Errorf("unexpected WebSocket request line: %s %s %s", request.Method, request.URL, request.Proto)
	}
	_ = request.Body.Close()
	return request, reader, nil
}

func readTestClientWebSocketFrame(reader *bufio.Reader) (testWebSocketFrame, error) {
	var frame testWebSocketFrame
	first, err := reader.ReadByte()
	if err != nil {
		return frame, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return frame, err
	}
	frame.fin = first&0x80 != 0
	frame.opcode = first & 0x0f
	length := uint64(second & 0x7f)
	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(reader, ext[:]); err != nil {
			return frame, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	} else if length == 127 {
		var ext [8]byte
		if _, err := io.ReadFull(reader, ext[:]); err != nil {
			return frame, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	if second&0x80 == 0 {
		return frame, fmt.Errorf("client WebSocket frame is not masked")
	}
	if length > 1<<20 {
		return frame, fmt.Errorf("test WebSocket frame is oversized: %d", length)
	}
	var mask [4]byte
	if _, err := io.ReadFull(reader, mask[:]); err != nil {
		return frame, err
	}
	frame.payload = make([]byte, int(length))
	if _, err := io.ReadFull(reader, frame.payload); err != nil {
		return frame, err
	}
	for index := range frame.payload {
		frame.payload[index] ^= mask[index%len(mask)]
	}
	return frame, nil
}

func writeTestServerWebSocketFrame(conn net.Conn, fin bool, opcode byte, payload []byte) error {
	first := opcode & 0x0f
	if fin {
		first |= 0x80
	}
	header := []byte{first}
	switch {
	case len(payload) < 126:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		return fmt.Errorf("test WebSocket frame payload too large: %d", len(payload))
	}
	if _, err := conn.Write(header); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func headerHasToken(value, token string) bool {
	for _, item := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(item), token) {
			return true
		}
	}
	return false
}
