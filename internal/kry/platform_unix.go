//go:build !windows

package kry

import (
	"fmt"
	"os/exec"
	"strings"
)

func platformOSVersion() (string, error) {
	output, err := exec.Command("uname", "-sr").Output()
	if err != nil {
		return "", fmt.Errorf("read kernel version with uname: %w", err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("uname returned an empty kernel version")
	}
	return value, nil
}
