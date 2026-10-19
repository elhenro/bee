package web_fetch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, contentType, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchURL_NonHTMLPassesThroughRaw(t *testing.T) {
	src := "fn main() {\n    let v: Vec<String> = Vec::new();\n    if a <b {\n        x();\n    }\n}"
	r, err := FetchURL(serve(t, "text/plain; charset=utf-8", src)+"/raw.rs", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Markdown != src {
		t.Errorf("text/plain body altered:\n got %q\nwant %q", r.Markdown, src)
	}

	js := `{"html": "<b>hi</b>", "n": 1}`
	r, err = FetchURL(serve(t, "application/json", js)+"/j.json", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Markdown != js {
		t.Errorf("json body altered: got %q want %q", r.Markdown, js)
	}
}

func TestFetchURL_HTMLStillConverted(t *testing.T) {
	r, err := FetchURL(serve(t, "text/html; charset=utf-8", "<html><body><h1>Title</h1></body></html>")+"/h", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Markdown, "# Title") {
		t.Errorf("html not converted: %q", r.Markdown)
	}
}

func TestIsHTML_SniffsWithoutHeader(t *testing.T) {
	if !isHTML("", []byte("<!doctype html><html><body>x</body></html>")) {
		t.Error("headerless html not sniffed")
	}
	if isHTML("", []byte(`{"a": 1}`)) {
		t.Error("headerless json sniffed as html")
	}
	if !isHTML("application/xhtml+xml", []byte("<html/>")) {
		t.Error("xhtml not recognised")
	}
}
