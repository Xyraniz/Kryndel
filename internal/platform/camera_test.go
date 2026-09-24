package platform

import (
	"image"
	"testing"
)

func TestCameraCaptureValidation(t *testing.T) {
	if err := validateCameraCapture("", 640, 480, 1<<20); err == nil {
		t.Fatal("empty camera device was accepted")
	}
	if err := validateCameraCapture("camera", 0, 480, 1<<20); err == nil {
		t.Fatal("zero camera width was accepted")
	}
	if err := validateCameraCapture("camera", 640, 480, 0); err == nil {
		t.Fatal("zero camera output limit was accepted")
	}
	if _, err := encodeCameraPNG(image.NewRGBA(image.Rect(0, 0, 1, 1)), 1); err == nil {
		t.Fatal("camera PNG output limit was ignored")
	}
}
