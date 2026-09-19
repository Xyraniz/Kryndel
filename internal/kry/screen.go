package kry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"

	"github.com/kbinani/screenshot"
)

const (
	maxScreenDimension = 16384
	maxScreenPixels    = 64 * 1024 * 1024
)

func validateScreenRegion(x, y, width, height int64) error {
	if width < 1 || height < 1 {
		return fmt.Errorf("screen capture dimensions must be positive")
	}
	if width > maxScreenDimension || height > maxScreenDimension {
		return fmt.Errorf("screen capture dimensions exceed %d pixels", maxScreenDimension)
	}
	if width > maxScreenPixels/height {
		return fmt.Errorf("screen capture region is too large")
	}
	return nil
}

func encodeScreenImage(img image.Image, maxBytes int64) ([]byte, error) {
	if img == nil {
		return nil, fmt.Errorf("screen capture returned no image")
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode screenshot as PNG: %w", err)
	}
	if int64(out.Len()) > maxBytes {
		return nil, fmt.Errorf("encoded screenshot exceeds output limit")
	}
	return out.Bytes(), nil
}

func screenCapturePNG(x, y, width, height int64, maxBytes int64) ([]byte, error) {
	if err := validateScreenRegion(x, y, width, height); err != nil {
		return nil, err
	}
	if maxBytes < 1 {
		return nil, fmt.Errorf("output limit must be positive")
	}
	img, err := screenshot.Capture(int(x), int(y), int(width), int(height))
	if err != nil {
		return nil, fmt.Errorf("capture screen region: %w", err)
	}
	return encodeScreenImage(img, maxBytes)
}

func screenDisplayCount() (int, error) {
	count := screenshot.NumActiveDisplays()
	if count < 1 {
		return 0, fmt.Errorf("no active displays found")
	}
	return count, nil
}

func screenDisplayBoundsJSON(index int64) (string, error) {
	if index < 0 || index > int64(^uint(0)>>1) {
		return "", fmt.Errorf("display index is out of range")
	}
	count, err := screenDisplayCount()
	if err != nil {
		return "", err
	}
	if index >= int64(count) {
		return "", fmt.Errorf("display index %d is out of range", index)
	}
	bounds := screenshot.GetDisplayBounds(int(index))
	value, err := json.Marshal(map[string]int{
		"x":      bounds.Min.X,
		"y":      bounds.Min.Y,
		"width":  bounds.Dx(),
		"height": bounds.Dy(),
	})
	if err != nil {
		return "", err
	}
	return string(value), nil
}

func screenCaptureDisplayPNG(index int64, maxBytes int64) ([]byte, error) {
	if index < 0 || index > int64(^uint(0)>>1) {
		return nil, fmt.Errorf("display index is out of range")
	}
	if maxBytes < 1 {
		return nil, fmt.Errorf("output limit must be positive")
	}
	count, err := screenDisplayCount()
	if err != nil {
		return nil, err
	}
	if index >= int64(count) {
		return nil, fmt.Errorf("display index %d is out of range", index)
	}
	bounds := screenshot.GetDisplayBounds(int(index))
	if err := validateScreenRegion(int64(bounds.Min.X), int64(bounds.Min.Y), int64(bounds.Dx()), int64(bounds.Dy())); err != nil {
		return nil, err
	}
	img, err := screenshot.CaptureDisplay(int(index))
	if err != nil {
		return nil, fmt.Errorf("capture display %d: %w", index, err)
	}
	return encodeScreenImage(img, maxBytes)
}
