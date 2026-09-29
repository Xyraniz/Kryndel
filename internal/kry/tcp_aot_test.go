package kry

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func runTCPParityProgram(t *testing.T, source func(int) string, native bool, serve func(net.Conn) error) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		done <- serve(conn)
	}()
	text := source(port)
	if native {
		output, status, runErr := buildAndRunNativeAOT(t, text)
		if runErr != nil {
			t.Fatalf("native TCP program failed to build or run: %v", runErr)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("loopback peer failed: %v; native status=%d output=%q", err, status, output)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("loopback peer did not finish")
		}
		return output, status
	}
	output, diagnostic := runInterpreterCapture(t, text)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loopback peer failed: %v; interpreter diagnostic=%v output=%q", err, diagnostic, output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("loopback peer did not finish")
	}
	if diagnostic != nil {
		return output + fmt.Sprintf("kryndel: %s\n", diagnostic.Message), 1
	}
	return output, 0
}

func TestCAOTTCPClientMatchesInterpreter(t *testing.T) {
	maxSource := DefaultLimits().MaxSourceBytes
	program := func(port int) string {
		return fmt.Sprintf(`fn main() -> Nil {
    match tcp_connect("127.0.0.1", %d) {
        ok(socket) => {
            println(tcp_send(socket, string_to_bytes("ping")))
            let mut response: String = ""
            let mut index: Int = 0
            while index < 4 {
                match tcp_receive(socket, 1) {
                    ok(part) => { response = response + bytes_to_string(part) }
                    err(problem) => { response = response + "read error: " + problem }
                }
                index = index + 1
            }
            println(response)
            println(tcp_receive(socket, 0))
            println(tcp_receive(socket, %d))
            tcp_close(socket)
            println(tcp_send(socket, string_to_bytes("x")))
            println(tcp_receive(socket, 1))
        }
        err(problem) => { println("connect error: " + problem) }
    }
}`, port, maxSource+1)
	}
	peer := func(conn net.Conn) error {
		request := make([]byte, 4)
		if _, err := io.ReadFull(conn, request); err != nil {
			return err
		}
		if string(request) != "ping" {
			return fmt.Errorf("server received %q, want ping", request)
		}
		_, err := conn.Write([]byte("pong"))
		return err
	}
	interpreted, interpStatus := runTCPParityProgram(t, program, false, peer)
	native, nativeStatus := runTCPParityProgram(t, program, true, peer)
	if interpStatus != 0 || nativeStatus != 0 || native != interpreted {
		t.Fatalf("TCP client results differ:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func TestCAOTTCPClientSendsAllBytes(t *testing.T) {
	const chunkSize = 1 << 20
	const repeatCount = chunkSize / len("xxxxxxxx")
	const sendCount = 8
	const payloadSize = chunkSize * sendCount
	program := func(port int) string {
		return fmt.Sprintf(`fn main() -> Nil {
    match string_repeat("xxxxxxxx", %d) {
        ok(text) => {
            let payload: Bytes = string_to_bytes(text)
            match tcp_connect("127.0.0.1", %d) {
                ok(socket) => {
                    let mut index: Int = 0
                    while index < %d {
                        println(tcp_send(socket, payload))
                        index = index + 1
                    }
                    tcp_close(socket)
                }
                err(problem) => { println("connect error: " + problem) }
            }
        }
        err(problem) => { println("repeat error: " + problem) }
    }
}`, repeatCount, port, sendCount)
	}
	peer := func(conn net.Conn) error {
		time.Sleep(100 * time.Millisecond)
		n, err := io.CopyN(io.Discard, conn, payloadSize)
		if err != nil {
			return err
		}
		if n != payloadSize {
			return fmt.Errorf("server received %d bytes, want %d", n, payloadSize)
		}
		return nil
	}
	interpreted, interpStatus := runTCPParityProgram(t, program, false, peer)
	native, nativeStatus := runTCPParityProgram(t, program, true, peer)
	if interpStatus != 0 || nativeStatus != 0 || native != interpreted || !strings.Contains(native, fmt.Sprintf("%d", chunkSize)) {
		t.Fatalf("TCP send did not complete identically:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func TestCAOTTCPConnectValidationMatchesInterpreter(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "zero port", src: `println(tcp_connect("127.0.0.1", 0))`},
		{name: "negative port", src: `println(tcp_connect("127.0.0.1", -1))`},
		{name: "port above range", src: `println(tcp_connect("127.0.0.1", 65536))`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := "fn main() -> Nil {\n    " + tc.src + "\n}\n"
			interpreted, diagnostic := runInterpreterCapture(t, source)
			if diagnostic != nil {
				t.Fatalf("interpreter rejected test program: %s", diagnostic.Message)
			}
			native, status, err := buildAndRunNativeAOT(t, source)
			if err != nil || status != 0 || native != interpreted {
				t.Fatalf("TCP validation differs: interpreter=%q native=(%d,%q) err=%v", interpreted, status, native, err)
			}
		})
	}
}

func TestCAOTTCPReceiveEOFMatchesInterpreter(t *testing.T) {
	program := func(port int) string {
		return fmt.Sprintf(`fn main() -> Nil {
    match tcp_connect("127.0.0.1", %d) {
        ok(socket) => {
            println(tcp_receive(socket, 1))
            tcp_close(socket)
        }
        err(problem) => { println("connect error: " + problem) }
    }
}`, port)
	}
	peer := func(net.Conn) error { return nil }
	interpreted, interpStatus := runTCPParityProgram(t, program, false, peer)
	native, nativeStatus := runTCPParityProgram(t, program, true, peer)
	if interpStatus != 0 || nativeStatus != 0 || native != interpreted || !strings.Contains(native, "EOF") {
		t.Fatalf("TCP EOF differs:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func TestCAOTTCPReceiveErrorStillCleansOpenSocket(t *testing.T) {
	program := func(port int) string {
		return fmt.Sprintf(`fn main() -> Nil {
    match tcp_connect("127.0.0.1", %d) {
        ok(socket) => { println(tcp_receive(socket, 1)) }
        err(problem) => { println("connect error: " + problem) }
    }
}`, port)
	}
	peer := func(net.Conn) error { return nil }
	interpreted, interpStatus := runTCPParityProgram(t, program, false, peer)
	native, nativeStatus := runTCPParityProgram(t, program, true, peer)
	if interpStatus == 0 || nativeStatus == 0 || native != interpreted || !strings.Contains(native, "err(EOF)") || !strings.Contains(native, "resource 'TcpSocket' was not closed") {
		t.Fatalf("TCP cleanup after receive error differs:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func TestCAOTTCPConnectionRefusalHasComparableError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf("fn main() -> Nil { println(tcp_connect(\"127.0.0.1\", %d)) }\n", port)
	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter rejected connection-refusal program: %s", diagnostic.Message)
	}
	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil || status != 0 {
		t.Fatalf("native connection-refusal case failed: status=%d err=%v output=%q", status, err, native)
	}
	for name, output := range map[string]string{"interpreter": interpreted, "C AOT": native} {
		if !strings.Contains(strings.ToLower(output), "refused") {
			t.Errorf("%s connection error is not comparable to connection refused: %q", name, output)
		}
	}
}

func TestCAOTTCPDoubleCloseMatchesInterpreter(t *testing.T) {
	program := func(port int) string {
		return fmt.Sprintf(`fn main() -> Nil {
    match tcp_connect("127.0.0.1", %d) {
        ok(socket) => {
            tcp_close(socket)
            tcp_close(socket)
        }
        err(problem) => { println("connect error: " + problem) }
    }
}`, port)
	}
	peer := func(conn net.Conn) error { _, err := io.Copy(io.Discard, conn); return err }
	interpreted, interpStatus := runTCPParityProgram(t, program, false, peer)
	native, nativeStatus := runTCPParityProgram(t, program, true, peer)
	if interpStatus == 0 || nativeStatus == 0 || native != interpreted || !strings.Contains(native, "TcpSocket handle is already closed") {
		t.Fatalf("double-close diagnostics differ:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func TestCAOTTCPResourceLeakMatchesInterpreter(t *testing.T) {
	program := func(port int) string {
		return fmt.Sprintf("fn main() -> Nil { println(tcp_connect(\"127.0.0.1\", %d)) }\n", port)
	}
	peer := func(conn net.Conn) error {
		_, err := io.Copy(io.Discard, conn)
		return err
	}
	interpreted, interpStatus := runTCPParityProgram(t, program, false, peer)
	native, nativeStatus := runTCPParityProgram(t, program, true, peer)
	if interpStatus == 0 || nativeStatus == 0 || native != interpreted || !strings.Contains(native, "resource 'TcpSocket' was not closed") {
		t.Fatalf("unclosed TCP resource diagnostics differ:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func runTCPInterpreterWithLimits(t *testing.T, source string, limits Limits) (string, *Diagnostic) {
	return runTCPInterpreterWithOperationAndRuntimeLimits(t, source, limits, limits)
}

func runTCPInterpreterWithOperationAndRuntimeLimits(t *testing.T, source string, operationLimits, runtimeLimits Limits) (string, *Diagnostic) {
	t.Helper()
	program, diagnostic := Parse(&Source{Name: "tcp-timeout.kry", Text: source}, operationLimits)
	if diagnostic != nil {
		return "", diagnostic
	}
	checker, diagnostic := Check(program, operationLimits)
	if diagnostic != nil {
		return "", diagnostic
	}
	runtimeValue, diagnostic := NewRuntime(program, checker, runtimeLimits, Sandbox{})
	if diagnostic != nil {
		return "", diagnostic
	}
	runtimeValue.Lim = operationLimits
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

func buildAndRunTCPNativeWithLimits(t *testing.T, source string, limits Limits) (string, int, error) {
	t.Helper()
	var target NativeTarget
	format := "elf"
	extension := ""
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		target = NativeTarget{OS: "linux", Arch: "amd64"}
	} else if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		if _, err := exec.LookPath("gcc"); err != nil {
			t.Skipf("Windows C compiler gcc is unavailable: %v", err)
		}
		target = NativeTarget{OS: "windows", Arch: "amd64"}
		format, extension = "exe", ".exe"
	} else {
		t.Skip("C AOT TCP timeout test requires linux/amd64 or windows/amd64")
	}
	program, diagnostic := Parse(&Source{Name: "tcp-timeout.kry", Text: source}, limits)
	if diagnostic != nil {
		return "", 0, diagnostic
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		return "", 0, diagnostic
	}
	image, err := BuildNative(program, checker, target, format)
	if err != nil {
		return "", 0, err
	}
	path := filepath.Join(t.TempDir(), "tcp-timeout"+extension)
	if err := os.WriteFile(path, image, 0o700); err != nil {
		return "", 0, err
	}
	output, err := exec.Command(path).CombinedOutput()
	if err == nil {
		return string(output), 0, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return string(output), exit.ExitCode(), nil
	}
	return string(output), -1, err
}

func TestCAOTTCPReceiveTimeoutMatchesInterpreter(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxWallTimeMS = 150
	source := func(port int) string {
		return fmt.Sprintf(`fn main() -> Nil {
    match tcp_connect("127.0.0.1", %d) {
        ok(socket) => {
            println(tcp_receive(socket, 1))
            tcp_close(socket)
        }
        err(problem) => { println("connect error: " + problem) }
    }
}`, port)
	}
	peer := func(net.Conn) error { time.Sleep(500 * time.Millisecond); return nil }
	interpreted, interpStatus := runTCPProgramWithLimitsAndPeer(t, source, limits, false, peer)
	native, nativeStatus := runTCPProgramWithLimitsAndPeer(t, source, limits, true, peer)
	normalizedNative := strings.ReplaceAll(native, "\r\n", "\n")
	if interpStatus != nativeStatus || interpStatus != 1 || !strings.Contains(strings.ToLower(interpreted), "i/o timeout") || !strings.Contains(strings.ToLower(normalizedNative), "i/o timeout") || !strings.HasSuffix(interpreted, "kryndel: wall-clock execution limit exceeded\n") || !strings.HasSuffix(normalizedNative, "kryndel: wall-clock execution limit exceeded\n") {
		t.Fatalf("TCP receive timeout differs:\ninterpreter (%d): %q\nC AOT (%d): %q", interpStatus, interpreted, nativeStatus, native)
	}
}

func runTCPProgramWithLimitsAndPeer(t *testing.T, source func(int) string, limits Limits, native bool, serve func(net.Conn) error) (string, int) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Second))
	defer listener.Close()
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		done <- serve(conn)
	}()
	text := source(listener.Addr().(*net.TCPAddr).Port)
	if native {
		output, status, runErr := buildAndRunTCPNativeWithLimits(t, text, limits)
		if runErr != nil {
			t.Fatalf("native TCP program failed to build or run: %v", runErr)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("loopback peer failed: %v; native status=%d output=%q", err, status, output)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("loopback peer did not finish")
		}
		return output, status
	}
	output, diagnostic := runTCPInterpreterWithLimits(t, text, limits)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loopback peer failed: %v; interpreter diagnostic=%v output=%q", err, diagnostic, output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("loopback peer did not finish")
	}
	if diagnostic != nil {
		return output + fmt.Sprintf("kryndel: %s\n", diagnostic.Message), 1
	}
	return output, 0
}

func TestCAOTTCPSendUsesOneAbsoluteDeadlineAcrossRetries(t *testing.T) {
	limits := DefaultLimits()
	program, diagnostic := Parse(&Source{Name: "tcp-deadline-policy.kry", Text: `fn main() -> Nil {
    match tcp_connect("127.0.0.1", 1) {
        ok(socket) => { println(tcp_send(socket, string_to_bytes("payload"))); tcp_close(socket) }
        err(problem) => { println(problem) }
    }
}`}, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	checker, diagnostic := Check(program, limits)
	if diagnostic != nil {
		t.Fatal(diagnostic)
	}
	cSource, err := GenerateC(program, checker)
	if err != nil {
		t.Fatal(err)
	}

	deadlineAfter, err := extractCFunction(cSource, "static unsigned long long k_tcp_deadline_after(")
	if err != nil {
		t.Fatal(err)
	}
	deadlineReached, err := extractCFunction(cSource, "static int k_tcp_deadline_reached(")
	if err != nil {
		t.Fatal(err)
	}
	operationDeadline, err := extractCFunction(cSource, "static unsigned long long k_tcp_operation_deadline(")
	if err != nil {
		t.Fatal(err)
	}
	sendBody, err := extractCFunction(cSource, "static KValue k_tcp_send_until(KValue value, KValue bytes, unsigned long long deadline)")
	if err != nil {
		t.Fatal(err)
	}
	sendWrapper, err := extractCFunction(cSource, "static KValue k_tcp_send(KValue value, KValue bytes)")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(operationDeadline, "k_tcp_deadline_after(k_tcp_now_ms(),(unsigned long long)timeout)") {
		t.Fatalf("operation deadline does not use the tested absolute-deadline helper:\n%s", operationDeadline)
	}
	if strings.Count(sendWrapper, "k_tcp_operation_deadline()") != 1 || !strings.Contains(sendWrapper, "k_tcp_send_until(value,bytes,k_tcp_operation_deadline())") {
		t.Fatalf("tcp_send must pass exactly one captured deadline to the retry helper:\n%s", sendWrapper)
	}
	loopAt := strings.Index(sendBody, "while (written<bytes.u.s.len)")
	deadlineAt := strings.Index(sendBody, "unsigned long long deadline)")
	if loopAt < 0 || deadlineAt < 0 || deadlineAt > loopAt {
		t.Fatalf("tcp_send retry helper must receive its deadline before entering the partial-write loop:\n%s", sendBody)
	}
	if !strings.Contains(sendBody, "k_tcp_wait_io(socket->fd,1,deadline)") || !strings.Contains(sendBody, "k_tcp_deadline_expired(deadline)") {
		t.Fatalf("tcp_send retries must reuse the captured deadline for readiness waits and expiry checks:\n%s", sendBody)
	}
	if strings.Contains(sendBody, "k_tcp_operation_deadline()") || strings.Contains(sendBody, "k_tcp_wait_io(socket->fd,1,k_tcp_operation_deadline())") {
		t.Fatalf("tcp_send recomputes its deadline inside the readiness wait:\n%s", sendBody)
	}

	target := NativeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if !(target.OS == "linux" && target.Arch == "amd64" || target.OS == "windows" && target.Arch == "amd64") {
		t.Skip("C deadline-helper execution test requires Linux/amd64 or Windows/amd64")
	}
	if target.OS == "windows" {
		if _, err := exec.LookPath("gcc"); err != nil {
			t.Skipf("Windows C compiler gcc is unavailable: %v", err)
		}
	}
	harness := `#include <limits.h>
#include <stdio.h>
` + deadlineAfter + `
` + deadlineReached + `
int main(void) {
    const unsigned long long retries[] = {100ULL,150ULL,250ULL,299ULL,300ULL,301ULL};
    const unsigned long long deadline = k_tcp_deadline_after(100ULL,200ULL);
    for (size_t i=0;i<sizeof(retries)/sizeof(retries[0]);i++) putchar(k_tcp_deadline_reached(retries[i],deadline)?'1':'0');
    putchar(' ');
    puts(k_tcp_deadline_after(ULLONG_MAX-4ULL,9ULL)==ULLONG_MAX?"saturated":"overflowed");
    return 0;
}`
	image, err := compileC(harness, target)
	if err != nil {
		t.Fatalf("compile deadline policy harness: %v", err)
	}
	extension := ""
	if target.OS == "windows" {
		extension = ".exe"
	}
	path := filepath.Join(t.TempDir(), "tcp-deadline-policy"+extension)
	if err := os.WriteFile(path, image, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("run deadline policy harness: %v; output=%q", err, output)
	}
	if got, want := strings.ReplaceAll(string(output), "\r\n", "\n"), "000011 saturated\n"; got != want {
		t.Fatalf("fixed deadline retry schedule differs: got %q want %q", got, want)
	}
}

func extractCFunction(source, signature string) (string, error) {
	searchFrom := 0
	for {
		start := strings.Index(source[searchFrom:], signature)
		if start < 0 {
			return "", fmt.Errorf("C source does not contain function %q", signature)
		}
		start += searchFrom
		afterSignature := start + len(signature)
		remainder := source[afterSignature:]
		open := strings.IndexByte(remainder, '{')
		semicolon := strings.IndexByte(remainder, ';')
		if semicolon >= 0 && (open < 0 || semicolon < open) {
			searchFrom = afterSignature + semicolon + 1
			continue
		}
		if open < 0 {
			return "", fmt.Errorf("C function %q has no body", signature)
		}
		bodyStart := afterSignature + open
		depth := 0
		for i := bodyStart; i < len(source); i++ {
			switch source[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return source[start : i+1], nil
				}
			}
		}
		return "", fmt.Errorf("C function %q has an unterminated body", signature)
	}
}
