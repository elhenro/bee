package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/loop"
)

var allFlags = []boolSetting{
	flagVerbose, flagShowThoughts, flagShowNudges, flagShowRecap, flagCompact,
	flagShowContextBar, flagShowCallDetail, flagHighlight, flagShellBangSilent,
	flagShowBanner, flagShowLoader, flagShowBee, flagShowContextPct, flagShowModel,
	flagShowCwd, flagShowEffort, flagShowTurnTimer, flagShowGitBranch,
	flagShowTotalTokens, flagShowLoaderIn, flagShowLoaderOut, flagShowLoaderRate,
}

// every flag's config pointer must be the field whose toml tag is the key it
// persists under, and no two flags may share a model field.
func TestBoolSettings_WiringMatchesKeys(t *testing.T) {
	seen := map[*bool]string{}
	for _, f := range allFlags {
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
	for _, f := range allFlags {
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
	for _, f := range allFlags {
		if s.getFlag(f) != f.def {
			t.Errorf("%s: get without tui != default", f.key)
		}
		if err := s.setFlag(f, true); err == nil {
			t.Errorf("%s: set without tui must error", f.key)
		}
	}
}
