package kry

import (
	"fmt"
	"runtime"
)

func windowsRegistryGet(path, key string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("Windows registry is available only on Windows")
	}
	return windowsRegistryGetNative(path, key)
}
func windowsServiceQuery(name string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("Windows services are available only on Windows")
	}
	return windowsServiceQueryNative(name)
}
func windowsEventLogWrite(source, message string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows Event Log is available only on Windows")
	}
	return windowsEventLogWriteNative(source, message)
}
func windowsRawInputRead() ([]byte, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("Raw Input is available only on Windows")
	}
	return windowsRawInputReadNative()
}
func windowsDeviceIoControl(device string, code int64, input []byte) ([]byte, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("DeviceIoControl is available only on Windows")
	}
	return windowsDeviceIoControlNative(device, code, input)
}
