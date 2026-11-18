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
	appVersion = "1.4.0"
)

//go:embed web/*
var webFiles embed.FS

type operation struct {
	Path           string  `json:"-"`
	Name           string  `json:"-"`
	Type           string  `json:"-"`
	On             string  `json:"-"`
	Off            *string `json:"-"`
	DeleteKeyOnOff bool    `json:"-"`
}

type tweak struct {
	ID          string      `json:"id"`
	Category    string      `json:"category"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Risk        string      `json:"risk"`
	Admin       bool        `json:"admin"`
	Restart     string      `json:"restart,omitempty"`
	Recommended bool        `json:"recommended"`
	Ops         []operation `json:"-"`
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
	DeleteKey   bool   `json:"deleteKey,omitempty"`
	RawType     uint32 `json:"rawType,omitempty"`
	RawData     string `json:"rawData,omitempty"`
	RawCaptured bool   `json:"rawCaptured,omitempty"`
}

type backupFile struct {
	Created    time.Time              `json:"created"`
	Values     map[string]backupValue `json:"values"`
	Order      []string               `json:"order,omitempty"`
	OrderKnown bool                   `json:"orderKnown"`
}

type app struct {
	mu         sync.Mutex
	backupPath string
	backup     backupFile
}

func str(v string) *string { return &v }

var tweaks = []tweak{
	{ID: "dark-mode", Category: "appearance", Title: "Тёмная тема", Description: "Тёмный режим для приложений и системного интерфейса.", Risk: "safe", Recommended: true, Restart: "explorer", Ops: []operation{
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "AppsUseLightTheme", Type: "REG_DWORD", On: "0", Off: str("1")},
		{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "SystemUsesLightTheme", Type: "REG_DWORD", On: "0", Off: str("1")},
	}},
	{ID: "transparency", Category: "appearance", Title: "Эффекты прозрачности", Description: "Включает Acrylic/Mica там, где это поддерживает Windows.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "EnableTransparency", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "accent-titlebars", Category: "appearance", Title: "Акцентный цвет заголовков", Description: "Показывает выбранный цвет Windows на заголовках и границах окон.", Risk: "safe", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\DWM`, Name: "ColorPrevalence", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "taskbar-left", Category: "taskbar", Title: "Пуск слева", Description: "Выравнивает значки панели задач по левому краю.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarAl", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-search", Category: "taskbar", Title: "Скрыть поиск", Description: "Освобождает место на панели задач; поиск остаётся доступен через меню Пуск.", Risk: "safe", Recommended: true, Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Search`, Name: "SearchboxTaskbarMode", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-widgets", Category: "taskbar", Title: "Скрыть виджеты", Description: "Убирает кнопку новостей и виджетов с панели задач.", Risk: "safe", Recommended: true, Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarDa", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-chat", Category: "taskbar", Title: "Скрыть Чат / Teams", Description: "Убирает кнопку чата с панели задач на поддерживаемых сборках.", Risk: "safe", Recommended: true, Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarMn", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "taskbar-end-task", Category: "taskbar", Title: "«Завершить задачу» в панели", Description: "Добавляет завершение приложения в контекстное меню его значка.", Risk: "safe", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced\TaskbarDeveloperSettings`, Name: "TaskbarEndTask", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "hide-task-view", Category: "taskbar", Title: "Скрыть представление задач", Description: "Убирает кнопку виртуальных рабочих столов с панели задач.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "ShowTaskViewButton", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "never-combine", Category: "taskbar", Title: "Не объединять кнопки", Description: "Показывает отдельную кнопку и подпись для каждого окна на поддерживаемых версиях Windows 11.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarGlomLevel", Type: "REG_DWORD", On: "2", Off: str("0")}}},
	{ID: "small-taskbar", Category: "taskbar", Title: "Компактная панель задач", Description: "Уменьшает значки панели задач. На новых сборках Windows параметр может игнорироваться.", Risk: "medium", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarSi", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "hide-copilot", Category: "taskbar", Title: "Скрыть Copilot", Description: "Убирает кнопку Copilot с панели задач, если она присутствует в вашей сборке.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "ShowCopilotButton", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "taskbar-badges", Category: "taskbar", Title: "Значки уведомлений", Description: "Показывает счётчики и индикаторы активности на кнопках панели задач.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "TaskbarBadges", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "all-tray-icons", Category: "taskbar", Title: "Все значки в трее", Description: "Не прячет фоновые приложения за стрелкой в области уведомлений.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer`, Name: "EnableAutoTray", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "taskbar-accent", Category: "taskbar", Title: "Акцентный цвет панели", Description: "Применяет выбранный цвет Windows к меню Пуск и панели задач.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, Name: "ColorPrevalence", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "multi-monitor-taskbar", Category: "taskbar", Title: "Панель на всех мониторах", Description: "Показывает панель задач на каждом подключённом дисплее.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "MMTaskbarEnabled", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "disable-recent", Category: "taskbar", Title: "Скрыть недавние документы", Description: "Не показывает историю документов в списках переходов приложений.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "Start_TrackDocs", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "show-extensions", Category: "explorer", Title: "Показывать расширения файлов", Description: "Показывает .exe, .jpg и другие расширения — полезно и безопаснее.", Risk: "safe", Recommended: true, Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "HideFileExt", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "show-hidden", Category: "explorer", Title: "Показывать скрытые файлы", Description: "Отображает скрытые элементы в Проводнике (системные защищённые файлы остаются скрыты).", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "Hidden", Type: "REG_DWORD", On: "1", Off: str("2")}}},
	{ID: "compact-explorer", Category: "explorer", Title: "Компактный вид Проводника", Description: "Уменьшает вертикальные отступы между файлами.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "UseCompactMode", Type: "REG_DWORD", On: "1", Off: str("0")}}},
	{ID: "disable-ad-id", Category: "privacy", Title: "Отключить рекламный ID", Description: "Запрещает приложениям использовать персональный рекламный идентификатор.", Risk: "safe", Recommended: true, Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\AdvertisingInfo`, Name: "Enabled", Type: "REG_DWORD", On: "0", Off: str("1")}}},
	{ID: "disable-suggestions", Category: "privacy", Title: "Отключить рекомендации и рекламу", Description: "Уменьшает предложения приложений, советы и рекламный контент Windows.", Risk: "safe", Recommended: true, Ops: []operation{
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
	{ID: "seconds-clock", Category: "system", Title: "Секунды в системных часах", Description: "Показывает секунды в часах панели задач; может немного увеличить расход энергии.", Risk: "safe", Restart: "explorer", Ops: []operation{{Path: `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`, Name: "ShowSecondsInSystemClock", Type: "REG_DWORD", On: "1", Off: str("0")}}},
}

var settingsPages = map[string]string{
	"personalize":   "ms-settings:personalization",
	"background":    "ms-settings:personalization-background",
	"colors":        "ms-settings:personalization-colors",
	"themes":        "ms-settings:themes",
	"start":         "ms-settings:personalization-start",
	"taskbar":       "ms-settings:taskbar",
	"display":       "ms-settings:display",
	"sound":         "ms-settings:sound",
	"bluetooth":     "ms-settings:bluetooth",
	"apps":          "ms-settings:appsfeatures",
	"startup":       "ms-settings:startupapps",
	"storage":       "ms-settings:storagesense",
	"accessibility": "ms-settings:easeofaccess-display",
	"update":        "ms-settings:windowsupdate",
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
	server := &http.Server{Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
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
	a := &app{backupPath: filepath.Join(dir, "backup.json"), backup: backupFile{Created: time.Now(), Values: map[string]backupValue{}, OrderKnown: true}}
	if b, err := os.ReadFile(a.backupPath); err == nil {
		_ = json.Unmarshal(b, &a.backup)
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
	if len(a.backup.Values) == 0 {
		a.backup.OrderKnown = true
	}
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
	if !ok {
		writeError(w, http.StatusNotFound, "Раздел параметров не найден")
		return
	}
	if err := launchSettings(uri); err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось открыть параметры Windows")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (a *app) handleState(w http.ResponseWriter, _ *http.Request) {
	states := make([]tweakState, 0, len(tweaks))
	for _, t := range tweaks {
		enabled, err := tweakEnabled(t)
		s := tweakState{tweak: t, Enabled: enabled, Available: true}
		if err != nil {
			s.Error = err.Error()
		}
		states = append(states, s)
	}
	writeJSON(w, map[string]any{"app": appName, "version": appVersion, "windows": windowsInfo(), "admin": isAdmin(), "backupCount": len(a.backup.Values), "backupOrderKnown": a.backup.OrderKnown, "tweaks": states})
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
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.apply(*t, req.Enabled); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "enabled": req.Enabled, "restart": t.Restart})
}

func (a *app) handleRecommended(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	applied := []string{}
	for _, t := range tweaks {
		if !t.Recommended || (t.Admin && !isAdmin()) {
			continue
		}
		if err := a.apply(t, true); err != nil {
			writeError(w, 500, fmt.Sprintf("%s: %v", t.Title, err))
			return
		}
		applied = append(applied, t.ID)
	}
	writeJSON(w, map[string]any{"ok": true, "applied": applied})
}

func (a *app) apply(t tweak, enabled bool) error {
	before := make([]backupValue, len(t.Ops))
	for i, op := range t.Ops {
		value, err := readBackupValue(op)
		if err != nil {
			return fmt.Errorf("%s: снимок: %w", t.Title, err)
		}
		before[i] = value
		a.rememberValue(op, value)
	}
	// The durable snapshot must exist before the first registry mutation.
	if err := a.saveBackup(); err != nil {
		return fmt.Errorf("%s: резервная копия: %w", t.Title, err)
	}
	for i, op := range t.Ops {
		err := applyOperation(op, enabled)
		if err == nil {
			var matches bool
			matches, err = operationMatches(op, enabled)
			if err == nil && !matches {
				err = errors.New("Windows не подтвердила новое значение")
			}
		}
		if err != nil {
			rollbackErr := restoreValues(before[:i+1])
			if rollbackErr != nil {
				return fmt.Errorf("%s: %v; автоматический откат: %w", t.Title, err, rollbackErr)
			}
			return fmt.Errorf("%s: %w (изменение отменено)", t.Title, err)
		}
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
	return backupValue{Path: op.Path, Name: op.Name, Existed: exists, KeyExisted: keyExists, Type: typ, Data: data, DeleteKey: op.DeleteKeyOnOff, RawType: rawType, RawData: base64.StdEncoding.EncodeToString(raw), RawCaptured: true}, nil
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
	if op.DeleteKeyOnOff {
		// Removing the whole key could destroy values or subkeys that were not
		// created by this application. Deleting the controlled value is enough.
		return regDelete(op.Path, op.Name)
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
	if op.DeleteKeyOnOff || op.Off == nil {
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
	if len(a.backup.Values) == 0 {
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
	values := make([]backupValue, 0, len(a.backup.Order))
	for _, key := range a.backup.Order {
		v, ok := a.backup.Values[key]
		if !ok {
			continue
		}
		values = append(values, v)
	}
	if err := restoreValues(values); err != nil {
		writeError(w, 500, "Не все значения удалось восстановить: "+err.Error())
		return
	}
	if _, wallpaper, exists, _ := regGet(`HKCU\Control Panel\Desktop`, "Wallpaper"); exists && wallpaper != "" {
		if err := refreshWallpaper(wallpaper); err != nil {
			writeError(w, 500, "Значения восстановлены, но не удалось обновить обои: "+err.Error())
			return
		}
	}
	if err := os.Remove(a.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		writeError(w, 500, "Значения восстановлены, но не удалось удалить резервную копию: "+err.Error())
		return
	}
	a.backup = backupFile{Created: time.Now(), Values: map[string]backupValue{}, Order: []string{}, OrderKnown: true}
	writeJSON(w, map[string]any{"ok": true, "restored": len(values)})
}

func (a *app) handleRestartExplorer(w http.ResponseWriter, _ *http.Request) {
	_ = exec.Command("taskkill.exe", "/F", "/IM", "explorer.exe").Run()
	time.Sleep(500 * time.Millisecond)
	if err := exec.Command("explorer.exe").Start(); err != nil {
		writeError(w, 500, err.Error())
		return
	}
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
		writeError(w, 500, "Не удалось создать точку восстановления: "+cleanOutput(out))
		return
	}
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
		writeError(w, 500, "Не удалось применить обои: "+err.Error())
		return
	}
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

func (a *app) handleCatalogWallpaper(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, 400, "Некорректный запрос")
		return
	}
	address, ok := wallpaperCatalog[req.ID]
	if !ok {
		writeError(w, 404, "Обои не найдены")
		return
	}
	client := &http.Client{
		Timeout: 35 * time.Second,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if next.URL.Hostname() != "images.unsplash.com" || len(via) > 4 {
				return errors.New("недопустимое перенаправление")
			}
			return nil
		},
	}
	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, address, nil)
	request.Header.Set("User-Agent", "Win11Customizer/1.3")
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
		writeError(w, 500, "Не удалось применить обои: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "id": req.ID})
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
	ops := []operation{
		{Path: `HKCU\Control Panel\Desktop`, Name: "Wallpaper", Type: "REG_SZ", On: path},
		{Path: `HKCU\Control Panel\Desktop`, Name: "WallpaperStyle", Type: "REG_SZ", On: "10"},
		{Path: `HKCU\Control Panel\Desktop`, Name: "TileWallpaper", Type: "REG_SZ", On: "0"},
	}
	before := make([]backupValue, len(ops))
	for i, op := range ops {
		value, err := readBackupValue(op)
		if err != nil {
			return err
		}
		before[i] = value
		a.rememberValue(op, value)
	}
	if err := a.saveBackup(); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return errors.New("не удалось сохранить изображение")
	}
	for _, op := range ops {
		err = applyOperation(op, true)
		if err == nil {
			var matches bool
			matches, err = operationMatches(op, true)
			if err == nil && !matches {
				err = errors.New("Windows не подтвердила параметр обоев")
			}
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = refreshWallpaper(path)
	}
	if err != nil {
		rollbackErr := restoreValues(before)
		if before[0].Existed && before[0].Data != "" {
			if refreshErr := refreshWallpaper(before[0].Data); refreshErr != nil {
				rollbackErr = errors.Join(rollbackErr, refreshErr)
			}
		}
		_ = os.Remove(path)
		if rollbackErr != nil {
			return fmt.Errorf("%v; автоматический откат: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func refreshWallpaper(path string) error {
	safePath := strings.ReplaceAll(path, "'", "''")
	script := `Add-Type -TypeDefinition 'using System.Runtime.InteropServices; public class WallpaperNative { [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern bool SystemParametersInfo(int a,int b,string c,int d); }'; if (-not [WallpaperNative]::SystemParametersInfo(20,0,'` + safePath + `',3)) { exit 1 }`
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", cleanOutput(out))
	}
	return nil
}

func (a *app) handleExport(w http.ResponseWriter, _ *http.Request) {
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
	if typ == "REG_DWORD" {
		ai, ea := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "0x"), 16, 64)
		bi, eb := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(b)), "0x"), 16, 64)
		if ea == nil && eb == nil {
			return ai == bi
		}
		ad, ea := strconv.ParseUint(strings.TrimSpace(a), 10, 64)
		bd, eb := strconv.ParseUint(strings.TrimSpace(b), 10, 64)
		return ea == nil && eb == nil && ad == bd
	}
	return a == b
}

func regGet(path, name string) (typ, data string, exists bool, err error) {
	args := []string{"query", path}
	if name == "" {
		args = append(args, "/ve")
	} else {
		args = append(args, "/v", name)
	}
	out, cmdErr := exec.Command("reg.exe", args...).CombinedOutput()
	if cmdErr != nil {
		return "", "", false, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		index := strings.Index(line, "REG_")
		if index < 0 {
			continue
		}
		rest := line[index:]
		end := strings.IndexAny(rest, " \t\r")
		if end < 0 {
			return rest, "", true, nil
		}
		return rest[:end], strings.TrimSpace(rest[end:]), true, nil
	}
	return "", "", false, nil
}

func regSet(path, name, typ, data string) error {
	args := []string{"add", path}
	if name == "" {
		args = append(args, "/ve")
	} else {
		args = append(args, "/v", name)
	}
	args = append(args, "/t", typ, "/d", data, "/f")
	out, err := exec.Command("reg.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", cleanOutput(out))
	}
	return nil
}

func regDelete(path, name string) error {
	_, _, exists, queryErr := regGet(path, name)
	if queryErr != nil {
		return queryErr
	}
	if !exists {
		return nil
	}
	args := []string{"delete", path}
	if name == "" {
		args = append(args, "/ve")
	} else {
		args = append(args, "/v", name)
	}
	args = append(args, "/f")
	out, err := exec.Command("reg.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", cleanOutput(out))
	}
	return nil
}

func findTweak(id string) (*tweak, bool) {
	for i := range tweaks {
		if tweaks[i].ID == id {
			return &tweaks[i], true
		}
	}
	return nil, false
}

func isAdmin() bool { return exec.Command("net.exe", "session").Run() == nil }

func windowsInfo() string {
	out, err := exec.Command("cmd.exe", "/c", "ver").CombinedOutput()
	if err != nil {
		return "Windows 11"
	}
	return strings.TrimSpace(string(out))
}

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
