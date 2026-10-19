package safety

import "testing"

func TestPlainSegments_RejectsHiddenExpansion(t *testing.T) {
	reject := []string{
		`find . -${Z:-exec} touch /tmp/x \;`,
		`find . {-exec,touch,/tmp/x,\;}`,
		`find . -{delete,print}`,
		`git diff ${Z:---output=/tmp/pwn}`,
		`git log $FLAGS`,
		`git log "$FLAGS"`,
		`echo $HOME`,
		`echo $1`,
		`echo $@`,
		"echo `id`",
		`echo "${HOME}"`,
		`ls {a..c}`,
		`ls {a,b}`,
		`ls x$[1+1]`,
	}
	for _, c := range reject {
		if segs, ok := PlainSegments(c); ok {
			t.Errorf("PlainSegments(%q) accepted: %v", c, segs)
		}
	}
	accept := []string{
		`echo '${HOME}'`,
		`echo '{a,b}'`,
		`echo "{a,b}"`,
		`grep '^func Test.*x' f.go`,
		`echo "cost $ 5"`,
		`echo $`,
		`ls {}`,
		`find . -name '*.go'`,
		`git log --oneline -5`,
	}
	for _, c := range accept {
		if _, ok := PlainSegments(c); !ok {
			t.Errorf("PlainSegments(%q) rejected a literal command", c)
		}
	}
}
