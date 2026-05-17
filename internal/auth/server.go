package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CallbackResult holds the auth code from the OAuth redirect.
type CallbackResult struct {
	Code  string
	State string
	Err   error
}

// LoopbackServer runs a one-shot HTTP server on a random localhost port. The
// caller registers s.URL as the redirect_uri, opens the authorize URL in a
// browser, and reads exactly one value from Result.
type LoopbackServer struct {
	URL    string
	Result <-chan CallbackResult
	srv    *http.Server
}

// StartLoopback binds 127.0.0.1:<random> and serves a single handler at path
// (defaults to /callback). Context cancellation triggers shutdown.
func StartLoopback(ctx context.Context, path string) (*LoopbackServer, error) {
	return StartLoopbackOn(ctx, path, 0)
}

// StartLoopbackOn is like StartLoopback but binds to a fixed port. Pass 0 for
// random. Used when the provider requires an exact redirect_uri match.
func StartLoopbackOn(ctx context.Context, path string, fixedPort int) (*LoopbackServer, error) {
	if path == "" {
		path = "/callback"
	}
	addr := fmt.Sprintf("127.0.0.1:%d", fixedPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	res := make(chan CallbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			msg := e
			if desc := q.Get("error_description"); desc != "" {
				msg = e + ": " + desc
			}
			// non-blocking: callback may fire only once but be defensive
			select {
			case res <- CallbackResult{Err: errors.New(msg)}:
			default:
			}
			fmt.Fprintf(w, "Login failed: %s. You can close this tab.", e)
			return
		}
		select {
		case res <- CallbackResult{Code: q.Get("code"), State: q.Get("state")}:
		default:
		}
		fmt.Fprint(w, "Login successful. You can close this tab.")
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	// shutdown on ctx done
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	// advertise as localhost (not 127.0.0.1): some OAuth servers (Hydra,
	// used by chatgpt.com auth) register the literal "localhost" redirect_uri
	// and reject the 127.0.0.1 form with authorize_hydra_invalid_request.
	// Browser resolves localhost -> 127.0.0.1, so the listener still catches.
	return &LoopbackServer{
		URL:    fmt.Sprintf("http://localhost:%d%s", port, path),
		Result: res,
		srv:    srv,
	}, nil
}

// Close stops the loopback server. Safe to call multiple times.
func (s *LoopbackServer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// BuildAuthorizeURL composes the full authorize URL with PKCE params.
func BuildAuthorizeURL(authorizeEndpoint, clientID, redirectURI, codeChallenge, state, scope string) string {
	return BuildAuthorizeURLWithExtras(authorizeEndpoint, clientID, redirectURI, codeChallenge, state, scope, nil)
}

// BuildAuthorizeURLWithExtras is like BuildAuthorizeURL but appends extra
// vendor-specific params (audience, prompt, id_token_hint). Extras override
// standard params if they collide — caller's responsibility.
func BuildAuthorizeURLWithExtras(authorizeEndpoint, clientID, redirectURI, codeChallenge, state, scope string, extras map[string]string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	if scope != "" {
		q.Set("scope", scope)
	}
	for k, v := range extras {
		q.Set(k, v)
	}
	sep := "?"
	if strings.Contains(authorizeEndpoint, "?") {
		sep = "&"
	}
	return authorizeEndpoint + sep + q.Encode()
}
