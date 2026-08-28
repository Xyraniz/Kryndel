//go:build windows

package kry

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

func runWindowsCommand(program string, args ...string) (string, error) {
	cmd := exec.Command(program, args...)
	out, err := cmd.CombinedOutput()
	if len(out) > 1<<20 {
		return "", fmt.Errorf("Windows command output exceeds limit")
	}
	if err != nil {
		return "", fmt.Errorf("%s: %s", program, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func windowsRegistryGetNative(path, key string) (string, error) {
	return runWindowsCommand("reg.exe", "query", path, "/v", key)
}
func windowsServiceQueryNative(name string) (string, error) {
	return runWindowsCommand("sc.exe", "query", name)
}
func windowsEventLogWriteNative(source, message string) error {
	_, err := runWindowsCommand("eventcreate.exe", "/t", "INFORMATION", "/id", "1", "/l", "APPLICATION", "/so", source, "/d", message)
	return err
}
func windowsRawInputReadNative() ([]byte, error) {
	return nil, fmt.Errorf("Raw Input requires a GUI window message pump; use the Windows host integration")
}

func windowsDeviceIoControlNative(device string, code int64, input []byte) ([]byte, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	createFile := kernel32.NewProc("CreateFileW")
	deviceControl := kernel32.NewProc("DeviceIoControl")
	closeHandle := kernel32.NewProc("CloseHandle")
	name, err := syscall.UTF16PtrFromString(device)
	if err != nil {
		return nil, err
	}
	h, _, callErr := createFile.Call(uintptr(unsafe.Pointer(name)), 0xc0000000, 0x3, 0, 3, 0, 0)
	if h == uintptr(syscall.InvalidHandle) {
		return nil, callErr
	}
	defer closeHandle.Call(h)
	out := make([]byte, 1<<20)
	var returned uint32
	var inPtr uintptr
	if len(input) > 0 {
		inPtr = uintptr(unsafe.Pointer(&input[0]))
	}
	var outPtr uintptr
	if len(out) > 0 {
		outPtr = uintptr(unsafe.Pointer(&out[0]))
	}
	ok, _, callErr := deviceControl.Call(h, uintptr(code), inPtr, uintptr(len(input)), outPtr, uintptr(len(out)), uintptr(unsafe.Pointer(&returned)), 0)
	if ok == 0 {
		return nil, callErr
	}
	return out[:returned], nil
}
