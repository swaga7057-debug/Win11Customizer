//go:build windows

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	regSetValueExW  = syscall.NewLazyDLL("advapi32.dll").NewProc("RegSetValueExW")
	regDeleteValueW = syscall.NewLazyDLL("advapi32.dll").NewProc("RegDeleteValueW")
	regCreateKeyExW = syscall.NewLazyDLL("advapi32.dll").NewProc("RegCreateKeyExW")
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

func createRegistryKey(path string) (syscall.Handle, error) {
	root, subkey, err := registryPath(path)
	if err != nil {
		return 0, err
	}
	name, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	var disposition uint32
	result, _, _ := regCreateKeyExW.Call(
		uintptr(root), uintptr(unsafe.Pointer(name)), 0, 0, 0,
		uintptr(syscall.KEY_SET_VALUE|syscall.KEY_QUERY_VALUE), 0,
		uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&disposition)),
	)
	if result != 0 {
		return 0, syscall.Errno(result)
	}
	return key, nil
}

func nativeRegistryText(path, valueName string) (string, string, bool, error) {
	valueType, data, exists, _, err := nativeReadRegistryValue(path, valueName)
	if err != nil || !exists {
		return "", "", exists, err
	}
	typeName := registryTypeName(valueType)
	switch valueType {
	case syscall.REG_DWORD:
		if len(data) < 4 {
			return "", "", false, errors.New("повреждённое значение REG_DWORD")
		}
		return typeName, strconv.FormatUint(uint64(binary.LittleEndian.Uint32(data)), 10), true, nil
	case syscall.REG_QWORD:
		if len(data) < 8 {
			return "", "", false, errors.New("повреждённое значение REG_QWORD")
		}
		return typeName, strconv.FormatUint(binary.LittleEndian.Uint64(data), 10), true, nil
	case syscall.REG_SZ, syscall.REG_EXPAND_SZ:
		return typeName, decodeRegistryUTF16(data), true, nil
	default:
		return typeName, fmt.Sprintf("%x", data), true, nil
	}
}

func nativeSetRegistryText(path, valueName, typeName, value string) error {
	valueType, data, err := encodeRegistryValue(typeName, value)
	if err != nil {
		return err
	}
	key, err := createRegistryKey(path)
	if err != nil {
		return err
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

func encodeRegistryValue(typeName, value string) (uint32, []byte, error) {
	switch strings.ToUpper(typeName) {
	case "REG_DWORD":
		n, err := parseRegistryNumber(value)
		if err != nil {
			return 0, nil, fmt.Errorf("неверное REG_DWORD: %w", err)
		}
		if n > uint64(^uint32(0)) {
			return 0, nil, errors.New("REG_DWORD выходит за диапазон uint32")
		}
		data := make([]byte, 4)
		binary.LittleEndian.PutUint32(data, uint32(n))
		return syscall.REG_DWORD, data, nil
	case "REG_QWORD":
		n, err := parseRegistryNumber(value)
		if err != nil {
			return 0, nil, fmt.Errorf("неверное REG_QWORD: %w", err)
		}
		data := make([]byte, 8)
		binary.LittleEndian.PutUint64(data, n)
		return syscall.REG_QWORD, data, nil
	case "REG_SZ", "REG_EXPAND_SZ":
		chars := utf16.Encode([]rune(value + "\x00"))
		data := make([]byte, len(chars)*2)
		for i, char := range chars {
			binary.LittleEndian.PutUint16(data[i*2:], char)
		}
		if strings.EqualFold(typeName, "REG_EXPAND_SZ") {
			return syscall.REG_EXPAND_SZ, data, nil
		}
		return syscall.REG_SZ, data, nil
	default:
		return 0, nil, fmt.Errorf("неподдерживаемый тип реестра: %s", typeName)
	}
}

func decodeRegistryUTF16(data []byte) string {
	chars := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		char := binary.LittleEndian.Uint16(data[i:])
		if char == 0 {
			break
		}
		chars = append(chars, char)
	}
	return string(utf16.Decode(chars))
}

func registryTypeName(valueType uint32) string {
	switch valueType {
	case syscall.REG_SZ:
		return "REG_SZ"
	case syscall.REG_EXPAND_SZ:
		return "REG_EXPAND_SZ"
	case syscall.REG_DWORD:
		return "REG_DWORD"
	case syscall.REG_QWORD:
		return "REG_QWORD"
	case syscall.REG_BINARY:
		return "REG_BINARY"
	default:
		return "REG_NONE"
	}
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
