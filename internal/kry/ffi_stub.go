//go:build !darwin && !freebsd && !linux && !windows

package kry

import "fmt"

func ffiOpenLibrary(path string) (uintptr, error) {
	return 0, fmt.Errorf("FFI dynamic libraries are not supported on this target")
}
func ffiLookupSymbol(handle uintptr, name string) (uintptr, error) {
	return 0, fmt.Errorf("FFI dynamic libraries are not supported on this target")
}
func ffiCloseLibrary(handle uintptr) error {
	return fmt.Errorf("FFI dynamic libraries are not supported on this target")
}
