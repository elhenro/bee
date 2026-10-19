package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
)

// alt+y must persist a TOML bool: a string value makes config.Load fail and
// bee refuse to start.
func TestAltY_PersistsBoolSoConfigStillLoads(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "x")
	m := NewModel(nil, "/tmp/work", "m", "workspace-write", caveman.Default)
	for i := 0; i < 2; i++ {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y"), Alt: true})
		m = nm.(Model)
		if _, err := config.Load(); err != nil {
			b, _ := os.ReadFile(config.ConfigPath())
			t.Fatalf("press %d: config no longer loads: %v\n%s", i+1, err, b)
		}
	}
}

// no PersistSetting call site may hand it a stringified bool/number.
func TestPersistSetting_NoStringifiedScalars(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 3 {
				return true
			}
			if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "PersistSetting" {
				return true
			}
			inner, ok := call.Args[2].(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := inner.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Sprintf", "Sprint", "FormatBool", "Itoa", "FormatInt":
					t.Errorf("%s: PersistSetting value is stringified via %s; pass the typed value", fset.Position(call.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
}
