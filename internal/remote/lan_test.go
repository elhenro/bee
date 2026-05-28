package remote

import (
	"net"
	"testing"
)

func TestLANIP(t *testing.T) {
	ip := LANIP()
	if ip == "" {
		return // none available (e.g. CI); not a failure
	}
	if net.ParseIP(ip) == nil {
		t.Errorf("LANIP() = %q, not a valid IP", ip)
	}
}
