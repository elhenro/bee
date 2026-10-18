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

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	keep := &keepBuf{tail: strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")}
	streamBody(w, resp.Body, keep)

	if in, outTok, ok := parseUsage(keep.buf); ok {
		p.mu.Lock()
		side.Wallet.Debit(side.Cost.MeterUsage(in, outTok))
		p.mu.Unlock()
	}
}

const (
	maxJSONKeep = 8 << 20  // whole-body parse needs the full document
	maxSSEKeep  = 64 << 10 // the usage chunk rides at the end of the stream
)

// keepBuf remembers what usage parsing needs: the first maxJSONKeep bytes of a
// plain body, or the last maxSSEKeep bytes of an event stream.
type keepBuf struct {
	buf  []byte
	tail bool
}

func (k *keepBuf) add(b []byte) {
	k.buf = append(k.buf, b...)
	if k.tail && len(k.buf) > maxSSEKeep {
		k.buf = append([]byte(nil), k.buf[len(k.buf)-maxSSEKeep:]...)
	}
	if !k.tail && len(k.buf) > maxJSONKeep {
		k.buf = k.buf[:maxJSONKeep]
	}
}

// streamBody copies src to w chunk by chunk, flushing so event streams reach
// the client live, and tees every chunk into keep.
func streamBody(w http.ResponseWriter, src io.Reader, keep *keepBuf) {
	fl, _ := w.(http.Flusher)
	chunk := make([]byte, 32<<10)
	for {
		n, err := src.Read(chunk)
		if n > 0 {
			keep.add(chunk[:n])
			if _, werr := w.Write(chunk[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
	}
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

type usageEnv struct {
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// parseUsage extracts prompt/completion token counts from an OpenAI-compatible
// response: a plain JSON body, or an event stream whose last data chunk with a
// usage block wins. Returns ok=false when no usage block is present.
func parseUsage(body []byte) (input, output int, ok bool) {
	var env usageEnv
	if json.Unmarshal(body, &env) == nil && env.Usage != nil {
		return env.Usage.PromptTokens, env.Usage.CompletionTokens, true
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		data, found := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
		if !found {
			continue
		}
		var e usageEnv
		// a truncated first line of a tail window fails to parse and is skipped
		if json.Unmarshal(bytes.TrimSpace(data), &e) == nil && e.Usage != nil {
			input, output, ok = e.Usage.PromptTokens, e.Usage.CompletionTokens, true
		}
	}
	return input, output, ok
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
