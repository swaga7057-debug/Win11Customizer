//go:build windows

package main

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	regSetValueExW  = syscall.NewLazyDLL("advapi32.dll").NewProc("RegSetValueExW")
	regDeleteValueW = syscall.NewLazyDLL("advapi32.dll").NewProc("RegDeleteValueW")
)

func registryPath(path string) (syscall.Handle, string, error) {
	upper := strings.ToUpper(path)
	for _, root := range []struct {
		prefix string
		handle syscall.Handle
	}{
		{`HKCU\`, syscall.Handle(syscall.HKEY_CURRENT_USER)},
		{`HKEY_CURRENT_USER\`, syscall.Handle(syscall.HKEY_CURRENT_USER)},
		{`HKLM\`, syscall.Handle(syscall.HKEY_LOCAL_MACHINE)},
		{`HKEY_LOCAL_MACHINE\`, syscall.Handle(syscall.HKEY_LOCAL_MACHINE)},
	} {
		if strings.HasPrefix(upper, root.prefix) {
			return root.handle, path[len(root.prefix):], nil
		}
	}
	return 0, "", fmt.Errorf("неподдерживаемый корень реестра: %s", path)
}

func openRegistryKey(path string, access uint32) (syscall.Handle, bool, error) {
	root, subkey, err := registryPath(path)
	if err != nil {
		return 0, false, err
	}
	name, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return 0, false, err
	}
	var key syscall.Handle
	err = syscall.RegOpenKeyEx(root, name, 0, access, &key)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) || errors.Is(err, syscall.ERROR_PATH_NOT_FOUND) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return key, true, nil
}

func nativeReadRegistryValue(path, valueName string) (uint32, []byte, bool, bool, error) {
	key, keyExists, err := openRegistryKey(path, syscall.KEY_QUERY_VALUE)
	if err != nil || !keyExists {
		return 0, nil, false, keyExists, err
	}
	defer syscall.RegCloseKey(key)
	name, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return 0, nil, false, true, err
	}
	var valueType, size uint32
	err = syscall.RegQueryValueEx(key, name, nil, &valueType, nil, &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return 0, nil, false, true, nil
	}
	if err != nil {
		return 0, nil, false, true, err
	}
	data := make([]byte, size)
	if size > 0 {
		err = syscall.RegQueryValueEx(key, name, nil, &valueType, &data[0], &size)
		if err != nil {
			return 0, nil, false, true, err
		}
		data = data[:size]
	}
	return valueType, data, true, true, nil
}

func nativeWriteRegistryValue(path, valueName string, valueType uint32, data []byte) error {
	key, exists, err := openRegistryKey(path, syscall.KEY_SET_VALUE)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("ключ реестра для восстановления не найден")
	}
	defer syscall.RegCloseKey(key)
	name, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}
	var dataPointer uintptr
	if len(data) > 0 {
		dataPointer = uintptr(unsafe.Pointer(&data[0]))
	}
	result, _, _ := regSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(name)), 0, uintptr(valueType), dataPointer, uintptr(len(data)))
	if result != 0 {
		return syscall.Errno(result)
	}
	return nil
}

func nativeDeleteRegistryValue(path, valueName string) error {
	key, exists, err := openRegistryKey(path, syscall.KEY_SET_VALUE)
	if err != nil || !exists {
		return err
	}
	defer syscall.RegCloseKey(key)
	name, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return err
	}
	result, _, _ := regDeleteValueW.Call(uintptr(key), uintptr(unsafe.Pointer(name)))
	if result == 0 || syscall.Errno(result) == syscall.ERROR_FILE_NOT_FOUND {
		return nil
	}
	return syscall.Errno(result)
}
