package main

import "testing"

func TestParseGoalMessage(t *testing.T) {
	cases := []struct {
		in       string
		wantCond string
		wantGoal bool
	}{
		{"/goal make tests pass", "make tests pass", true},
		{"  /goal fix build  ", "fix build", true},
		{"/goal", "", true},
		{"/goal clear", "", true},
		{"/goal stop", "", true},
		{"/goal show", "", true},
		{"do something else", "", false},
		{"/model gpt", "", false},
	}
	for _, c := range cases {
		gotCond, gotGoal := parseGoalMessage(c.in)
		if gotGoal != c.wantGoal || gotCond != c.wantCond {
			t.Errorf("parseGoalMessage(%q) = (%q, %v), want (%q, %v)",
				c.in, gotCond, gotGoal, c.wantCond, c.wantGoal)
		}
	}
}
