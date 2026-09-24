package platform

import (
	"image"
	"testing"
)

func TestScreenCaptureValidation(t *testing.T) {
	if err := validateScreenRegion(0, 0, 0, 10); err == nil {
		t.Fatal("zero-width screen region was accepted")
	}
	if err := validateScreenRegion(0, 0, maxScreenDimension+1, 10); err == nil {
		t.Fatal("oversized screen region was accepted")
	}
	if _, err := encodeScreenImage(image.NewRGBA(image.Rect(0, 0, 1, 1)), 1); err == nil {
		t.Fatal("PNG output limit was ignored")
	}
}
