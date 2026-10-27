package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

// every flag's config pointer must be the field whose toml tag is the key it
// persists under, and no two flags may share a model field.
func TestBoolSettings_WiringMatchesKeys(t *testing.T) {
	seen := map[*bool]string{}
	for _, f := range boolSettings {
		m, c := &Model{}, &config.Config{}
		mp, cp := f.ptrs(m, c)
		if prev, dup := seen[mp]; dup {
			t.Errorf("flag %s reuses model field of %s", f.key, prev)
		}
		seen[mp] = f.key
		rv := reflect.ValueOf(c).Elem()
		found := ""
		for i := 0; i < rv.NumField(); i++ {
			if rv.Field(i).Kind() == reflect.Bool && rv.Field(i).Addr().Interface().(*bool) == cp {
				found, _, _ = strings.Cut(rv.Type().Field(i).Tag.Get("toml"), ",")
			}
		}
		if found != f.key {
			t.Errorf("flag %s writes cfg field with toml key %q", f.key, found)
		}
	}
}

func TestBoolSettings_SetGetRoundTrip(t *testing.T) {
	t.Setenv("BEE_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	eng := &loop.Engine{Cfg: config.Defaults()}
	s := &tuiSide{m: &Model{eng: eng}}
	for _, f := range boolSettings {
		for _, v := range []bool{true, false} {
			if err := s.setFlag(f, v); err != nil {
				t.Fatalf("%s: %v", f.key, err)
			}
			if got := s.getFlag(f); got != v {
				t.Errorf("%s: get = %v after set %v", f.key, got, v)
			}
			_, cp := f.ptrs(s.m, &eng.Cfg)
			if *cp != v {
				t.Errorf("%s: engine config = %v after set %v", f.key, *cp, v)
			}
		}
	}
}

func TestBoolSettings_NoTUI(t *testing.T) {
	s := &tuiSide{}
	for _, f := range boolSettings {
		if s.getFlag(f) != f.def {
			t.Errorf("%s: get without tui != default", f.key)
		}
		if err := s.setFlag(f, true); err == nil {
			t.Errorf("%s: set without tui must error", f.key)
		}
	}
}

// the settings pane rows and the bool table must list the same keys.
func TestBoolSettings_MatchSettingsRows(t *testing.T) {
	rows := map[string]bool{}
	for _, r := range settingsRows {
		if !r.isInt {
			rows[r.key] = true
		}
	}
	for _, f := range boolSettings {
		if !rows[f.key] {
			t.Errorf("flag %s has no settings row", f.key)
		}
		delete(rows, f.key)
	}
	for k := range rows {
		t.Errorf("settings row %s has no flag", k)
	}
}

// opening the pane snapshots every flag off the model; a toggle message flips
// the model field live and persists the key.
func TestSettingsPane_SnapshotAndToggle(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("BEE_CONFIG", cfgPath)
	m := NewModel(nil, "", "", "", caveman.Off)
	m.showNudges = true
	nm, _ := m.onOpenSettings(openSettingsMsg{})
	m = nm.(Model)
	for _, f := range boolSettings {
		if got := m.settingsPane.on[f.key]; got != f.value(&m) {
			t.Errorf("%s: pane %v, model %v", f.key, got, f.value(&m))
		}
	}
	if !m.settingsPane.on["show_nudges"] {
		t.Error("snapshot lost show_nudges")
	}
	nm, _ = m.onSettingsToggle(settingsToggleMsg{key: "show_nudges", value: false})
	m = nm.(Model)
	if m.showNudges {
		t.Error("toggle did not flip the model field")
	}
	if b, _ := os.ReadFile(cfgPath); !strings.Contains(string(b), "show_nudges = false") {
		t.Errorf("toggle not persisted: %q", b)
	}
}
