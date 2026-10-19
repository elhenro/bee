package safety

import (
	"path"
	"strings"
)

// trustedBinDirs are the only absolute locations a command word may name
// without being treated as an unknown program.
var trustedBinDirs = map[string]bool{
	"/bin": true, "/usr/bin": true, "/usr/local/bin": true, "/opt/homebrew/bin": true,
}

// PlainSegments splits a command line into simple-command argv slices. It
// reports ok=false for anything that is not a plain sequence of commands
// joined by && || ; |: redirections, background `&`, heredocs, command or
// process substitution, newlines, or an unterminated quote. `bash -c "..."`
// style wrappers are unwrapped so their script is judged as well.
func PlainSegments(cmd string) (segs [][]string, ok bool) {
	return plainSegments(cmd, 2)
}

func plainSegments(cmd string, depth int) ([][]string, bool) {
	if strings.ContainsAny(cmd, "\n\r") {
		return nil, false // the tokenizer treats newlines as spaces; they separate commands
	}
	toks, err := Tokenize(cmd)
	if err != nil || len(toks) == 0 {
		return nil, false
	}
	var segs [][]string
	var cur []string
	flush := func() bool {
		if len(cur) == 0 {
			return false
		}
		segs, cur = append(segs, cur), nil
		return true
	}
	for _, t := range toks {
		if t.Kind == TokenOp {
			switch t.Value {
			case "&&", "||", ";", "|":
				if !flush() {
					return nil, false
				}
				continue
			}
			return nil, false
		}
		if hasSubstitution(t) || hasDollarQuote(t) || hasHiddenExpansion(t) {
			return nil, false
		}
		cur = append(cur, t.Value)
	}
	if !flush() {
		return nil, false // trailing operator
	}
	return unwrapShells(segs, depth)
}

// hasSubstitution reports command/process substitution in any context where
// the shell would still expand it (everything except single quotes).
func hasSubstitution(t Token) bool {
	for _, s := range t.Segments {
		if s.Quote == QuoteSingle {
			continue
		}
		if strings.ContainsAny(s.Text, "`") || strings.Contains(s.Text, "$(") ||
			strings.Contains(s.Text, "<(") || strings.Contains(s.Text, ">(") {
			return true
		}
	}
	return false
}

// hasDollarQuote flags $'...' and $\"...\" words. The shell decodes escapes
// inside them (a literal \\n becomes a newline), which the tokenizer does
// not, so the decoded text could hide extra commands from the checks here.
func hasDollarQuote(t Token) bool {
	return strings.Contains(t.Raw, "$'") || strings.Contains(t.Raw, `$"`)
}

// unwrapShells replaces `sh|bash|zsh -c|-lc <script>` segments with the
// segments of their script, so the wrapped commands are what gets judged.
func unwrapShells(segs [][]string, depth int) ([][]string, bool) {
	var out [][]string
	for _, argv := range segs {
		if len(argv) == 3 && isShell(argv[0]) && (argv[1] == "-c" || argv[1] == "-lc") {
			if depth <= 0 {
				return nil, false
			}
			inner, ok := plainSegments(argv[2], depth-1)
			if !ok {
				return nil, false
			}
			out = append(out, inner...)
			continue
		}
		out = append(out, argv)
	}
	return out, true
}

func isShell(w string) bool {
	switch CommandName(w) {
	case "sh", "bash", "zsh":
		return true
	}
	return false
}

// CommandName resolves a command word to the program name used for
// allow-list matching. Bare names pass through; absolute paths resolve only
// inside trusted bin dirs. Anything else (./x, ~/x, $VAR) returns "" so a
// local script named like a safe tool is never mistaken for it.
func CommandName(word string) string {
	if word == "" || strings.ContainsAny(word, "$~") {
		return ""
	}
	if !strings.Contains(word, "/") {
		return word
	}
	// shell words are slash paths on every host, so path not filepath:
	// filepath treats "/bin/ls" as relative on windows
	if path.IsAbs(word) && trustedBinDirs[path.Dir(word)] {
		return path.Base(word)
	}
	return ""
}

// unsafeFlags lists per-command flags that turn a read-only tool into a
// writer or executor. A leading "git <sub>" style key matches the first two
// words.
var unsafeFlags = map[string][]string{
	"find": {"-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint", "-fprint0", "-fprintf", "-fls"},
	"fd":   {"-x", "--exec", "-X", "--exec-batch"},
	"rg":   {"--pre", "--hostname-bin"},
	"tree": {"-o"},
	"date": {"-s", "--set"},
	"file": {"-C", "--compile"},
	"git":  {"--output", "--ext-diff", "--exec-path"},
}

// gitBranchReadFlags is the closed set of `git branch` flags that only list.
var gitBranchReadFlags = map[string]bool{
	"-a": true, "-r": true, "-v": true, "-vv": true, "--all": true, "--remotes": true,
	"--verbose": true, "--list": true, "-l": true, "--show-current": true,
}

// UnsafeInvocation reports whether argv uses a flag or form that makes an
// otherwise read-only command mutate state or run other programs.
func UnsafeInvocation(argv []string) bool {
	if len(argv) == 0 {
		return true
	}
	name := CommandName(argv[0])
	if name == "" {
		return true
	}
	args := argv[1:]
	if name == "go" && !goArgsSafe(args) {
		return true
	}
	if name == "git" && len(args) > 0 {
		switch args[0] {
		case "branch":
			for _, a := range args[1:] {
				if !gitBranchReadFlags[a] {
					return true
				}
			}
		case "remote":
			for _, a := range args[1:] {
				if a != "-v" && a != "--verbose" {
					return true
				}
			}
		}
	}
	for _, bad := range unsafeFlags[name] {
		for _, a := range args {
			if matchesUnsafeFlag(name, bad, a) {
				return true
			}
		}
	}
	return false
}

// matchesUnsafeFlag reports whether arg could select the denied flag bad.
// It is deliberately wider than equality: long options may be abbreviated
// (--out for --output) and short ones clustered or glued to a value
// (-ao, -ofile). A false positive only costs an approval prompt.
func matchesUnsafeFlag(cmd, bad, arg string) bool {
	if arg == bad {
		return true
	}
	if strings.HasPrefix(bad, "--") {
		name, _, _ := strings.Cut(arg, "=")
		return len(name) >= 3 && strings.HasPrefix(name, "--") && strings.HasPrefix(bad, name)
	}
	if len(bad) == 2 && strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
		return strings.ContainsRune(arg[1:], rune(bad[1]))
	}
	return false
}

// goSafeFlags is a closed set of flags per go subcommand. go's flag space is
// too large to denylist (-test.* aliases, -ldflags, -C, -vettool, ...), so
// any flag not listed here makes the invocation unsafe. Non-flag arguments
// (packages, patterns, flag values) are not policed.
var goSafeFlags = map[string][]string{
	"build":    {"v", "race", "tags", "p"},
	"test":     {"v", "race", "tags", "p", "run", "count", "short", "timeout", "json", "failfast", "shuffle", "parallel", "cover", "bench", "benchmem", "benchtime"},
	"vet":      {"v", "tags", "p"},
	"list":     {"f", "json", "m", "e", "deps", "test", "find", "tags"},
	"env":      {"json"},
	"version":  {"m", "v"},
	"download": {"json"}, // go mod download
	"verify":   nil,      // go mod verify
}

// goArgsSafe reports whether every flag in a go command line is on the
// subcommand's allow set. Unknown subcommands allow no flags at all, so a
// user allow-list entry like "go run" cannot smuggle -toolexec through.
func goArgsSafe(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if strings.HasPrefix(args[0], "-") {
		return false // go -C dir ...: global flags before the subcommand
	}
	sub, rest := args[0], args[1:]
	if sub == "mod" {
		if len(rest) == 0 {
			return true
		}
		sub, rest = rest[0], rest[1:]
		if sub != "download" && sub != "verify" {
			sub = "mod " + sub // never in goSafeFlags: no flags allowed
		}
	}
	allowed := goSafeFlags[sub] // unknown subcommands get an empty set: no flags
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") || a == "-" {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		ok := false
		for _, f := range allowed {
			if f == name {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
