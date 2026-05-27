package web_fetch

import (
	"strings"
	"testing"
	"time"
)

func TestHTMLToMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name: "simple paragraph",
			html: `<html><body><p>Hello World</p></body></html>`,
			expected: "Hello World",
		},
		{
			name: "headings",
			html: `<html><body><h1>Title</h1><h2>Subtitle</h2></body></html>`,
			expected: "# Title\n\n## Subtitle",
		},
		{
			name: "links",
			html: `<html><body><a href="https://example.com">Link Text</a></body></html>`,
			expected: "[Link Text](https://example.com)",
		},
		{
			name: "bold and italic",
			html: `<html><body><strong>Bold</strong> <em>Italic</em></body></html>`,
			expected: "**Bold** *Italic*",
		},
		{
			name: "skip script",
			html: `<html><body><script>alert('hi')</script><p>Content</p></body></html>`,
			expected: "Content",
		},
		{
			name: "skip style",
			html: `<html><body><style>body{color:red}</style><p>Content</p></body></html>`,
			expected: "Content",
		},
		{
			name: "lists",
			html: `<html><body><ul><li>Item 1</li><li>Item 2</li></ul></body></html>`,
			expected: "- Item 1\n- Item 2",
		},
		{
			name: "images",
			html: `<html><body><img src="https://example.com/img.png" alt="Alt Text"></body></html>`,
			expected: "![Alt Text](https://example.com/img.png)",
		},
		{
			name: "code blocks",
			html: `<html><body><pre><code>console.log('hi')</code></pre></body></html>`,
			expected: "```console.log('hi')```",
		},
		{
			name: "blockquote",
			html: `<html><body><blockquote>Quoted text</blockquote></body></html>`,
			expected: "> Quoted text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := htmlToMarkdown([]byte(tt.html))
			if err != nil {
				t.Fatalf("htmlToMarkdown error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDomainPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  DomainPolicy
		domain  string
		allowed bool
	}{
		{
			name: "allow all",
			policy: DomainPolicy{
				Allow: nil,
				Block: nil,
			},
			domain:  "example.com",
			allowed: true,
		},
		{
			name: "block specific domain",
			policy: DomainPolicy{
				Block: []string{"blocked.com"},
			},
			domain:  "blocked.com",
			allowed: false,
		},
		{
			name: "allow subdomain of blocked",
			policy: DomainPolicy{
				Block: []string{"example.com"},
			},
			domain:  "sub.example.com",
			allowed: false,
		},
		{
			name: "allow specific domain",
			policy: DomainPolicy{
				Allow: []string{"allowed.com"},
			},
			domain:  "allowed.com",
			allowed: true,
		},
		{
			name: "deny when not in allow list",
			policy: DomainPolicy{
				Allow: []string{"allowed.com"},
			},
			domain:  "other.com",
			allowed: false,
		},
		{
			name: "block takes precedence",
			policy: DomainPolicy{
				Allow: []string{"example.com"},
				Block: []string{"example.com"},
			},
			domain:  "example.com",
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.policy.IsAllowed(tt.domain)
			if result != tt.allowed {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.domain, result, tt.allowed)
			}
		})
	}
}

func TestCache(t *testing.T) {
	c := NewCache(15*time.Minute, 100)

	// Test set and get
	entry := &CacheEntry{
		Bytes:     100,
		Code:      200,
		CodeText:  "OK",
		Content:   "test content",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	c.Set("https://example.com", entry)

	got, ok := c.Get("https://example.com")
	if !ok {
		t.Fatal("cache miss for existing key")
	}
	if got.Content != "test content" {
		t.Errorf("got content %q, want %q", got.Content, "test content")
	}

	// Test cache miss
	_, ok = c.Get("https://other.com")
	if ok {
		t.Error("cache hit for non-existent key")
	}

	// Test expiration
	expiredEntry := &CacheEntry{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	c.Set("https://expired.com", expiredEntry)

	_, ok = c.Get("https://expired.com")
	if ok {
		t.Error("cache hit for expired entry")
	}

	// Test clear
	c.Set("https://keep.com", entry)
	c.Clear()

	_, ok = c.Get("https://keep.com")
	if ok {
		t.Error("cache hit after clear")
	}
}

func TestNormalizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "  hello world  ",
			expected: "hello world",
		},
		{
			input:    "\thello\nworld\t",
			expected: "hello\nworld",
		},
		{
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		result := NormalizeString(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeString(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFetchURL(t *testing.T) {
	// Test with invalid URL
	_, err := FetchURL("not-a-url", DefaultDomainPolicy())
	if err == nil {
		t.Error("expected error for invalid URL")
	}

	// Test with blocked domain
	_, err = FetchURL("https://localhost/test", DefaultDomainPolicy())
	if err == nil {
		t.Error("expected error for blocked domain")
	}

	// Test with allowed domain (will likely fail to connect, but that's ok)
	_, err = FetchURL("https://example.com", &DomainPolicy{
		Allow: []string{"example.com"},
	})
	// We expect either success or connection error, but not domain policy error
	if err != nil && strings.Contains(err.Error(), "not allowed") {
		t.Errorf("unexpected domain policy error: %v", err)
	}
}