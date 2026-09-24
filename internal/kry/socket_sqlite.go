package kry

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

func sqlitePath(sb Sandbox, path string) (string, error) {
	if path == ":memory:" || strings.HasPrefix(path, "file::memory:") {
		return path, nil
	}
	return sb.Resolve(path, true)
}

func sqliteOpen(sb Sandbox, path string) (*sqliteHandle, error) {
	resolved, err := sqlitePath(sb, path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", resolved)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &sqliteHandle{db: db}, nil
}

func sqliteClose(h *sqliteHandle) error {
	if h == nil {
		return fmt.Errorf("invalid SQLite handle")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("SQLite handle is already closed")
	}
	h.closed = true
	return h.db.Close()
}

func (h *sqliteHandle) isClosed() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func sqliteExec(h *sqliteHandle, query string) (int64, error) {
	if h == nil {
		return 0, fmt.Errorf("invalid SQLite handle")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, fmt.Errorf("SQLite handle is closed")
	}
	result, err := h.db.Exec(query)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func sqliteCell(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func sqliteQuery(h *sqliteHandle, query string, maxRows int) ([][]string, error) {
	if h == nil {
		return nil, fmt.Errorf("invalid SQLite handle")
	}
	if maxRows < 1 {
		return nil, fmt.Errorf("SQLite row limit must be positive")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, fmt.Errorf("SQLite handle is closed")
	}
	rows, err := h.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([][]string, 0)
	for rows.Next() {
		if len(result) >= maxRows {
			return nil, fmt.Errorf("SQLite result exceeds configured row limit")
		}
		cells := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make([]string, len(columns))
		for i, cell := range cells {
			row[i], err = sqliteCell(cell)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func socketDeadline(maxWallMS int64) time.Time {
	return time.Now().Add(networkTimeout(maxWallMS))
}

func networkTimeout(maxWallMS int64) time.Duration {
	if maxWallMS <= 0 {
		maxWallMS = DefaultLimits().MaxWallTimeMS
	}
	return time.Duration(maxWallMS) * time.Millisecond
}

func validPort(port int64, allowZero bool) error {
	if (allowZero && port == 0) || (port >= 1 && port <= 65535) {
		return nil
	}
	if allowZero {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	return fmt.Errorf("port must be between 1 and 65535")
}

func tcpConnect(host string, port int64, timeout time.Duration) (*tcpSocketHandle, error) {
	if err := validPort(port, false); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: timeout}).Dial("tcp", net.JoinHostPort(host, strconv.FormatInt(port, 10)))
	if err != nil {
		return nil, err
	}
	return &tcpSocketHandle{conn: conn}, nil
}

func tcpListen(host string, port int64) (*tcpListenerHandle, error) {
	if err := validPort(port, true); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.FormatInt(port, 10)))
	if err != nil {
		return nil, err
	}
	return &tcpListenerHandle{listener: listener}, nil
}

func tcpClose(h *tcpSocketHandle) error {
	if h == nil {
		return fmt.Errorf("invalid TcpSocket handle")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("TcpSocket handle is already closed")
	}
	h.closed = true
	return h.conn.Close()
}

func (h *tcpSocketHandle) isClosed() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func tcpListenerClose(h *tcpListenerHandle) error {
	if h == nil {
		return fmt.Errorf("invalid TcpListener handle")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("TcpListener handle is already closed")
	}
	h.closed = true
	return h.listener.Close()
}

func (h *tcpListenerHandle) isClosed() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func udpBind(host string, port int64) (*udpSocketHandle, error) {
	if err := validPort(port, true); err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(host), Port: int(port)})
	if err != nil {
		return nil, err
	}
	return &udpSocketHandle{conn: conn}, nil
}

func udpClose(h *udpSocketHandle) error {
	if h == nil {
		return fmt.Errorf("invalid UdpSocket handle")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("UdpSocket handle is already closed")
	}
	h.closed = true
	return h.conn.Close()
}

func (h *udpSocketHandle) isClosed() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

func udpReceive(h *udpSocketHandle, max int, deadline time.Time) ([]byte, *net.UDPAddr, error) {
	if h == nil {
		return nil, nil, fmt.Errorf("invalid UdpSocket handle")
	}
	if max < 1 {
		return nil, nil, fmt.Errorf("receive size must be positive")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, fmt.Errorf("UdpSocket handle is closed")
	}
	if err := h.conn.SetReadDeadline(deadline); err != nil {
		return nil, nil, err
	}
	data := make([]byte, max)
	n, addr, err := h.conn.ReadFromUDP(data)
	if err != nil {
		return nil, nil, err
	}
	return data[:n], addr, nil
}

func udpSenderJSON(data []byte, addr *net.UDPAddr) (string, error) {
	value := map[string]any{
		"address": addr.IP.String(),
		"port":    addr.Port,
		"data":    base64.StdEncoding.EncodeToString(data),
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
