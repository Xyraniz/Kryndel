//go:build windows

package kry

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	wmQuit       = 0x0012
	pmNoRemove   = 0x0000
)

type keyboardHookStruct struct {
	VKCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type point struct {
	X int32
	Y int32
}

type mouseHookStruct struct {
	Point     point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type windowsMessage struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   point
	Private uint32
}

type inputHookLoopResult struct {
	err error
}

var rtlMoveMemory = syscall.NewLazyDLL("kernel32.dll").NewProc("RtlMoveMemory")

func windowsInputReadNative(kind string, timeoutMS int64) (string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "keyboard" && kind != "mouse" {
		return "", fmt.Errorf("input hook kind must be keyboard or mouse")
	}
	if timeoutMS < 1 || timeoutMS > 60_000 {
		return "", fmt.Errorf("input hook timeout must be between 1 and 60000 milliseconds")
	}
	events := make(chan string, 1)
	ready := make(chan inputHookLoopResult, 1)
	finished := make(chan inputHookLoopResult, 1)
	go runInputHookLoop(kind, time.Duration(timeoutMS)*time.Millisecond, events, ready, finished)
	if result := <-ready; result.err != nil {
		return "", result.err
	}
	result := <-finished
	if result.err != nil {
		return "", result.err
	}
	select {
	case event := <-events:
		return event, nil
	default:
		return "", fmt.Errorf("input hook timed out waiting for a %s event", kind)
	}
}

func runInputHookLoop(kind string, timeout time.Duration, events chan<- string, ready chan<- inputHookLoopResult, finished chan<- inputHookLoopResult) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	user32 := syscall.NewLazyDLL("user32.dll")
	setWindowsHookEx := user32.NewProc("SetWindowsHookExW")
	unhookWindowsHookEx := user32.NewProc("UnhookWindowsHookEx")
	callNextHookEx := user32.NewProc("CallNextHookEx")
	getMessage := user32.NewProc("GetMessageW")
	peekMessage := user32.NewProc("PeekMessageW")
	postThreadMessage := user32.NewProc("PostThreadMessageW")
	getCurrentThreadID := user32.NewProc("GetCurrentThreadId")

	threadID, _, _ := getCurrentThreadID.Call()
	hookID := uintptr(whKeyboardLL)
	if kind == "mouse" {
		hookID = whMouseLL
	}
	callback := syscall.NewCallback(func(code int, wParam, lParam uintptr) uintptr {
		if code >= 0 {
			if event, err := encodeInputEvent(kind, wParam, lParam); err == nil {
				select {
				case events <- event:
					_, _, _ = postThreadMessage.Call(threadID, wmQuit, 0, 0)
				default:
				}
			}
		}
		result, _, _ := callNextHookEx.Call(0, uintptr(code), wParam, lParam)
		return result
	})
	hook, _, callErr := setWindowsHookEx.Call(hookID, callback, 0, 0)
	if hook == 0 {
		ready <- inputHookLoopResult{err: fmt.Errorf("SetWindowsHookEx failed: %w", callErr)}
		return
	}
	defer unhookWindowsHookEx.Call(hook)
	var initial windowsMessage
	_, _, _ = peekMessage.Call(uintptr(unsafe.Pointer(&initial)), 0, 0, 0, pmNoRemove)
	ready <- inputHookLoopResult{}

	timerDone := make(chan struct{})
	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			_, _, _ = postThreadMessage.Call(threadID, wmQuit, 0, 0)
		case <-timerDone:
		}
	}()

	var message windowsMessage
	var loopErr error
	for {
		result, _, err := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if result == 0 {
			break
		}
		if result == ^uintptr(0) {
			loopErr = fmt.Errorf("GetMessage failed: %w", err)
			break
		}
	}
	close(timerDone)
	runtime.KeepAlive(callback)
	finished <- inputHookLoopResult{err: loopErr}
}

func encodeInputEvent(kind string, wParam, lParam uintptr) (string, error) {
	if lParam == 0 {
		return "", fmt.Errorf("Windows input hook returned a null event")
	}
	if kind == "keyboard" {
		var value keyboardHookStruct
		rtlMoveMemory.Call(uintptr(unsafe.Pointer(&value)), lParam, unsafe.Sizeof(value))
		return marshalInputEvent(map[string]any{
			"kind":       "keyboard",
			"message":    uint32(wParam),
			"vk_code":    value.VKCode,
			"scan_code":  value.ScanCode,
			"flags":      value.Flags,
			"time":       value.Time,
			"extra_info": value.ExtraInfo,
		})
	}
	var value mouseHookStruct
	rtlMoveMemory.Call(uintptr(unsafe.Pointer(&value)), lParam, unsafe.Sizeof(value))
	return marshalInputEvent(map[string]any{
		"kind":       "mouse",
		"message":    uint32(wParam),
		"x":          value.Point.X,
		"y":          value.Point.Y,
		"mouse_data": value.MouseData,
		"flags":      value.Flags,
		"time":       value.Time,
		"extra_info": value.ExtraInfo,
	})
}

func marshalInputEvent(value map[string]any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode Windows input event: %w", err)
	}
	return string(data), nil
}
