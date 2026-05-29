package greeter

import "testing"

func TestGreeting(t *testing.T) {
	if got := Greeting(); got != "hi world" {
		t.Fatalf("Greeting()=%q want %q", got, "hi world")
	}
}
