package config

import (
	"net"
	"net/url"
	"strings"
)

// localProviderNames lists provider ids that run on-host. Used to gate
// behavior that's worth the round-trip on hosted providers but wasteful
// on slow local models (cost rendering, auto-mode classifier, etc.).
var localProviderNames = map[string]bool{
	"ollama":    true,
	"lmstudio":  true,
	"llamacpp":  true,
	"llama-cpp": true,
	"vllm":      true,
	"localai":   true,
	"omlx":      true,
}

// IsLocalProvider returns true when the named provider runs on-host. Local
// providers skip cost rendering, skip the auto-mode classifier, and use a
// 32k context floor when the model registry has no window.
func IsLocalProvider(name string) bool {
	return localProviderNames[strings.ToLower(name)]
}

// IsLoopbackURL returns true when u points at localhost / 127.x.x.x / ::1.
func IsLoopbackURL(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := p.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
