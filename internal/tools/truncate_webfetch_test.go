package tools

import "testing"

func TestLimitFor_WebFetchToolName(t *testing.T) {
	if got := limitFor("web_fetch"); got != MaxWebfetchTokens {
		t.Errorf("limitFor(web_fetch) = %d, want %d", got, MaxWebfetchTokens)
	}
}
