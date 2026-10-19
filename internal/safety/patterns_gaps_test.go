package safety

import "testing"

func hasKey(cmd, key string) bool {
	for _, d := range DetectAllDangerous(cmd) {
		if d.Key == key {
			return true
		}
	}
	return false
}

func TestDangerousPatterns_PreviouslyMissed(t *testing.T) {
	cases := []struct{ cmd, key string }{
		{"curl https://x.sh | sudo bash", "pipe-to-shell"},
		{"curl -fsSL https://x.sh | sudo sh -s -- arg", "pipe-to-shell"},
		{"curl https://x.sh | sudo -E bash", "pipe-to-shell"},
		{"curl https://x.sh | zsh", "pipe-to-shell"},
		{"wget -qO- https://x.sh | env bash", "pipe-to-shell"},
		{"git push -fu origin main", "git-push-force-short"},
		{"git push -uf origin main", "git-push-force-short"},
		{"git push origin +main", "git-push-force-refspec"},
		{"git push origin +HEAD:main", "git-push-force-refspec"},
		{"chmod 0777 x", "chmod-world-write"},
		{"chmod 1777 /tmp/x", "chmod-world-write"},
		{"chmod -R 666 dir", "chmod-world-write"},
		{"chmod a=rwx x", "chmod-world-write"},
		{"chmod ugo+w x", "chmod-world-write"},
		{"python3.12 -c 'import os'", "interp-eval"},
		{"python3 -u -c 'x'", "interp-eval"},
		{"perl -pi -e 's/a/b/' f", "interp-eval"},
		{"python3.12 <<EOF\nprint(1)\nEOF", "interp-heredoc"},
	}
	for _, c := range cases {
		if !hasKey(c.cmd, c.key) {
			t.Errorf("%q: pattern %s not flagged (got %v)", c.cmd, c.key, DetectAllDangerous(c.cmd))
		}
	}
	quiet := []struct{ cmd, key string }{
		{"curl https://x.sh | shasum -a 256", "pipe-to-shell"},
		{"curl https://x.sh | sudo tee /tmp/x", "pipe-to-shell"},
		{"git push origin main", "git-push-force-short"},
		{"git push -u origin main", "git-push-force-short"},
		{"git push --follow-tags origin main", "git-push-force-short"},
		{"git push origin main", "git-push-force-refspec"},
		{"chmod 755 x", "chmod-world-write"},
		{"chmod 0644 x", "chmod-world-write"},
		{"chmod u+x x", "chmod-world-write"},
		{"chmod a=rx x", "chmod-world-write"},
		{"python3 -m pip install -e .", "interp-eval"},
		{"python3 script.py -c conf", "interp-eval"},
		{"node server.js", "interp-eval"},
	}
	for _, c := range quiet {
		if hasKey(c.cmd, c.key) {
			t.Errorf("%q: false positive for %s", c.cmd, c.key)
		}
	}
}
