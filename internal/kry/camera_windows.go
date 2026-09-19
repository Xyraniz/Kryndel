//go:build windows

package kry

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func cameraCapture(ctx context.Context, device string, width, height int64, maxBytes int64) ([]byte, error) {
	if err := validateCameraCapture(device, width, height, maxBytes); err != nil {
		return nil, err
	}
	ffmpeg := os.Getenv("KRYNDEL_FFMPEG")
	if ffmpeg == "" {
		var err error
		ffmpeg, err = exec.LookPath("ffmpeg")
		if err != nil {
			return nil, fmt.Errorf("ffmpeg is required for Windows webcam capture: %w", err)
		}
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "dshow",
		"-video_size", strconv.FormatInt(width, 10) + "x" + strconv.FormatInt(height, 10),
		"-i", "video=" + device,
		"-frames:v", "1",
		"-f", "image2pipe", "-vcodec", "png", "pipe:1",
	}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare ffmpeg stdout: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &limitedStringWriter{Builder: &stderr, Limit: 64 << 10}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxBytes+1))
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("read webcam frame: %w", readErr)
	}
	if int64(len(data)) > maxBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("encoded camera frame exceeds output limit")
	}
	if err := cmd.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return nil, fmt.Errorf("ffmpeg webcam capture: %w", err)
		}
		return nil, fmt.Errorf("ffmpeg webcam capture: %s", message)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ffmpeg returned an empty camera frame")
	}
	return data, nil
}

type limitedStringWriter struct {
	Builder *strings.Builder
	Limit   int
}

func (w *limitedStringWriter) Write(p []byte) (int, error) {
	original := len(p)
	remaining := w.Limit - w.Builder.Len()
	if remaining <= 0 {
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = w.Builder.Write(p)
	return original, nil
}
