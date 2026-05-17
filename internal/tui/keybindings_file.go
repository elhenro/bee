package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"

	"github.com/charmbracelet/bubbles/key"
)

// LoadKeyMap returns DefaultKeyMap merged with overrides from
// <home>/keybindings.json. Missing or malformed file returns defaults silently.
// Unknown field names are ignored so users can pin to old configs safely.
func LoadKeyMap(home string) KeyMap {
	km := DefaultKeyMap()
	if home == "" {
		return km
	}
	p := filepath.Join(home, "keybindings.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return km
	}
	var overrides map[string][]string
	if err := json.Unmarshal(b, &overrides); err != nil {
		return km
	}

	v := reflect.ValueOf(&km).Elem()
	bindingType := reflect.TypeOf(key.Binding{})
	for name, keys := range overrides {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if f.Type() != bindingType {
			continue
		}
		if !f.CanSet() {
			continue
		}
		// preserve existing help description if present
		help := ""
		if existing, ok := f.Interface().(key.Binding); ok {
			help = existing.Help().Desc
		}
		newBinding := key.NewBinding(
			key.WithKeys(keys...),
			key.WithHelp(joinFirst(keys), help),
		)
		f.Set(reflect.ValueOf(newBinding))
	}
	return km
}

func joinFirst(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}
