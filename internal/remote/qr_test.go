package remote

import (
	"strings"
	"testing"
)

func TestRenderQR(t *testing.T) {
	out, err := RenderQR("http://192.168.1.2:8765")
	if err != nil {
		t.Fatalf("RenderQR error: %v", err)
	}
	if out == "" {
		t.Fatal("RenderQR returned empty output")
	}
	if !strings.ContainsAny(out, "█▀▄") {
		t.Error("output contains no block runes")
	}
}
