package arena

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Side is one combatant's view through the metering proxy: where its model
// calls really go (Upstream), an optional Model override for asymmetric routing
// (red→opus, blue→local), and the Wallet/Cost the proxy debits per call.
type Side struct {
	Name     string
	Upstream *url.URL
	Model    string // when set, the request's "model" field is rewritten to this
	Wallet   *Wallet
	Cost     CostSchedule
}

// MeteringProxy is the referee-side egress chokepoint. Containers reach it as
// their model endpoint (base_url = http://referee:PORT/<side>/v1). It forwards
// to the side's real upstream, meters the response usage into the side's wallet,
// and refuses a bankrupt side with 402 so it can no longer think.
type MeteringProxy struct {
	mu    sync.Mutex
	sides map[string]*Side
	rt    http.RoundTripper
}

// NewMeteringProxy builds a proxy over the given sides, keyed by Name.
func NewMeteringProxy(sides ...*Side) *MeteringProxy {
	m := map[string]*Side{}
	for _, s := range sides {
		m[s.Name] = s
	}
	return &MeteringProxy{sides: m, rt: http.DefaultTransport}
}

func (p *MeteringProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, rest := splitSide(r.URL.Path)
	side, ok := p.sides[name]
	if !ok {
		http.Error(w, "unknown side", http.StatusNotFound)
		return
	}

	p.mu.Lock()
	bankrupt := side.Wallet.Bankrupt()
	p.mu.Unlock()
	if bankrupt {
		http.Error(w, "bankrupt: out of nectar", http.StatusPaymentRequired)
		return
	}

	body, _ := io.ReadAll(r.Body)
	if side.Model != "" {
		body = rewriteModel(body, side.Model)
	}

	out := *side.Upstream
	out.Path = strings.TrimRight(side.Upstream.Path, "/") + rest
	out.RawQuery = r.URL.RawQuery
	up, err := http.NewRequestWithContext(r.Context(), r.Method, out.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyHeaders(up.Header, r.Header)
	up.ContentLength = int64(len(body))

	resp, err := p.rt.RoundTrip(up)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)

	if in, outTok, ok := parseUsage(payload); ok {
		p.mu.Lock()
		side.Wallet.Debit(side.Cost.MeterUsage(in, outTok))
		p.mu.Unlock()
	}

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(payload)
}

// splitSide pulls the leading /<name> segment off the path and returns the rest
// (which becomes the upstream path). "/red/v1/x" → ("red", "/v1/x").
func splitSide(p string) (name, rest string) {
	p = strings.TrimPrefix(p, "/")
	i := strings.IndexByte(p, '/')
	if i < 0 {
		return p, "/"
	}
	return p[:i], p[i:]
}

// parseUsage extracts prompt/completion token counts from an OpenAI-compatible
// response body (buffered). Returns ok=false when no usage block is present.
func parseUsage(body []byte) (input, output int, ok bool) {
	var env struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Usage == nil {
		return 0, 0, false
	}
	return env.Usage.PromptTokens, env.Usage.CompletionTokens, true
}

// rewriteModel replaces the top-level "model" field so the proxy controls which
// model each side actually hits. On any parse failure the original body is
// returned unchanged.
func rewriteModel(body []byte, model string) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	m["model"] = model
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
