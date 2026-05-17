package config

import "testing"

func TestDefaults_OpenRouterPreconfigured(t *testing.T) {
	c := Defaults()

	if c.DefaultProvider != "openrouter" {
		t.Errorf("DefaultProvider = %q, want openrouter", c.DefaultProvider)
	}
	if c.DefaultModel != "deepseek/deepseek-v4-flash" {
		t.Errorf("DefaultModel = %q, want deepseek/deepseek-v4-flash", c.DefaultModel)
	}

	or, ok := c.Providers["openrouter"]
	if !ok {
		t.Fatal("openrouter provider missing")
	}
	if or.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("openrouter base_url = %q", or.BaseURL)
	}
	if or.WireAPI != "chat" {
		t.Errorf("openrouter wire_api = %q, want chat", or.WireAPI)
	}
	if or.EnvKey != "OPENROUTER_API_KEY" {
		t.Errorf("openrouter env_key = %q", or.EnvKey)
	}
}

func TestDefaults_ThreeProfiles(t *testing.T) {
	c := Defaults()
	for _, name := range []string{"tiny", "normal", "large"} {
		if _, ok := c.Profiles[name]; !ok {
			t.Errorf("profile %q missing", name)
		}
	}
	// tiny should have the smallest budget, large the largest
	tiny := c.Profiles["tiny"]
	normal := c.Profiles["normal"]
	large := c.Profiles["large"]
	if !(tiny.SystemPromptBudget < normal.SystemPromptBudget && normal.SystemPromptBudget < large.SystemPromptBudget) {
		t.Errorf("budget order broken: tiny=%d normal=%d large=%d",
			tiny.SystemPromptBudget, normal.SystemPromptBudget, large.SystemPromptBudget)
	}
	if tiny.MemoryTopK != 1 {
		t.Errorf("tiny.MemoryTopK = %d, want 1", tiny.MemoryTopK)
	}
}

func TestDefaults_AnthropicAPIKey(t *testing.T) {
	// Only the direct API-key provider should ship. The OAuth subscription
	// path that impersonated Claude Code was removed.
	c := Defaults()
	ap, ok := c.Providers["anthropic"]
	if !ok {
		t.Fatal("anthropic provider missing")
	}
	if ap.WireAPI != "anthropic-messages" {
		t.Errorf("anthropic wire_api = %q, want anthropic-messages", ap.WireAPI)
	}
	if ap.EnvKey != "ANTHROPIC_API_KEY" {
		t.Errorf("anthropic env_key = %q, want ANTHROPIC_API_KEY", ap.EnvKey)
	}
	if ap.OAuth != nil {
		t.Error("anthropic should not have an oauth block — api-key only")
	}
	if _, has := c.Providers["claude"]; has {
		t.Error("claude (subscription/oauth) provider must be removed")
	}
}

func TestDefaults_SandboxAndMemory(t *testing.T) {
	c := Defaults()
	if c.Sandbox.Scope != "workspace-write" {
		t.Errorf("sandbox scope = %q", c.Sandbox.Scope)
	}
	if c.Sandbox.Approval != "on-request" {
		t.Errorf("sandbox approval = %q", c.Sandbox.Approval)
	}
	if !c.Memory.Enabled {
		t.Error("memory should be enabled by default")
	}
	if c.Memory.TopK != 3 {
		t.Errorf("memory top_k = %d, want 3", c.Memory.TopK)
	}
	if c.Caveman != "auto" {
		t.Errorf("caveman = %q, want auto (resolves to profile.Caveman in ApplyProfile)", c.Caveman)
	}
}
