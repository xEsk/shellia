package config

import (
	"strings"
	"testing"
)

func TestNormalizeVisualStyle(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback VisualStyle
		want     VisualStyle
	}{
		{name: "trims plain", input: " plain ", fallback: VisualStyleGuide, want: VisualStylePlain},
		{name: "lowercases guide", input: "GUIDE", fallback: VisualStylePlain, want: VisualStyleGuide},
		{name: "accepts bands", input: "bands", fallback: VisualStylePlain, want: VisualStyleBands},
		{name: "accepts cards", input: "cards", fallback: VisualStylePlain, want: VisualStyleCards},
		{name: "empty keeps fallback", input: "", fallback: VisualStyleGuide, want: VisualStyleGuide},
		{name: "unknown keeps fallback", input: "unknown", fallback: VisualStyleGuide, want: VisualStyleGuide},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeVisualStyle(tt.input, tt.fallback); got != tt.want {
				t.Fatalf("normalizeVisualStyle(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestVisualStyleConfigContract(t *testing.T) {
	cfg := defaultConfig()
	if cfg.VisualStyle != VisualStylePlain {
		t.Fatalf("defaultConfig().VisualStyle = %q, want %q", cfg.VisualStyle, VisualStylePlain)
	}

	fileCfg := FileConfig{}
	fileCfg.UI.Style = " cards "
	applyFileConfig(&cfg, fileCfg)
	if cfg.VisualStyle != VisualStyleCards {
		t.Fatalf("applyFileConfig() VisualStyle = %q, want %q", cfg.VisualStyle, VisualStyleCards)
	}

	if !strings.Contains(defaultConfigTemplate(), "style = \"plain\"") {
		t.Fatal("defaultConfigTemplate() lacks ui.style default")
	}
	for _, style := range []string{"plain", "guide", "bands", "cards"} {
		if !strings.Contains(defaultConfigTemplate(), style) {
			t.Fatalf("defaultConfigTemplate() lacks documented %q style", style)
		}
	}
}
