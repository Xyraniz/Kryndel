//go:build linux

package platform

import (
	"image"
	"image/color"
	"testing"
)

func TestDecodeV4L2YUYVHandlesOddWidthRows(t *testing.T) {
	frame := []byte{
		16, 128, 235, 128, 81, 90, 145, 240,
		16, 128, 16, 128, 16, 128, 16, 128,
	}
	decoded, err := decodeV4L2Frame(frame, fourCC("YUYV"), 3, 2)
	if err != nil {
		t.Fatalf("decode odd-width YUYV frame: %v", err)
	}
	img, ok := decoded.(*image.RGBA)
	if !ok {
		t.Fatalf("decoded image type = %T, want *image.RGBA", decoded)
	}
	if got, want := img.Bounds(), image.Rect(0, 0, 3, 2); got != want {
		t.Fatalf("decoded bounds = %v, want %v", got, want)
	}
	if got, want := img.RGBAAt(0, 0), (color.RGBA{A: 255}); got != want {
		t.Fatalf("first pixel = %#v, want %#v", got, want)
	}
	if got, want := img.RGBAAt(1, 0), (color.RGBA{R: 255, G: 255, B: 255, A: 255}); got != want {
		t.Fatalf("second pixel = %#v, want %#v", got, want)
	}
	if got, want := img.RGBAAt(0, 1), (color.RGBA{A: 255}); got != want {
		t.Fatalf("first pixel of second row = %#v, want %#v", got, want)
	}

	if _, err := decodeV4L2Frame(frame[:len(frame)-1], fourCC("YUYV"), 3, 2); err == nil {
		t.Fatal("truncated odd-width YUYV frame was accepted")
	}
}
