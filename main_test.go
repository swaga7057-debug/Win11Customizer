package main

import (
	"strings"
	"testing"
)

func TestEqualRegistryDWORD(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{"0x0", "0"}, {"0x1", "1"}, {"0x00000002", "2"}, {"1", "0x1"},
	} {
		if !equalRegistry(tc.got, tc.want, "REG_DWORD") {
			t.Fatalf("expected %q and %q to be equal", tc.got, tc.want)
		}
	}
	if equalRegistry("0x1", "0", "REG_DWORD") {
		t.Fatal("different DWORD values reported equal")
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
