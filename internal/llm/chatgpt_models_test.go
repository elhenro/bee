package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/auth"
)

func TestFetchChatGPTModels(t *testing.T) {
	saveTok(t, withHomeDir(t), "chatgpt", &auth.Token{AccessToken: "AT", AccountID: "acct-1"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("client_version") == "" {
			t.Errorf("url = %s", r.URL)
		}
		for k, want := range map[string]string{"Authorization": "Bearer AT", "chatgpt-account-id": "acct-1", "originator": "codex_cli_rs", "Accept": "application/json"} {
			if got := r.Header.Get(k); got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		fmt.Fprint(w, `{"models":[
			{"slug":"gpt-5","display_name":"GPT-5","context_window":272000,"visibility":"list"},
			{"slug":"hidden","visibility":"hide"},
			{"slug":"","visibility":"list"},
			{"slug":"bare","context_window":1}]}`)
	}))
	defer srv.Close()

	got, err := FetchChatGPTModels(context.Background(), srv.URL+"/", "chatgpt-account-id")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "gpt-5" || got[0].Name != "GPT-5" || got[0].ContextLength != 272000 || got[1].Name != "bare" {
		t.Errorf("models = %+v", got)
	}
}

func TestFetchChatGPTModels_NotLoggedIn(t *testing.T) {
	withHomeDir(t)
	if _, err := FetchChatGPTModels(context.Background(), "http://unused", ""); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("err = %v", err)
	}
}
