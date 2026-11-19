package main

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	appName    = "Win11 Customizer"
	appVersion = "1.6.0"
)

//go:embed web/*
var webFiles embed.FS

type operation struct {
	Path string  `json:"-"`
	Name string  `json:"-"`
	Type string  `json:"-"`
	On   string  `json:"-"`
	Off  *string `json:"-"`
}

type tweak struct {
	ID           string      `json:"id"`
	Category     string      `json:"category"`
	Title        string      `json:"title"`
	Description  string      `json:"description"`
	Risk         string      `json:"risk"`
	Admin        bool        `json:"admin"`
	Restart      string      `json:"restart,omitempty"`
	Recommended  bool        `json:"recommended"`
	Experimental bool        `json:"experimental,omitempty"`
	MinBuild     int         `json:"minBuild,omitempty"`
	MaxBuild     int         `json:"maxBuild,omitempty"`
	Ops          []operation `json:"-"`
}

type tweakState struct {
	tweak
	Enabled   bool   `json:"enabled"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type backupValue struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Existed     bool   `json:"existed"`
	KeyExisted  bool   `json:"keyExisted,omitempty"`
	Type        string `json:"type,omitempty"`
	Data        string `json:"data,omitempty"`
	RawType     uint32 `json:"rawType,omitempty"`
	RawData     string `json:"rawData,omitempty"`
	RawCaptured bool   `json:"rawCaptured,omitempty"`
}

type backupFile struct {
	Created       time.Time              `json:"created"`
	Values        map[string]backupValue `json:"values"`
	Order         []string               `json:"order,omitempty"`
	OrderKnown    bool                   `json:"orderKnown"`
	ThemeCaptured bool                   `json:"themeCaptured,omitempty"`
	PreviousTheme string                 `json:"previousTheme,omitempty"`
}

type app struct {
	slides     *slideShow
	mu         sync.Mutex
	backupPath string
	backup     backupFile
	windows    windowsVersion
	audit      *auditLogger
}

func str(v string) *string { return &v }

var tweaks = []tweak{
	{ID: "dark-mode", Category: "appearance", Title: "Тёмная тема", Description: "Тёмный режим для приложений и системного интерфейса.", Risk: "safe", Recommended: true, Restart: "explorer", Experimental: true, Ops: []operation{
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "AppsUseLightTheme", Type: "REG_DWORD", On: "0", Off: str("1")},
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "SystemUsesLightTheme", Type: "REG_DWORD", On: "0", Off: str("1")},
	}},
	{ID: "transparency", Category: "appearance", Title: "Эффекты прозрачности", Description: "Включает Acrylic/Mica там, где это поддерживает Windows.", Risk: "safe", Restart: "explorer", Experimental: true, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "EnableTransparency", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "accent-titlebars", Category: "appearance", Title: "Акцентный цвет заголовков", Description: "Показывает выбранный цвет Windows на заголовках и границах окон.", Risk: "safe", Experimental: true, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\DWM`, Name: "ColorPrevalence", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "taskbar-left", Category: "taskbar", Title: "Пуск слева", Description: "Выравнивает значки панели задач по левому краю.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarAl", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-search", Category: "taskbar", Title: "Скрыть поиск", Description: "Освобождает место на панели задач; поиск остаётся доступен через меню Пуск.", Risk: "safe", Recommended: true, Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Search`, Name: "SearchboxTaskbarMode", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-widgets", Category: "taskbar", Title: "Скрыть виджеты", Description: "Убирает кнопку новостей и виджетов с панели задач.", Risk: "safe", Recommended: true, Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarDa", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-chat", Category: "taskbar", Title: "Скрыть Чат / Teams", Description: "Убирает кнопку чата с панели задач на поддерживаемых сборках.", Risk: "safe", Recommended: true, Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarMn", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "taskbar-end-task", Category: "taskbar", Title: "«Завершить задачу» в панели", Description: "Добавляет завершение приложения в контекстное меню его значка.", Risk: "safe", Experimental: true, MinBuild: 22631, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced\TaskbarDeveloperSettings`, Name: "TaskbarEndTask", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "hide-task-view", Category: "taskbar", Title: "Скрыть представление задач", Description: "Убирает кнопку виртуальных рабочих столов с панели задач.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "ShowTaskViewButton", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "never-combine", Category: "taskbar", Title: "Не объединять кнопки", Description: "Показывает отдельную кнопку и подпись для каждого окна на поддерживаемых версиях Windows 11.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22621, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarGlomLevel", Type: "REG_DWORD", On: "2", Off: str("0")}}},
	{ID: "small-taskbar", Category: "taskbar", Title: "Компактная панель задач", Description: "Уменьшает значки панели задач. На новых сборках Windows параметр может игнорироваться.", Risk: "medium", Restart: "explorer", Experimental: true, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarSi", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-copilot", Category: "taskbar", Title: "Скрыть Copilot", Description: "Убирает кнопку Copilot с панели задач, если она присутствует в вашей сборке.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22621, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "ShowCopilotButton", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "taskbar-badges", Category: "taskbar", Title: "Значки уведомлений", Description: "Показывает счётчики и индикаторы активности на кнопках панели задач.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarBadges", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "all-tray-icons", Category: "taskbar", Title: "Все значки в трее", Description: "Не прячет фоновые приложения за стрелкой в области уведомлений.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer`, Name: "EnableAutoTray", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "taskbar-accent", Category: "taskbar", Title: "Акцентный цвет панели", Description: "Применяет выбранный цвет Windows к меню Пуск и панели задач.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "ColorPrevalence", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "multi-monitor-taskbar", Category: "taskbar", Title: "Панель на всех мониторах", Description: "Показывает панель задач на каждом подключённом дисплее.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "MMTaskbarEnabled", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "disable-recent", Category: "taskbar", Title: "Скрыть недавние документы", Description: "Не показывает историю документов в списках переходов приложений.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22000, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "Start_TrackDocs", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "show-extensions", Category: "explorer", Title: "Показывать расширения файлов", Description: "Показывает .exe, .jpg и другие расширения — полезно и безопаснее.", Risk: "safe", Recommended: true, Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "HideFileExt", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "show-hidden", Category: "explorer", Title: "Показывать скрытые файлы", Description: "Отображает скрытые элементы в Проводнике (системные защищённые файлы остаются скрыты).", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "Hidden", Type: "REG_DWORD", On: "1", Off: str("2")}}},
	{ID: "compact-explorer", Category: "explorer", Title: "Компактный вид Проводника", Description: "Уменьшает вертикальные отступы между файлами.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "UseCompactMode", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "disable-ad-id", Category: "privacy", Title: "Отключить рекламный ID", Description: "Запрещает приложениям использовать персональный рекламный идентификатор.", Risk: "safe", Recommended: true, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, Name: "Enabled", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "disable-suggestions", Category: "privacy", Title: "Отключить рекомендации и рекламу", Description: "Уменьшает предложения приложений, советы и рекламный контент Windows.", Risk: "safe", Recommended: true, Experimental: true, Ops: []operation{
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager`, Name: "SubscribedContent-338388Enabled", Type: "REG_DWORD", On: "0", Off: str("1")},
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager`, Name: "SubscribedContent-353694Enabled", Type: "REG_DWORD", On: "0", Off: str("1")},
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\ContentDeliveryManager`, Name: "SoftLandingEnabled", Type: "REG_DWORD", On: "0", Off: str("1")},
	}},
	{ID: "disable-tailored", Category: "privacy", Title: "Отключить персонализированные советы", Description: "Не использует диагностические данные для персональных рекомендаций.", Risk: "safe", Recommended: true, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Privacy`, Name: "TailoredExperiencesWithDiagnosticDataEnabled", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "disable-activity", Category: "privacy", Title: "Отключить журнал действий", Description: "Отключает публикацию и отправку истории активности для устройства.", Risk: "medium", Admin: true, Restart: "signout", Ops: []operation{
		{Path: `HKLM\SOFTWARE\Policies\Microsoft\Windows\System`, Name: "EnableActivityFeed", Type: "REG_DWORD", On: "0", Off: nil},
		{Path: `HKLM\SOFTWARE\Policies\Microsoft\Windows\System`, Name: "PublishUserActivities", Type: "REG_DWORD", On: "0", Off: nil},
		{Path: `HKLM\SOFTWARE\Policies\Microsoft\Windows\System`, Name: "UploadUserActivities", Type: "REG_DWORD", On: "0", Off: nil},
	}},
	{ID: "game-mode", Category: "gaming", Title: "Игровой режим", Description: "Разрешает Windows приоритизировать ресурсы для запущенной игры.", Risk: "safe", Recommended: true, Ops: []operation{
		{Path: `HKCU\Software\Microsoft\GameBar`, Name: "AllowAutoGameMode", Type: "REG_DWORD", On: "1", Off: str("0")},
		{Path: `HKCU\Software\Microsoft\GameBar`, Name: "AutoGameModeEnabled", Type: "REG_DWORD", On: "1", Off: str("0")},
	}},
	{ID: "disable-captures", Category: "gaming", Title: "Отключить фоновую запись игр", Description: "Отключает Game DVR и фоновый захват. Не влияет на ручные скриншоты сторонними программами.", Risk: "safe", Recommended: true, Ops: []operation{
		{Path: `HKCU\System\GameConfigStore`, Name: "GameDVR_Enabled", Type: "REG_DWORD", On: "0", Off: str("1")},
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\GameDVR`, Name: "AppCaptureEnabled", Type: "REG_DWORD", On: "0", Off: str("1")},
	}},
	{ID: "mouse-accel", Category: "gaming", Title: "Отключить ускорение мыши", Description: "Использует линейное движение указателя — удобно для стабильной игровой мышечной памяти.", Risk: "safe", Ops: []operation{
		{Path: `HKCU\Control Panel\Mouse`, Name: "MouseSpeed", Type: "REG_SZ", On: "0", Off: str("1")},
		{Path: `HKCU\Control Panel\Mouse`, Name: "MouseThreshold1", Type: "REG_SZ", On: "0", Off: str("6")},
		{Path: `HKCU\Control Panel\Mouse`, Name: "MouseThreshold2", Type: "REG_SZ", On: "0", Off: str("10")},
	}},
	{ID: "menu-delay", Category: "system", Title: "Быстрые меню", Description: "Сокращает задержку появления подменю с 400 до 100 мс.", Risk: "safe", Recommended: true, Restart: "signout", Ops: []operation{{Path: `HKCU\Control Panel\Desktop`, Name: "MenuShowDelay", Type: "REG_SZ", On: "100", Off: str("400")}}},
	{ID: "long-paths", Category: "system", Title: "Длинные пути Win32", Description: "Разрешает совместимым приложениям работать с путями длиннее 260 символов.", Risk: "safe", Recommended: true, Admin: true, Restart: "reboot", Ops: []operation{{Path: `HKLM\SYSTEM\CurrentControlSet\Control\FileSystem`, Name: "LongPathsEnabled", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "seconds-clock", Category: "system", Title: "Секунды в системных часах", Description: "Показывает секунды в часах панели задач; может немного увеличить расход энергии.", Risk: "safe", Restart: "explorer", Experimental: true, MinBuild: 22621, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "ShowSecondsInSystemClock", Type: "REG_DWORD", On: "1", Off: str("0")}}},
}

var settingsPages = map[string]string{
	"personalize":    "ms-settings:personalization",
	"background":     "ms-settings:personalization-background",
	"colors":         "ms-settings:personalization-colors",
	"themes":         "ms-settings:themes",
	"start":          "ms-settings:personalization-start",
	"taskbar":        "ms-settings:taskbar",
	"display":        "ms-settings:display",
	"sound":          "ms-settings:sound",
	"bluetooth":      "ms-settings:bluetooth",
	"apps":           "ms-settings:appsfeatures",
	"startup":        "ms-settings:startupapps",
	"storage":        "ms-settings:storagesense",
	"accessibility":  "ms-settings:easeofaccess-display",
	"visual-effects": "ms-settings:easeofaccess-visualeffects",
	"mouse-pointer":  "ms-settings:easeofaccess-mousepointer",
	"mouse":          "ms-settings:mousetouchpad",
	"fonts":          "ms-settings:fonts",
	"lockscreen":     "ms-settings:lockscreen",
	"update":         "ms-settings:windowsupdate",
}

func main() {
	if runtime.GOOS != "windows" {
		fmt.Println(appName, "предназначен для Windows 11")
		return
	}
	a := newApp()
	token := randomToken()
	mux := http.NewServeMux()
	a.routes(mux, token)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	addr := "http://" + listener.Addr().String() + "/#token=" + token
	go func() {
		time.Sleep(250 * time.Millisecond)
		if err := openBrowser(addr); err != nil {
			log.Printf("browser: %v", err)
		}
	}()
	server := &http.Server{
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Printf("%s %s: %s", appName, appVersion, addr)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newApp() *app {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Win11Customizer")
	_ = os.MkdirAll(dir, 0755)
	a := &app{backupPath: filepath.Join(dir, "backup.json"), backup: backupFile{Created: time.Now(), Values: map[string]backupValue{}, OrderKnown: true}, audit: newAuditLogger(filepath.Join(dir, "audit.jsonl"))}
	if b, err := os.ReadFile(a.backupPath); err == nil {
		if err := json.Unmarshal(b, &a.backup); err != nil {
			corruptPath := a.backupPath + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10)
			_ = os.Rename(a.backupPath, corruptPath)
			a.backup = backupFile{Created: time.Now(), Values: map[string]backupValue{}, OrderKnown: true}
			a.audit.write("backup.load", "error", map[string]any{"error": err.Error(), "preservedAs": corruptPath})
		}
	}
	if a.backup.Values == nil {
		a.backup.Values = map[string]backupValue{}
	}
	if len(a.backup.Order) == 0 && len(a.backup.Values) > 0 {
		a.backup.OrderKnown = false
		for key := range a.backup.Values {
			a.backup.Order = append(a.backup.Order, key)
		}
		sort.Strings(a.backup.Order)
	}
	if a.backup.ThemeCaptured && !containsString(a.backup.Order, backupThemeMarker) {
		a.backup.Order = append(a.backup.Order, backupThemeMarker)
		a.backup.OrderKnown = false
	}
	if len(a.backup.Values) == 0 && !a.backup.ThemeCaptured {
		a.backup.OrderKnown = true
	}
	a.windows = detectWindowsVersion()
	a.audit.write("application.start", "ok", map[string]any{"version": appVersion, "build": a.windows.Build})
	return a
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data: https://images.unsplash.com; connect-src 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (a *app) routes(mux *http.ServeMux, token string) {
	assets, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Customizer-Token") != token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/state", auth(a.handleState))
	mux.HandleFunc("POST /api/apply", auth(a.handleApply))
	mux.HandleFunc("POST /api/apply-recommended", auth(a.handleRecommended))
	mux.HandleFunc("POST /api/restore", auth(a.handleRestore))
	mux.HandleFunc("POST /api/restart-explorer", auth(a.handleRestartExplorer))
	mux.HandleFunc("POST /api/restore-point", auth(a.handleRestorePoint))
	mux.HandleFunc("POST /api/wallpaper", auth(a.handleWallpaper))
	mux.HandleFunc("POST /api/wallpaper/catalog", auth(a.handleCatalogWallpaper))
	mux.HandleFunc("POST /api/slides", auth(a.handleSlides))
	mux.HandleFunc("GET /api/slides", auth(a.handleSlides))
	mux.HandleFunc("POST /api/theme", auth(a.handleTheme))
	mux.HandleFunc("GET /api/profile", auth(a.handleProfileExport))
	mux.HandleFunc("POST /api/profile/preview", auth(a.handleProfilePreview))
	mux.HandleFunc("POST /api/profile/apply", auth(a.handleProfileApply))
	mux.HandleFunc("GET /api/logs", auth(a.handleLogsExport))
	mux.HandleFunc("POST /api/open-settings", auth(a.handleOpenSettings))
	mux.HandleFunc("GET /api/export", auth(a.handleExport))
}

func (a *app) handleOpenSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	uri, ok := settingsPages[req.ID]
	if !ok && req.ID != "screensaver" {
		writeError(w, http.StatusNotFound, "Раздел параметров не найден")
		return
	}
	var err error
	if req.ID == "screensaver" {
		err = launchScreenSaverSettings()
	} else {
		err = launchSettings(uri)
	}
	if err != nil {
		a.audit.write("settings.open", "error", map[string]any{"id": req.ID, "error": err.Error()})
		writeError(w, http.StatusInternalServerError, "Не удалось открыть параметры Windows")
		return
	}
	a.audit.write("settings.open", "ok", map[string]any{"id": req.ID})
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleState(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	states := make([]tweakState, 0, len(tweaks))
	for _, t := range tweaks {
		available, compatibilityError := tweakCompatibility(t, a.windows.Build)
		enabled, err := tweakEnabled(t)
		s := tweakState{tweak: t, Enabled: enabled, Available: available}
		if compatibilityError != "" {
			s.Error = compatibilityError
		}
		if err != nil {
			s.Error = err.Error()
		}
		states = append(states, s)
	}
	writeJSON(w, map[string]any{"app": appName, "version": appVersion, "windows": a.windows, "admin": isAdmin(), "backupCount": len(a.backup.Values), "themeBackup": a.backup.ThemeCaptured, "backupOrderKnown": a.backup.OrderKnown, "tweaks": states})
}

func (a *app) handleApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, 400, "Некорректный запрос")
		return
	}
	t, ok := findTweak(req.ID)
	if !ok {
		writeError(w, 404, "Настройка не найдена")
		return
	}
	if t.Admin && !isAdmin() {
		writeError(w, 403, "Для этой настройки перезапустите программу от имени администратора")
		return
	}
	if available, reason := tweakCompatibility(*t, a.windows.Build); !available {
		writeError(w, 409, reason)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.apply(*t, req.Enabled); err != nil {
		a.audit.write("tweak.apply", "error", map[string]any{"id": t.ID, "enabled": req.Enabled, "error": err.Error()})
		writeError(w, 500, err.Error())
		return
	}
	a.audit.write("tweak.apply", "ok", map[string]any{"id": t.ID, "enabled": req.Enabled})
	writeJSON(w, map[string]any{"ok": true, "enabled": req.Enabled, "restart": t.Restart})
}

func (a *app) handleRecommended(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	profile := profileFile{Schema: profileSchema, Tweaks: map[string]bool{}}
	applied := make([]string, 0)
	for _, t := range tweaks {
		available, _ := tweakCompatibility(t, a.windows.Build)
		if !t.Recommended || !available || (t.Admin && !isAdmin()) {
			continue
		}
		profile.Tweaks[t.ID] = true
		enabled, err := tweakEnabled(t)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		if !enabled {
			applied = append(applied, t.ID)
		}
	}
	if _, err := a.applyProfile(profile); err != nil {
		a.audit.write("recommended.apply", "error", map[string]any{"error": err.Error()})
		writeError(w, 500, err.Error())
		return
	}
	a.audit.write("recommended.apply", "ok", map[string]any{"applied": len(applied)})
	writeJSON(w, map[string]any{"ok": true, "applied": applied})
}

func (a *app) apply(t tweak, enabled bool) error {
	plans := make([]registryPlan, 0, len(t.Ops))
	for _, op := range t.Ops {
		plans = append(plans, registryPlan{op: op, enabled: enabled})
	}
	if _, err := a.executeRegistryTransaction(plans); err != nil {
		return fmt.Errorf("%s: %w", t.Title, err)
	}
	return nil
}

func readBackupValue(op operation) (backupValue, error) {
	rawType, raw, exists, keyExists, err := nativeReadRegistryValue(op.Path, op.Name)
	if err != nil {
		return backupValue{}, err
	}
	typ, data, _, textErr := regGet(op.Path, op.Name)
	if textErr != nil && exists {
		return backupValue{}, textErr
	}
	return backupValue{Path: op.Path, Name: op.Name, Existed: exists, KeyExisted: keyExists, Type: typ, Data: data, RawType: rawType, RawData: base64.StdEncoding.EncodeToString(raw), RawCaptured: true}, nil
}

func (a *app) rememberValue(op operation, value backupValue) {
	key := strings.ToLower(op.Path + "|" + op.Name)
	if _, ok := a.backup.Values[key]; ok {
		return
	}
	a.backup.Values[key] = value
	a.backup.Order = append(a.backup.Order, key)
}

func applyOperation(op operation, enabled bool) error {
	if enabled {
		return regSet(op.Path, op.Name, op.Type, op.On)
	}
	if op.Off == nil {
		return regDelete(op.Path, op.Name)
	}
	return regSet(op.Path, op.Name, op.Type, *op.Off)

}

func operationMatches(op operation, enabled bool) (bool, error) {
	_, data, exists, err := regGet(op.Path, op.Name)
	if err != nil {
		return false, err
	}
	if enabled {
		return exists && equalRegistry(data, op.On, op.Type), nil
	}
	if op.Off == nil {
		return !exists, nil
	}
	return exists && equalRegistry(data, *op.Off, op.Type), nil
}

func restoreValues(values []backupValue) error {
	var restoreErr error
	for i := len(values) - 1; i >= 0; i-- {
		value := values[i]
		var err error
		if value.Existed {
			if value.RawCaptured {
				var raw []byte
				raw, err = base64.StdEncoding.DecodeString(value.RawData)
				if err == nil {
					err = nativeWriteRegistryValue(value.Path, value.Name, value.RawType, raw)
				}
			} else {
				err = regSet(value.Path, value.Name, value.Type, value.Data)
			}
		} else {
			err = nativeDeleteRegistryValue(value.Path, value.Name)
		}
		if err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("%s\\%s: %w", value.Path, value.Name, err))
		}
	}
	return restoreErr
}

func (a *app) saveBackup() error {
	b, err := json.MarshalIndent(a.backup, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.backupPath + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.backupPath)
}

func (a *app) handleRestore(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopSlides()
	if len(a.backup.Values) == 0 && !a.backup.ThemeCaptured {
		writeJSON(w, map[string]any{"ok": true, "restored": 0})
		return
	}
	if !isAdmin() {
		for _, v := range a.backup.Values {
			if strings.HasPrefix(strings.ToUpper(v.Path), "HKLM\\") {
				writeError(w, 403, "В резервной копии есть системные параметры. Запустите программу от имени администратора")
				return
			}
		}
	}
	order := append([]string(nil), a.backup.Order...)
	if a.backup.ThemeCaptured && !containsString(order, backupThemeMarker) {
		// Backups created before v1.5 did not record the theme in the operation order.
		order = append(order, backupThemeMarker)
	}
	restored := 0
	var restoreErr error
	for index := len(order) - 1; index >= 0; index-- {
		key := order[index]
		if key == backupThemeMarker {
			if a.backup.PreviousTheme != "" {
				restoreErr = errors.Join(restoreErr, restoreWindowsTheme(a.backup.PreviousTheme))
			}
			continue
		}
		value, ok := a.backup.Values[key]
		if !ok {
			continue
		}
		if err := restoreValues([]backupValue{value}); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
		restored++
	}
	if restoreErr != nil {
		a.audit.write("backup.restore", "error", map[string]any{"stage": "ordered-restore", "error": restoreErr.Error()})
		writeError(w, 500, "Не все параметры удалось восстановить: "+restoreErr.Error())
		return
	}
	if _, wallpaper, exists, _ := regGet(`HKCU\Control Panel\Desktop`, "Wallpaper"); exists && wallpaper != "" {
		if err := refreshWallpaper(wallpaper); err != nil {
			a.audit.write("backup.restore", "error", map[string]any{"stage": "wallpaper", "error": err.Error()})
			writeError(w, 500, "Значения восстановлены, но не удалось обновить обои: "+err.Error())
			return
		}
	}
	if err := os.Remove(a.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.audit.write("backup.restore", "error", map[string]any{"stage": "finalize", "error": err.Error()})
		writeError(w, 500, "Значения восстановлены, но не удалось удалить резервную копию: "+err.Error())
		return
	}
	a.backup = backupFile{Created: time.Now(), Values: map[string]backupValue{}, Order: []string{}, OrderKnown: true}
	a.audit.write("backup.restore", "ok", map[string]any{"restored": restored})
	writeJSON(w, map[string]any{"ok": true, "restored": restored})
}

func (a *app) handleRestartExplorer(w http.ResponseWriter, _ *http.Request) {
	_ = exec.Command("taskkill.exe", "/F", "/IM", "explorer.exe").Run()
	time.Sleep(500 * time.Millisecond)
	if err := exec.Command("explorer.exe").Start(); err != nil {
		a.audit.write("explorer.restart", "error", map[string]any{"error": err.Error()})
		writeError(w, 500, err.Error())
		return
	}
	a.audit.write("explorer.restart", "ok", nil)
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleRestorePoint(w http.ResponseWriter, _ *http.Request) {
	if !isAdmin() {
		writeError(w, 403, "Запустите программу от имени администратора")
		return
	}
	cmd := `Checkpoint-Computer -Description 'Win11 Customizer' -RestorePointType 'MODIFY_SETTINGS'`
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", cmd).CombinedOutput()
	if err != nil {
		a.audit.write("restore-point.create", "error", map[string]any{"error": cleanOutput(out)})
		writeError(w, 500, "Не удалось создать точку восстановления: "+cleanOutput(out))
		return
	}
	a.audit.write("restore-point.create", "ok", nil)
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleWallpaper(w http.ResponseWriter, r *http.Request) {
	const maxWallpaperSize = 15 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxWallpaperSize+(1<<20))
	file, _, err := r.FormFile("wallpaper")
	if err != nil {
		writeError(w, 400, "Выберите изображение JPEG, PNG или BMP")
		return
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	data, err := io.ReadAll(io.LimitReader(file, maxWallpaperSize+1))
	if err != nil || len(data) == 0 {
		writeError(w, 400, "Не удалось прочитать изображение")
		return
	}
	if len(data) > maxWallpaperSize {
		writeError(w, 413, "Размер изображения не должен превышать 15 МБ")
		return
	}
	mime := http.DetectContentType(data)
	if err = a.installWallpaper(data, mime); err != nil {
		a.audit.write("wallpaper.install", "error", map[string]any{"source": "file", "error": err.Error()})
		writeError(w, 500, "Не удалось применить обои: "+err.Error())
		return
	}
	a.audit.write("wallpaper.install", "ok", map[string]any{"source": "file", "mime": mime, "bytes": len(data)})
	writeJSON(w, map[string]any{"ok": true, "mime": mime})
}

var wallpaperCatalog = map[string]string{
	"ocean":     "https://images.unsplash.com/photo-1507525428034-b723cf961d3e?auto=format&fit=crop&w=2560&q=90",
	"mountains": "https://images.unsplash.com/photo-1464822759023-fed622ff2c3b?auto=format&fit=crop&w=2560&q=90",
	"forest":    "https://images.unsplash.com/photo-1441974231531-c6227db76b6e?auto=format&fit=crop&w=2560&q=90",
	"city":      "https://images.unsplash.com/photo-1518005020951-eccb494ad742?auto=format&fit=crop&w=2560&q=90",
	"space":     "https://images.unsplash.com/photo-1444703686981-a3abbc4d4fe3?auto=format&fit=crop&w=2560&q=90",
	"minimal":   "https://images.unsplash.com/photo-1557682250-33bd709cbe85?auto=format&fit=crop&w=2560&q=90",
}

func unsplashWallpaperURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "images.unsplash.com" || u.User != nil || !strings.HasPrefix(u.Path, "/photo-") || strings.Contains(u.Path[1:], "/") || len(u.Path) <= len("/photo-") {
		return "", errors.New("Вставьте прямую ссылку на фото с images.unsplash.com")
	}
	u.RawQuery = "fit=max&fm=jpg&q=90&w=2560"
	u.Fragment = ""
	return u.String(), nil
}

func (a *app) handleCatalogWallpaper(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeError(w, 400, "Некорректный запрос")
		return
	}
	address, ok := wallpaperCatalog[req.ID]
	if req.ID == "custom" {
		var err error
		address, err = unsplashWallpaperURL(req.URL)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
	} else if !ok {
		writeError(w, 404, "Обои не найдены")
		return
	}
	client := &http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: wallpaperRedirectPolicy,
	}
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, address, nil)
	request.Header.Set("User-Agent", "Win11Customizer/"+appVersion)
	response, err := client.Do(request)
	if err != nil {
		writeError(w, 502, "Не удалось скачать обои. Проверьте интернет")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeError(w, 502, fmt.Sprintf("Сервер обоев ответил %d", response.StatusCode))
		return
	}
	const maxDownload = 20 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxDownload+1))
	if err != nil || len(data) == 0 || len(data) > maxDownload {
		writeError(w, 502, "Получено повреждённое или слишком большое изображение")
		return
	}
	if err = a.installWallpaper(data, http.DetectContentType(data)); err != nil {
		a.audit.write("wallpaper.install", "error", map[string]any{"source": req.ID, "error": err.Error()})
		writeError(w, 500, "Не удалось применить обои: "+err.Error())
		return
	}
	a.audit.write("wallpaper.install", "ok", map[string]any{"source": req.ID, "bytes": len(data)})
	writeJSON(w, map[string]any{"ok": true, "id": req.ID})
}

func wallpaperRedirectPolicy(next *http.Request, via []*http.Request) error {
	if next.URL.Scheme != "https" || next.URL.Hostname() != "images.unsplash.com" || len(via) > 4 {
		return errors.New("недопустимое перенаправление")
	}
	return nil
}

func (a *app) installWallpaper(data []byte, mime string) error {
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/bmp": ".bmp"}[mime]
	if ext == "" {
		return errors.New("поддерживаются только JPEG, PNG и BMP")
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Win11Customizer")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("не удалось создать папку программы")
	}
	path := filepath.Join(dir, "wallpaper-"+strconv.FormatInt(time.Now().UnixNano(), 10)+ext)
	path, err := filepath.Abs(path)
	if err != nil {
		return errors.New("не удалось определить полный путь изображения")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.WriteFile(path, data, 0600); err != nil {
		return errors.New("не удалось сохранить изображение")
	}
	if err := a.applyWallpaperPath(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	a.stopSlides()
	return nil
}

// Caller holds a.mu. Source images always remain owned by their caller.
func (a *app) applyWallpaperPath(path string) error {
	ops := []operation{
		{Path: `HKCU\Control Panel\Desktop`, Name: "Wallpaper", Type: "REG_SZ", On: path},
		{Path: `HKCU\Control Panel\Desktop`, Name: "WallpaperStyle", Type: "REG_SZ", On: "10"},
		{Path: `HKCU\Control Panel\Desktop`, Name: "TileWallpaper", Type: "REG_SZ", On: "0"},
	}
	plans := make([]registryPlan, 0, len(ops))
	for _, op := range ops {
		plans = append(plans, registryPlan{op: op, enabled: true})
	}
	before, err := a.executeRegistryTransaction(plans)
	if err != nil {
		return err
	}
	err = refreshWallpaper(path)
	if err != nil {
		rollbackErr := restoreValues(before)
		if before[0].Existed && before[0].Data != "" {
			if refreshErr := refreshWallpaper(before[0].Data); refreshErr != nil {
				rollbackErr = errors.Join(rollbackErr, refreshErr)
			}
		}
		if rollbackErr != nil {
			return fmt.Errorf("%v; автоматический откат: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func refreshWallpaper(path string) error {
	return setWallpaperNative(path)
}

func (a *app) handleExport(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Content-Disposition", `attachment; filename="win11-customizer-backup.json"`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(a.backup)
}

func tweakEnabled(t tweak) (bool, error) {
	for _, op := range t.Ops {
		_, data, exists, err := regGet(op.Path, op.Name)
		if err != nil {
			return false, err
		}
		if !exists || !equalRegistry(data, op.On, op.Type) {
			return false, nil
		}
	}
	return true, nil
}

func equalRegistry(a, b, typ string) bool {
	if typ == "REG_DWORD" || typ == "REG_QWORD" {
		ai, ea := parseRegistryNumber(a)
		bi, eb := parseRegistryNumber(b)
		return ea == nil && eb == nil && ai == bi
	}
	return a == b
}

func parseRegistryNumber(value string) (uint64, error) {
	text := strings.ToLower(strings.TrimSpace(value))
	base := 10
	if strings.HasPrefix(text, "0x") {
		text = strings.TrimPrefix(text, "0x")
		base = 16
	}
	return strconv.ParseUint(text, base, 64)
}

func regGet(path, name string) (typ, data string, exists bool, err error) {
	return nativeRegistryText(path, name)
}

func regSet(path, name, typ, data string) error {
	return nativeSetRegistryText(path, name, typ, data)
}

func regDelete(path, name string) error {
	return nativeDeleteRegistryValue(path, name)
}

func findTweak(id string) (*tweak, bool) {
	for i := range tweaks {
		if tweaks[i].ID == id {
			return &tweaks[i], true
		}
	}
	return nil, false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func isAdmin() bool { return exec.Command("net.exe", "session").Run() == nil }

func openBrowser(url string) error {
	paths := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return exec.Command(p, "--app="+url, "--window-size=1280,860").Start()
		}
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}

func cleanOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "операция не выполнена"
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": message})
}
