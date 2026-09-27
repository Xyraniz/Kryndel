package kry

import (
	"fmt"
	"strings"
	"testing"
)

func TestCAOTAESGCMMatchesInterpreter(t *testing.T) {
	const key = "31bdadd96698c204aa9ce1448ea94ae1fb4a9a0b3c9d773b51bb1822666b8f22"
	const wrongKey = "0000000000000000000000000000000000000000000000000000000000000000"
	const nonce = "0d18e06c7c725ac9e362e1ce"
	const plaintext = "2db5168e932556f8089a0622981d017d"
	const ciphertext = "fa4362189661d163fcd6a56d8bf0405ad636ac1bbedd5cc3ee727dc2ab4a9489"
	const tamperedCiphertext = "fa4362189661d163fcd6a56d8bf0405ad636ac1bbedd5cc3ee727dc2ab4a9488"

	source := fmt.Sprintf(`fn show_encrypt(label: String, result: Result[Bytes, String]) -> Nil {
    match result {
        ok(value) => { println(label + ":ok:" + hex_encode(value)) }
        err(problem) => { println(label + ":err:" + problem) }
    }
}
fn show_decrypt(label: String, result: Result[Bytes, String]) -> Nil {
    match result {
        ok(value) => { println(label + ":ok:" + hex_encode(value)) }
        err(problem) => { println(label + ":err:" + problem) }
    }
}
fn main() -> Result[Nil, String] {
    let key: Bytes = hex_decode(%q)?
    let wrong_key: Bytes = hex_decode(%q)?
    let nonce: Bytes = hex_decode(%q)?
    let plaintext: Bytes = hex_decode(%q)?
    let ciphertext: Bytes = hex_decode(%q)?
    let tampered: Bytes = hex_decode(%q)?
    let short_key: Bytes = hex_decode("00")?
    let short_nonce: Bytes = hex_decode("00")?
    let short_ciphertext: Bytes = hex_decode("00")?

    show_encrypt("encrypt_vector", crypto_aes_gcm_encrypt(key, nonce, plaintext))
    show_decrypt("decrypt_vector", crypto_aes_gcm_decrypt(key, nonce, ciphertext))
    show_decrypt("decrypt_wrong_key", crypto_aes_gcm_decrypt(wrong_key, nonce, ciphertext))
    show_decrypt("decrypt_wrong_tag", crypto_aes_gcm_decrypt(key, nonce, tampered))
    show_decrypt("decrypt_short_ciphertext", crypto_aes_gcm_decrypt(key, nonce, short_ciphertext))
    show_encrypt("encrypt_short_key", crypto_aes_gcm_encrypt(short_key, nonce, plaintext))
    show_decrypt("decrypt_short_key", crypto_aes_gcm_decrypt(short_key, nonce, ciphertext))
    show_encrypt("encrypt_short_nonce", crypto_aes_gcm_encrypt(key, short_nonce, plaintext))
    show_decrypt("decrypt_short_nonce", crypto_aes_gcm_decrypt(key, short_nonce, ciphertext))
    return ok(nil)
}
`, key, wrongKey, nonce, plaintext, ciphertext, tamperedCiphertext)

	interpreted, diagnostic := runInterpreterCapture(t, source)
	if diagnostic != nil {
		t.Fatalf("interpreter failed: %s", diagnostic.Message)
	}
	want := strings.Join([]string{
		"encrypt_vector:ok:" + ciphertext,
		"decrypt_vector:ok:" + plaintext,
		"decrypt_wrong_key:err:AES-GCM authentication failed",
		"decrypt_wrong_tag:err:AES-GCM authentication failed",
		"decrypt_short_ciphertext:err:AES-GCM authentication failed",
		"encrypt_short_key:err:AES-256-GCM key must be 32 bytes",
		"decrypt_short_key:err:AES-256-GCM key must be 32 bytes",
		"encrypt_short_nonce:err:AES-GCM nonce must be 12 bytes",
		"decrypt_short_nonce:err:AES-GCM nonce must be 12 bytes",
		"",
	}, "\n")
	if interpreted != want {
		t.Fatalf("interpreter AES-GCM result differs from the NIST vector and error contract:\n got %q\nwant %q", interpreted, want)
	}

	native, status, err := buildAndRunNativeAOT(t, source)
	if err != nil {
		t.Fatalf("C AOT AES-GCM program failed to build or run: %v", err)
	}
	if status != 0 || native != interpreted {
		t.Fatalf("AES-GCM behavior differs:\ninterpreter (0): %q\nC AOT (%d): %q", interpreted, status, native)
	}
}

func TestDirectELFRejectsAESGCMWithExplicitDiagnostics(t *testing.T) {
	cases := map[string]string{
		"crypto_aes_gcm_encrypt": `fn use(key: Bytes, nonce: Bytes, plaintext: Bytes) -> Result[Bytes, String] { return crypto_aes_gcm_encrypt(key, nonce, plaintext) }`,
		"crypto_aes_gcm_decrypt": `fn use(key: Bytes, nonce: Bytes, ciphertext: Bytes) -> Result[Bytes, String] { return crypto_aes_gcm_decrypt(key, nonce, ciphertext) }`,
	}
	for builtin, function := range cases {
		t.Run(builtin, func(t *testing.T) {
			source := function + "\nfn main() -> Nil { return nil }\n"
			program, diagnostic := Parse(&Source{Name: "direct-aes-gcm.kry", Text: source}, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			checker, diagnostic := Check(program, DefaultLimits())
			if diagnostic != nil {
				t.Fatal(diagnostic.Message)
			}
			_, err := BuildDirectELF(program, checker, NativeTarget{OS: "linux", Arch: "amd64"})
			want := fmt.Sprintf(`builtin %q is not listed as supported by the elf-direct backend`, builtin)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected explicit ELF-direct rejection %q, got %v", want, err)
			}
		})
	}
}
