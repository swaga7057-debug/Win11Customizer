package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const profileSchema = 1

type profileFile struct {
	Schema         int             `json:"schema"`
	AppVersion     string          `json:"appVersion"`
	Name           string          `json:"name"`
	Created        time.Time       `json:"created"`
	WindowsBuild   int             `json:"windowsBuild"`
	InterfaceTheme string          `json:"interfaceTheme,omitempty"`
	Tweaks         map[string]bool `json:"tweaks"`
}

type profilePreview struct {
	Changes  int             `json:"changes"`
	Warnings []string        `json:"warnings"`
	Restarts []string        `json:"restarts"`
	Items    []profileChange `json:"items"`
}

type profileChange struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Enabled      bool   `json:"enabled"`
	Restart      string `json:"restart,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
}

func (a *app) handleProfileExport(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	theme := r.URL.Query().Get("theme")
	if !validInterfaceTheme(theme) {
		theme = "ocean"
	}
	profile := profileFile{
		Schema: profileSchema, AppVersion: appVersion, Name: "Мой профиль",
		Created: time.Now(), WindowsBuild: a.windows.Build, InterfaceTheme: theme,
		Tweaks: make(map[string]bool, len(tweaks)),
	}
	for _, item := range tweaks {
		enabled, err := tweakEnabled(item)
		if err != nil {
			writeError(w, 500, "Не удалось прочитать настройку "+item.Title+": "+err.Error())
			return
		}
		profile.Tweaks[item.ID] = enabled
	}
	w.Header().Set("Content-Disposition", `attachment; filename="win11-customizer-profile.json"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(profile); err != nil {
		a.audit.write("profile.export", "error", map[string]any{"error": err.Error()})
		return
	}
	a.audit.write("profile.export", "ok", map[string]any{"tweaks": len(profile.Tweaks)})
}

func (a *app) handleProfilePreview(w http.ResponseWriter, r *http.Request) {
	profile, err := decodeProfile(w, r)
	if err != nil {
		a.audit.write("profile.preview", "error", map[string]any{"error": err.Error()})
		writeError(w, 400, err.Error())
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	preview, err := a.previewProfile(profile)
	if err != nil {
		a.audit.write("profile.preview", "error", map[string]any{"error": err.Error()})
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, preview)
}

func (a *app) handleProfileApply(w http.ResponseWriter, r *http.Request) {
	profile, err := decodeProfile(w, r)
	if err != nil {
		a.audit.write("profile.apply", "error", map[string]any{"error": err.Error()})
		writeError(w, 400, err.Error())
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	changed, err := a.applyProfile(profile)
	if err != nil {
		a.audit.write("profile.apply", "error", map[string]any{"error": err.Error()})
		writeError(w, 500, err.Error())
		return
	}
	a.audit.write("profile.apply", "ok", map[string]any{"changed": changed, "sourceBuild": profile.WindowsBuild})
	writeJSON(w, map[string]any{"ok": true, "changed": changed, "interfaceTheme": profile.InterfaceTheme})
}

func decodeProfile(w http.ResponseWriter, r *http.Request) (profileFile, error) {
	var profile profileFile
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return profile, errors.New("Некорректный файл профиля")
	}
	if err := validateProfile(profile); err != nil {
		return profile, err
	}
	return profile, nil
}

func validateProfile(profile profileFile) error {
	if profile.Schema != profileSchema {
		return fmt.Errorf("Неподдерживаемая версия профиля: %d", profile.Schema)
	}
	if len(profile.Tweaks) == 0 || len(profile.Tweaks) > len(tweaks) {
		return errors.New("Профиль не содержит допустимого набора настроек")
	}
	if len([]rune(profile.Name)) > 100 {
		return errors.New("Название профиля слишком длинное")
	}
	for id := range profile.Tweaks {
		if _, ok := findTweak(id); !ok {
			return fmt.Errorf("Неизвестная настройка в профиле: %s", id)
		}
	}
	if profile.InterfaceTheme != "" && !validInterfaceTheme(profile.InterfaceTheme) {
		return errors.New("Неизвестная тема интерфейса в профиле")
	}
	return nil
}

func validInterfaceTheme(theme string) bool {
	switch theme {
	case "ocean", "aurora", "sunset", "midnight", "liquid":
		return true
	default:
		return false
	}
}

func (a *app) previewProfile(profile profileFile) (profilePreview, error) {
	preview := profilePreview{Warnings: []string{}, Restarts: []string{}, Items: []profileChange{}}
	restarts := map[string]bool{}
	ids := sortedProfileIDs(profile)
	for _, id := range ids {
		desired := profile.Tweaks[id]
		item, _ := findTweak(id)
		current, err := tweakEnabled(*item)
		if err != nil {
			return preview, err
		}
		if current == desired {
			continue
		}
		if available, reason := tweakCompatibility(*item, a.windows.Build); !available {
			return preview, fmt.Errorf("%s: %s", item.Title, reason)
		}
		if item.Admin && !isAdmin() {
			return preview, fmt.Errorf("%s требует запуска от имени администратора", item.Title)
		}
		preview.Changes++
		preview.Items = append(preview.Items, profileChange{ID: item.ID, Title: item.Title, Enabled: desired, Restart: item.Restart, Experimental: item.Experimental})
		if item.Experimental {
			preview.Warnings = append(preview.Warnings, item.Title+" зависит от сборки Windows")
		}
		if item.Restart != "" {
			restarts[item.Restart] = true
		}
	}
	for restart := range restarts {
		preview.Restarts = append(preview.Restarts, restart)
	}
	sort.Strings(preview.Restarts)
	return preview, nil
}

func (a *app) applyProfile(profile profileFile) (int, error) {
	preview, err := a.previewProfile(profile)
	if err != nil || preview.Changes == 0 {
		return preview.Changes, err
	}
	plans := make([]registryPlan, 0)
	seen := map[string]string{}
	for _, id := range sortedProfileIDs(profile) {
		desired := profile.Tweaks[id]
		item, _ := findTweak(id)
		current, err := tweakEnabled(*item)
		if err != nil {
			return 0, err
		}
		if current == desired {
			continue
		}
		for _, op := range item.Ops {
			key := strings.ToLower(op.Path + "|" + op.Name)
			signature := operationSignature(op, desired)
			if previous, ok := seen[key]; ok {
				if previous != signature {
					return 0, fmt.Errorf("Профиль содержит конфликтующие настройки для %s", op.Name)
				}
				continue
			}
			seen[key] = signature
			plans = append(plans, registryPlan{op: op, enabled: desired})
		}
	}
	if _, err := a.executeRegistryTransaction(plans); err != nil {
		return 0, err
	}
	return preview.Changes, nil
}

func operationSignature(op operation, enabled bool) string {
	if enabled {
		return "set|" + op.Type + "|" + op.On
	}
	if op.Off == nil {
		return "delete"
	}
	return "set|" + op.Type + "|" + *op.Off
}

func sortedProfileIDs(profile profileFile) []string {
	ids := make([]string, 0, len(profile.Tweaks))
	for id := range profile.Tweaks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
