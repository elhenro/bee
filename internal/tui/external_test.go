package tui

import "testing"

func TestOnExternalDone_SuspendTracksJob(t *testing.T) {
	m := newTestModel(t)
	// child suspended (Ctrl-Z): record it so ctrl+o can resume.
	out, _ := m.onExternalDone(externalDoneMsg{what: "edit", suspended: true, pid: 4242, dir: "/work"})
	m2 := out.(Model)
	if m2.suspendedJob == nil {
		t.Fatal("suspended child should be tracked")
	}
	if m2.suspendedJob.pid != 4242 || m2.suspendedJob.dir != "/work" {
		t.Fatalf("job=%+v want pid 4242 dir /work", m2.suspendedJob)
	}
	// child later exits: tracking must clear.
	out, _ = m2.onExternalDone(externalDoneMsg{what: "edit", suspended: false, pid: 4242})
	if out.(Model).suspendedJob != nil {
		t.Fatal("exited child must clear tracking")
	}
}

func TestOnExternalDone_ErrorSurfaces(t *testing.T) {
	m := newTestModel(t)
	out, _ := m.onExternalDone(externalDoneMsg{what: "edit", err: errStub})
	if out.(Model).lastErr == "" {
		t.Fatal("launch error should surface in lastErr")
	}
}

var errStub = stubErr("boom")

type stubErr string

func (e stubErr) Error() string { return string(e) }

func TestSplitPathLine(t *testing.T) {
	cases := []struct {
		in       string
		wantFile string
		wantLine int
	}{
		{"main.go:12", "main.go", 12},
		{"main.go", "main.go", 0},
		{"", "", 0},
		{"pkg/app.go:3", "pkg/app.go", 3},
		{"main.go:abc", "main.go:abc", 0}, // non-numeric suffix: treat whole as path
		{":12", ":12", 0},                 // no path before colon
	}
	for _, c := range cases {
		f, l := splitPathLine(c.in)
		if f != c.wantFile || l != c.wantLine {
			t.Errorf("splitPathLine(%q)=(%q,%d) want (%q,%d)", c.in, f, l, c.wantFile, c.wantLine)
		}
	}
}
