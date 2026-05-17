package safety

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var secretBasenamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\.env(\..+)?$`),
	regexp.MustCompile(`(?i)^.*\.pem$`),
	regexp.MustCompile(`(?i)^.*\.key$`),
	regexp.MustCompile(`(?i)^.*\.p12$`),
	regexp.MustCompile(`(?i)^.*\.pfx$`),
	regexp.MustCompile(`(?i)^id_(rsa|dsa|ecdsa|ed25519)(\.pub)?$`),
	regexp.MustCompile(`(?i)^known_hosts$`),
	regexp.MustCompile(`(?i)^authorized_keys$`),
	regexp.MustCompile(`(?i)^htpasswd$`),
	regexp.MustCompile(`(?i)^\.netrc$`),
	regexp.MustCompile(`(?i)^credentials$`),
	regexp.MustCompile(`(?i)^\.pgpass$`),
	regexp.MustCompile(`(?i)^\.npmrc$`),
	regexp.MustCompile(`(?i)^\.pypirc$`),
	regexp.MustCompile(`(?i)^secrets?\.(json|ya?ml|toml)$`),
}

var secretPathSegments = []string{
	"/.ssh/",
	"/.gnupg/",
	"/.aws/",
	"/.azure/",
	"/.kube/",
	"/.docker/",
	"/.config/gh/",
	"/.config/git/",
	"/.git/",
}

var forbiddenWritePrefixes = []string{
	"/etc/",
	"/var/db/",
	"/System/",
	"/Library/Keychains/",
	"/private/etc/",
	"/private/var/db/",
}

// normalize collapses path separators to "/" and resolves "." / ".." segments
// without touching the filesystem. relative paths stay relative.
func normalize(p string) string {
	if p == "" {
		return p
	}
	clean := filepath.Clean(p)
	return filepath.ToSlash(clean)
}

func baseName(p string) string {
	norm := normalize(p)
	if i := strings.LastIndex(norm, "/"); i >= 0 {
		return norm[i+1:]
	}
	return norm
}

// CheckReadable returns nil when path is safe to auto-read, or an error
// describing why it was refused. defense layer in front of the read tool —
// the sandbox is the primary boundary; this stops obvious secret exfil
// before bytes ever reach the model.
func CheckReadable(path string) error {
	if path == "" {
		return nil
	}
	base := baseName(path)
	for _, re := range secretBasenamePatterns {
		if re.MatchString(base) {
			return fmt.Errorf("refused: %q matches a known secret-file pattern", base)
		}
	}
	norm := normalize(path)
	for _, seg := range secretPathSegments {
		if strings.Contains(norm, seg) {
			dir := strings.Trim(seg, "/")
			return fmt.Errorf("refused: path is inside protected directory (%s)", dir)
		}
	}
	return nil
}

// CheckWritable inherits all read restrictions and adds system-dir blocks.
// applies to write / patch / shell-redirect-target paths.
func CheckWritable(path string) error {
	if err := CheckReadable(path); err != nil {
		return err
	}
	norm := normalize(path)
	for _, prefix := range forbiddenWritePrefixes {
		if strings.HasPrefix(norm, prefix) {
			return fmt.Errorf("refused: writes under %q are not allowed", prefix)
		}
	}
	return nil
}
