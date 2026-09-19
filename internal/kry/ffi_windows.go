//go:build windows

package kry

import (
	"golang.org/x/sys/windows"
)

func ffiOpenLibrary(path string) (uintptr, error) {
	handle, err := windows.LoadLibrary(path)
	return uintptr(handle), err
}

func ffiLookupSymbol(handle uintptr, name string) (uintptr, error) {
	return windows.GetProcAddress(windows.Handle(handle), name)
}

func ffiCloseLibrary(handle uintptr) error { return windows.FreeLibrary(windows.Handle(handle)) }
