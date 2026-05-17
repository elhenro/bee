package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

func TestLoadKeyMap_NoFile(t *testing.T) {
	dir := t.TempDir()
	km := LoadKeyMap(dir)
	def := DefaultKeyMap()
	if !sameBinding(km.Submit, def.Submit) {
		t.Errorf("expected default Submit binding, got %v", km.Submit.Keys())
	}
}

func TestLoadKeyMap_Overrides(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"Submit":["enter","ctrl+m"],"SessionTree":["ctrl+r"]}`
	if err := os.WriteFile(filepath.Join(dir, "keybindings.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	km := LoadKeyMap(dir)
	if !hasKey(km.Submit, "ctrl+m") {
		t.Errorf("expected ctrl+m on Submit, got %v", km.Submit.Keys())
	}
	if !hasKey(km.SessionTree, "ctrl+r") {
		t.Errorf("expected ctrl+r on SessionTree, got %v", km.SessionTree.Keys())
	}
}

func TestLoadKeyMap_BadJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keybindings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	km := LoadKeyMap(dir)
	def := DefaultKeyMap()
	if !sameBinding(km.Submit, def.Submit) {
		t.Errorf("expected default on bad json, got %v", km.Submit.Keys())
	}
}

func TestLoadKeyMap_UnknownField(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"Bogus":["x"]}`
	if err := os.WriteFile(filepath.Join(dir, "keybindings.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// must not panic; defaults are preserved
	km := LoadKeyMap(dir)
	def := DefaultKeyMap()
	if !sameBinding(km.Submit, def.Submit) {
		t.Errorf("unexpected mutation from unknown field")
	}
}

func TestLoadKeyMap_EmptyHome(t *testing.T) {
	km := LoadKeyMap("")
	def := DefaultKeyMap()
	if !sameBinding(km.Submit, def.Submit) {
		t.Errorf("expected defaults when home is empty")
	}
}

// helpers
func sameBinding(a, b key.Binding) bool {
	aks := a.Keys()
	bks := b.Keys()
	if len(aks) != len(bks) {
		return false
	}
	for i := range aks {
		if aks[i] != bks[i] {
			return false
		}
	}
	return true
}

func hasKey(b key.Binding, want string) bool {
	for _, k := range b.Keys() {
		if k == want {
			return true
		}
	}
	return false
}
