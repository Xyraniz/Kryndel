package kry

import (
	"encoding/hex"
	"testing"
)

// TestCryptoKnownVectors checks the new primitives against published test
// vectors so the implementation is verified, not merely self-consistent.
func TestCryptoKnownVectors(t *testing.T) {
	// SHA-512("abc")
	if got := hex.EncodeToString(mustHash(t, "crypto_sha512", []byte("abc"))); got != "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f" {
		t.Fatalf("sha512 mismatch: %s", got)
	}
	// SHA-384("abc")
	if got := hex.EncodeToString(mustHash(t, "crypto_sha384", []byte("abc"))); got != "cb00753f45a35e8bb5a03d699ac65007272c32ab0eded1631a8b605a43ff5bed8086072ba1e7cc2358baeca134c825a7" {
		t.Fatalf("sha384 mismatch: %s", got)
	}
	// SHA-1("abc")
	if got := hex.EncodeToString(mustHash(t, "crypto_sha1", []byte("abc"))); got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Fatalf("sha1 mismatch: %s", got)
	}
	// MD5("abc")
	if got := hex.EncodeToString(mustHash(t, "crypto_md5", []byte("abc"))); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("md5 mismatch: %s", got)
	}

	// RFC 5869 Test Case 1 for HKDF-SHA-256.
	ikm, _ := hex.DecodeString("0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b")
	salt, _ := hex.DecodeString("000102030405060708090a0b0c")
	info, _ := hex.DecodeString("f0f1f2f3f4f5f6f7f8f9")
	okm, err := cryptoHKDFSHA256(ikm, salt, info, 42)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(okm); got != "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865" {
		t.Fatalf("hkdf mismatch: %s", got)
	}

	// RFC 6070 PBKDF2-HMAC-SHA-256 vector: P="password", S="salt", c=4096, dkLen=32.
	dk, err := cryptoPBKDF2SHA256([]byte("password"), []byte("salt"), 4096, 32)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(dk); got != "c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a" {
		t.Fatalf("pbkdf2 mismatch: %s", got)
	}

	// NIST AES-256-GCM test vector (gcmEncryptExtIV256, key/nonce/plaintext).
	key, _ := hex.DecodeString("31bdadd96698c204aa9ce1448ea94ae1fb4a9a0b3c9d773b51bb1822666b8f22")
	nonce, _ := hex.DecodeString("0d18e06c7c725ac9e362e1ce")
	plaintext, _ := hex.DecodeString("2db5168e932556f8089a0622981d017d")
	ct, err := cryptoAESGCMEncrypt(key, nonce, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(ct); got != "fa4362189661d163fcd6a56d8bf0405a"+"d636ac1bbedd5cc3ee727dc2ab4a9489" {
		t.Fatalf("aes-gcm mismatch: %s", got)
	}
	pt, err := cryptoAESGCMDecrypt(key, nonce, ct)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(pt) != hex.EncodeToString(plaintext) {
		t.Fatalf("aes-gcm roundtrip mismatch: %s", hex.EncodeToString(pt))
	}
}

func mustHash(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	out, err := cryptoHash(name, data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
