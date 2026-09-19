package kry

// crypto.go implements the cryptographic primitives exposed to Kryndel source
// programs. Everything here is built on the Go standard library only, so the
// toolchain keeps building with CGO_ENABLED=0 and no third-party modules.
//
// The primitives are deliberately small and explicit:
//
//   - hashing: SHA-256, SHA-512, SHA-384, SHA-1, MD5
//   - message authentication: HMAC-SHA-256
//   - authenticated encryption: AES-256-GCM
//   - key derivation: PBKDF2-HMAC-SHA-256 and HKDF-SHA-256
//   - byte helpers: constant-time equality and XOR
//
// Every operation that can fail returns a Result so callers never receive a
// silently truncated or unauthenticated value.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"errors"
)

// cryptoHash dispatches the fixed-size hash builtins. It returns the raw digest
// bytes for the requested algorithm.
func cryptoHash(name string, data []byte) ([]byte, error) {
	switch name {
	case "crypto_sha256":
		h := sha256.Sum256(data)
		return h[:], nil
	case "crypto_sha512":
		h := sha512.Sum512(data)
		return h[:], nil
	case "crypto_sha384":
		h := sha512.Sum384(data)
		return h[:], nil
	case "crypto_sha1":
		h := sha1.Sum(data)
		return h[:], nil
	case "crypto_md5":
		h := md5.Sum(data)
		return h[:], nil
	}
	return nil, errors.New("unknown hash algorithm")
}

// cryptoHMACSHA256 authenticates data with HMAC-SHA-256.
func cryptoHMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// cryptoAESGCMEncrypt seals plaintext with AES-256-GCM. The key must be exactly
// 32 bytes and the nonce exactly 12 bytes (the GCM standard size). The returned
// ciphertext includes the 16-byte authentication tag appended by GCM.
func cryptoAESGCMEncrypt(key, nonce, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256-GCM key must be 32 bytes")
	}
	if len(nonce) != 12 {
		return nil, errors.New("AES-GCM nonce must be 12 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Seal(nil, nonce, plaintext, nil), nil
}

// cryptoAESGCMDecrypt opens a sealed AES-256-GCM ciphertext. Authentication
// failure is reported as an error rather than returning unauthenticated bytes.
func cryptoAESGCMDecrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("AES-256-GCM key must be 32 bytes")
	}
	if len(nonce) != 12 {
		return nil, errors.New("AES-GCM nonce must be 12 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("AES-GCM authentication failed")
	}
	return plaintext, nil
}

// cryptoPBKDF2SHA256 derives a key with PBKDF2-HMAC-SHA-256 (RFC 8018). It is
// implemented directly so the toolchain does not depend on x/crypto.
func cryptoPBKDF2SHA256(password, salt []byte, iterations, length int) ([]byte, error) {
	if iterations < 1 {
		return nil, errors.New("PBKDF2 iterations must be at least 1")
	}
	if length < 1 || length > 1<<20 {
		return nil, errors.New("PBKDF2 length must be between 1 and 1048576")
	}
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (length + hashLen - 1) / hashLen
	var derived []byte
	buf := make([]byte, 4)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf)
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iterations; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:length], nil
}

// cryptoHKDFSHA256 expands input keying material with HKDF-SHA-256 (RFC 5869).
// An empty salt is replaced with a zero-filled block of the hash length, as the
// RFC requires.
func cryptoHKDFSHA256(ikm, salt, info []byte, length int) ([]byte, error) {
	if length < 1 || length > 255*sha256.Size {
		return nil, errors.New("HKDF length must be between 1 and 8160")
	}
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	// Extract.
	extract := hmac.New(sha256.New, salt)
	extract.Write(ikm)
	prk := extract.Sum(nil)
	// Expand.
	var out []byte
	var block []byte
	counter := byte(1)
	for len(out) < length {
		expand := hmac.New(sha256.New, prk)
		expand.Write(block)
		expand.Write(info)
		expand.Write([]byte{counter})
		block = expand.Sum(nil)
		out = append(out, block...)
		counter++
	}
	return out[:length], nil
}

// cryptoConstantTimeEqual compares two byte slices without leaking their
// contents through timing. Length differences are folded into the result.
func cryptoConstantTimeEqual(a, b []byte) bool {
	if len(a) != len(b) {
		// Still perform a comparison to keep the cost independent of where the
		// difference is, then force the result to false.
		subtle.ConstantTimeCompare(a, a)
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

// cryptoXOR returns the byte-wise XOR of two equal-length slices.
func cryptoXOR(a, b []byte) ([]byte, error) {
	if len(a) != len(b) {
		return nil, errors.New("xor requires equal-length byte sequences")
	}
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out, nil
}

// base64URLEncode encodes bytes with the URL-safe alphabet and no padding.
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

// base64URLDecode decodes URL-safe base64, accepting both padded and unpadded
// input.
func base64URLDecode(text string) ([]byte, error) {
	if data, err := base64.RawURLEncoding.DecodeString(text); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(text)
}
