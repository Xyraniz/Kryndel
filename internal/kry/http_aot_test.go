package kry

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func runHTTPInterpreterWithRuntimeLimits(t *testing.T, source string, limits Limits) (string, *Diagnostic) {
	t.Helper()
	program, diagnostic := Parse(&Source{Name: "http-aot.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		return "", diagnostic
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		return "", diagnostic
	}
	runtimeValue, diagnostic := NewRuntime(program, checker, DefaultLimits(), Sandbox{})
	if diagnostic != nil {
		return "", diagnostic
	}
	runtimeValue.Lim = limits
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

func buildAndRunHTTPCAOT(t *testing.T, source string, limits Limits) (string, int, error) {
	t.Helper()
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "linux" && runtime.GOOS != "windows") {
		t.Skip("C AOT HTTP test requires Linux or Windows amd64")
	}
	program, diagnostic := Parse(&Source{Name: "http-aot.kry", Text: source}, DefaultLimits())
	if diagnostic != nil {
		return "", 0, diagnostic
	}
	checker, diagnostic := Check(program, DefaultLimits())
	if diagnostic != nil {
		return "", 0, diagnostic
	}
	checker.Lim, checker.Env.Lim = limits, limits
	format := "elf"
	if runtime.GOOS == "windows" {
		format = "exe"
	}
	image, err := BuildNative(program, checker, NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}, format)
	if err != nil {
		return "", 0, err
	}
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	path := filepath.Join(t.TempDir(), "http-aot"+extension)
	if err := os.WriteFile(path, image, 0o700); err != nil {
		return "", 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path).CombinedOutput()
	outputText := strings.ReplaceAll(string(output), "\r\n", "\n")
	if err == nil {
		return outputText, 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return outputText, exit.ExitCode(), nil
	}
	return outputText, -1, err
}

func TestCAOTHTTPRequestMatchesInterpreter(t *testing.T) {
	type observedRequest struct {
		method string
		uri    string
		body   string
	}
	var observedMu sync.Mutex
	var observed []observedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		_ = r.Body.Close()
		observedMu.Lock()
		observed = append(observed, observedRequest{method: r.Method, uri: r.RequestURI, body: string(data)})
		observedMu.Unlock()
		switch r.URL.Path {
		case "/echo path":
			if r.Method != "PATCH" || r.RequestURI != "/echo%20path?q=a%20b" || string(data) != "payload λ" {
				t.Errorf("request method/URI/body = %q %q %q", r.Method, r.RequestURI, data)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "accepted ✓")
		case "/status":
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, "private response")
		case "/chunked":
			w.(http.Flusher).Flush()
			_, _ = io.WriteString(w, "chunked ✓")
		case "/invalid-utf8":
			w.Header().Set("Content-Length", "2")
			_, _ = w.Write([]byte{0xff, 0xfe})
		default:
			t.Errorf("unexpected HTTP request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := fmt.Sprintf(`fn main() -> Nil {
    println(http_request("PATCH", %q, "payload λ"))
    println(http_request("GET", %q, ""))
    println(http_request("GET", %q, ""))
    println(http_request("GET", %q, ""))
    return nil
}
`, server.URL+"/echo%20path?q=a%20b#fragment", server.URL+"/status", server.URL+"/chunked", server.URL+"/invalid-utf8")
	want := "ok(accepted ✓)\nerr(HTTP status 418)\nok(chunked ✓)\nerr(response is not valid UTF-8)\n"
	interpreted, diagnostic := runHTTPInterpreterWithRuntimeLimits(t, source, DefaultLimits())
	if diagnostic != nil || interpreted != want {
		t.Fatalf("interpreter output = %q, diagnostic=%v; want %q", interpreted, diagnostic, want)
	}
	native, status, err := buildAndRunHTTPCAOT(t, source, DefaultLimits())
	if err != nil || status != 0 || native != interpreted {
		t.Fatalf("C AOT HTTP output differs: interpreter=%q C AOT (%d, %v)=%q", interpreted, status, err, native)
	}
	observedMu.Lock()
	gotRequests := append([]observedRequest(nil), observed...)
	observedMu.Unlock()
	if len(gotRequests) != 8 {
		t.Fatalf("server observed %d requests, want 8: %#v", len(gotRequests), gotRequests)
	}
	for i := 0; i < len(gotRequests); i += 4 {
		wantRequests := []observedRequest{
			{method: "PATCH", uri: "/echo%20path?q=a%20b", body: "payload λ"},
			{method: "GET", uri: "/status"},
			{method: "GET", uri: "/chunked"},
			{method: "GET", uri: "/invalid-utf8"},
		}
		for j, wantRequest := range wantRequests {
			if gotRequests[i+j] != wantRequest {
				t.Errorf("request %d = %#v, want %#v", i+j, gotRequests[i+j], wantRequest)
			}
		}
	}
}

func TestCAOTHTTPRequestLimitAndTimeoutMatchInterpreter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			w.Header().Set("Content-Length", "65")
			_, _ = io.WriteString(w, strings.Repeat("x", 65))
		case "/slow":
			time.Sleep(400 * time.Millisecond)
			_, _ = io.WriteString(w, "late")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Run("response limit", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxSourceBytes = 64
		source := fmt.Sprintf("fn main() -> Nil { println(http_request(\"GET\", %q, \"\")); return nil }\n", server.URL+"/large")
		interpreted, diagnostic := runHTTPInterpreterWithRuntimeLimits(t, source, limits)
		if diagnostic != nil || interpreted != "err(response exceeds configured input limit)\n" {
			t.Fatalf("interpreter output = %q, diagnostic=%v", interpreted, diagnostic)
		}
		native, status, err := buildAndRunHTTPCAOT(t, source, limits)
		if err != nil || status != 0 || native != interpreted {
			t.Fatalf("C AOT limit output differs: interpreter=%q C AOT (%d, %v)=%q", interpreted, status, err, native)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxWallTimeMS = 120
		source := fmt.Sprintf("fn main() -> Nil { println(http_request(\"GET\", %q, \"\")); return nil }\n", server.URL+"/slow")
		interpreted, diagnostic := runHTTPInterpreterWithRuntimeLimits(t, source, limits)
		if diagnostic != nil || !strings.Contains(strings.ToLower(interpreted), "timeout") && !strings.Contains(strings.ToLower(interpreted), "deadline exceeded") {
			t.Fatalf("interpreter timeout output = %q, diagnostic=%v", interpreted, diagnostic)
		}
		native, status, err := buildAndRunHTTPCAOT(t, source, limits)
		if err != nil || status != 0 || !strings.Contains(strings.ToLower(native), "timeout") {
			t.Fatalf("C AOT timeout output = %q, status=%d, err=%v", native, status, err)
		}
	})
}

func TestCAOTHTTPRequestExplicitlyRejectsHTTPS(t *testing.T) {
	source := `fn main() -> Nil { println(http_request("GET", "https://example.invalid/", "")); return nil }`
	native, status, err := buildAndRunHTTPCAOT(t, source, DefaultLimits())
	if err != nil || status != 0 || !strings.Contains(native, "HTTPS is not supported by the C AOT backend") {
		t.Fatalf("HTTPS rejection output = %q, status=%d, err=%v", native, status, err)
	}
}

func TestCAOTHTTPRequestRejectsOverflowingContentLength(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				serverErr <- acceptErr
				return
			}
			reader := bufio.NewReader(connection)
			for {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					_ = connection.Close()
					serverErr <- readErr
					return
				}
				if line == "\r\n" {
					break
				}
			}
			_, writeErr := io.WriteString(connection, "HTTP/1.1 200 OK\r\nContent-Length: 18446744073709551616\r\nConnection: close\r\n\r\nx")
			_ = connection.Close()
			if writeErr != nil {
				serverErr <- writeErr
				return
			}
		}
		serverErr <- nil
	}()

	url := "http://" + listener.Addr().String() + "/overflow"
	source := fmt.Sprintf("fn main() -> Nil { println(http_request(\"GET\", %q, \"\")); return nil }\n", url)
	interpreted, diagnostic := runHTTPInterpreterWithRuntimeLimits(t, source, DefaultLimits())
	if diagnostic != nil || !strings.HasPrefix(interpreted, "err(") {
		t.Fatalf("interpreter accepted overflowing Content-Length: output=%q diagnostic=%v", interpreted, diagnostic)
	}
	native, status, err := buildAndRunHTTPCAOT(t, source, DefaultLimits())
	if err != nil || status != 0 || !strings.HasPrefix(native, "err(") {
		t.Fatalf("C AOT accepted overflowing Content-Length: output=%q status=%d err=%v", native, status, err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("raw HTTP fixture failed: %v", err)
	}
}
