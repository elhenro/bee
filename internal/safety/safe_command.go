package safety

import (
	"path/filepath"
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
		if hasSubstitution(t) {
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
	if filepath.IsAbs(word) && trustedBinDirs[filepath.Dir(word)] {
		return filepath.Base(word)
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
	"go":   {"-o", "-exec", "-toolexec", "-overlay", "-coverprofile", "-cpuprofile", "-memprofile", "-blockprofile", "-mutexprofile", "-trace"},
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
			if a == bad || (strings.HasPrefix(bad, "--") && strings.HasPrefix(a, bad+"=")) ||
				(name == "go" && strings.HasPrefix(a, bad+"=")) {
				return true
			}
		}
	}
	return false
}
