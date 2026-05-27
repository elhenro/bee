// Local environment probes for `bee doctor`: bee dirs, sandbox helper,
// loaded config + per-provider credentials. Pure read-only.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
)

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
	// Resolve the auth dir once; probe failures are best-effort (warn, don't crash).
	authDir, authErr := auth.DefaultDir()
	for name, p := range cfg.Providers {
		label := "provider:" + name
		if p.EnvKey == "" {
			out = append(out, check{label, "ok", "no key required", ""})
			continue
		}
		if os.Getenv(p.EnvKey) != "" {
			out = append(out, check{label, "ok", p.EnvKey + " set", ""})
		} else if authErr == nil && auth.HasAPIKey(authDir, name) {
			out = append(out, check{label, "ok", "stored key", ""})
		} else {
			out = append(out, check{label, "warn", p.EnvKey + " not set", "export " + p.EnvKey + "=…"})
		}
	}
	return out, cfg
}
