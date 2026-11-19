//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxThemePackageFiles = 512
	maxThemeExpandedSize = 100 << 20
)

func validateThemePackage(path string) error {
	tempDir, err := os.MkdirTemp("", "w11c-theme-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	if err := expandThemePackage(path, tempDir); err != nil {
		return err
	}
	count, total, themeFiles := 0, int64(0), 0
	err = filepath.WalkDir(tempDir, func(itemPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		count++
		if count > maxThemePackageFiles {
			return errors.New("слишком много файлов в пакете темы")
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("пакет темы содержит файл неподдерживаемого типа %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > maxThemeExpandedSize {
			return errors.New("распакованный пакет темы превышает 100 МБ")
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".theme" {
			themeFiles++
			if info.Size() > 2<<20 {
				return errors.New("файл .theme в пакете превышает 2 МБ")
			}
			data, err := os.ReadFile(itemPath)
			if err != nil {
				return err
			}
			if err := validateThemeFile(".theme", data); err != nil {
				return err
			}
			return nil
		}
		if err := validateThemeAsset(itemPath, extension); err != nil {
			return fmt.Errorf("небезопасный файл %s: %w", entry.Name(), err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if themeFiles == 0 {
		return errors.New("в пакете не найден файл .theme")
	}
	return nil
}

func expandThemePackage(path, tempDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var output bytes.Buffer
	cmd := exec.CommandContext(ctx, "expand.exe", "-F:*", path, tempDir)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Windows не смогла запустить проверку пакета темы: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if ctx.Err() == context.DeadlineExceeded {
				return errors.New("проверка пакета темы превысила 20 секунд")
			}
			if err != nil {
				return fmt.Errorf("Windows не смогла проверить пакет темы: %s", cleanOutput(output.Bytes()))
			}
			return nil
		case <-ticker.C:
			count, total, err := themeDirectoryUsage(tempDir)
			if err != nil {
				cancel()
				<-done
				return fmt.Errorf("не удалось проверить распакованные файлы темы: %w", err)
			}
			if count > maxThemePackageFiles || total > maxThemeExpandedSize {
				cancel()
				<-done
				return errors.New("распаковка пакета темы остановлена: превышен лимит 512 файлов или 100 МБ")
			}
		}
	}
}

func themeDirectoryUsage(root string) (int, int64, error) {
	count := 0
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		total += info.Size()
		return nil
	})
	return count, total, err
}

func validateThemeAsset(path, extension string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) >= 2 && bytes.Equal(data[:2], []byte("MZ")) {
		return errors.New("обнаружен исполняемый PE-файл")
	}
	hasPrefix := func(prefix []byte) bool { return len(data) >= len(prefix) && bytes.Equal(data[:len(prefix)], prefix) }
	switch extension {
	case ".jpg", ".jpeg":
		if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
			return nil
		}
	case ".png":
		if hasPrefix([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
			return nil
		}
	case ".bmp":
		if hasPrefix([]byte("BM")) {
			return nil
		}
	case ".gif":
		if hasPrefix([]byte("GIF87a")) || hasPrefix([]byte("GIF89a")) {
			return nil
		}
	case ".tif", ".tiff":
		if hasPrefix([]byte{'I', 'I', 42, 0}) || hasPrefix([]byte{'M', 'M', 0, 42}) {
			return nil
		}
	case ".wav":
		if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
			return nil
		}
	case ".cur":
		if hasPrefix([]byte{0, 0, 2, 0}) {
			return nil
		}
	case ".ani":
		if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "ACON" {
			return nil
		}
	default:
		return fmt.Errorf("формат %q не входит в безопасный список", extension)
	}
	return fmt.Errorf("содержимое не соответствует формату %s", extension)
}
