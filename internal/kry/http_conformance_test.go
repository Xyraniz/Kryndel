package kry

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestHTTPBuiltinsInterpreterAgainstLoopbackServer(t *testing.T) {
	type observedRequest struct {
		method        string
		path          string
		authorization string
		body          string
	}
	var mu sync.Mutex
	var observed []observedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		_ = r.Body.Close()
		mu.Lock()
		observed = append(observed, observedRequest{
			method:        r.Method,
			path:          r.URL.Path,
			authorization: r.Header.Get("Authorization"),
			body:          string(body),
		})
		mu.Unlock()

		switch r.URL.Path {
		case "/get":
			if r.Method != http.MethodGet || len(body) != 0 || r.Header.Get("Authorization") != "" {
				t.Errorf("http_get request = %s body=%q authorization=%q", r.Method, body, r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, "hello hé")
		case "/echo":
			if r.Method != http.MethodPost || string(body) != "payload λ" || r.Header.Get("Authorization") != "" {
				t.Errorf("http_request request = %s body=%q authorization=%q", r.Method, body, r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "accepted:"+string(body))
		case "/auth":
			if r.Method != http.MethodPut || string(body) != "auth body" || r.Header.Get("Authorization") != "Bearer builtin-test-token" {
				t.Errorf("http_request_auth request = %s body=%q authorization=%q", r.Method, body, r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, "authenticated ✓")
		case "/status":
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, "private-status-response")
		case "/auth-status":
			if r.Header.Get("Authorization") != "Bearer builtin-test-token" {
				t.Errorf("authenticated error request authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "private-auth-response")
		case "/invalid-utf8":
			_, _ = w.Write([]byte{0xff, 0xfe})
		default:
			t.Errorf("unexpected HTTP builtin request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := fmt.Sprintf(`fn show(label: String, result: Result[String, String]) -> Nil {
    match result {
        ok(value) => { println(label + ":ok:" + value) }
        err(problem) => { println(label + ":err:" + problem) }
    }
}
fn main() -> Nil {
    show("get", http_get(%q))
    show("get_status", http_get(%q))
    show("request", http_request("POST", %q, "payload λ"))
    show("request_status", http_request("GET", %q, ""))
    show("auth", http_request_auth("PUT", %q, "auth body", "builtin-test-token"))
    show("auth_status", http_request_auth("GET", %q, "", "builtin-test-token"))
    show("invalid_utf8", http_request("GET", %q, ""))
    show("empty_auth", http_request_auth("GET", %q, "", ""))
    return nil
}
`, server.URL+"/get", server.URL+"/status", server.URL+"/echo", server.URL+"/status", server.URL+"/auth", server.URL+"/auth-status", server.URL+"/invalid-utf8", server.URL+"/must-not-be-requested")
	output, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("HTTP builtin interpreter program failed: %s", diagnostic.Message)
	}
	wantOutput := strings.Join([]string{
		"get:ok:hello hé",
		"get_status:err:HTTP status 418",
		"request:ok:accepted:payload λ",
		"request_status:err:HTTP status 418",
		"auth:ok:authenticated ✓",
		"auth_status:err:HTTP status 401",
		"invalid_utf8:err:response is not valid UTF-8",
		"empty_auth:err:empty authentication token",
		"",
	}, "\n")
	if output != wantOutput {
		t.Fatalf("HTTP builtin output mismatch:\n got %q\nwant %q", output, wantOutput)
	}
	for _, privateText := range []string{"builtin-test-token", "private-status-response", "private-auth-response"} {
		if strings.Contains(output, privateText) {
			t.Errorf("HTTP builtin output leaked %q: %q", privateText, output)
		}
	}

	wantRequests := []observedRequest{
		{method: http.MethodGet, path: "/get"},
		{method: http.MethodGet, path: "/status"},
		{method: http.MethodPost, path: "/echo", body: "payload λ"},
		{method: http.MethodGet, path: "/status"},
		{method: http.MethodPut, path: "/auth", authorization: "Bearer builtin-test-token", body: "auth body"},
		{method: http.MethodGet, path: "/auth-status", authorization: "Bearer builtin-test-token"},
		{method: http.MethodGet, path: "/invalid-utf8"},
	}
	mu.Lock()
	gotRequests := append([]observedRequest(nil), observed...)
	mu.Unlock()
	if len(gotRequests) != len(wantRequests) {
		t.Fatalf("HTTP builtins sent %d requests, want %d: %#v", len(gotRequests), len(wantRequests), gotRequests)
	}
	for i, want := range wantRequests {
		if got := gotRequests[i]; got != want {
			t.Errorf("HTTP request %d = %#v, want %#v", i, got, want)
		}
	}
}

func TestNativeBackendsRejectHTTPBuiltinsWithExplicitDiagnostics(t *testing.T) {
	builtins := map[string]string{
		"http_get":          `fn main() -> Nil { http_get("http://127.0.0.1/"); return nil }`,
		"http_request":      `fn main() -> Nil { http_request("GET", "http://127.0.0.1/", ""); return nil }`,
		"http_request_auth": `fn main() -> Nil { http_request_auth("GET", "http://127.0.0.1/", "", "token"); return nil }`,
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
	for builtin, source := range builtins {
		program, diagnostic := Parse(&Source{Name: "native-http-unsupported.kry", Text: source}, DefaultLimits())
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
