package agents

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxLinkHops bounds symlink resolution, as the kernel does, so a loop ends.
const maxLinkHops = 40

// linkChain resolves rel inside root one path component at a time and
// returns every in-repo path the resolution passes through: each symlink,
// including symlinked parent directories, and the final target. A branch
// that changes any of them changes what the gate reads. Resolution stops at
// a link that leaves root (outside the repo a branch cannot reach) or at a
// missing component, which is kept since a branch could create it.
func linkChain(root, rel string) []string {
	var chain []string
	cur := path.Clean(filepath.ToSlash(rel))
	for hop := 0; hop < maxLinkHops; hop++ {
		parts := strings.Split(cur, "/")
		next := ""
		for i := range parts {
			prefix := path.Join(parts[:i+1]...)
			fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(prefix)))
			if err != nil {
				return append(chain, cur)
			}
			if fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			chain = append(chain, prefix)
			target, err := os.Readlink(filepath.Join(root, filepath.FromSlash(prefix)))
			if err != nil {
				return chain
			}
			if filepath.IsAbs(target) {
				r, err := filepath.Rel(root, target)
				if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
					return chain
				}
				target = filepath.ToSlash(r)
			} else {
				target = path.Join(path.Dir(prefix), filepath.ToSlash(target))
			}
			if target == ".." || strings.HasPrefix(target, "../") {
				return chain
			}
			next = path.Join(append([]string{target}, parts[i+1:]...)...)
			break
		}
		if next == "" {
			return append(chain, cur)
		}
		cur = next
	}
	return chain
}
