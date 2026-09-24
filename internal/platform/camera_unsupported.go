//go:build !windows && !linux

package platform

import (
	"context"
	"fmt"
)

func CaptureCamera(ctx context.Context, device string, width, height int64, maxBytes int64) ([]byte, error) {
	return nil, fmt.Errorf("camera capture is supported on Windows and Linux only")
}
