//go:build darwin || freebsd || linux

package kry

import "github.com/ebitengine/purego"

func ffiOpenLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
}

func ffiLookupSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}

func ffiCloseLibrary(handle uintptr) error { return purego.Dlclose(handle) }
