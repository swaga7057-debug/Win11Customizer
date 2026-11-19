//go:build !windows

package main

import "errors"

func launchSettings(string) error {
	return errors.New("параметры доступны только в Windows")
}

func shellOpen(string) error {
	return errors.New("запуск файлов доступен только в Windows")
}

func launchScreenSaverSettings() error {
	return errors.New("настройка заставки доступна только в Windows")
}
