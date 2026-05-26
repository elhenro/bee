package config

import (
	"reflect"
	"testing"
)

func TestActiveProfile_Named(t *testing.T) {
	c := Defaults()
	c.Profile = "tiny"
	p := ActiveProfile(c)
	if p.MemoryTopK != 1 {
		t.Errorf("tiny.MemoryTopK = %d, want 1", p.MemoryTopK)
	}
}

func TestTinyProfile_ReadGrepCapsAndSkipApplyPatch(t *testing.T) {
	c := Defaults()
	c.Profile = "tiny"
	p := ActiveProfile(c)
	if !p.SkipApplyPatch {
		t.Error("tiny.SkipApplyPatch = false, want true")
	}
	if p.ReadDefaultLines != 100 {
		t.Errorf("tiny.ReadDefaultLines = %d, want 100", p.ReadDefaultLines)
	}
	if p.ReadMaxLines != 500 {
		t.Errorf("tiny.ReadMaxLines = %d, want 500", p.ReadMaxLines)
	}
	if p.GrepMaxMatches != 50 {
		t.Errorf("tiny.GrepMaxMatches = %d, want 50", p.GrepMaxMatches)
	}
	if p.NoMutationStallThreshold != 3 {
		t.Errorf("tiny.NoMutationStallThreshold = %d, want 3 (nudge after 3 read-only turns)", p.NoMutationStallThreshold)
	}
}

func TestTinyProfile_ToolFormatXML(t *testing.T) {
	c := Defaults()
	c.Profile = "tiny"
	p := ActiveProfile(c)
	if p.ToolFormat != "xml" {
		t.Errorf("tiny.ToolFormat = %q, want %q (local/small models need textmode wrapper to parse bare-JSON envelopes)", p.ToolFormat, "xml")
	}
}

func TestNormalProfile_NoSkipApplyPatch(t *testing.T) {
	c := Defaults()
	c.Profile = "normal"
	p := ActiveProfile(c)
	if p.SkipApplyPatch {
		t.Error("normal.SkipApplyPatch = true, want false")
	}
}

func TestActiveProfile_FallbackToNormal(t *testing.T) {
	c := Defaults()
	c.Profile = "does-not-exist"
	p := ActiveProfile(c)
	want := c.Profiles["normal"]
	if p.SystemPromptBudget != want.SystemPromptBudget {
		t.Errorf("fallback profile budget = %d, want normal's %d",
			p.SystemPromptBudget, want.SystemPromptBudget)
	}
}

func TestActiveProfile_FallbackToZero(t *testing.T) {
	c := Config{Profile: "ghost", Profiles: map[string]Profile{}}
	p := ActiveProfile(c)
	if !reflect.DeepEqual(p, Profile{}) {
		t.Errorf("expected zero profile, got %+v", p)
	}
}

func TestApplyProfile_FillsCavemanWhenEmpty(t *testing.T) {
	c := Defaults()
	c.Profile = "tiny"
	c.Caveman = "" // simulate unset
	out := ApplyProfile(c)
	if out.Caveman != "ultra" {
		t.Errorf("ApplyProfile caveman = %q, want ultra (from tiny profile)", out.Caveman)
	}
}

func TestApplyProfile_ResolvesAutoCaveman(t *testing.T) {
	c := Defaults()
	c.Profile = "tiny"
	c.Caveman = "auto" // sentinel: defer to profile
	out := ApplyProfile(c)
	if out.Caveman != "ultra" {
		t.Errorf("ApplyProfile caveman = %q, want ultra (from tiny profile)", out.Caveman)
	}
}

func TestApplyProfile_PreservesExplicitCaveman(t *testing.T) {
	c := Defaults()
	c.Profile = "tiny"
	c.Caveman = "off" // explicit user opt-out
	out := ApplyProfile(c)
	if out.Caveman != "off" {
		t.Errorf("ApplyProfile overwrote explicit caveman: got %q", out.Caveman)
	}
}

func TestResolveAutoProfile_PicksTinyForFlash(t *testing.T) {
	cases := map[string]string{
		"deepseek/deepseek-v4-flash":   "tiny",
		"gpt-4o-mini":                  "tiny",
		"google/gemini-2.0-flash-lite": "tiny",
		"anthropic/claude-haiku-4-5":   "tiny",
		"llama3.1:8b":                  "tiny",
		"qwen2.5:7b":                   "tiny",
		"claude-opus-4-7":              "large",
		"anthropic/claude-sonnet-4-7":  "large",
		"gpt-5-codex":                  "normal",
		"":                             "normal",
	}
	for model, want := range cases {
		if got := ResolveAutoProfile(model); got != want {
			t.Errorf("ResolveAutoProfile(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestApplyProfile_ResolvesAuto(t *testing.T) {
	c := Defaults()
	c.Profile = "auto"
	c.DefaultModel = "deepseek/deepseek-v4-flash"
	c.Caveman = ""
	out := ApplyProfile(c)
	if out.Profile != "tiny" {
		t.Errorf("auto → expected tiny for flash, got %q", out.Profile)
	}
	if out.Caveman != "ultra" {
		t.Errorf("auto→tiny should pick caveman=ultra, got %q", out.Caveman)
	}
}

func TestApplyProfile_LocalProviderForcesEditMode(t *testing.T) {
	c := Defaults()
	c.DefaultProvider = "ollama"
	c.Mode = "auto"
	out := ApplyProfile(c)
	if out.Mode != "edit" {
		t.Errorf("auto+local should flip to edit, got %q", out.Mode)
	}
}

func TestApplyProfile_HostedProviderKeepsAutoMode(t *testing.T) {
	c := Defaults()
	c.DefaultProvider = "openai"
	c.Mode = "auto"
	out := ApplyProfile(c)
	if out.Mode != "auto" {
		t.Errorf("auto+hosted should stay auto, got %q", out.Mode)
	}
}

func TestResolveAutoProfileForProvider_LocalForcesTiny(t *testing.T) {
	cases := []struct {
		provider, model string
		want            string
	}{
		// local providers force tiny regardless of model size.
		{"ollama", "qwen3.5:35b", "tiny"},
		{"ollama", "llama3.1:70b", "tiny"},
		{"lmstudio", "deepseek-r1-distill-32b", "tiny"},
		{"omlx", "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit", "tiny"},
		// hosted: fall back to model heuristic.
		{"openrouter", "claude-opus-4-7", "large"},
		{"openrouter", "deepseek/deepseek-v4-flash", "tiny"},
		{"openai", "gpt-5-codex", "normal"},
	}
	for _, c := range cases {
		got := ResolveAutoProfileForProvider(c.provider, c.model)
		if got != c.want {
			t.Errorf("ResolveAutoProfileForProvider(%q,%q) = %q, want %q",
				c.provider, c.model, got, c.want)
		}
	}
}
