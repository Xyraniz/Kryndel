//go:build linux

package kry

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"time"

	"github.com/blackjack/webcam"
)

func cameraCapture(ctx context.Context, device string, width, height int64, maxBytes int64) ([]byte, error) {
	if err := validateCameraCapture(device, width, height, maxBytes); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wc, err := webcam.Open(device)
	if err != nil {
		return nil, fmt.Errorf("open V4L2 camera: %w", err)
	}
	defer wc.Close()

	format, ok := preferredCameraFormat(wc.GetSupportedFormats())
	if !ok {
		return nil, fmt.Errorf("camera exposes no supported pixel formats")
	}
	actual, actualWidth, actualHeight, err := wc.SetImageFormat(format, uint32(width), uint32(height))
	if err != nil {
		return nil, fmt.Errorf("configure V4L2 camera format: %w", err)
	}
	if err := wc.StartStreaming(); err != nil {
		return nil, fmt.Errorf("start V4L2 camera stream: %w", err)
	}
	defer wc.StopStreaming()
	frameDeadline := time.Now().Add(5 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(frameDeadline)
		if remaining <= 0 {
			return nil, fmt.Errorf("wait for camera frame: timeout")
		}
		timeoutMS := uint32(remaining / time.Millisecond)
		if timeoutMS > 100 {
			timeoutMS = 100
		}
		if err := wc.WaitForFrame(timeoutMS); err != nil {
			var timeout *webcam.Timeout
			if errors.As(err, &timeout) {
				continue
			}
			return nil, fmt.Errorf("wait for camera frame: %w", err)
		}
		break
	}
	frame, err := wc.ReadFrame()
	if err != nil {
		return nil, fmt.Errorf("read camera frame: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(frame)) > maxBytes {
		return nil, fmt.Errorf("raw camera frame exceeds input limit")
	}
	if actual == fourCC("MJPG") || actual == fourCC("JPEG") {
		img, err := jpeg.Decode(bytes.NewReader(frame))
		if err != nil {
			return nil, fmt.Errorf("decode MJPEG camera frame: %w", err)
		}
		return encodeCameraPNG(img, maxBytes)
	}
	img, err := decodeV4L2Frame(frame, actual, int(actualWidth), int(actualHeight))
	if err != nil {
		return nil, err
	}
	return encodeCameraPNG(img, maxBytes)
}

func preferredCameraFormat(formats map[webcam.PixelFormat]string) (webcam.PixelFormat, bool) {
	for format, description := range formats {
		lower := strings.ToLower(description)
		if strings.Contains(lower, "mjpeg") || strings.Contains(lower, "motion-jpeg") {
			return format, true
		}
	}
	for format := range formats {
		return format, true
	}
	return 0, false
}

func fourCC(value string) webcam.PixelFormat {
	if len(value) != 4 {
		return 0
	}
	return webcam.PixelFormat(uint32(value[0]) | uint32(value[1])<<8 | uint32(value[2])<<16 | uint32(value[3])<<24)
}

func decodeV4L2Frame(frame []byte, format webcam.PixelFormat, width, height int) (image.Image, error) {
	if width < 1 || height < 1 {
		return nil, fmt.Errorf("camera returned invalid frame dimensions")
	}
	switch format {
	case fourCC("YUYV"):
		if len(frame) < width*height*2 {
			return nil, fmt.Errorf("truncated YUYV camera frame")
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for i, p := 0, 0; i < width*height; i, p = i+2, p+4 {
			y0, u, y1, v := int(frame[p])-16, int(frame[p+1])-128, int(frame[p+2])-16, int(frame[p+3])-128
			x := i % width
			y := i / width
			img.SetRGBA(x, y, yuvPixel(y0, u, v))
			if x+1 < width {
				img.SetRGBA(x+1, y, yuvPixel(y1, u, v))
			}
		}
		return img, nil
	case fourCC("RGB3"):
		if len(frame) < width*height*3 {
			return nil, fmt.Errorf("truncated RGB24 camera frame")
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for i, p := 0, 0; i < width*height; i, p = i+1, p+3 {
			img.SetRGBA(i%width, i/width, color.RGBA{R: frame[p], G: frame[p+1], B: frame[p+2], A: 255})
		}
		return img, nil
	default:
		return nil, fmt.Errorf("unsupported V4L2 pixel format %q", formatString(format))
	}
}

func yuvPixel(y, u, v int) color.RGBA {
	r := clampByte(298*y + 409*v)
	g := clampByte(298*y - 100*u - 208*v)
	b := clampByte(298*y + 516*u)
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func clampByte(value int) uint8 {
	value = (value + 128) >> 8
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value)
}

func formatString(format webcam.PixelFormat) string {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(format))
	return string(buf[:])
}
