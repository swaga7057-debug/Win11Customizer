//go:build !windows

package main

import "errors"

func validateThemePackage(string) error {
	return errors.New("пакеты тем можно проверять только в Windows")
}
