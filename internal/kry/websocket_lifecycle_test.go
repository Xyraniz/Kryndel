package kry

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
)

func TestWebSocketCloseReportsDoubleCloseAndUseAfterClose(t *testing.T) {
	client, peer := net.Pipe()
	defer peer.Close()
	go func() { _, _ = io.Copy(io.Discard, peer) }()
	connection := &websocketConn{conn: client, read: bufio.NewReader(client)}
	if err := connection.close(); err != nil {
		t.Fatal(err)
	}
	if err := connection.sendText("after close"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected use-after-close error, got %v", err)
	}
	if err := connection.close(); err == nil || !strings.Contains(err.Error(), "already closed") {
		t.Fatalf("expected double-close error, got %v", err)
	}
}
