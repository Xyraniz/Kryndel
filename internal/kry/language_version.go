package kry

import (
	"fmt"
	"strconv"
	"strings"
)

// LanguageVersion identifies the source-language rules implemented by this
// compiler. It is independent of the compiler release number.
const LanguageVersion = "1.0.0"

func validLanguageVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}

func checkLanguageVersion(version string) error {
	if !validLanguageVersion(version) {
		return fmt.Errorf("language_version %q must use MAJOR.MINOR.PATCH", version)
	}
	if version != LanguageVersion {
		return fmt.Errorf("unsupported language_version %q; this compiler supports %s", version, LanguageVersion)
	}
	return nil
}
