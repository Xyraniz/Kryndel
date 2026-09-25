package kry

import (
	"fmt"
	"strconv"
	"strings"
)

const CompilerVersion = "1.3.0"

type semanticVersion struct {
	major, minor, patch uint64
}

type versionConstraint struct {
	operator string
	version  semanticVersion
}

func parseSemanticVersion(raw string) (semanticVersion, error) {
	value := strings.TrimSpace(raw)
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, fmt.Errorf("expected a three-part version")
	}
	values := [3]uint64{}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, fmt.Errorf("invalid version component %q", part)
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return semanticVersion{}, fmt.Errorf("invalid version component %q", part)
		}
		values[i] = n
	}
	return semanticVersion{major: values[0], minor: values[1], patch: values[2]}, nil
}

func parseVersionConstraint(raw string) (versionConstraint, error) {
	value := strings.TrimSpace(raw)
	if value == "*" {
		return versionConstraint{operator: "*"}, nil
	}
	op := "="
	base := value
	for _, candidate := range []string{">=", "^", "~"} {
		if strings.HasPrefix(value, candidate) {
			op = candidate
			base = strings.TrimSpace(strings.TrimPrefix(value, candidate))
			break
		}
	}
	version, err := parseSemanticVersion(base)
	if err != nil {
		return versionConstraint{}, fmt.Errorf("invalid version constraint %q: %w", raw, err)
	}
	return versionConstraint{operator: op, version: version}, nil
}

func validVersion(value string) bool {
	_, err := parseSemanticVersion(value)
	return err == nil
}

func satisfies(version, constraint string) bool {
	candidate, err := parseSemanticVersion(version)
	if err != nil {
		return false
	}
	requirement, err := parseVersionConstraint(constraint)
	if err != nil {
		return false
	}
	switch requirement.operator {
	case "*":
		return true
	case ">=":
		return compareSemanticVersions(candidate, requirement.version) >= 0
	case "~":
		return candidate.major == requirement.version.major && candidate.minor == requirement.version.minor && compareSemanticVersions(candidate, requirement.version) >= 0
	case "^":
		base := requirement.version
		if base.major > 0 {
			return candidate.major == base.major && compareSemanticVersions(candidate, base) >= 0
		}
		if base.minor > 0 {
			return candidate.major == 0 && candidate.minor == base.minor && compareSemanticVersions(candidate, base) >= 0
		}
		return candidate.major == 0 && candidate.minor == 0 && candidate.patch == base.patch
	default:
		return compareSemanticVersions(candidate, requirement.version) == 0
	}
}

func compareVersion(a, b string) int {
	left, leftErr := parseSemanticVersion(a)
	right, rightErr := parseSemanticVersion(b)
	if leftErr != nil || rightErr != nil {
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return compareSemanticVersions(left, right)
}

func compareSemanticVersions(a, b semanticVersion) int {
	left := [...]uint64{a.major, a.minor, a.patch}
	right := [...]uint64{b.major, b.minor, b.patch}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}

func ValidateCompilerRequirement(packageName, requirement string) error {
	if _, err := parseVersionConstraint(requirement); err != nil {
		return fmt.Errorf("package %q has invalid Kryndel requirement %q: %w", packageName, requirement, err)
	}
	if !satisfies(CompilerVersion, requirement) {
		return fmt.Errorf("package %q requires Kryndel %q, current compiler is %s", packageName, requirement, CompilerVersion)
	}
	return nil
}
