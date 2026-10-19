package browser

import (
	"errors"
	"net"
	"testing"
)

func stubLookup(t *testing.T, fn func(string) ([]net.IP, error)) {
	t.Helper()
	prev := lookupIP
	lookupIP = fn
	t.Cleanup(func() { lookupIP = prev })
}

func TestValidateNavURL_FailsClosedOnDNSError(t *testing.T) {
	stubLookup(t, func(string) ([]net.IP, error) { return nil, errors.New("no such host") })
	if err := validateNavURL("http://unresolvable.example/"); err == nil {
		t.Error("unresolvable host must be refused, not waved through")
	}
}

func TestValidateNavURL_NonCanonicalNumericHosts(t *testing.T) {
	// the resolver is never consulted for these; a lookup would hide the bug
	stubLookup(t, func(string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil })
	for _, u := range []string{
		"http://0177.0.0.1/", "http://127.1/", "http://2130706433/", "http://0x7f000001/",
		"http://0x7f.0.0.1/", "http://10.1/", "http://1.2.3.0377/",
	} {
		if err := validateNavURL(u); err == nil {
			t.Errorf("validateNavURL(%q) = nil, want refusal", u)
		}
	}
}

func TestValidateNavURL_StillAllowsPublicHosts(t *testing.T) {
	stubLookup(t, func(h string) ([]net.IP, error) {
		if h == "internal.example" {
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	for _, u := range []string{"https://example.com/a", "http://cafe.be/", "http://8.8.8.8/", "https://sub.domain.example:8443/x"} {
		if err := validateNavURL(u); err != nil {
			t.Errorf("validateNavURL(%q) = %v, want nil", u, err)
		}
	}
	for _, u := range []string{"http://internal.example/", "http://127.0.0.1/", "http://[::1]/", "file:///etc/passwd"} {
		if err := validateNavURL(u); err == nil {
			t.Errorf("validateNavURL(%q) = nil, want refusal", u)
		}
	}
}
