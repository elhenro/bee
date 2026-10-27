package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func writeCfg(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readCfg(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPersistSetting_KeepsCommentsAndOrder(t *testing.T) {
	initial := `# my bee config
default_provider = "openrouter"   # daily driver
verbose = false # noisy
zeta = 1

# sandbox notes
[sandbox]
scope = "read-only" # keep
`
	path := writeCfg(t, initial)
	if err := PersistSetting(path, "verbose", true); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(initial, "verbose = false # noisy", "verbose = true # noisy", 1)
	if got := readCfg(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPersistPick_KeepsCommentsAndQuotedHash(t *testing.T) {
	initial := `default_provider = "old#1" # pick
default_model = 'old-m'    # model
`
	path := writeCfg(t, initial)
	if err := PersistPick(path, "openrouter", "x/y"); err != nil {
		t.Fatal(err)
	}
	got := readCfg(t, path)
	for _, want := range []string{"# pick", "# model", "openrouter", "x/y"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "old") {
		t.Errorf("old values survived:\n%s", got)
	}
	var tree map[string]any
	if err := toml.Unmarshal([]byte(got), &tree); err != nil || tree["default_model"] != "x/y" {
		t.Errorf("parse = %v, %v", tree, err)
	}
}

func TestPersistSetting_InsertsBeforeFirstTable(t *testing.T) {
	cases := map[string]struct{ initial, want string }{
		"empty":                {"", "yolo = true\n"},
		"no table, no newline": {"a = 1", "a = 1\nyolo = true\n"},
		"before table with comment": {
			"a = 1\n\n# the sandbox\n[sandbox]\nscope = 'x'\n",
			"a = 1\nyolo = true\n\n# the sandbox\n[sandbox]\nscope = 'x'\n",
		},
		"same key inside a table is not the top-level key": {
			"[tui]\nyolo = false\n",
			"yolo = true\n[tui]\nyolo = false\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeCfg(t, c.initial)
			if err := PersistSetting(path, "yolo", true); err != nil {
				t.Fatal(err)
			}
			if got := readCfg(t, path); got != c.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}

func TestPersistSetting_ArrayFallsBackToTree(t *testing.T) {
	path := writeCfg(t, "a = 1\ndisabled_tools = [\n  'x',\n  'y',\n]\n")
	if err := PersistSetting(path, "disabled_tools", []string{"bash"}); err != nil {
		t.Fatal(err)
	}
	var tree struct {
		A     int      `toml:"a"`
		Tools []string `toml:"disabled_tools"`
	}
	if err := toml.Unmarshal([]byte(readCfg(t, path)), &tree); err != nil || tree.A != 1 || len(tree.Tools) != 1 || tree.Tools[0] != "bash" {
		t.Errorf("tree = %+v, %v", tree, err)
	}
}

func TestPersistSetting_MultilineExistingValueFallsBack(t *testing.T) {
	path := writeCfg(t, "note = \"\"\"\nline\n\"\"\"\nverbose = false\n")
	if err := PersistSetting(path, "note", "short"); err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := toml.Unmarshal([]byte(readCfg(t, path)), &tree); err != nil || tree["note"] != "short" || tree["verbose"] != false {
		t.Errorf("tree = %v, %v", tree, err)
	}
}

// a quoted spelling of the key is invisible to the line editor; inserting a
// second copy would be invalid toml, so the tree path must take over.
func TestPersistSetting_QuotedKeyNotDuplicated(t *testing.T) {
	path := writeCfg(t, "\"verbose\" = false\n")
	if err := PersistSetting(path, "verbose", true); err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := toml.Unmarshal([]byte(readCfg(t, path)), &tree); err != nil || tree["verbose"] != true {
		t.Errorf("tree = %v, %v", tree, err)
	}
}

func TestPersistSetting_UnparseableLeavesFileAlone(t *testing.T) {
	const broken = "this is = = not toml\n"
	path := writeCfg(t, broken)
	if err := PersistSetting(path, "verbose", true); err == nil {
		t.Fatal("want parse error")
	}
	if got := readCfg(t, path); got != broken {
		t.Errorf("file changed: %q", got)
	}
}

func TestPersistSetting_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows reports 0666 for every writable file")
	}
	path := writeCfg(t, "a = 1\n") // 0644
	if err := PersistSetting(path, "b", 2); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("existing mode changed to %v", fi.Mode().Perm())
	}
	fresh := filepath.Join(t.TempDir(), "sub", "config.toml")
	if err := PersistSetting(fresh, "b", 2); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(fresh); fi.Mode().Perm() != 0o600 {
		t.Errorf("new file mode = %v, want 0600", fi.Mode().Perm())
	}
}

// parallel writers each add their own key; the lock keeps every one of them.
func TestPersistSetting_ConcurrentWritersKeepAllKeys(t *testing.T) {
	path := writeCfg(t, "# keep me\n")
	const n = 16
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := PersistSetting(path, fmt.Sprintf("k%02d", i), i); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got := readCfg(t, path)
	var tree map[string]any
	if err := toml.Unmarshal([]byte(got), &tree); err != nil || len(tree) != n {
		t.Errorf("want %d keys, got %d (%v)\n%s", n, len(tree), err, got)
	}
	if !strings.HasPrefix(got, "# keep me\n") {
		t.Errorf("comment lost:\n%s", got)
	}
}
