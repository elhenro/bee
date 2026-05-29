package paginator

import (
	"reflect"
	"testing"
)

func TestPageFirst(t *testing.T) {
	got := Page([]int{10, 20, 30, 40, 50}, 1, 2)
	want := []int{10, 20}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Page page1 = %v want %v", got, want)
	}
}

func TestPageSecond(t *testing.T) {
	got := Page([]int{10, 20, 30, 40, 50}, 2, 2)
	want := []int{30, 40}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Page page2 = %v want %v", got, want)
	}
}
