package web_fetch

import (
	"net"
	"testing"
)

func TestDomainPolicy_CaseAndTrailingDot(t *testing.T) {
	block := &DomainPolicy{Block: []string{"evil.com"}}
	for _, h := range []string{"EVIL.com", "Evil.Com", "evil.com.", "SUB.EVIL.COM", "sub.evil.com."} {
		if block.IsAllowed(h) {
			t.Errorf("block list bypassed with host %q", h)
		}
	}
	if !block.IsAllowed("notevil.com") {
		t.Error("notevil.com must not match evil.com")
	}
	allow := &DomainPolicy{Allow: []string{"Example.com"}}
	for _, h := range []string{"example.com", "EXAMPLE.com", "www.example.com.", "WWW.Example.Com"} {
		if !allow.IsAllowed(h) {
			t.Errorf("allow list rejected host %q", h)
		}
	}
	if allow.IsAllowed("example.com.evil.com") {
		t.Error("suffix trick passed the allow list")
	}
}

func TestIsBlockedIP_ExtraRanges(t *testing.T) {
	for ip, want := range map[string]bool{
		"0.0.0.1": true, "0.255.255.255": true, "100.64.0.1": true, "100.127.255.254": true,
		"224.0.0.1": true, "ff02::1": true, "127.0.0.1": true, "10.0.0.1": true, "169.254.169.254": true,
		"100.63.255.255": false, "100.128.0.1": false, "1.1.1.1": false, "8.8.8.8": false, "2606:4700::1111": false,
	} {
		if got := isBlockedIP(net.ParseIP(ip)); got != want {
			t.Errorf("isBlockedIP(%s) = %v, want %v", ip, got, want)
		}
	}
}
