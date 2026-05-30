package browser

import (
	"context"
	"testing"
)

func TestNew_RegistersCoreTools(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true"})
	names := map[string]bool{}
	for _, x := range ts {
		names[x.Spec().Name] = true
	}
	for _, want := range []string{"browser_open", "browser_snapshot", "browser_console", "browser_click", "browser_type"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	if names["browser_screenshot"] {
		t.Error("screenshot must be absent when no vision model set")
	}
}

func TestNew_AddsScreenshotWhenVisionSet(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true", VisionModel: "llava", VisionEndpoint: "http://x"})
	found := false
	for _, x := range ts {
		if x.Spec().Name == "browser_screenshot" {
			found = true
		}
	}
	if !found {
		t.Error("screenshot tool should be registered when vision model set")
	}
}

func TestOpen_MissingURLIsError(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true"})
	for _, x := range ts {
		if x.Spec().Name == "browser_open" {
			res, err := x.Run(context.Background(), map[string]any{})
			if err != nil {
				t.Fatalf("unexpected go error: %v", err)
			}
			if !res.IsError {
				t.Error("missing url should be IsError result")
			}
		}
	}
}

func TestClick_MissingRefIsError(t *testing.T) {
	ts := New(Options{ChromePath: "/bin/true"})
	for _, x := range ts {
		if x.Spec().Name == "browser_click" {
			res, _ := x.Run(context.Background(), map[string]any{})
			if !res.IsError {
				t.Error("missing ref should be IsError")
			}
		}
	}
}
