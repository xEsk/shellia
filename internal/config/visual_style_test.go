package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVisualStylesListsEverySelectableStyle(t *testing.T) {
	want := []VisualStyle{VisualStylePlain, VisualStyleGuide, VisualStyleBands, VisualStyleCards}
	if got := visualStyles(); !reflect.DeepEqual(got, want) {
		t.Fatalf("visualStyles() = %#v, want %#v", got, want)
	}
}

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
	if cfg.VisualStyle != VisualStyleGuide {
		t.Fatalf("defaultConfig().VisualStyle = %q, want %q", cfg.VisualStyle, VisualStyleGuide)
	}

	fileCfg := FileConfig{}
	fileCfg.UI.Style = " cards "
	applyFileConfig(&cfg, fileCfg)
	if cfg.VisualStyle != VisualStyleCards {
		t.Fatalf("applyFileConfig() VisualStyle = %q, want %q", cfg.VisualStyle, VisualStyleCards)
	}

	if !strings.Contains(defaultConfigTemplate(), "style = \"guide\"") {
		t.Fatal("defaultConfigTemplate() lacks ui.style default")
	}
	for _, style := range []string{"plain", "guide", "bands", "cards"} {
		if !strings.Contains(defaultConfigTemplate(), style) {
			t.Fatalf("defaultConfigTemplate() lacks documented %q style", style)
		}
	}
}

func TestUpdateVisualStyleTOMLReplacesStyleInsideUI(t *testing.T) {
	input := "default_model = \"openai\"\n\n[ui]\n# keep this comment\nstyle = \"plain\"\nno_color = false\n\n[context]\ninclude_cwd = true\n"
	want := "default_model = \"openai\"\n\n[ui]\n# keep this comment\nstyle = \"cards\"\nno_color = false\n\n[context]\ninclude_cwd = true\n"
	if got := updateVisualStyleTOML(input, VisualStyleCards); got != want {
		t.Fatalf("updateVisualStyleTOML() = %q, want %q", got, want)
	}
}

func TestUpdateVisualStyleTOMLInsertsMissingUIStyle(t *testing.T) {
	input := "default_model = \"openai\"\n\n[ui]\nno_color = false\n\n[context]\ninclude_cwd = true\n"
	want := "default_model = \"openai\"\n\n[ui]\nno_color = false\nstyle = \"guide\"\n\n[context]\ninclude_cwd = true\n"
	if got := updateVisualStyleTOML(input, VisualStyleGuide); got != want {
		t.Fatalf("updateVisualStyleTOML() = %q, want %q", got, want)
	}
}

func TestUpdateVisualStyleTOMLAppendsMissingUISection(t *testing.T) {
	input := "default_model = \"openai\"\n"
	want := "default_model = \"openai\"\n\n[ui]\nstyle = \"bands\"\n"
	if got := updateVisualStyleTOML(input, VisualStyleBands); got != want {
		t.Fatalf("updateVisualStyleTOML() = %q, want %q", got, want)
	}
}

func TestPersistVisualStyleKeepsConfigBodyAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	input := "[ui]\nstyle = \"plain\"\n\n[context]\ninclude_cwd = true\n"
	if err := os.WriteFile(path, []byte(input), 0o640); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() before update error = %v", err)
	}
	cfg := defaultConfig()
	cfg.ConfigPath = path
	if err := persistVisualStyle(cfg, VisualStyleCards); err != nil {
		t.Fatalf("persistVisualStyle() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); got != "[ui]\nstyle = \"cards\"\n\n[context]\ninclude_cwd = true\n" {
		t.Fatalf("config = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("permissions = %o, want 640", got)
	}
	if os.SameFile(before, info) {
		t.Fatal("persistVisualStyle() rewrote the existing inode instead of atomically replacing it")
	}
}
