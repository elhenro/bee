package safety

import "regexp"

// pattern matches a known secret shape. when kind == "env-assign" the regex
// has three capture groups (name, quote, value) and is rewritten so the value
// is replaced while the assignment shape stays readable.
type pattern struct {
	kind string
	re   *regexp.Regexp
}

var patterns = []pattern{
	// anthropic before openai: sk-ant-… also matches the openai sk- prefix.
	{"anthropic-key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{"openai-key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{20,}\b`)},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"github-token", regexp.MustCompile(`\bgh[opsur]_[A-Za-z0-9]{36,}\b`)},
	{"github-pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}\b`)},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"slack-token", regexp.MustCompile(`\bxox[bpsare]-[A-Za-z0-9-]{10,}\b`)},
	{"stripe-key", regexp.MustCompile(`\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{24,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)},
	{"bearer", regexp.MustCompile(`\bBearer\s+[A-Za-z0-9._-]{20,}`)},
	// env-assign: matches NAME=value, NAME="value", NAME='value', or NAME: value.
	// alternation in the value group avoids RE2's lack of backrefs for quote matching.
	{"env-assign", regexp.MustCompile(`(?i)\b((?:[A-Z][A-Z0-9_]*)?(?:API[_-]?KEY|SECRET(?:[_-]?KEY)?|ACCESS[_-]?TOKEN|AUTH[_-]?TOKEN|PASSWORD|PASSWD|PRIVATE[_-]?KEY|CLIENT[_-]?SECRET)[A-Z0-9_]*)\s*[:=]\s*(?:"[^"\n]+"|'[^'\n]+'|[^\s"';|&]+)`)},
}

// Redact scans text for known secret shapes and replaces matches with a
// sentinel. Pure regex sweep, no allocation when no patterns hit. Safe to call
// on tool output before folding it into model context.
func Redact(text string) string {
	if text == "" {
		return text
	}
	out := text
	for _, p := range patterns {
		if p.kind == "env-assign" {
			out = p.re.ReplaceAllString(out, "${1}=<REDACTED>")
			continue
		}
		out = p.re.ReplaceAllString(out, "<REDACTED:"+p.kind+">")
	}
	return out
}
