package web_fetch

import (
	"net"
	"strings"
)

// isBlockedIP reports whether an IP is in a range the fetcher must never reach
// (loopback, private, unspecified, link-local, multicast, "this network" and
// carrier-grade NAT; e.g. cloud metadata 169.254/16).
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 0.0.0.0/8 reaches the local host on linux; 100.64.0.0/10 is shared NAT space
		return v4[0] == 0 || (v4[0] == 100 && v4[1]&0xc0 == 0x40)
	}
	return false
}

// normHost lowercases and drops a trailing root dot so "Example.COM." and
// "example.com" compare equal.
func normHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

// hostMatches reports whether host equals entry or is a subdomain of it.
func hostMatches(host, entry string) bool {
	entry = normHost(entry)
	if entry == "" {
		return false
	}
	return host == entry || strings.HasSuffix(host, "."+entry)
}
