// `bee doctor` — read-only preflight check.
//
// Reports environment health: bee dirs, sandbox helper, configured
// providers' creds, active config/profile/caveman level. Prints a
// human-readable table by default; --json emits one machine record.
//
// Pure-Go subcommand-dispatch model — no shim sprays to check.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"
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
