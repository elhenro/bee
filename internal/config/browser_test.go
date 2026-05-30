package config

import (
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestBrowserConfig_Defaults(t *testing.T) {
	var c Config
	if c.Browser.Enabled {
		t.Error("Browser.Enabled should default false")
	}
	if c.Browser.Headless {
		t.Error("Browser.Headless should default false (headful)")
	}
}

func TestBrowserConfig_TOML(t *testing.T) {
	const src = `
[browser]
enabled = true
headless = true
chrome_path = "/custom/chrome"

[browser.vision]
model = "llava"
endpoint = "http://localhost:11434"
`
	var c Config
	if err := toml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	if !c.Browser.Enabled || !c.Browser.Headless {
		t.Errorf("flags not decoded: %+v", c.Browser)
	}
	if c.Browser.ChromePath != "/custom/chrome" {
		t.Errorf("chrome_path: %q", c.Browser.ChromePath)
	}
	if c.Browser.Vision.Model != "llava" || c.Browser.Vision.Endpoint != "http://localhost:11434" {
		t.Errorf("vision not decoded: %+v", c.Browser.Vision)
	}
}
