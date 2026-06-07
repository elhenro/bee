package safety

import "regexp"

// DangerousPattern flags commands that should prompt the user before running.
// Distinct from hardline rules in shell.go which refuse outright. Match →
// "ask first", not "deny".
type DangerousPattern struct {
	Re   *regexp.Regexp
	Key  string // stable id for session cache + persistent allowlist
	Desc string // shown to user when prompting
}

var dangerousPatterns = []DangerousPattern{
	// filesystem
	{regexp.MustCompile(`\brm\s+(-[^\s]*\s+)*(-[^\s]*r|--recursive\b)`), "rm-recursive", "recursive delete"},
	{regexp.MustCompile(`\bchmod\s+(-[^\s]*\s+)*(777|666|o\+[rwx]*w|a\+[rwx]*w)\b`), "chmod-world-write", "world-writable permissions"},
	{regexp.MustCompile(`\bchown\s+(-[^\s]*)?R\s+root\b`), "chown-root", "recursive chown to root"},
	{regexp.MustCompile(`\bxargs\b.*\brm\b`), "xargs-rm", "xargs piped into rm"},
	{regexp.MustCompile(`\bfind\b.*-exec(?:dir)?\s+(/\S*/)?rm\b`), "find-exec-rm", "find -exec rm"},
	{regexp.MustCompile(`\bfind\b.*-delete\b`), "find-delete", "find -delete"},

	// network -> shell
	{regexp.MustCompile(`\b(curl|wget)\b.*\|\s*(ba)?sh\b`), "pipe-to-shell", "pipe remote content into shell"},
	{regexp.MustCompile(`\b(bash|sh|zsh|ksh)\s+<\s*<?\s*\(\s*(curl|wget)\b`), "exec-remote-procsub", "execute remote script via process substitution"},

	// sudo with non-interactive privilege flags
	{regexp.MustCompile(`\bsudo\b[^;|&\n]*?\s+(-[sSaA]\b|--stdin\b|--askpass\b)`), "sudo-priv-flag", "sudo with stdin/askpass/shell flag"},

	// arbitrary code via interpreter flags / heredoc
	{regexp.MustCompile(`\b(python[23]?|perl|ruby|node)\s+-[ec]\s+`), "interp-eval", "interpreter -e/-c eval"},
	{regexp.MustCompile(`\b(python[23]?|perl|ruby|node)\s+<<`), "interp-heredoc", "interpreter heredoc exec"},
	{regexp.MustCompile(`\b(bash|sh|zsh|ksh)\s+-[^\s]*c(\s+|$)`), "shell-dash-c", "nested shell -c invocation"},

	// process control
	{regexp.MustCompile(`\bkill\s+-9\s+-1\b`), "kill-all", "kill all processes"},
	{regexp.MustCompile(`\bpkill\s+-9\b`), "pkill-9", "force kill processes"},
	{regexp.MustCompile(`\bkillall\s+(-[^\s]*\s+)*-(9|KILL|SIGKILL)\b`), "killall-9", "force kill via killall"},
	{regexp.MustCompile(`\bkill\b.*\$\(\s*pgrep\b`), "kill-pgrep", "kill via pgrep expansion"},

	// git destructive
	{regexp.MustCompile(`\bgit\s+reset\s+--hard\b`), "git-reset-hard", "git reset --hard (destroys uncommitted changes)"},
	{regexp.MustCompile(`\bgit\s+push\b.*--force\b`), "git-push-force", "git force push"},
	{regexp.MustCompile(`\bgit\s+push\b.*\s-f\b`), "git-push-force-short", "git force push (-f)"},
	{regexp.MustCompile(`\bgit\s+clean\s+-[^\s]*f`), "git-clean-force", "git clean with force"},
	{regexp.MustCompile(`\bgit\s+branch\s+-D\b`), "git-branch-delete", "git branch force delete"},
	{regexp.MustCompile(`\bgit\s+checkout\s+(?:[^\s]+\s+)*(--(\s|$)|\.(\s|$)|-f\b|--force\b)`), "git-checkout-discard", "git checkout discarding local changes"},
	{regexp.MustCompile(`\bgit\s+restore\s+(?:[^\s]+\s+)*(\.(\s|$)|--worktree\b)`), "git-restore-discard", "git restore discarding working-tree changes"},

	// writes to sensitive paths
	{regexp.MustCompile(`>>?\s*["']?(/etc/|/private/etc/)`), "write-etc", "write to system config"},
	{regexp.MustCompile(`>>?\s*["']?(~|\$HOME|\$\{HOME\})/\.(ssh|aws|gnupg|kube)/`), "write-creds-dir", "write to credentials directory"},
	{regexp.MustCompile(`>>?\s*["']?(\./|/)?[^\s]*\.env(\.[^/\s"']*)?(\s|$|;|&|\|)`), "write-dotenv", "write to .env file"},
	{regexp.MustCompile(`\btee\b.*["']?(/etc/|~/\.(ssh|aws)/)`), "tee-sensitive", "tee into sensitive path"},

	// chmod +x then exec (two-step bypass)
	{regexp.MustCompile(`\bchmod\s+\+x\b.*[;&|]+\s*\./`), "chmod-exec", "chmod +x followed by execution"},

	// persistence: writes that auto-run on next shell/login, or that rewrite
	// bee's own trust config (config.toml under ~/.bee)
	{regexp.MustCompile(`>>?\s*["']?(~|\$HOME|\$\{HOME\})/\.(zshrc|bashrc|bash_profile|bash_login|profile|zprofile|zshenv)\b`), "write-shell-rc", "write to a shell startup file"},
	{regexp.MustCompile(`>>?\s*["']?(~|\$HOME|\$\{HOME\})/\.bee/`), "write-bee-config", "write to bee's own config/state directory"},
	{regexp.MustCompile(`(LaunchAgents|LaunchDaemons)/|\blaunchctl\s+(load|bootstrap|enable)\b`), "launch-agent", "install/enable a launch agent"},
	{regexp.MustCompile(`\bcrontab\b`), "crontab", "modify cron jobs"},

	// remote code via git inline config (core.pager/alias/fsmonitor run commands)
	{regexp.MustCompile(`\bgit\s+(-c|--config-env)\b`), "git-config-inject", "git with inline -c config (can execute commands)"},

	// data exfiltration: upload a local file to a remote endpoint
	{regexp.MustCompile(`\b(curl|wget)\b[^|]*\s(--data(-binary|-raw|-urlencode)?|-d|--upload-file|-T)\b[^|]*@`), "curl-upload-file", "upload a local file via curl/wget"},
	{regexp.MustCompile(`\bscp\b\s+\S+\s+\S+:`), "scp-remote", "copy a file to a remote host via scp"},
}

// DetectDangerous returns the first matching pattern key + description, or
// empty strings + false when the command matches no dangerous pattern.
func DetectDangerous(cmd string) (key, desc string, matched bool) {
	for _, c := range matchForms(cmd) {
		for _, p := range dangerousPatterns {
			if p.Re.MatchString(c) {
				return p.Key, p.Desc, true
			}
		}
	}
	return "", "", false
}

// DangerousKeys returns every registered pattern key. Used by config
// validation to verify allowlist entries reference real patterns.
func DangerousKeys() []string {
	out := make([]string, len(dangerousPatterns))
	for i, p := range dangerousPatterns {
		out[i] = p.Key
	}
	return out
}
