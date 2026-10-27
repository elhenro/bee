package safety

import "testing"

func TestRedact_PreviouslyMissed(t *testing.T) {
	hide := map[string]string{
		"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY": "aws_secret_access_key = <REDACTED>",
		"[default]\naws_secret_access_key=wJalrXUtn/K7MDENG\nregion=eu":    "[default]\naws_secret_access_key=<REDACTED>\nregion=eu",
		`{"password": "hunter2", "user": "bob"}`:                           `{"password": <REDACTED>, "user": "bob"}`,
		`{"db_password":"a\"b","x":1}`:                                     `{"db_password":<REDACTED>,"x":1}`,
		`{"apiKey": "abc123"}`:                                             `{"apiKey": <REDACTED>}`,
		"password = hunter2":                                               "password = <REDACTED>",
		"  password = hunter2   # prod":                                    "  password = <REDACTED>   # prod",
		"DATABASE_URL=postgres://u:pass@host:5432/db":                      "DATABASE_URL=postgres://u:<REDACTED>@host:5432/db",
		"connect postgres://u:p@ss@host/db now":                            "connect postgres://u:<REDACTED>@host/db now",
		"curl -H 'Authorization: Basic dXNlcjpwYXNzd29yZA=='":              "curl -H 'Authorization: Basic <REDACTED>'",
		"//registry.npmjs.org/:_authToken=abc123def456":                    "//registry.npmjs.org/:_authToken=<REDACTED>",
	}
	for in, want := range hide {
		if out := Redact(in); out != want {
			t.Errorf("Redact(%q) = %q want %q", in, out, want)
		}
	}
	// prefix and body are joined at run time so no token-shaped literal sits in
	// the source for secret scanners to flag
	tokens := []string{
		"glpat-" + "abcdefghijklmnopqrst",
		"gsk_" + "abcdefghijklmnopqrstuvwx",
		"npm_" + "abcdefghijklmnopqrstuvwxyz0123456789",
		"hf_" + "abcdefghijklmnopqrstuvwxyzABCDEFGH",
		"SG." + "abcdefghijklmnop_QR.abcdefghijklmnopqrstuvwxyz0123456789",
	}
	for _, tok := range tokens {
		if out := Redact("x " + tok + " y"); out != "x <REDACTED:"+kindOf(out)+"> y" {
			t.Errorf("token %q leaked: %q", tok, out)
		}
	}
}

// kindOf pulls the kind out of a sentinel so the token table needs no kinds.
func kindOf(out string) string {
	const pre = "<REDACTED:"
	i := len(pre) + 2
	for j := i; j < len(out); j++ {
		if out[j] == '>' {
			return out[i:j]
		}
	}
	return ""
}

func TestRedact_PreviouslyMissedKeepsCode(t *testing.T) {
	keep := []string{
		"http://localhost:8080/path",
		"ssh://git@github.com/org/repo",
		"https://example.com:8443/a@b",
		`{"token_count": "12", "name": "x"}`,
		`{"password": ""}`,
		`"password" is required`,
		"password = hash(raw)",
		"password = os.environ['PW']",
		"password = nil;",
		"password == hunter2",
		"npm_install_all_the_things = true",
		"hf_model_name = bert",
		"Basic authentication is required",
		"Authorization: Bearer",
		"SG.x.y",
		"token = {a: 1}",
		"glpat-short",
	}
	for _, in := range keep {
		if out := Redact(in); out != in {
			t.Errorf("mangled: %q -> %q", in, out)
		}
	}
}
