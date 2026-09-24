package platform

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
)

const (
	maxCameraDimension = 8192
	maxCameraPixels    = 16 * 1024 * 1024
)

func validateCameraCapture(device string, width, height int64, maxBytes int64) error {
	if device == "" {
		return fmt.Errorf("camera device must not be empty")
	}
	if bytes.IndexByte([]byte(device), 0) >= 0 {
		return fmt.Errorf("camera device contains a NUL byte")
	}
	if width < 1 || height < 1 {
		return fmt.Errorf("camera dimensions must be positive")
	}
	if width > maxCameraDimension || height > maxCameraDimension {
		return fmt.Errorf("camera dimensions exceed %d pixels", maxCameraDimension)
	}
	if width > maxCameraPixels/height {
		return fmt.Errorf("camera frame is too large")
	}
	if maxBytes < 1 {
		return fmt.Errorf("output limit must be positive")
	}
	return nil
}

func encodeCameraPNG(img image.Image, maxBytes int64) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("camera returned no image")
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode camera frame as PNG: %w", err)
	}
	if int64(out.Len()) > maxBytes {
		return nil, fmt.Errorf("encoded camera frame exceeds output limit")
	}
	return out.Bytes(), nil
}
