package safety

import (
	"regexp"
	"strings"
)

// pattern matches a known secret shape. kinds ending in "-assign" have the
// groups (name, separator, optional trailer) and are rewritten so only the
// value is replaced while the assignment shape stays readable.
type pattern struct {
	kind string
	re   *regexp.Regexp
}

// secretName is the secret-ish identifier part shared by the assign patterns.
const secretName = `(?:API[_-]?KEY|SECRET(?:[_-]?KEY)?|ACCESS[_-]?TOKEN|AUTH[_-]?TOKEN|PASSWORD|PASSWD|PRIVATE[_-]?KEY|CLIENT[_-]?SECRET)`

// anyName is a case-insensitive secret-ish name; a bare `token` counts only as
// the whole word so `token_limit: 100` is left alone.
const anyName = `(?:[a-z_][a-z0-9_]*)?` + secretName + `[a-z0-9_]*|token`

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
	{"gitlab-token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)},
	{"groq-key", regexp.MustCompile(`\bgsk_[A-Za-z0-9]{20,}\b`)},
	{"npm-token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{30,}\b`)},
	{"hf-token", regexp.MustCompile(`\bhf_[A-Za-z0-9]{30,}\b`)},
	{"sendgrid-key", regexp.MustCompile(`\bSG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`)},
	// PEM private key block (RSA/EC/OPENSSH/PGP) — redact the whole armored body
	{"pem-private-key", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)},
	// env-assign: UPPERCASE NAME=value, NAME="value", NAME='value', NAME: value.
	// case-sensitive on purpose: a lowercase `apiKey := load()` is code, not a
	// secret. a value may not start with `=` so comparisons never match.
	{"env-assign", regexp.MustCompile(`\b((?:[A-Z][A-Z0-9_]*)?` + secretName + `[A-Z0-9_]*)([ \t]*(?::=|[:=])[ \t]*)(?:"[^"\n]+"|'[^'\n]+'|[^\s"';|&=][^\s"';|&]*)`)},
	// kv-assign: any-case names. quoted literal on either separator; bare
	// `name=value` with no spaces (dotenv lines, cli flags, url queries) when
	// the value is not code; yaml `name: scalar` alone on its line, optional
	// trailing comment. a value containing `(` is a call, one starting with `=`
	// is a comparison: both stay untouched, as does `:=`.
	{"kv-assign", regexp.MustCompile(`(?i)\b(` + anyName + `)([ \t]*[:=][ \t]*)(?:"[^"\n]+"|'[^'\n]+')`)},
	{"kv-assign", regexp.MustCompile(`(?im)\b(` + anyName + `)(=)[^\s()&;"'=][^\s()&;]*([\s&;]|$)`)},
	{"kv-assign", regexp.MustCompile(`(?im)\b(` + anyName + `)([ \t]*:[ \t]*)[^\s#=("'][^\s(]*((?:[ \t]+#.*)?[ \t]*\r?)$`)},
	// ini `name = value` alone on its line (~/.aws/credentials, .my.cnf). a
	// value with brackets, parens or a trailing `;` is code and stays.
	{"kv-assign", regexp.MustCompile(`(?im)^([ \t]*(?:` + anyName + `))([ \t]*=[ \t]*)[^\s()\[\]{};#=<"'][^\s()\[\]{};]*((?:[ \t]+[#;].*)?[ \t]*\r?)$`)},
	// quoted key with a string value: json `"password": "x"`.
	{"json-assign", regexp.MustCompile(`(?i)(["'](?:` + anyName + `)["'])([ \t]*:[ \t]*)(?:"(?:[^"\\\n]|\\.)+"|'[^'\n]+')`)},
	// userinfo password in a url: scheme://user:pass@host.
	{"url-assign", regexp.MustCompile(`\b([a-z][a-z0-9+.-]*://[^\s:/@]+)(:)[^\s/]+(@)`)},
	{"basic-assign", regexp.MustCompile(`(?i)\b(Authorization)(:[ \t]*Basic[ \t]+)[A-Za-z0-9+/=_-]{8,}`)},
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
		if strings.HasSuffix(p.kind, "-assign") {
			out = p.re.ReplaceAllString(out, "${1}${2}<REDACTED>${3}")
			continue
		}
		out = p.re.ReplaceAllString(out, "<REDACTED:"+p.kind+">")
	}
	return out
}
