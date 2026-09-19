//go:build !windows

package kry

import "fmt"

func windowsInputReadNative(kind string, timeoutMS int64) (string, error) {
	return "", fmt.Errorf("low-level input hooks are available only on Windows")
}
