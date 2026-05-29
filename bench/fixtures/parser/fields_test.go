package parser

import (
	"reflect"
	"testing"
)

func TestSplitFieldsBasic(t *testing.T) {
	got := SplitFields("a, b, c")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitFields basic = %#v want %#v", got, want)
	}
}
