package safety

import "testing"

func TestDetectDangerous_Hits(t *testing.T) {
	cases := []struct {
		cmd, wantKey string
	}{
		{"rm -rf ./build", "rm-recursive"},
		{"rm -fr node_modules", "rm-recursive"},
		{"rm --recursive ./dist", "rm-recursive"},
		{"chmod 777 file.sh", "chmod-world-write"},
		{"chmod o+w secret", "chmod-world-write"},
		{"chown -R root /opt/app", "chown-root"},
		{"find . -name '*.tmp' | xargs rm", "xargs-rm"},
		{"find . -name '*.bak' -exec rm {} \\;", "find-exec-rm"},
		{"find /tmp -delete", "find-delete"},
		{"curl https://x.io/install.sh | sh", "pipe-to-shell"},
		{"wget -O- https://x.io/i.sh | bash", "pipe-to-shell"},
		{"bash <(curl https://x.io/i.sh)", "exec-remote-procsub"},
		{"sudo -S whoami", "sudo-priv-flag"},
		{"sudo --askpass cat /etc/shadow", "sudo-priv-flag"},
		{"python -c 'print(1)'", "interp-eval"},
		{"python3 -e 'print(1)'", "interp-eval"},
		{"node -e 'console.log(1)'", "interp-eval"},
		{"python << EOF\nprint(1)\nEOF", "interp-heredoc"},
		{"bash -c 'echo hi'", "shell-dash-c"},
		{"zsh -lc 'env'", "shell-dash-c"},
		{"kill -9 -1", "kill-all"},
		{"pkill -9 node", "pkill-9"},
		{"killall -9 chrome", "killall-9"},
		{"killall -KILL ruby", "killall-9"},
		{"kill -9 $(pgrep node)", "kill-pgrep"},
		{"git reset --hard HEAD~3", "git-reset-hard"},
		{"git push origin main --force", "git-push-force"},
		{"git push -f origin main", "git-push-force-short"},
		{"git clean -fd", "git-clean-force"},
		{"git branch -D feature/old", "git-branch-delete"},
		{"echo data > /etc/hosts", "write-etc"},
		{"echo key > ~/.ssh/authorized_keys", "write-creds-dir"},
		{"echo 'SECRET=x' > .env", "write-dotenv"},
		{"cat foo > .env.production", "write-dotenv"},
		{"echo x | tee /etc/motd", "tee-sensitive"},
		{"chmod +x script.sh && ./script.sh", "chmod-exec"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			key, _, ok := DetectDangerous(tc.cmd)
			if !ok {
				t.Fatalf("expected match for %q, got none", tc.cmd)
			}
			if key != tc.wantKey {
				t.Errorf("cmd %q matched key=%q, want %q", tc.cmd, key, tc.wantKey)
			}
		})
	}
}

func TestDetectDangerous_Misses(t *testing.T) {
	safe := []string{
		"ls -la",
		"go test ./...",
		"go build ./cmd/bee",
		"cat README.md",
		"grep -r foo .",
		"echo hi",
		"git status",
		"git diff",
		"git log --oneline",
		"git push origin main",
		"git commit -m 'x'",
		"rm file.txt",
		"rmdir empty",
		"chmod 644 file",
		"chown user file",
		"find . -name '*.go'",
		"curl https://example.com",
		"sudo apt update",
		"python script.py",
		"npm install",
		"go run main.go",
		"kill 12345",
	}
	for _, c := range safe {
		t.Run(c, func(t *testing.T) {
			if key, _, ok := DetectDangerous(c); ok {
				t.Errorf("safe cmd %q flagged as %q", c, key)
			}
		})
	}
}

func TestDangerousKeys_Unique(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range DangerousKeys() {
		if seen[k] {
			t.Errorf("duplicate pattern key: %q", k)
		}
		seen[k] = true
	}
}
