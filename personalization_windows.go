//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var systemParametersInfoW = syscall.NewLazyDLL("user32.dll").NewProc("SystemParametersInfoW")

func setWallpaperNative(path string) error {
	value, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	const (
		spiSetDesktopWallpaper = 0x0014
		spifUpdateIniFile      = 0x0001
		spifSendChange         = 0x0002
	)
	result, _, callErr := systemParametersInfoW.Call(
		spiSetDesktopWallpaper,
		0,
		uintptr(unsafe.Pointer(value)),
		spifUpdateIniFile|spifSendChange,
	)
	if result == 0 {
		return fmt.Errorf("SystemParametersInfoW: %w", callErr)
	}
	return nil
}
