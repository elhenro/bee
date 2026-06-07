package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/arena"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
)

// runWars is the host-side referee: it builds the metering proxy, stands up two
// combatant containers on a private network, runs the match to a verdict, and
// records it. The heavy lifting lives in internal/arena.
func runWars(args []string) {
	fs := flag.NewFlagSet("wars", flag.ContinueOnError)
	redModel := fs.String("red-model", "", "model id for the red combatant (default: default_model)")
	blueModel := fs.String("blue-model", "", "model id for the blue combatant (default: default_model)")
	economy := fs.String("economy", "", "economy preset: scarcity|blitz|marathon (empty = normal)")
	vuln := fs.String("vuln", "", "override defender vuln module: cmdi|traversal")
	startTokens := fs.Int("start-tokens", 0, "override starting nectar per side")
	rounds := fs.Int("rounds", 0, "override round cap")
	sealed := fs.Bool("sealed", false, "airtight --internal combat net (needs a sibling model container)")
	proxyPort := fs.Int("proxy-port", 8800, "host port for the metering model proxy")
	proxyURL := fs.String("proxy-url", "http://host.docker.internal:8800", "container-reachable proxy base URL")
	image := fs.String("image", "bee-wars:latest", "combatant container image tag")
	build := fs.Bool("build", false, "docker build the image before the match")
	ledger := fs.String("ledger", "", "match ledger path (default ~/.bee/wars/ledger.jsonl)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Defaults()
	}

	// economy gears: preset, then per-flag overrides
	acfg := arena.DefaultConfig()
	if *economy != "" {
		p, ok := arena.Preset(*economy)
		if !ok {
			fmt.Fprintf(os.Stderr, "bee wars: unknown economy preset %q\n", *economy)
			os.Exit(2)
		}
		acfg = p
	}
	if *startTokens > 0 {
		acfg.StartTokens = *startTokens
	}
	if *rounds > 0 {
		acfg.Rounds = *rounds
	}
	if *vuln != "" {
		acfg.Vuln = *vuln
	}
	acfg.Sealed = *sealed

	// the proxy forwards to the configured OpenAI-compatible endpoint
	prov, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok || strings.TrimSpace(prov.BaseURL) == "" {
		fmt.Fprintf(os.Stderr, "bee wars: provider %q needs a base_url for the metering proxy\n", cfg.DefaultProvider)
		os.Exit(1)
	}
	upURL, err := url.Parse(prov.BaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee wars: bad provider base_url: %v\n", err)
		os.Exit(1)
	}

	redM := orDefault(*redModel, cfg.DefaultModel)
	blueM := orDefault(*blueModel, cfg.DefaultModel)
	startRed, startBlue := arena.HandicapStart(acfg.StartTokens, 1500, 1500)

	red := newCombatant("red", redM, startRed)
	blue := newCombatant("blue", blueM, startBlue)

	proxy := arena.NewMeteringProxy(
		&arena.Side{Name: "red", Upstream: upURL, Model: redM, Wallet: red.Wallet, Cost: acfg.Cost},
		&arena.Side{Name: "blue", Upstream: upURL, Model: blueM, Wallet: blue.Wallet, Cost: acfg.Cost},
	)
	go func() {
		_ = http.ListenAndServe(fmt.Sprintf(":%d", *proxyPort), proxy)
	}()

	rt, err := arena.NewDockerRuntime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee wars: %v\n", err)
		os.Exit(1)
	}
	if *build {
		fmt.Fprintln(os.Stderr, "bee wars: building image...")
		cmd := exec.Command("docker", "build", "-f", "Dockerfile.wars", "-t", *image, ".")
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "bee wars: build: %v\n", err)
			os.Exit(1)
		}
	}

	matchID := uuid.NewString()[:8]
	m := arena.NewMatch(matchID, red, blue, rt, proxy, *proxyURL, *image, acfg)

	fmt.Fprintf(os.Stderr, "bee wars: match %s — red(%s) vs blue(%s), %s economy, sealed=%v\n",
		matchID, redM, blueM, acfg.Economy, acfg.Sealed)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	res, err := m.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee wars: %v\n", err)
	}

	fmt.Printf("winner: %s (%s) — red %d / blue %d nectar left\n",
		res.Winner, res.Reason, res.RedBalance, res.BlueBalance)

	if err := arena.AppendResult(warsLedgerPath(*ledger), res); err != nil {
		fmt.Fprintf(os.Stderr, "bee wars: ledger: %v\n", err)
	}
	if storeDir, derr := knowledge.StoreDir(); derr == nil {
		if _, werr := arena.WritePostMortem(filepath.Join(storeDir, "wars"), res); werr != nil {
			fmt.Fprintf(os.Stderr, "bee wars: post-mortem: %v\n", werr)
		}
	}
}

// newCombatant builds one side with a fresh wallet and a random canary secret.
func newCombatant(name, model string, start int) *arena.Combatant {
	secret := "FLAG{" + uuid.NewString() + "}"
	sum := sha256.Sum256([]byte(secret))
	return &arena.Combatant{
		Name: name, Model: model,
		Wallet:     arena.NewWallet(start),
		Secret:     secret,
		SecretHash: hex.EncodeToString(sum[:]),
		Elo:        1500,
	}
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func warsLedgerPath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "wars-ledger.jsonl"
	}
	return filepath.Join(home, ".bee", "wars", "ledger.jsonl")
}
