package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"
)

type auditLogger struct {
	mu   sync.Mutex
	path string
}

type auditEntry struct {
	Time    time.Time      `json:"time"`
	Event   string         `json:"event"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

func newAuditLogger(path string) *auditLogger {
	return &auditLogger{path: path}
}

func (logger *auditLogger) write(event, status string, details map[string]any) {
	if logger == nil {
		return
	}
	entry, err := json.Marshal(auditEntry{Time: time.Now(), Event: event, Status: status, Details: details})
	if err != nil {
		return
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if info, statErr := os.Stat(logger.path); statErr == nil && info.Size() > 5<<20 {
		_ = os.Remove(logger.path + ".1")
		_ = os.Rename(logger.path, logger.path+".1")
	}
	file, err := os.OpenFile(logger.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	_, _ = file.Write(append(entry, '\n'))
	_ = file.Close()
}

func (a *app) handleLogsExport(w http.ResponseWriter, _ *http.Request) {
	if a.audit == nil {
		writeError(w, 404, "Журнал пока не создан")
		return
	}
	a.audit.mu.Lock()
	defer a.audit.mu.Unlock()
	data, err := os.ReadFile(a.audit.path)
	if err != nil && !os.IsNotExist(err) {
		writeError(w, 500, "Не удалось прочитать журнал")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="win11-customizer-audit.jsonl"`)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	_, _ = w.Write(data)
}
