package kry

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

var errWebSocketReceiveTimeout = errors.New("WebSocket receive timed out")

type websocketReadResult struct {
	opcode  byte
	payload []byte
	err     error
}

type websocketConn struct {
	conn        net.Conn
	read        *bufio.Reader
	ctx         context.Context
	timeout     time.Duration
	writeMu     sync.Mutex
	readMu      sync.Mutex
	pendingRead chan websocketReadResult
	stateMu     sync.Mutex
	closed      bool
}

func connectWebSocket(ctx context.Context, raw string, timeout time.Duration) (*websocketConn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = networkTimeout(0)
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("WebSocket URL must use ws or wss")
	}
	host := u.Host
	if u.Port() == "" {
		port := "80"
		if u.Scheme == "wss" {
			port = "443"
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}
	var conn net.Conn
	dialer := &net.Dialer{}
	if u.Scheme == "wss" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}
		conn, err = tlsDialer.DialContext(dialCtx, "tcp", host)
	} else {
		conn, err = dialer.DialContext(dialCtx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*websocketConn, error) { _ = conn.Close(); return nil, e }
	deadline, ok := dialCtx.Deadline()
	if !ok {
		deadline = time.Now().Add(timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fail(err)
	}
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
	line, err := readWebSocketHandshakeLine(br)
	if err != nil {
		return fail(err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 101") && !strings.HasPrefix(line, "HTTP/2 101") {
		return fail(fmt.Errorf("WebSocket handshake rejected: %s", strings.TrimSpace(line)))
	}
	headers := map[string]string{}
	headerBytes := len(line)
	for {
		line, err = readWebSocketHandshakeLine(br)
		if err != nil {
			return fail(err)
		}
		headerBytes += len(line)
		if headerBytes > 64<<10 {
			return fail(fmt.Errorf("WebSocket handshake headers exceed configured limit"))
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
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}
	return &websocketConn{conn: conn, read: br, ctx: ctx, timeout: timeout}, nil
}

func readWebSocketHandshakeLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return "", fmt.Errorf("WebSocket handshake line exceeds configured limit")
		}
		return "", err
	}
	return string(line), nil
}

func (w *websocketConn) sendText(message string) error {
	if w.isClosed() {
		return fmt.Errorf("WebSocket handle is closed")
	}
	return w.sendFrame(0x1, []byte(message))
}
func (w *websocketConn) sendBinary(message []byte) error {
	if w.isClosed() {
		return fmt.Errorf("WebSocket handle is closed")
	}
	return w.sendFrame(0x2, message)
}
func (w *websocketConn) sendPong(data []byte) error {
	if w.isClosed() {
		return fmt.Errorf("WebSocket handle is closed")
	}
	return w.sendFrame(0xA, data)
}
func (w *websocketConn) close() error {
	w.stateMu.Lock()
	if w.closed {
		w.stateMu.Unlock()
		return fmt.Errorf("WebSocket handle is already closed")
	}
	w.closed = true
	w.stateMu.Unlock()
	frameErr := w.sendFrame(0x8, []byte{0x03, 0xE8})
	closeErr := w.conn.Close()
	if frameErr != nil {
		return frameErr
	}
	return closeErr
}

func (w *websocketConn) isClosed() bool {
	if w == nil {
		return true
	}
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	return w.closed
}

func (w *websocketConn) sendFrame(opcode byte, payload []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if w.ctx != nil {
		if err := w.ctx.Err(); err != nil {
			return err
		}
	}
	if len(payload) > 16<<20 {
		return fmt.Errorf("WebSocket message exceeds configured limit")
	}
	timeout := w.timeout
	if timeout <= 0 {
		timeout = networkTimeout(0)
	}
	deadline := time.Now().Add(timeout)
	if w.ctx != nil {
		if ctxDeadline, ok := w.ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
	}
	if err := w.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	stop := func() bool { return true }
	if w.ctx != nil {
		stop = context.AfterFunc(w.ctx, func() { _ = w.conn.SetWriteDeadline(time.Now()) })
	}
	defer func() {
		stop()
		if w.ctx == nil || w.ctx.Err() == nil {
			_ = w.conn.SetWriteDeadline(time.Time{})
		}
	}()
	if w.ctx != nil {
		if err := w.ctx.Err(); err != nil {
			return err
		}
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
	return w.receiveTextTimeout(max, 0)
}

// receiveTextTimeout keeps the frame parser running across caller timeouts. A
// socket deadline in the middle of a frame would consume part of that frame
// and make the next read start at the wrong byte, so the reader goroutine owns
// the stream until it has produced a complete text frame or a terminal error.
func (w *websocketConn) receiveTextTimeout(max int, timeout time.Duration) (string, error) {
	opcode, payload, err := w.receiveMessageTimeout(max, timeout)
	if err != nil {
		return "", err
	}
	if opcode != 0x1 {
		return "", fmt.Errorf("WebSocket message is binary; use websocket_receive_binary")
	}
	if !validUTF8(payload) {
		return "", fmt.Errorf("WebSocket text frame is not UTF-8")
	}
	return string(payload), nil
}

func (w *websocketConn) receiveBinaryTimeout(max int, timeout time.Duration) ([]byte, error) {
	opcode, payload, err := w.receiveMessageTimeout(max, timeout)
	if err != nil {
		return nil, err
	}
	if opcode != 0x2 {
		return nil, fmt.Errorf("WebSocket message is text; use websocket_receive")
	}
	return payload, nil
}

func (w *websocketConn) receiveMessageTimeout(max int, timeout time.Duration) (byte, []byte, error) {
	w.readMu.Lock()
	defer w.readMu.Unlock()
	if w.isClosed() {
		return 0, nil, fmt.Errorf("WebSocket handle is closed")
	}
	if w.pendingRead == nil {
		pending := make(chan websocketReadResult, 1)
		w.pendingRead = pending
		go func() {
			opcode, payload, err := w.receiveMessageRaw(max)
			pending <- websocketReadResult{opcode: opcode, payload: payload, err: err}
		}()
	}
	pending := w.pendingRead
	if timeout <= 0 {
		result := <-pending
		w.pendingRead = nil
		return result.opcode, result.payload, result.err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-pending:
		w.pendingRead = nil
		return result.opcode, result.payload, result.err
	case <-timer.C:
		return 0, nil, errWebSocketReceiveTimeout
	}
}

func (w *websocketConn) receiveMessageRaw(max int) (byte, []byte, error) {
	if w.isClosed() {
		return 0, nil, fmt.Errorf("WebSocket handle is closed")
	}
	if w.ctx != nil {
		if err := w.ctx.Err(); err != nil {
			return 0, nil, err
		}
		stop := context.AfterFunc(w.ctx, func() { _ = w.conn.SetReadDeadline(time.Now()) })
		defer stop()
	}
	var messageOpcode byte
	var message []byte
	collecting := false
	for {
		fin, opcode, payload, err := w.receiveFrame(max)
		if err != nil {
			if w.ctx != nil && w.ctx.Err() != nil {
				return 0, nil, w.ctx.Err()
			}
			w.stateMu.Lock()
			w.closed = true
			w.stateMu.Unlock()
			_ = w.conn.Close()
			return 0, nil, err
		}
		switch opcode {
		case 0x8, 0x9, 0xA:
			if !fin || len(payload) > 125 {
				return 0, nil, fmt.Errorf("invalid fragmented or oversized WebSocket control frame")
			}
			if opcode == 0x9 {
				if err := w.sendPong(payload); err != nil {
					return 0, nil, err
				}
				continue
			}
			if opcode == 0xA {
				continue
			}
			w.stateMu.Lock()
			w.closed = true
			w.stateMu.Unlock()
			_ = w.conn.Close()
			code := 1005
			reason := ""
			if len(payload) == 1 {
				return 0, nil, fmt.Errorf("WebSocket close frame has a one-byte payload")
			}
			if len(payload) >= 2 {
				code = int(binary.BigEndian.Uint16(payload[:2]))
				reasonBytes := payload[2:]
				if !validUTF8(reasonBytes) {
					return 0, nil, fmt.Errorf("WebSocket close reason is not UTF-8 (code %d)", code)
				}
				reason = string(reasonBytes)
			}
			return 0, nil, fmt.Errorf("WebSocket peer closed with code %d: %s", code, reason)
		case 0x0:
			if !collecting {
				return 0, nil, fmt.Errorf("unexpected WebSocket continuation frame")
			}
			if len(payload) > max-len(message) {
				return 0, nil, fmt.Errorf("WebSocket message exceeds configured limit")
			}
			message = append(message, payload...)
			if fin {
				return messageOpcode, message, nil
			}
		case 0x1, 0x2:
			if collecting {
				return 0, nil, fmt.Errorf("new WebSocket data frame before the previous message finished")
			}
			if fin {
				return opcode, payload, nil
			}
			if len(payload) > max {
				return 0, nil, fmt.Errorf("WebSocket message exceeds configured limit")
			}
			messageOpcode, message, collecting = opcode, payload, true
		default:
			return 0, nil, fmt.Errorf("unsupported WebSocket frame opcode %d", opcode)
		}
	}
}
func (w *websocketConn) receiveFrame(max int) (bool, byte, []byte, error) {
	first, err := w.read.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	second, err := w.read.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	if first&0x70 != 0 {
		return false, 0, nil, fmt.Errorf("WebSocket extension bits are not negotiated")
	}
	opcode := first & 0x0f
	fin := first&0x80 != 0
	n := int64(second & 0x7f)
	if n == 126 {
		var b [2]byte
		if _, err = io.ReadFull(w.read, b[:]); err != nil {
			return false, 0, nil, err
		}
		n = int64(b[0])<<8 | int64(b[1])
	} else if n == 127 {
		var b [8]byte
		if _, err = io.ReadFull(w.read, b[:]); err != nil {
			return false, 0, nil, err
		}
		for _, x := range b {
			n = (n << 8) | int64(x)
		}
	}
	if n < 0 || n > int64(max) {
		return false, 0, nil, fmt.Errorf("WebSocket frame exceeds configured limit")
	}
	masked := second&0x80 != 0
	if masked {
		return false, 0, nil, fmt.Errorf("server WebSocket frames must not be masked")
	}
	if opcode >= 0x8 && (!fin || n > 125) {
		return false, 0, nil, fmt.Errorf("invalid WebSocket control frame")
	}
	payload := make([]byte, n)
	if _, err = io.ReadFull(w.read, payload); err != nil {
		return false, 0, nil, err
	}
	return fin, opcode, payload, nil
}
func randomRequestID() string { n, _ := rand.Int(rand.Reader, big.NewInt(1<<62)); return n.String() }
