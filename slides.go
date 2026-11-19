package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type slideShow struct {
	files    []string
	index    int
	interval time.Duration
	timer    *time.Timer
	failure  string
}

func slideFiles(folder string) ([]string, error) {
	folder = filepath.Clean(folder)
	if !filepath.IsAbs(folder) || strings.HasPrefix(filepath.ToSlash(folder), "//") {
		return nil, errors.New("Укажите полный путь к локальной папке")
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, errors.New("Не удалось открыть папку")
	}
	var files []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".jpg", ".jpeg", ".png", ".bmp":
			files = append(files, filepath.Join(folder, entry.Name()))
		}
	}
	if len(files) < 2 {
		return nil, errors.New("Выберите папку минимум с двумя изображениями JPEG, PNG или BMP")
	}
	return files, nil
}

func (a *app) stopSlides() {
	if a.slides != nil && a.slides.timer != nil {
		a.slides.timer.Stop()
	}
	a.slides = nil
}

func (a *app) advanceSlide(s *slideShow) error {
	next := (s.index + 1) % len(s.files)
	if err := a.applyWallpaperPath(s.files[next]); err != nil {
		return err
	}
	s.index = next
	return nil
}

func (a *app) scheduleSlide(s *slideShow) {
	s.timer = time.AfterFunc(s.interval, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.slides != s {
			return
		}
		if err := a.advanceSlide(s); err != nil {
			s.failure = err.Error()
			a.audit.write("slides.advance", "error", map[string]any{"error": s.failure})
			return
		}
		a.scheduleSlide(s)
	})
}

func (a *app) handleSlides(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Method == http.MethodPost {
		var req struct {
			Action, Folder string
			Minutes        int
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req) != nil {
			writeError(w, 400, "Некорректный запрос")
			return
		}
		switch req.Action {
		case "stop":
			a.stopSlides()
		case "start":
			if req.Minutes < 1 || req.Minutes > 1440 {
				writeError(w, 400, "Интервал от 1 до 1440 минут")
				return
			}
			files, err := slideFiles(req.Folder)
			if err != nil {
				writeError(w, 400, err.Error())
				return
			}
			s := &slideShow{files: files, index: -1, interval: time.Duration(req.Minutes) * time.Minute}
			if err := a.advanceSlide(s); err != nil {
				writeError(w, 500, err.Error())
				return
			}
			a.stopSlides()
			a.slides = s
			a.scheduleSlide(s)
		default:
			writeError(w, 400, "Неизвестное действие")
			return
		}
	}
	status := "Слайд-шоу остановлено"
	if s := a.slides; s != nil {
		status = "Сейчас: " + filepath.Base(s.files[s.index])
		if s.failure != "" {
			status = "Смена остановлена: " + s.failure
		}
	}
	writeJSON(w, map[string]string{"status": status})
}
