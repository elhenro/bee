package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestStartLoopback_CallbackRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv, err := StartLoopback(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Errorf("unexpected loopback URL: %s", srv.URL)
	}
	if !strings.HasSuffix(srv.URL, "/callback") {
		t.Errorf("expected /callback suffix, got %s", srv.URL)
	}

	// hit the callback as the browser would
	resp, err := http.Get(srv.URL + "?code=abc&state=xyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case r := <-srv.Result:
		if r.Err != nil {
			t.Fatalf("unexpected err: %v", r.Err)
		}
		if r.Code != "abc" || r.State != "xyz" {
			t.Errorf("got %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callback")
	}
}

func TestStartLoopback_ErrorParam(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv, err := StartLoopback(ctx, "/cb")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	resp, err := http.Get(srv.URL + "?error=access_denied&error_description=user+said+no")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case r := <-srv.Result:
		if r.Err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(r.Err.Error(), "access_denied") {
			t.Errorf("error missing param: %v", r.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestStartLoopback_CtxCancelShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := StartLoopback(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	// give shutdown goroutine a moment
	time.Sleep(200 * time.Millisecond)
	// next request should fail
	client := http.Client{Timeout: 500 * time.Millisecond}
	_, err = client.Get(srv.URL)
	if err == nil {
		t.Error("expected server to be down after ctx cancel")
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	got := BuildAuthorizeURL(
		"https://auth.example/authorize",
		"client123",
		"http://127.0.0.1:9000/callback",
		"chal",
		"state42",
		"openid email",
	)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "client123" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("code_challenge") != "chal" {
		t.Errorf("code_challenge = %q", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("state") != "state42" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("scope") != "openid email" {
		t.Errorf("scope = %q", q.Get("scope"))
	}
}

func TestBuildAuthorizeURL_PreservesExistingQuery(t *testing.T) {
	got := BuildAuthorizeURL(
		"https://auth.example/authorize?foo=bar",
		"c", "r", "ch", "s", "",
	)
	if !strings.Contains(got, "foo=bar&") {
		t.Errorf("expected foo=bar preserved, got %s", got)
	}
	if strings.Contains(got, "scope=") {
		t.Errorf("empty scope should be omitted: %s", got)
	}
}
