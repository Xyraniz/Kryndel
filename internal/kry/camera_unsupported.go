//go:build !windows && !linux

package kry

import (
	"context"
	"fmt"
)

func cameraCapture(ctx context.Context, device string, width, height int64, maxBytes int64) ([]byte, error) {
	return nil, fmt.Errorf("camera capture is supported on Windows and Linux only")
}
