//go:build !windows

package main

import "errors"

func setWallpaperNative(string) error {
	return errors.New("обои можно установить только в Windows")
}
