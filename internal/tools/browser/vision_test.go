package browser

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDescribeImage_PostsAndReturnsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/generate") {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if req["model"] != "llava" {
			t.Errorf("model not sent: %v", req["model"])
		}
		imgs, ok := req["images"].([]any)
		if !ok || len(imgs) != 1 {
			t.Errorf("images not sent: %v", req["images"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"response": "a red login button"})
	}))
	defer srv.Close()

	vc := visionClient{model: "llava", endpoint: srv.URL}
	got, err := vc.describe(context.Background(), []byte{0x89, 0x50}, "what do you see?")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a red login button" {
		t.Errorf("got %q", got)
	}
}

func TestDescribeImage_EndpointDownErrors(t *testing.T) {
	vc := visionClient{model: "llava", endpoint: "http://127.0.0.1:0"}
	if _, err := vc.describe(context.Background(), []byte{1}, "x"); err == nil {
		t.Error("expected error when endpoint unreachable")
	}
}
