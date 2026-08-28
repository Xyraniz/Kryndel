package kry

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strings"
	"sync"
)

type websocketConn struct {
	conn    net.Conn
	read    *bufio.Reader
	writeMu sync.Mutex
}

func connectWebSocket(raw string) (*websocketConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("WebSocket URL must use ws or wss")
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	var conn net.Conn
	if u.Scheme == "wss" {
		conn, err = tls.Dial("tcp", host, &tls.Config{ServerName: strings.Split(u.Host, ":")[0], MinVersion: tls.VersionTLS12})
	} else {
		conn, err = net.Dial("tcp", host)
	}
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*websocketConn, error) { _ = conn.Close(); return nil, e }
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return fail(err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		return fail(err)
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return fail(err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 101") && !strings.HasPrefix(line, "HTTP/2 101") {
		return fail(fmt.Errorf("WebSocket handshake rejected: %s", strings.TrimSpace(line)))
	}
	headers := map[string]string{}
	for {
		line, err = br.ReadString('\n')
		if err != nil {
			return fail(err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
		}
	}
	digest := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	expected := base64.StdEncoding.EncodeToString(digest[:])
	if headers["sec-websocket-accept"] != expected {
		return fail(fmt.Errorf("invalid WebSocket accept key"))
	}
	return &websocketConn{conn: conn, read: br}, nil
}

func (w *websocketConn) sendText(message string) error { return w.sendFrame(0x1, []byte(message)) }
func (w *websocketConn) sendPong(data []byte) error    { return w.sendFrame(0xA, data) }
func (w *websocketConn) close() error {
	_ = w.sendFrame(0x8, []byte{0x03, 0xE8})
	return w.conn.Close()
}

func (w *websocketConn) sendFrame(opcode byte, payload []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if len(payload) > 16<<20 {
		return fmt.Errorf("WebSocket message exceeds configured limit")
	}
	key := make([]byte, 4)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	var h []byte
	first := byte(0x80 | (opcode & 0x0f))
	n := len(payload)
	switch {
	case n < 126:
		h = []byte{first, byte(0x80 | n)}
	case n <= 65535:
		h = []byte{first, 0xFE, byte(n >> 8), byte(n)}
	default:
		h = []byte{first, 0xFF, byte(uint64(n) >> 56), byte(uint64(n) >> 48), byte(uint64(n) >> 40), byte(uint64(n) >> 32), byte(uint64(n) >> 24), byte(uint64(n) >> 16), byte(uint64(n) >> 8), byte(n)}
	}
	if _, err := w.conn.Write(append(h, key...)); err != nil {
		return err
	}
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ key[i%4]
	}
	_, err := w.conn.Write(masked)
	return err
}
func (w *websocketConn) receiveText(max int) (string, error) {
	for {
		opcode, payload, err := w.receiveFrame(max)
		if err != nil {
			return "", err
		}
		switch opcode {
		case 0x1:
			if !validUTF8(payload) {
				return "", fmt.Errorf("WebSocket text frame is not UTF-8")
			}
			return string(payload), nil
		case 0x8:
			_ = w.conn.Close()
			return "", fmt.Errorf("WebSocket peer closed")
		case 0x9:
			if err := w.sendPong(payload); err != nil {
				return "", err
			}
		default:
		}
	}
}
func (w *websocketConn) receiveFrame(max int) (byte, []byte, error) {
	first, err := w.read.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := w.read.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	opcode := first & 0x0f
	n := int64(second & 0x7f)
	if n == 126 {
		var b [2]byte
		if _, err = io.ReadFull(w.read, b[:]); err != nil {
			return 0, nil, err
		}
		n = int64(b[0])<<8 | int64(b[1])
	} else if n == 127 {
		var b [8]byte
		if _, err = io.ReadFull(w.read, b[:]); err != nil {
			return 0, nil, err
		}
		for _, x := range b {
			n = (n << 8) | int64(x)
		}
	}
	if n < 0 || n > int64(max) {
		return 0, nil, fmt.Errorf("WebSocket frame exceeds configured limit")
	}
	masked := second&0x80 != 0
	var key [4]byte
	if masked {
		if _, err = io.ReadFull(w.read, key[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, n)
	if _, err = io.ReadFull(w.read, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return opcode, payload, nil
}
func randomRequestID() string { n, _ := rand.Int(rand.Reader, big.NewInt(1<<62)); return n.String() }
