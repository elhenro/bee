package safety

import (
	"reflect"
	"testing"
)

func TestPlainSegments(t *testing.T) {
	cases := []struct {
		cmd  string
		want [][]string
		ok   bool
	}{
		{"ls -la", [][]string{{"ls", "-la"}}, true},
		{"ls && cat 'a b'", [][]string{{"ls"}, {"cat", "a b"}}, true},
		{`bash -lc "ls; pwd"`, [][]string{{"ls"}, {"pwd"}}, true},
		{"ls; ", nil, false},
		{"&& ls", nil, false},
		{"ls > f", nil, false},
		{"ls\npwd", nil, false},
		{"echo $(id)", nil, false},
		{`echo "$(id)"`, nil, false},
		{`echo '$(id)'`, [][]string{{"echo", "$(id)"}}, true},
		{`bash -c "bash -c 'bash -c \"ls\"'"`, nil, false}, // depth cap
	}
	for _, tc := range cases {
		got, ok := PlainSegments(tc.cmd)
		if ok != tc.ok || (ok && !reflect.DeepEqual(got, tc.want)) {
			t.Errorf("PlainSegments(%q) = %v, %v; want %v, %v", tc.cmd, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCommandName(t *testing.T) {
	for word, want := range map[string]string{
		"ls": "ls", "/bin/ls": "ls", "/usr/local/bin/rg": "rg",
		"./ls": "", "/tmp/ls": "", "~/bin/ls": "", "$CMD": "", "": "",
	} {
		if got := CommandName(word); got != want {
			t.Errorf("CommandName(%q) = %q, want %q", word, got, want)
		}
	}
}
