//go:build !windows

package main

import "errors"

func nativeReadRegistryValue(string, string) (uint32, []byte, bool, bool, error) {
	return 0, nil, false, false, errors.New("реестр доступен только в Windows")
}

func nativeWriteRegistryValue(string, string, uint32, []byte) error {
	return errors.New("реестр доступен только в Windows")
}

func nativeDeleteRegistryValue(string, string) error {
	return errors.New("реестр доступен только в Windows")
}
