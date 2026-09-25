//go:build linux

package platform

import (
	"image"
	"image/color"
	"testing"

	"github.com/blackjack/webcam"
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

func TestPreferredCameraFormatIsDeterministicAndDecodable(t *testing.T) {
	formats := map[webcam.PixelFormat]string{
		fourCC("RGB3"): "RGB24",
		fourCC("YUYV"): "YUYV 4:2:2",
		fourCC("JPEG"): "JPEG",
		fourCC("MJPG"): "Motion-JPEG",
	}
	for i := 0; i < 20; i++ {
		got, ok := preferredCameraFormat(formats)
		if !ok || got != fourCC("MJPG") {
			t.Fatalf("preferred camera format = %q, %v; want MJPG, true", formatString(got), ok)
		}
	}
	got, ok := preferredCameraFormat(map[webcam.PixelFormat]string{
		fourCC("RGB3"): "RGB24",
		fourCC("YUYV"): "YUYV 4:2:2",
	})
	if !ok || got != fourCC("YUYV") {
		t.Fatalf("preferred camera fallback = %q, %v; want YUYV, true", formatString(got), ok)
	}
	if _, ok := preferredCameraFormat(map[webcam.PixelFormat]string{fourCC("H264"): "H.264"}); ok {
		t.Fatal("unsupported camera format was selected")
	}
}
