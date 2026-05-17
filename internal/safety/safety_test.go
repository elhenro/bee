package safety

import (
	"strings"
	"testing"
)

func TestRedact_KnownPatterns(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"openai", "key=sk-proj-abc123def456ghi789jklm token", "<REDACTED:openai-key>"},
		{"anthropic", "sk-ant-abcdef0123456789abcdef0123456789xx done", "<REDACTED:anthropic-key>"},
		{"aws", "AKIAIOSFODNN7EXAMPLE in log", "<REDACTED:aws-access-key>"},
		{"github", "ghp_abcdefghijklmnopqrstuvwxyz0123456789ABC tail", "<REDACTED:github-token>"},
		{"google", "AIzaSyAbcdefghijklmnopqrstuvwxyz1234567 ok", "<REDACTED:google-api-key>"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c said", "<REDACTED:jwt>"},
		{"bearer", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123", "<REDACTED:bearer>"},
		{"stripe", "sk_live_abcdefghijklmnopqrstuvwx tail", "<REDACTED:stripe-key>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("Redact(%q) = %q; want substring %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedact_EnvAssign(t *testing.T) {
	in := `OPENAI_API_KEY="hunter2" and DB_PASSWORD=foo123 and SOME_SECRET_KEY='abc'`
	out := Redact(in)
	if strings.Contains(out, "hunter2") || strings.Contains(out, "foo123") || strings.Contains(out, "'abc'") {
		t.Fatalf("env values leaked: %s", out)
	}
	if !strings.Contains(out, "OPENAI_API_KEY=") || !strings.Contains(out, "<REDACTED>") {
		t.Fatalf("env name/sentinel missing: %s", out)
	}
}

func TestRedact_Idempotent(t *testing.T) {
	in := "sk-proj-abcdefghijklmnopqrstu1234 hi"
	once := Redact(in)
	twice := Redact(once)
	if once != twice {
		t.Fatalf("not idempotent: %q vs %q", once, twice)
	}
}

func TestRedact_NoMatch(t *testing.T) {
	in := "just a plain log line with no secrets"
	if Redact(in) != in {
		t.Fatalf("plain text mutated")
	}
}

func TestCheckReadable_Blocks(t *testing.T) {
	bad := []string{
		"/Users/foo/.env",
		"/Users/foo/project/.env.production",
		"/Users/foo/.ssh/id_rsa",
		"/Users/foo/.aws/credentials",
		"./secrets.yaml",
		"/repo/.git/HEAD",
		"./server.key",
		"/etc/pki/cert.pem",
	}
	for _, p := range bad {
		if err := CheckReadable(p); err == nil {
			t.Errorf("CheckReadable(%q) should have refused", p)
		}
	}
}

func TestCheckReadable_Allows(t *testing.T) {
	ok := []string{
		"/Users/foo/project/main.go",
		"README.md",
		"./cmd/bee/run.go",
		"/tmp/output.log",
	}
	for _, p := range ok {
		if err := CheckReadable(p); err != nil {
			t.Errorf("CheckReadable(%q) refused: %v", p, err)
		}
	}
}

func TestCheckWritable_BlocksSystemDirs(t *testing.T) {
	if err := CheckWritable("/etc/passwd"); err == nil {
		t.Fatal("write to /etc/passwd should be refused")
	}
	if err := CheckWritable("/System/Library/foo"); err == nil {
		t.Fatal("write to /System should be refused")
	}
}

func TestCheckShellCommand_Blocks(t *testing.T) {
	bad := []string{
		"rm -rf /",
		"rm -rf / ;",
		"rm -fr '/'",
		"rm --recursive --force /",
		"sudo rm -rf / --no-preserve-root",
		"dd if=/dev/zero of=/dev/disk2 bs=1m",
		"mkfs.ext4 /dev/sda1",
		"diskutil eraseDisk JHFS+ none disk2",
		"parted /dev/sda mklabel gpt",
	}
	for _, c := range bad {
		if err := CheckShellCommand(c); err == nil {
			t.Errorf("CheckShellCommand(%q) should have refused", c)
		}
	}
}

func TestCheckShellCommand_Allows(t *testing.T) {
	ok := []string{
		"ls -la",
		"go test ./...",
		"rm -rf ./build",
		"rm -rf node_modules",
		"echo hi",
		"cat /etc/hosts",
	}
	for _, c := range ok {
		if err := CheckShellCommand(c); err != nil {
			t.Errorf("CheckShellCommand(%q) refused: %v", c, err)
		}
	}
}
