package main

import (
	"fmt"
	"strconv"
)

type windowsVersion struct {
	Product        string `json:"product"`
	DisplayVersion string `json:"displayVersion"`
	Build          int    `json:"build"`
	Supported      bool   `json:"supported"`
}

func detectWindowsVersion() windowsVersion {
	const path = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	version := windowsVersion{Product: "Windows 11"}
	if _, product, exists, err := regGet(path, "ProductName"); err == nil && exists && product != "" {
		version.Product = product
	}
	if _, display, exists, err := regGet(path, "DisplayVersion"); err == nil && exists {
		version.DisplayVersion = display
	}
	if _, buildText, exists, err := regGet(path, "CurrentBuildNumber"); err == nil && exists {
		version.Build, _ = strconv.Atoi(buildText)
	}
	// Windows 11 starts at build 22000. Unknown builds stay unsupported so
	// version-bound tweaks cannot be applied blindly.
	version.Supported = version.Build >= 22000
	return version
}

func tweakCompatibility(t tweak, build int) (bool, string) {
	if build > 0 && build < 22000 {
		return false, "Эта настройка предназначена только для Windows 11"
	}
	if build == 0 && (t.MinBuild > 0 || t.MaxBuild > 0) {
		return false, "Не удалось определить сборку Windows"
	}
	if t.MinBuild > 0 && build < t.MinBuild {
		return false, fmt.Sprintf("Требуется Windows 11 build %d или новее", t.MinBuild)
	}
	if t.MaxBuild > 0 && build > t.MaxBuild {
		return false, fmt.Sprintf("Поддерживается только до Windows build %d", t.MaxBuild)
	}
	return true, ""
}
