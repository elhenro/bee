package wire

import "testing"

func TestOutsideStrings(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1}`, `{"":1}`},
		{`{"a{":"}\"{","b":[1]}`, `{"":"","":[1]}`},
		{`"unterminated {`, `"`},
		{`x\"y`, `x\"`}, // a backslash outside a string escapes nothing
		{``, ``},
	}
	for _, c := range cases {
		var got []byte
		for _, b := range OutsideStrings(c.in) {
			got = append(got, b)
		}
		if string(got) != c.want {
			t.Errorf("OutsideStrings(%q) = %q, want %q", c.in, got, c.want)
		}
		var gotB []byte
		for _, b := range OutsideStrings([]byte(c.in)) {
			gotB = append(gotB, b)
		}
		if string(gotB) != c.want {
			t.Errorf("OutsideStrings([]byte(%q)) = %q, want %q", c.in, gotB, c.want)
		}
	}
}

func TestOutsideStringsStopsEarly(t *testing.T) {
	n := 0
	for range OutsideStrings(`{}{}{}`) {
		if n++; n == 2 {
			break
		}
	}
	if n != 2 {
		t.Errorf("n = %d", n)
	}
}
