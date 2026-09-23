package kry

// artifact_crypt.go adds an authenticated, passphrase-protected container for
// Kryndel artifacts. A sealed artifact is the plain KRYNATIVE4 byte stream
// wrapped in AES-256-GCM, with the encryption key derived from a passphrase via
// PBKDF2-HMAC-SHA-256.
//
// The format is deliberately self-describing and versioned so future revisions
// can change the KDF or cipher without breaking old files:
//
//   magic       9 bytes   "KRYSEAL1\x00"
//   version     u32 LE    currently 1
//   iterations  u32 LE    PBKDF2 iteration count
//   salt        16 bytes  random per file
//   nonce       12 bytes  random per file (GCM standard size)
//   length      u64 LE    ciphertext length (plaintext + 16-byte GCM tag)
//   ciphertext  length    AES-256-GCM(plaintext)
//
// Because GCM is authenticated, a wrong passphrase or any tampering is detected
// before a single byte of the inner artifact is trusted.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
)

const sealedMagic = "KRYSEAL1\x00"
const sealedVersion = 1

// DefaultSealIterations is the PBKDF2 work factor used when the caller does not
// specify one. It is high enough to make offline guessing expensive while
// keeping interactive builds responsive.
const DefaultSealIterations = 200000

// MinSealIterations and MaxSealIterations bound the accepted work factor so a
// hostile artifact cannot force an unbounded amount of key stretching.
const (
	MinSealIterations = 1000
	MaxSealIterations = 10000000
)

// IsSealedArtifact reports whether data begins with the sealed-container magic.
func IsSealedArtifact(data []byte) bool {
	return len(data) >= len(sealedMagic) && string(data[:len(sealedMagic)]) == sealedMagic
}

// EncryptArtifact wraps a plain artifact byte stream in an authenticated,
// passphrase-protected container. iterations <= 0 selects DefaultSealIterations.
func EncryptArtifact(plain []byte, passphrase string, iterations int) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("encryption requires a non-empty passphrase")
	}
	if iterations <= 0 {
		iterations = DefaultSealIterations
	}
	if iterations < MinSealIterations || iterations > MaxSealIterations {
		return nil, errors.New("PBKDF2 iteration count out of range")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := cryptoPBKDF2SHA256([]byte(passphrase), salt, iterations, 32)
	if err != nil {
		return nil, err
	}
	sealed, err := cryptoAESGCMEncrypt(key, nonce, plain)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(sealedMagic)
	writeU32(&b, sealedVersion)
	writeU32(&b, uint32(iterations))
	b.Write(salt)
	b.Write(nonce)
	writeU64(&b, uint64(len(sealed)))
	b.Write(sealed)
	return b.Bytes(), nil
}

// DecryptArtifact opens a sealed container and returns the inner artifact bytes.
// A wrong passphrase, a truncated file, or any tampering yields an error and no
// plaintext.
func DecryptArtifact(data []byte, passphrase string, lim Limits) ([]byte, error) {
	if !IsSealedArtifact(data) {
		return nil, errors.New("not a sealed artifact")
	}
	if len(data) > lim.MaxArtifactBytes {
		return nil, errors.New("sealed artifact exceeds configured input size limit")
	}
	r := bytes.NewReader(data[len(sealedMagic):])
	ver, ok := readU32(r)
	if !ok || ver != sealedVersion {
		return nil, errors.New("unsupported sealed artifact version")
	}
	iterations, ok := readU32(r)
	if !ok || iterations < MinSealIterations || iterations > MaxSealIterations {
		return nil, errors.New("invalid sealed artifact work factor")
	}
	salt := make([]byte, 16)
	if n, err := r.Read(salt); err != nil || n != len(salt) {
		return nil, errors.New("truncated sealed artifact salt")
	}
	nonce := make([]byte, 12)
	if n, err := r.Read(nonce); err != nil || n != len(nonce) {
		return nil, errors.New("truncated sealed artifact nonce")
	}
	length, ok := readU64(r)
	if !ok || length < 16 || length > uint64(r.Len()) {
		return nil, errors.New("invalid sealed artifact length")
	}
	ciphertext := make([]byte, length)
	if n, err := r.Read(ciphertext); err != nil || n != len(ciphertext) {
		return nil, errors.New("truncated sealed artifact ciphertext")
	}
	if r.Len() != 0 {
		return nil, errors.New("sealed artifact has trailing bytes")
	}
	if passphrase == "" {
		return nil, errors.New("artifact is encrypted; a passphrase is required")
	}
	key, err := cryptoPBKDF2SHA256([]byte(passphrase), salt, int(iterations), 32)
	if err != nil {
		return nil, err
	}
	plain, err := cryptoAESGCMDecrypt(key, nonce, ciphertext)
	if err != nil {
		return nil, errors.New("decryption failed: wrong passphrase or corrupted artifact")
	}
	return plain, nil
}

var _ = binary.LittleEndian
