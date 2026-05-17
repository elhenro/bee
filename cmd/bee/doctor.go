// `bee doctor` — read-only preflight check.
//
// Reports environment health: bee dirs, sandbox helper, configured
// providers' creds, active config/profile/caveman level. Prints a
// human-readable table by default; --json emits one machine record.
//
// Pure-Go subcommand-dispatch model — no shim sprays to check.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
)

// doctorHTTPClient is overridable from tests via setDoctorHTTPClient. The
// ollama checks share it so a single httptest.Server can mock both /api/tags
// and /api/show.
var doctorHTTPClient = &http.Client{Timeout: 2 * time.Second}

func setDoctorHTTPClient(c *http.Client) func() {
	prev := doctorHTTPClient
	doctorHTTPClient = c
	return func() { doctorHTTPClient = prev }
}

type check struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // ok | warn | fail | info
	Detail  string `json:"detail"`
	Remedy  string `json:"remedy,omitempty"`
}

type report struct {
	Version string  `json:"version"`
	OS      string  `json:"os"`
	Arch    string  `json:"arch"`
	Checks  []check `json:"checks"`
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit a single JSON record instead of a table")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	rep := report{Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	rep.Checks = append(rep.Checks, checkBeeBinary())
	rep.Checks = append(rep.Checks, checkBeeHome())
	rep.Checks = append(rep.Checks, checkSkillsDir())
	rep.Checks = append(rep.Checks, checkSessionsDir())
	rep.Checks = append(rep.Checks, checkKnowledgeStore())
	rep.Checks = append(rep.Checks, checkSandboxHelper())
	cfgChecks, cfg := checkConfigLoaded()
	rep.Checks = append(rep.Checks, cfgChecks...)
	rep.Checks = append(rep.Checks, checkOllama(cfg)...)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "bee doctor: encode:", err)
			os.Exit(1)
		}
		os.Exit(exitCodeFor(rep.Checks))
	}

	printTable(rep)
	os.Exit(exitCodeFor(rep.Checks))
}

func checkBeeBinary() check {
	self, err := os.Executable()
	if err != nil {
		return check{"bee binary", "warn", "cannot resolve path", ""}
	}
	return check{"bee binary", "ok", self, ""}
}

func checkBeeHome() check {
	home, err := os.UserHomeDir()
	if err != nil {
		return check{"~/.bee", "fail", "no HOME", "set $HOME"}
	}
	dir := filepath.Join(home, ".bee")
	if v := os.Getenv("BEE_HOME"); v != "" {
		dir = v
	}
	info, err := os.Stat(dir)
	if err != nil {
		return check{"~/.bee", "warn", "missing — created on first run", "run `bee` once"}
	}
	if !info.IsDir() {
		return check{"~/.bee", "fail", dir + " is not a directory", "remove and re-run bee"}
	}
	return check{"~/.bee", "ok", dir, ""}
}

func checkSkillsDir() check {
	dir := skills.BaseDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return check{"skills dir", "warn", dir + " missing", "run `bee` to seed bundled skills"}
	}
	return check{"skills dir", "ok", fmt.Sprintf("%s (%d entries)", dir, len(entries)), ""}
}

func checkSessionsDir() check {
	id, err := session.Path("00000000-0000-0000-0000-000000000000")
	if err != nil {
		return check{"sessions dir", "warn", err.Error(), ""}
	}
	dir := filepath.Dir(id)
	if _, err := os.Stat(dir); err != nil {
		return check{"sessions dir", "warn", dir + " missing", "first run creates it"}
	}
	return check{"sessions dir", "ok", dir, ""}
}

func checkKnowledgeStore() check {
	dir, err := knowledge.StoreDir()
	if err != nil || dir == "" {
		return check{"knowledge store", "info", "not configured", ""}
	}
	if _, err := os.Stat(dir); err != nil {
		return check{"knowledge store", "info", dir + " missing (no records yet)", ""}
	}
	return check{"knowledge store", "ok", dir, ""}
}

func checkSandboxHelper() check {
	switch runtime.GOOS {
	case "darwin":
		if p, err := exec.LookPath("sandbox-exec"); err == nil {
			return check{"sandbox helper", "ok", p, ""}
		}
		return check{"sandbox helper", "warn", "sandbox-exec missing", "install Xcode CLT or accept degraded sandbox"}
	case "linux":
		if p, err := exec.LookPath("bwrap"); err == nil {
			return check{"sandbox helper", "ok", p, ""}
		}
		return check{"sandbox helper", "warn", "bwrap missing", "apt install bubblewrap (or equivalent)"}
	case "windows":
		return check{"sandbox helper", "info", "windows uses WSL2 stub", ""}
	default:
		return check{"sandbox helper", "info", runtime.GOOS + " unsupported", ""}
	}
}

func checkConfig() []check {
	cs, _ := checkConfigLoaded()
	return cs
}

// checkConfigLoaded mirrors checkConfig but also returns the cfg it loaded
// so downstream checks (ollama) don't need a second Load() call.
func checkConfigLoaded() ([]check, config.Config) {
	out := []check{}
	cfg, err := config.Load()
	if err != nil {
		// load failures are not always fatal — keys missing is common.
		out = append(out, check{"config", "warn", err.Error(), "set the env var or edit ~/.bee/config.toml"})
		// fall back to defaults to still report profile/caveman.
		cfg = config.Defaults()
	} else {
		out = append(out, check{"config", "ok", config.ConfigPath(), ""})
	}
	out = append(out, check{"profile", "info", cfg.Profile, ""})
	out = append(out, check{"caveman", "info", cfg.Caveman, ""})
	out = append(out, check{"default provider", "info", cfg.DefaultProvider, ""})
	out = append(out, check{"default model", "info", cfg.DefaultModel, ""})

	// per-provider creds: scan all configured, not just the default. Local
	// providers with no EnvKey are always "ok".
	for name, p := range cfg.Providers {
		label := "provider:" + name
		if p.EnvKey == "" {
			out = append(out, check{label, "ok", "no key required", ""})
			continue
		}
		if os.Getenv(p.EnvKey) == "" {
			out = append(out, check{label, "warn", p.EnvKey + " not set", "export " + p.EnvKey + "=…"})
		} else {
			out = append(out, check{label, "ok", p.EnvKey + " set", ""})
		}
	}
	return out, cfg
}

// checkOllama only runs when ollama is the active provider. Daemon down +
// model-not-pulled are WARN — bee should still work with other providers
// configured. A successful tags fetch also probes /api/show so we surface
// the real num_ctx (rather than the misleading fallback).
func checkOllama(cfg config.Config) []check {
	if cfg.DefaultProvider != "ollama" {
		return nil
	}
	p, ok := cfg.Providers["ollama"]
	if !ok {
		return []check{{Name: "ollama", Status: "warn", Detail: "provider config missing"}}
	}

	base := llm.OllamaBaseURL(p.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return []check{{Name: "ollama", Status: "warn", Detail: "build request: " + err.Error()}}
	}
	resp, err := doctorHTTPClient.Do(req)
	if err != nil {
		return []check{{
			Name:   "ollama",
			Status: "warn",
			Detail: "daemon not responding at " + base,
			Remedy: "start ollama (`ollama serve`) or remove ollama as default provider",
		}}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return []check{{
			Name:   "ollama",
			Status: "warn",
			Detail: fmt.Sprintf("daemon returned %d at %s/api/tags", resp.StatusCode, base),
		}}
	}

	var tags struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return []check{{Name: "ollama", Status: "warn", Detail: "decode tags: " + err.Error()}}
	}

	if !hasOllamaModel(tags.Models, cfg.DefaultModel) {
		return []check{{
			Name:   "ollama",
			Status: "warn",
			Detail: fmt.Sprintf("model %s not pulled", cfg.DefaultModel),
			Remedy: "ollama pull " + cfg.DefaultModel,
		}}
	}

	// model present — probe num_ctx for the OK detail and warm the cache so
	// the loop's contextBudget reflects reality from the first turn.
	pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pcancel()
	n, _ := llm.ProbeOllamaContext(pctx, doctorHTTPClient, p.BaseURL, cfg.DefaultModel)
	if n > 0 {
		llm.RememberContextLength(cfg.DefaultModel, n)
		return []check{{
			Name:   "ollama",
			Status: "ok",
			Detail: fmt.Sprintf("model pulled, num_ctx=%d", n),
		}}
	}
	return []check{{
		Name:   "ollama",
		Status: "ok",
		Detail: "model pulled (num_ctx unknown)",
	}}
}

func hasOllamaModel(models []struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}, want string) bool {
	for _, m := range models {
		if m.Name == want || m.Model == want {
			return true
		}
	}
	return false
}

func printTable(rep report) {
	fmt.Printf("bee %s — %s/%s\n\n", rep.Version, rep.OS, rep.Arch)
	nameW := 8
	for _, c := range rep.Checks {
		if n := len(c.Name); n > nameW {
			nameW = n
		}
	}
	for _, c := range rep.Checks {
		sym := statusSym(c.Status)
		fmt.Printf("  %s  %-*s  %s\n", sym, nameW, c.Name, c.Detail)
		if c.Remedy != "" {
			fmt.Printf("     %-*s  → %s\n", nameW, "", c.Remedy)
		}
	}
	fmt.Println()
	fail, warn, _, _ := tally(rep.Checks)
	fmt.Printf("%d fail, %d warn\n", fail, warn)
}

func statusSym(s string) string {
	switch s {
	case "ok":
		return "✓"
	case "warn":
		return "!"
	case "fail":
		return "✗"
	default:
		return "·"
	}
}

func tally(cs []check) (fail, warn, ok, info int) {
	for _, c := range cs {
		switch c.Status {
		case "fail":
			fail++
		case "warn":
			warn++
		case "ok":
			ok++
		default:
			info++
		}
	}
	return
}

// exitCodeFor: any fail = 1, otherwise 0. warns don't fail CI.
func exitCodeFor(cs []check) int {
	for _, c := range cs {
		if c.Status == "fail" {
			return 1
		}
	}
	return 0
}
