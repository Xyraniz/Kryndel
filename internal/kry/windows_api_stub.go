//go:build !windows

package kry

import "fmt"

func windowsRegistryGetNative(path, key string) (string, error) {
	return "", fmt.Errorf("Windows registry is not available on this target")
}
func windowsServiceQueryNative(name string) (string, error) {
	return "", fmt.Errorf("Windows services are not available on this target")
}
func windowsEventLogWriteNative(source, message string) error {
	return fmt.Errorf("Windows Event Log is not available on this target")
}
func windowsRawInputReadNative() ([]byte, error) {
	return nil, fmt.Errorf("Windows Raw Input is not available on this target")
}
func windowsDeviceIoControlNative(device string, code int64, input []byte) ([]byte, error) {
	return nil, fmt.Errorf("DeviceIoControl is not available on this target")
}
