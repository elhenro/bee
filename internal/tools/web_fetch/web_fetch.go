package web_fetch

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// CacheEntry stores fetched content with metadata
type CacheEntry struct {
	Bytes      int
	Code       int
	CodeText   string
	ContentType string
	Content    string
	ExpiresAt  time.Time
}

// Cache implements a simple LRU cache with TTL
type Cache struct {
	mu     sync.RWMutex
	items  map[string]*CacheEntry
	order  []string
	maxLen int
	ttl    time.Duration
}

// NewCache creates a new cache with the given TTL and max length
func NewCache(ttl time.Duration, maxLen int) *Cache {
	return &Cache{
		items:  make(map[string]*CacheEntry),
		order:  make([]string, 0),
		maxLen: maxLen,
		ttl:    ttl,
	}
}

// Get retrieves an entry from the cache
func (c *Cache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	entry, exists := c.items[key]
	if !exists {
		return nil, false
	}
	
	// Check if entry has expired
	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	
	return entry, true
}

// Set adds an entry to the cache
func (c *Cache) Set(key string, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	// If key already exists, update it
	if _, exists := c.items[key]; exists {
		c.items[key] = entry
		return
	}
	
	// If cache is full, remove the oldest entry
	if len(c.order) >= c.maxLen {
		oldest := c.order[0]
		delete(c.items, oldest)
		c.order = c.order[1:]
	}
	
	c.items[key] = entry
	c.order = append(c.order, key)
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.items = make(map[string]*CacheEntry)
	c.order = make([]string, 0)
}

var cache = NewCache(15*time.Minute, 50*1024*1024) // 15min TTL, 50MB max

// DomainPolicy controls which domains are allowed/blocked
type DomainPolicy struct {
	Allow []string
	Block []string
}

// IsAllowed checks if a domain is allowed based on the policy
func (p *DomainPolicy) IsAllowed(domain string) bool {
	// If block list is not empty, check it first
	if len(p.Block) > 0 {
		for _, blocked := range p.Block {
			if domain == blocked || strings.HasSuffix(domain, "."+blocked) {
				return false
			}
		}
	}
	
	// If allow list is not empty, check it
	if len(p.Allow) > 0 {
		for _, allowed := range p.Allow {
			if domain == allowed || strings.HasSuffix(domain, "."+allowed) {
				return true
			}
		}
		return false
	}
	
	// If no allow list, allow all (unless blocked)
	return true
}

// DefaultDomainPolicy returns a default policy that allows all domains
func DefaultDomainPolicy() *DomainPolicy {
	return &DomainPolicy{
		Allow: nil,
		Block: nil,
	}
}

// FetchResult contains the result of a web fetch operation
type FetchResult struct {
	URL         string `json:"url"`
	Code        int    `json:"code"`
	CodeText    string `json:"code_text"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
	Markdown    string `json:"markdown"`
	DurationMs  int64  `json:"duration_ms"`
}

// FetchURL fetches content from a URL and converts it to markdown
func FetchURL(rawURL string, policy *DomainPolicy) (*FetchResult, error) {
	start := time.Now()
	
	// Parse and validate URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	
	// Check domain policy
	if policy != nil && !policy.IsAllowed(parsedURL.Hostname()) {
		return nil, fmt.Errorf("domain %s is not allowed", parsedURL.Hostname())
	}
	
	// Check cache
	if entry, exists := cache.Get(rawURL); exists {
		return &FetchResult{
			URL:         rawURL,
			Code:        entry.Code,
			CodeText:    entry.CodeText,
			ContentType: entry.ContentType,
			Bytes:       entry.Bytes,
			Markdown:    entry.Content,
			DurationMs:  time.Since(start).Milliseconds(),
		}, nil
	}
	
	// Fetch the URL
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set a realistic user agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Bee/1.0; +https://github.com/elhenro/bee)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()
	
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	
	// Convert HTML to markdown
	markdown, err := htmlToMarkdown(body)
	if err != nil {
		return nil, fmt.Errorf("failed to convert HTML to markdown: %w", err)
	}
	
	// Create cache entry
	entry := &CacheEntry{
		Bytes:       len(body),
		Code:        resp.StatusCode,
		CodeText:    resp.Status,
		ContentType: resp.Header.Get("Content-Type"),
		Content:     markdown,
		ExpiresAt:   time.Now().Add(15 * time.Minute),
	}
	
	// Store in cache
	cache.Set(rawURL, entry)
	
	return &FetchResult{
		URL:         rawURL,
		Code:        resp.StatusCode,
		CodeText:    resp.Status,
		ContentType: resp.Header.Get("Content-Type"),
		Bytes:       len(body),
		Markdown:    markdown,
		DurationMs:  time.Since(start).Milliseconds(),
	}, nil
}

// htmlToMarkdown converts HTML content to markdown
func htmlToMarkdown(htmlContent []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(htmlContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}
	
	var buf bytes.Buffer
	// Start from the body element (skip html/head wrappers)
	body := findBody(doc)
	if body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			renderNode(c, &buf)
		}
	}
	
	// Clean up excessive whitespace while preserving markdown structure
	result := normalizeMarkdown(buf.String())
	return result, nil
}

// findBody finds the body element in the HTML document
func findBody(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "body" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findBody(c); found != nil {
			return found
		}
	}
	return nil
}

// renderNode renders a single HTML node to markdown
func renderNode(n *html.Node, w io.Writer) {
	// Skip script and style tags
	if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
		return
	}
	
	// Handle text nodes
	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			io.WriteString(w, text)
		} else if strings.ContainsAny(n.Data, " \t\n\r") {
			// Preserve single space for whitespace-only text nodes (e.g., between inline elements)
			io.WriteString(w, " ")
		}
		return
	}
	
	// Handle element nodes
	if n.Type == html.ElementNode {
		// Render children first
		var childrenContent bytes.Buffer
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			renderNode(c, &childrenContent)
		}
		childText := childrenContent.String()
		
		switch n.Data {
		case "h1":
			io.WriteString(w, "\n\n# ")
			io.WriteString(w, childText)
		case "h2":
			io.WriteString(w, "\n\n## ")
			io.WriteString(w, childText)
		case "h3":
			io.WriteString(w, "\n\n### ")
			io.WriteString(w, childText)
		case "h4":
			io.WriteString(w, "\n\n#### ")
			io.WriteString(w, childText)
		case "h5":
			io.WriteString(w, "\n\n##### ")
			io.WriteString(w, childText)
		case "h6":
			io.WriteString(w, "\n\n###### ")
			io.WriteString(w, childText)
		case "p":
			io.WriteString(w, "\n\n")
			io.WriteString(w, childText)
		case "br":
			io.WriteString(w, "\n")
		case "strong", "b":
			io.WriteString(w, "**")
			io.WriteString(w, childText)
			io.WriteString(w, "**")
		case "em", "i":
			io.WriteString(w, "*")
			io.WriteString(w, childText)
			io.WriteString(w, "*")
		case "a":
			href := getAttr(n, "href")
			if href != "" {
				io.WriteString(w, "[")
				io.WriteString(w, childText)
				io.WriteString(w, "](")
				io.WriteString(w, href)
				io.WriteString(w, ")")
			} else {
				io.WriteString(w, childText)
			}
		case "ul":
			// ul renders its children (li items)
			io.WriteString(w, "\n\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				renderNode(c, w)
			}
		case "ol":
			// ol renders its children (li items)
			io.WriteString(w, "\n\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				renderNode(c, w)
			}
		case "li":
			io.WriteString(w, "\n- ")
			io.WriteString(w, childText)
		case "blockquote":
			io.WriteString(w, "\n\n> ")
			io.WriteString(w, childText)
		case "pre":
			// Check if child is code element
			var codeText string
			var hasCodeChild bool
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "code" {
					hasCodeChild = true
					// Extract text content from code element
					for cc := c.FirstChild; cc != nil; cc = cc.NextSibling {
						if cc.Type == html.TextNode {
							codeText += cc.Data
						}
					}
					break
				}
			}
			if hasCodeChild {
				io.WriteString(w, "\n\n```")
				io.WriteString(w, codeText)
				io.WriteString(w, "```\n\n")
			} else {
				io.WriteString(w, "\n\n```")
				io.WriteString(w, childText)
				io.WriteString(w, "```\n\n")
			}
		case "code":
			// code inside pre is handled by pre; standalone code gets backticks
			// Check if parent is pre by looking for pre in ancestor chain
			io.WriteString(w, "`")
			io.WriteString(w, childText)
			io.WriteString(w, "`")
		case "img":
			alt := getAttr(n, "alt")
			src := getAttr(n, "src")
			if alt != "" {
				io.WriteString(w, "![")
				io.WriteString(w, alt)
				io.WriteString(w, "](")
				io.WriteString(w, src)
				io.WriteString(w, ")")
			}
		default:
			// For other elements, just render children
			io.WriteString(w, childText)
		}
	}
}

// getAttr returns the value of an attribute from a node
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// normalizeMarkdown collapses excessive whitespace while preserving markdown structure
func normalizeMarkdown(s string) string {
	// Trim leading/trailing whitespace
	s = strings.TrimSpace(s)
	
	// Collapse multiple blank lines to at most one blank line
	// A blank line is two consecutive newlines
	var buf bytes.Buffer
	runes := []rune(s)
	consecutiveNewlines := 0
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\n' {
			consecutiveNewlines++
			if consecutiveNewlines <= 2 {
				buf.WriteRune(runes[i])
			}
			// Skip additional newlines beyond 2
		} else {
			consecutiveNewlines = 0
			buf.WriteRune(runes[i])
		}
	}
	return buf.String()
}

// NormalizeString trims whitespace and normalizes Unicode
func NormalizeString(s string) string {
	trimmed := strings.TrimSpace(s)
	// Simple NFC normalization
	result := make([]byte, 0, len(trimmed))
	for _, r := range trimmed {
		result = append(result, []byte(string(r))...)
	}
	return string(result)
}