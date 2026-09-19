package kry

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runInterp parses, checks and runs a source string in the interpreter while
// capturing everything the program prints. It is the shared harness for the
// hardening tests below so every assertion is made against real output rather
// than an assumed result.
func runInterp(t *testing.T, src string) string {
	t.Helper()
	p, d := Parse(&Source{Name: "test.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatalf("check: %s", d.Message)
	}
	sb := Sandbox{Root: t.TempDir(), Restricted: true}
	r, d := NewRuntime(p, c, DefaultLimits(), sb)
	if d != nil {
		t.Fatal(d)
	}
	old := os.Stdout
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wr
	runErr := r.run()
	_ = wr.Close()
	os.Stdout = old
	out, _ := io.ReadAll(rd)
	if runErr != nil {
		t.Fatalf("run: %s", runErr.Message)
	}
	return string(out)
}

// TestCryptoBuiltinsInterpreter exercises every new crypto primitive through the
// language surface and checks the results against published vectors so the
// interpreter path is verified end-to-end, not just the Go helpers.
func TestCryptoBuiltinsInterpreter(t *testing.T) {
	src := `fn main() -> Nil {
    println(hex_encode(crypto_sha512(string_to_bytes("abc"))))
    println(hex_encode(crypto_sha384(string_to_bytes("abc"))))
    println(hex_encode(crypto_sha1(string_to_bytes("abc"))))
    println(hex_encode(crypto_md5(string_to_bytes("abc"))))
    println(hex_encode(crypto_hmac_sha256(string_to_bytes("key"), string_to_bytes("msg"))))
    match crypto_pbkdf2_sha256(string_to_bytes("password"), string_to_bytes("salt"), 4096, 32) {
        ok(dk) => { println(hex_encode(dk)) }
        err(e) => { println("pbkdf2 err") }
    }
    match crypto_hkdf_sha256(string_to_bytes("ikm"), string_to_bytes("salt"), string_to_bytes("info"), 16) {
        ok(okm) => { println(hex_encode(okm)) }
        err(e) => { println("hkdf err") }
    }
    println(str(crypto_constant_time_equal(string_to_bytes("abc"), string_to_bytes("abc"))))
    println(str(crypto_constant_time_equal(string_to_bytes("abc"), string_to_bytes("abd"))))
    match crypto_xor(string_to_bytes("abc"), string_to_bytes("xyz")) {
        ok(x) => { println(hex_encode(x)) }
        err(e) => { println("xor err") }
    }
    println(base64url_encode(string_to_bytes("hello world")))
    match base64url_decode("aGVsbG8gd29ybGQ") {
        ok(b) => { println(bytes_to_string(b)) }
        err(e) => { println("b64 err") }
    }
    return nil
}
`
	got := runInterp(t, src)
	want := strings.Join([]string{
		"ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f",
		"cb00753f45a35e8bb5a03d699ac65007272c32ab0eded1631a8b605a43ff5bed8086072ba1e7cc2358baeca134c825a7",
		"a9993e364706816aba3e25717850c26c9cd0d89d",
		"900150983cd24fb0d6963f7d28e17f72",
		"2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628",
		"c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a",
		"fe8f9615d2374c0d17f77d1aeaf408c2",
		"true",
		"false",
		"191b19",
		"aGVsbG8gd29ybGQ",
		"hello world",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("crypto interpreter output mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestCryptoBuiltinsNegative checks that the crypto builtins reject malformed
// input with an err(...) result instead of panicking or silently succeeding.
func TestCryptoBuiltinsNegative(t *testing.T) {
	src := `fn main() -> Nil {
    match crypto_aes_gcm_encrypt(string_to_bytes("short"), string_to_bytes("0123456789ab"), string_to_bytes("data")) {
        ok(x) => { println("enc ok") }
        err(e) => { println("enc err") }
    }
    match crypto_aes_gcm_decrypt(string_to_bytes("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), string_to_bytes("0123456789ab"), string_to_bytes("not-a-valid-tag")) {
        ok(x) => { println("dec ok") }
        err(e) => { println("dec err") }
    }
    match crypto_xor(string_to_bytes("abc"), string_to_bytes("abcd")) {
        ok(x) => { println("xor ok") }
        err(e) => { println("xor err") }
    }
    match crypto_pbkdf2_sha256(string_to_bytes("pw"), string_to_bytes("salt"), 0, 32) {
        ok(x) => { println("pbkdf2 ok") }
        err(e) => { println("pbkdf2 err") }
    }
    match base64url_decode("!!!not base64!!!") {
        ok(x) => { println("b64 ok") }
        err(e) => { println("b64 err") }
    }
    return nil
}
`
	got := runInterp(t, src)
	want := "enc err\ndec err\nxor err\npbkdf2 err\nb64 err\n"
	if got != want {
		t.Fatalf("crypto negative output mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestSealedArtifactRoundtrip verifies the encrypted artifact container: a
// correct passphrase recovers the exact plaintext, while a wrong passphrase,
// tampering, truncation and a missing passphrase are all rejected.
func TestSealedArtifactRoundtrip(t *testing.T) {
	plain := []byte("KRYNATIVE3\x00this is the inner artifact payload")
	sealed, err := EncryptArtifact(plain, "correct horse battery staple", 2000)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsSealedArtifact(sealed) {
		t.Fatal("sealed artifact does not carry the expected magic")
	}
	if bytes.Contains(sealed, []byte("inner artifact payload")) {
		t.Fatal("plaintext leaked into the sealed container")
	}
	got, err := DecryptArtifact(sealed, "correct horse battery staple", DefaultLimits())
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip mismatch: %q", got)
	}

	if _, err := DecryptArtifact(sealed, "wrong passphrase", DefaultLimits()); err == nil {
		t.Fatal("wrong passphrase was accepted")
	}
	if _, err := DecryptArtifact(sealed, "", DefaultLimits()); err == nil {
		t.Fatal("missing passphrase was accepted")
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := DecryptArtifact(tampered, "correct horse battery staple", DefaultLimits()); err == nil {
		t.Fatal("tampered artifact was accepted")
	}

	truncated := sealed[:len(sealed)-4]
	if _, err := DecryptArtifact(truncated, "correct horse battery staple", DefaultLimits()); err == nil {
		t.Fatal("truncated artifact was accepted")
	}

	if _, err := EncryptArtifact(plain, "", 0); err == nil {
		t.Fatal("empty passphrase was accepted for encryption")
	}
	if _, err := EncryptArtifact(plain, "pw", 10); err == nil {
		t.Fatal("iteration count below the minimum was accepted")
	}
	if _, err := EncryptArtifact(plain, "pw", MaxSealIterations+1); err == nil {
		t.Fatal("iteration count above the maximum was accepted")
	}
}

// TestSealedArtifactDeterministicDecrypt confirms two encryptions of the same
// plaintext differ (random salt/nonce) yet both decrypt back to the plaintext.
func TestSealedArtifactDeterministicDecrypt(t *testing.T) {
	plain := []byte("payload")
	a, err := EncryptArtifact(plain, "pw", 1000)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncryptArtifact(plain, "pw", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions produced identical bytes; salt/nonce are not random")
	}
	for _, sealed := range [][]byte{a, b} {
		got, err := DecryptArtifact(sealed, "pw", DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, plain) {
			t.Fatalf("roundtrip mismatch: %q", got)
		}
	}
}

// TestPythonConvenienceBuiltins checks the new helpers that close common Python
// ergonomics gaps: negative-index slicing, template formatting, enumeration and
// zipping.
func TestPythonConvenienceBuiltins(t *testing.T) {
	src := `fn main() -> Nil {
    match string_slice("hello world", 0, 5) {
        ok(s) => { println(s) }
        err(e) => { println("slice err") }
    }
    match string_slice("hello world", -5, -1) {
        ok(s) => { println(s) }
        err(e) => { println("slice err") }
    }
    match string_slice("abc", 2, 1) {
        ok(s) => { println("[" + s + "]") }
        err(e) => { println("slice err") }
    }
    let nums: Array[Int] = [10, 20, 30, 40, 50]
    println(str(array_slice_range(nums, 1, 3)))
    println(str(array_slice_range(nums, -2, -1)))
    println(str(array_slice_range(nums, 0, 99)))
    println(string_format("{} + {} = {}", ["1", "2", "3"]))
    println(string_format("literal {{braces}} and {}", ["x"]))
    println(string_format("missing {}", []))
    println(str(array_indices(nums)))
    println(str(array_zip([1, 2, 3], [4, 5])))
    return nil
}
`
	got := runInterp(t, src)
	want := strings.Join([]string{
		"hello",
		"worl",
		"[]",
		"[20, 30]",
		"[40]",
		"[10, 20, 30, 40, 50]",
		"1 + 2 = 3",
		"literal {braces} and x",
		"missing {}",
		"[0, 1, 2, 3, 4]",
		"[[1, 4], [2, 5]]",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("python-convenience output mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestDefaultParameters checks that omitted trailing arguments fall back to the
// declared defaults in the interpreter.
func TestDefaultParameters(t *testing.T) {
	src := `fn greet(name: String, greeting: String = "Hello", punct: String = "!") -> String {
    return greeting + ", " + name + punct
}
fn main() -> Nil {
    println(greet("Ada"))
    println(greet("Ada", "Hi"))
    println(greet("Ada", "Hi", "?"))
    return nil
}
`
	got := runInterp(t, src)
	want := "Hello, Ada!\nHi, Ada!\nHi, Ada?\n"
	if got != want {
		t.Fatalf("default parameter output mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestDefaultParameterDiagnostics checks the checker rejects a required
// parameter that follows a defaulted one and a default of the wrong type.
func TestDefaultParameterDiagnostics(t *testing.T) {
	bad := `fn f(a: Int = 1, b: Int) -> Int { return a + b }
fn main() -> Nil { return nil }
`
	if _, d := Parse(&Source{Name: "bad.kry", Text: bad}, DefaultLimits()); d == nil {
		t.Fatal("required parameter after a defaulted one was accepted")
	}
	mismatch := `fn f(a: Int = "x") -> Int { return a }
fn main() -> Nil { return nil }
`
	p, d := Parse(&Source{Name: "mismatch.kry", Text: mismatch}, DefaultLimits())
	if d != nil {
		t.Fatalf("parse: %s", d.Message)
	}
	if _, d = Check(p, DefaultLimits()); d == nil {
		t.Fatal("default value of the wrong type was accepted")
	}
}

// TestNativeCryptoParity builds a program that exercises the crypto builtins
// natively and checks the ELF output matches the interpreter byte-for-byte.
func TestNativeCryptoParity(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native parity test requires linux/amd64")
	}
	src := `fn main() -> Nil {
    println(hex_encode(crypto_sha512(string_to_bytes("abc"))))
    println(hex_encode(crypto_sha384(string_to_bytes("abc"))))
    println(hex_encode(crypto_sha1(string_to_bytes("abc"))))
    println(hex_encode(crypto_md5(string_to_bytes("abc"))))
    match crypto_pbkdf2_sha256(string_to_bytes("password"), string_to_bytes("salt"), 4096, 32) {
        ok(dk) => { println(hex_encode(dk)) }
        err(e) => { println("pbkdf2 err") }
    }
    match crypto_hkdf_sha256(string_to_bytes("ikm"), string_to_bytes("salt"), string_to_bytes("info"), 16) {
        ok(okm) => { println(hex_encode(okm)) }
        err(e) => { println("hkdf err") }
    }
    match crypto_xor(string_to_bytes("abc"), string_to_bytes("xyz")) {
        ok(x) => { println(hex_encode(x)) }
        err(e) => { println("xor err") }
    }
    println(base64url_encode(string_to_bytes("hello world")))
    return nil
}
`
	want := runInterp(t, src)
	got := runNativeELF(t, src)
	if got != want {
		t.Fatalf("native crypto parity mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestNativePythonConvenienceParity checks the Python-convenience builtins and
// default parameters produce identical output in the native backend.
func TestNativePythonConvenienceParity(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native parity test requires linux/amd64")
	}
	src := `fn greet(name: String, greeting: String = "Hello") -> String {
    return greeting + ", " + name
}
fn main() -> Nil {
    match string_slice("hello world", -5, -1) {
        ok(s) => { println(s) }
        err(e) => { println("slice err") }
    }
    let nums: Array[Int] = [10, 20, 30, 40, 50]
    println(str(array_slice_range(nums, 1, 3)))
    println(string_format("{} + {} = {}", ["1", "2", "3"]))
    println(str(array_indices(nums)))
    println(str(array_zip([1, 2, 3], [4, 5])))
    println(greet("Ada"))
    return nil
}
`
	want := runInterp(t, src)
	got := runNativeELF(t, src)
	if got != want {
		t.Fatalf("native convenience parity mismatch:\n got %q\nwant %q", got, want)
	}
}

// TestNativeObfuscation checks that string literals are absent from an
// obfuscated binary while the program still produces identical output.
func TestNativeObfuscation(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("native obfuscation test requires linux/amd64")
	}
	secret := "SUPER_SECRET_LITERAL_9f3a"
	src := `fn main() -> Nil {
    println("` + secret + `")
    return nil
}
`
	p, d := Parse(&Source{Name: "main.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	plain, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatal(err)
	}
	obf, err := BuildNativeOpts(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf", true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(plain, []byte(secret)) {
		t.Fatal("expected the plaintext literal in the non-obfuscated binary")
	}
	if bytes.Contains(obf, []byte(secret)) {
		t.Fatal("obfuscated binary still contains the plaintext literal")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "obf")
	if err := os.WriteFile(path, obf, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("obfuscated executable failed: %v", err)
	}
	if string(out) != secret+"\n" {
		t.Fatalf("obfuscated output mismatch: %q", out)
	}
}

// TestNativePEParity builds a Windows PE for the crypto surface and, when wine
// is available, checks it produces the same output as the interpreter.
func TestNativePEParity(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("PE parity test requires linux/amd64")
	}
	if _, err := exec.LookPath("x86_64-w64-mingw32-gcc"); err != nil {
		t.Skip("mingw-w64 not installed")
	}
	src := `fn main() -> Nil {
    println(hex_encode(crypto_sha1(string_to_bytes("abc"))))
    println(hex_encode(crypto_md5(string_to_bytes("abc"))))
    match crypto_xor(string_to_bytes("abc"), string_to_bytes("xyz")) {
        ok(x) => { println(hex_encode(x)) }
        err(e) => { println("xor err") }
    }
    return nil
}
`
	want := runInterp(t, src)
	p, d := Parse(&Source{Name: "main.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	pe, err := BuildNative(p, c, NativeTarget{OS: "windows", Arch: "amd64"}, "exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("wine"); err != nil {
		t.Skip("wine not installed; PE built successfully")
	}
	path := filepath.Join(t.TempDir(), "program.exe")
	if err := os.WriteFile(path, pe, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("wine", path).Output()
	if err != nil {
		t.Skipf("wine could not run the PE: %v", err)
	}
	got := strings.ReplaceAll(string(out), "\r\n", "\n")
	if got != want {
		t.Fatalf("PE parity mismatch:\n got %q\nwant %q", got, want)
	}
}

// runNativeELF compiles src to a Linux ELF, runs it and returns its stdout.
func runNativeELF(t *testing.T, src string) string {
	t.Helper()
	p, d := Parse(&Source{Name: "main.kry", Text: src}, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	c, d := Check(p, DefaultLimits())
	if d != nil {
		t.Fatal(d)
	}
	elf, err := BuildNative(p, c, NativeTarget{OS: "linux", Arch: "amd64"}, "elf")
	if err != nil {
		t.Fatalf("native build: %v", err)
	}
	path := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(path, elf, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).Output()
	if err != nil {
		t.Fatalf("native executable failed: %v", err)
	}
	return string(out)
}

// TestCryptoHelperVectors re-checks the Go-level helpers directly so a
// regression in the primitives is caught even if the language surface changes.
func TestCryptoHelperVectors(t *testing.T) {
	if got := hex.EncodeToString(cryptoHMACSHA256([]byte("key"), []byte("msg"))); got != "2d93cbc1be167bcb1637a4a23cbff01a7878f0c50ee833954ea5221bb1b8c628" {
		t.Fatalf("hmac mismatch: %s", got)
	}
	if !cryptoConstantTimeEqual([]byte("abc"), []byte("abc")) {
		t.Fatal("constant-time equal returned false for equal inputs")
	}
	if cryptoConstantTimeEqual([]byte("abc"), []byte("abd")) {
		t.Fatal("constant-time equal returned true for different inputs")
	}
	if cryptoConstantTimeEqual([]byte("abc"), []byte("abcd")) {
		t.Fatal("constant-time equal returned true for different lengths")
	}
	x, err := cryptoXOR([]byte("abc"), []byte("xyz"))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(x) != "191b19" {
		t.Fatalf("xor mismatch: %s", hex.EncodeToString(x))
	}
	if _, err := cryptoXOR([]byte("abc"), []byte("abcd")); err == nil {
		t.Fatal("xor accepted mismatched lengths")
	}
	enc := base64URLEncode([]byte("hello world"))
	if enc != "aGVsbG8gd29ybGQ" {
		t.Fatalf("base64url encode mismatch: %s", enc)
	}
	dec, err := base64URLDecode(enc)
	if err != nil || string(dec) != "hello world" {
		t.Fatalf("base64url decode mismatch: %q %v", dec, err)
	}
}
