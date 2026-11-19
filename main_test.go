package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEqualRegistryDWORD(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{"0x0", "0"}, {"0x1", "1"}, {"0x00000002", "2"}, {"1", "0x1"}, {"0x10", "16"},
	} {
		if !equalRegistry(tc.got, tc.want, "REG_DWORD") {
			t.Fatalf("expected %q and %q to be equal", tc.got, tc.want)
		}
	}
	if equalRegistry("0x1", "0", "REG_DWORD") {
		t.Fatal("different DWORD values reported equal")
	}
	if equalRegistry("10", "16", "REG_DWORD") {
		t.Fatal("decimal values were compared as hexadecimal")
	}
}

func TestTweakIDsAreUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, tw := range tweaks {
		if tw.ID == "" || tw.Title == "" || tw.Category == "" || len(tw.Ops) == 0 {
			t.Fatalf("incomplete tweak: %+v", tw)
		}
		if seen[tw.ID] {
			t.Fatalf("duplicate tweak id %q", tw.ID)
		}
		seen[tw.ID] = true
		for _, op := range tw.Ops {
			if op.Path == "" || op.Type == "" {
				t.Fatalf("incomplete operation in %q", tw.ID)
			}
		}
	}
}

func TestFindTweak(t *testing.T) {
	if tw, ok := findTweak("show-extensions"); !ok || tw.Title == "" {
		t.Fatal("known tweak not found")
	}
	if _, ok := findTweak("does-not-exist"); ok {
		t.Fatal("unknown tweak found")
	}
}

func TestSettingsPagesUseAllowlistedWindowsURIs(t *testing.T) {
	if len(settingsPages) < 8 {
		t.Fatalf("expected a useful settings catalog, got %d entries", len(settingsPages))
	}
	seen := map[string]bool{}
	for id, uri := range settingsPages {
		if id == "" || !strings.HasPrefix(uri, "ms-settings:") {
			t.Fatalf("invalid settings page %q: %q", id, uri)
		}
		if seen[uri] {
			t.Fatalf("duplicate settings URI %q", uri)
		}
		seen[uri] = true
	}
}

func TestTweakCompatibility(t *testing.T) {
	if ok, _ := tweakCompatibility(tweak{}, 19045); ok {
		t.Fatal("Windows 10 build reported compatible")
	}
	item := tweak{MinBuild: 22621, MaxBuild: 26100}
	if ok, _ := tweakCompatibility(item, 22000); ok {
		t.Fatal("old build reported compatible")
	}
	if ok, reason := tweakCompatibility(item, 22631); !ok || reason != "" {
		t.Fatalf("supported build rejected: %s", reason)
	}
	if ok, _ := tweakCompatibility(item, 26200); ok {
		t.Fatal("newer unsupported build reported compatible")
	}
}

func TestValidateProfile(t *testing.T) {
	valid := profileFile{Schema: profileSchema, InterfaceTheme: "liquid", Tweaks: map[string]bool{"show-extensions": true}}
	if err := validateProfile(valid); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
	invalid := valid
	invalid.Tweaks = map[string]bool{"not-a-setting": true}
	if err := validateProfile(invalid); err == nil {
		t.Fatal("unknown setting accepted")
	}
}

func TestValidateThemeFile(t *testing.T) {
	if err := validateThemeFile(".theme", []byte("[Theme]\nDisplayName=Test")); err != nil {
		t.Fatalf("valid theme rejected: %v", err)
	}
	if err := validateThemeFile(".themepack", []byte("MSCFpayload")); err != nil {
		t.Fatalf("valid theme pack rejected: %v", err)
	}
	if err := validateThemeFile(".exe", []byte("MZ")); err == nil {
		t.Fatal("executable accepted as a theme")
	}
	if err := validateThemeFile(".theme", []byte("[Theme]\nWallpaper=https://example.com/a.jpg")); err == nil {
		t.Fatal("theme with remote resource accepted")
	}
}

func TestWallpaperRedirectPolicyRequiresHTTPSAllowlist(t *testing.T) {
	allowed := &http.Request{URL: &url.URL{Scheme: "https", Host: "images.unsplash.com", Path: "/photo"}}
	if err := wallpaperRedirectPolicy(allowed, nil); err != nil {
		t.Fatalf("allowed redirect rejected: %v", err)
	}
	for _, raw := range []string{"http://images.unsplash.com/photo", "https://example.com/photo"} {
		parsed, _ := url.Parse(raw)
		if err := wallpaperRedirectPolicy(&http.Request{URL: parsed}, nil); err == nil {
			t.Fatalf("unsafe redirect accepted: %s", raw)
		}
	}
}

func TestThemeOnlyBackupDoesNotSkipRestore(t *testing.T) {
	application := &app{
		backupPath: filepath.Join(t.TempDir(), "backup.json"),
		backup:     backupFile{Values: map[string]backupValue{}, ThemeCaptured: true, PreviousTheme: "previous.theme"},
		audit:      newAuditLogger(filepath.Join(t.TempDir(), "audit.jsonl")),
	}
	response := httptest.NewRecorder()
	application.handleRestore(response, httptest.NewRequest(http.MethodPost, "/api/restore", nil))
	if response.Code == http.StatusOK {
		t.Fatal("theme-only backup was incorrectly treated as empty")
	}
}

func TestAuditLoggerWritesJSONLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger := newAuditLogger(path)
	logger.write("test.event", "ok", map[string]any{"value": 1})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"event":"test.event"`) || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("unexpected audit entry: %s", data)
	}
}

func TestProtectedRoutesAndSettingsAllowlist(t *testing.T) {
	application := &app{audit: newAuditLogger(filepath.Join(t.TempDir(), "audit.jsonl"))}
	mux := http.NewServeMux()
	application.routes(mux, "secret")
	handler := securityHeaders(mux)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized request returned %d", unauthorized.Code)
	}
	if unauthorized.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing")
	}

	request := httptest.NewRequest(http.MethodPost, "/api/open-settings", strings.NewReader(`{"id":"not-allowed"}`))
	request.Header.Set("X-Customizer-Token", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown settings target returned %d", response.Code)
	}
}

func TestCorruptBackupIsPreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ProgramData", dir)
	appDir := filepath.Join(dir, "Win11Customizer")
	if err := os.MkdirAll(appDir, 0700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(appDir, "backup.json")
	if err := os.WriteFile(backupPath, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	application := newApp()
	if len(application.backup.Values) != 0 {
		t.Fatal("corrupt backup loaded as valid")
	}
	matches, err := filepath.Glob(backupPath + ".corrupt-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("corrupt backup was not preserved: %v, %v", matches, err)
	}
}
