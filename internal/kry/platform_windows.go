//go:build windows

package kry

import (
	"fmt"
	"syscall"
	"unsafe"
)

type rtlOSVersionInfo struct {
	Size             uint32
	Major            uint32
	Minor            uint32
	Build            uint32
	PlatformID       uint32
	ServicePack      [128]uint16
	ServicePackMajor uint16
	ServicePackMinor uint16
	SuiteMask        uint16
	ProductType      byte
	Reserved         byte
}

func platformOSVersion() (string, error) {
	ntdll := syscall.NewLazyDLL("ntdll.dll")
	rtlGetVersion := ntdll.NewProc("RtlGetVersion")
	info := rtlOSVersionInfo{Size: uint32(unsafe.Sizeof(rtlOSVersionInfo{}))}
	status, _, _ := rtlGetVersion.Call(uintptr(unsafe.Pointer(&info)))
	if status != 0 {
		return "", fmt.Errorf("RtlGetVersion failed with status 0x%x", status)
	}
	return fmt.Sprintf("Windows %d.%d (build %d)", info.Major, info.Minor, info.Build), nil
}
