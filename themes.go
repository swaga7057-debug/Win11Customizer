package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const backupThemeMarker = "@theme"

func (a *app) handleTheme(w http.ResponseWriter, r *http.Request) {
	const maxThemeSize = 50 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxThemeSize+(1<<20))
	file, header, err := r.FormFile("theme")
	if err != nil {
		writeError(w, 400, "Выберите файл .theme, .themepack или .deskthemepack")
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	data, err := io.ReadAll(io.LimitReader(file, maxThemeSize+1))
	if err != nil || len(data) == 0 || len(data) > maxThemeSize {
		writeError(w, 400, "Файл темы пуст, повреждён или превышает 50 МБ")
		return
	}
	extension := strings.ToLower(filepath.Ext(header.Filename))
	if err := validateThemeFile(extension, data); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := a.installTheme(extension, data); err != nil {
		a.audit.write("theme.install", "error", map[string]any{"extension": extension, "error": err.Error()})
		writeError(w, 500, "Не удалось установить тему: "+err.Error())
		return
	}
	a.audit.write("theme.install", "ok", map[string]any{"extension": extension, "bytes": len(data)})
	writeJSON(w, map[string]any{"ok": true})
}

func validateThemeFile(extension string, data []byte) error {
	switch extension {
	case ".theme":
		if len(data) > 2<<20 {
			return errors.New("Файл .theme не должен превышать 2 МБ")
		}
		asciiTheme := bytes.Contains(bytes.ToLower(data), []byte("[theme]"))
		utf16Theme := bytes.Contains(data, []byte{'[', 0, 'T', 0, 'h', 0, 'e', 0, 'm', 0, 'e', 0, ']', 0})
		if !asciiTheme && !utf16Theme {
			return errors.New("Файл не похож на тему Windows")
		}
		if err := validateThemeText(data); err != nil {
			return err
		}
	case ".themepack", ".deskthemepack":
		if len(data) < 4 || string(data[:4]) != "MSCF" {
			return errors.New("Пакет темы не является корректным CAB-файлом Windows")
		}
	default:
		return errors.New("Поддерживаются только .theme, .themepack и .deskthemepack")
	}
	return nil
}

func (a *app) installTheme(extension string, data []byte) error {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Win11Customizer", "themes")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "theme-"+strconv.FormatInt(time.Now().UnixNano(), 10)+extension)
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	if extension != ".theme" {
		if err := validateThemePackage(path); err != nil {
			_ = os.Remove(path)
			return err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.backup.ThemeCaptured {
		_, previous, exists, err := regGet(`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes`, "CurrentTheme")
		if err != nil {
			return err
		}
		a.backup.ThemeCaptured = true
		a.backup.Order = append(a.backup.Order, backupThemeMarker)
		if exists {
			a.backup.PreviousTheme = previous
		}
	}
	if err := a.saveBackup(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := shellOpen(path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("Windows не приняла файл темы: %w", err)
	}
	return nil
}

func restoreWindowsTheme(path string) error {
	if err := shellOpen(path); err != nil {
		return fmt.Errorf("не удалось запустить прежнюю тему: %w", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_, current, exists, err := regGet(`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes`, "CurrentTheme")
		if err == nil && exists && strings.EqualFold(filepath.Clean(current), filepath.Clean(path)) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("Windows не подтвердила восстановление прежней темы")
}

func validateThemeText(data []byte) error {
	text := strings.ToLower(decodeThemeText(data))
	for _, forbidden := range []string{"http://", "https://", "file://", `\\`} {
		if strings.Contains(text, forbidden) {
			return errors.New("Тема содержит внешнюю или сетевую ссылку")
		}
	}
	for _, extension := range []string{".exe", ".dll", ".scr", ".com", ".bat", ".cmd", ".ps1", ".vbs", ".js", ".msi", ".lnk"} {
		if strings.Contains(text, extension) {
			return fmt.Errorf("Тема ссылается на потенциально исполняемый файл %s", extension)
		}
	}
	return nil
}

func decodeThemeText(data []byte) string {
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		data = data[2:]
	}
	zeroes := 0
	for i := 1; i < len(data) && i < 200; i += 2 {
		if data[i] == 0 {
			zeroes++
		}
	}
	if zeroes < 4 {
		return string(data)
	}
	chars := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		char := binary.LittleEndian.Uint16(data[i:])
		if char == 0 {
			continue
		}
		chars = append(chars, char)
	}
	return string(utf16.Decode(chars))
}
