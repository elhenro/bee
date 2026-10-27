package web_fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchURLContext_CancelAbortsRequest(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	_, err := FetchURLContext(ctx, srv.URL+"/hang", nil, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("cancel took %v, request was not aborted", d)
	}
}

func TestNewClient_SharesTransportPerScope(t *testing.T) {
	if newClient(nil, false).Transport != newClient(nil, false).Transport {
		t.Error("open transport rebuilt per client")
	}
	if newClient(nil, true).Transport != newClient(nil, true).Transport {
		t.Error("confined transport rebuilt per client")
	}
	if newClient(nil, false).Transport == newClient(nil, true).Transport {
		t.Error("scopes must not share a transport")
	}
}
