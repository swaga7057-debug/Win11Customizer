//go:build !windows

package main

import "errors"

func launchSettings(string) error {
	return errors.New("параметры доступны только в Windows")
}
