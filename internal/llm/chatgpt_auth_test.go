package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elhenro/bee/internal/auth"
)

func TestChatGPT_Refresh_SkipsWhenTokenAlreadyRotated(t *testing.T) {
	dir := withHomeDir(t)
	saveTok(t, dir, "chatgpt", &auth.Token{AccessToken: "ROTATED", RefreshToken: "RT2", ExpiresIn: 3600})

	var hits int
	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer tokSrv.Close()

	p := NewChatGPT(ChatGPTConfig{Name: "chatgpt", ClientID: "cid", TokenEndpoint: tokSrv.URL})
	ok, err := p.refresh(context.Background(), "STALE")
	if err != nil || !ok {
		t.Fatalf("refresh = %v, %v; want true, nil", ok, err)
	}
	if hits != 0 {
		t.Errorf("refresh endpoint hit %d times; the rotated refresh_token must not be spent again", hits)
	}
}
