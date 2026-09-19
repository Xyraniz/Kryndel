package kry

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	mrand "math/rand"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// uuidV4 returns an RFC 9562 version-4 UUID. The bytes come from the
// operating system CSPRNG; math/rand is deliberately not used for identity
// values.
func uuidV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("uuid entropy source: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16])), nil
}

func parseUUID(value string) ([]byte, error) {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return nil, fmt.Errorf("invalid UUID")
	}
	b, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(b) != 16 {
		return nil, fmt.Errorf("invalid UUID")
	}
	return b, nil
}

// uuidV5 implements the standards-mandated SHA-1 name-based UUID algorithm.
// UUID v5 uses SHA-1 as part of its wire-format specification; this is not a
// password or message-integrity primitive.
func uuidV5(namespace, name string) (string, error) {
	ns, err := parseUUID(namespace)
	if err != nil {
		return "", err
	}
	h := sha1.New()
	_, _ = h.Write(ns)
	_, _ = h.Write([]byte(name))
	b := h.Sum(nil)[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16])), nil
}

func validUUID(value string) bool {
	b, err := parseUUID(strings.ToLower(value))
	return err == nil && len(b) == 16
}

func dotenvParse(data string) (map[string]string, error) {
	values := make(map[string]string)
	for lineNo, raw := range strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf(".env line %d: expected KEY=value", lineNo+1)
		}
		key := strings.TrimSpace(line[:eq])
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			return nil, fmt.Errorf(".env line %d: invalid variable name %q", lineNo+1, key)
		}
		value, err := dotenvValue(strings.TrimSpace(line[eq+1:]))
		if err != nil {
			return nil, fmt.Errorf(".env line %d: %w", lineNo+1, err)
		}
		// Match common dotenv behavior: references use values already defined in
		// the file first and the host environment as a fallback.
		value = os.Expand(value, func(name string) string {
			if v, ok := values[name]; ok {
				return v
			}
			return os.Getenv(name)
		})
		values[key] = value
	}
	return values, nil
}

func dotenvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value[0] == '\'' {
		end := strings.LastIndexByte(value[1:], '\'')
		if end < 0 {
			return "", fmt.Errorf("unterminated single-quoted value")
		}
		end++
		if suffix := strings.TrimSpace(value[end+1:]); suffix != "" && !strings.HasPrefix(suffix, "#") {
			return "", fmt.Errorf("unexpected text after single-quoted value")
		}
		return value[1:end], nil
	}
	if value[0] == '"' {
		end := strings.LastIndexByte(value[1:], '"')
		if end < 0 {
			return "", fmt.Errorf("unterminated double-quoted value")
		}
		end++
		if suffix := strings.TrimSpace(value[end+1:]); suffix != "" && !strings.HasPrefix(suffix, "#") {
			return "", fmt.Errorf("unexpected text after double-quoted value")
		}
		decoded, err := strconv.Unquote(value[:end+1])
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted value: %w", err)
		}
		return decoded, nil
	}
	if comment := strings.Index(value, " #"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	return value, nil
}

func platformOS() string      { return runtime.GOOS }
func platformArch() string    { return runtime.GOARCH }
func platformRuntime() string { return runtime.Version() }

func newRandom(seed int64) *randomHandle {
	return &randomHandle{rng: mrand.New(mrand.NewSource(seed))}
}

func randomInt(h *randomHandle, min, max int64) (int64, error) {
	if h == nil {
		return 0, fmt.Errorf("invalid Random handle")
	}
	if min > max {
		return 0, fmt.Errorf("random_int requires min <= max")
	}
	// An inclusive Int range can cover the complete signed domain, whose size
	// is 2^64 and therefore cannot be represented in an Int or uint64.
	if min == (-1<<63) && max == (1<<63-1) {
		h.mu.Lock()
		v := int64(h.rng.Uint64() ^ (uint64(1) << 63))
		h.mu.Unlock()
		return v, nil
	}
	h.mu.Lock()
	span := uint64(max) - uint64(min) + 1
	if span <= uint64(1<<63) {
		v := min + h.rng.Int63n(int64(span))
		h.mu.Unlock()
		return v, nil
	}
	// Rejection sampling removes modulo bias for ranges wider than Int63.
	limit := ^uint64(0) - (^uint64(0) % span)
	var sample uint64
	for {
		sample = h.rng.Uint64()
		if sample < limit {
			break
		}
	}
	v := int64(uint64(min) + sample%span)
	h.mu.Unlock()
	return v, nil
}

func randomFloat(h *randomHandle) (float64, error) {
	if h == nil {
		return 0, fmt.Errorf("invalid Random handle")
	}
	h.mu.Lock()
	v := h.rng.Float64()
	h.mu.Unlock()
	return v, nil
}
