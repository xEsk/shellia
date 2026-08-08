package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "shellia/internal/config"
)

func TestSwitchInteractiveThemePersistsAndReplacesRenderer(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstyle = \"plain\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = stdout.Close() })

	cfg := defaultConfig()
	cfg.ConfigPath = path
	cfg.NoColor = true
	deps := defaultRuntimeDeps()
	deps.Stdout = stdout
	deps.StdoutIsTerminal = func(*os.File) bool { return true }
	deps.Renderer = newRenderer(stdout, presentation{Style: configpkg.VisualStylePlain})
	previous := deps.Renderer

	if err := switchInteractiveTheme(&cfg, &deps, "test-user", "CARDS"); err != nil {
		t.Fatalf("switchInteractiveTheme() error = %v", err)
	}
	if cfg.VisualStyle != configpkg.VisualStyleCards {
		t.Fatalf("VisualStyle = %q, want cards", cfg.VisualStyle)
	}
	if deps.Renderer == previous {
		t.Fatal("renderer was not replaced")
	}
	deps.Renderer.UserTurn(interactiveModeAI, "selected theme")
	rendered, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatalf("ReadFile(renderer output) error = %v", err)
	}
	if !strings.Contains(string(rendered), "╭─ test-user") || !strings.Contains(string(rendered), "selected theme") {
		t.Fatalf("renderer output = %q, want cards geometry", rendered)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `style = "cards"`) {
		t.Fatalf("config = %q, want cards", data)
	}
}

func TestSwitchInteractiveThemeFailureKeepsConfigAndRenderer(t *testing.T) {
	cfg := defaultConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "missing.toml")
	cfg.VisualStyle = configpkg.VisualStylePlain
	deps := defaultRuntimeDeps()
	deps.Renderer = newRenderer(deps.Stdout, presentation{Style: configpkg.VisualStylePlain})
	previous := deps.Renderer

	if err := switchInteractiveTheme(&cfg, &deps, "test-user", "guide"); err == nil {
		t.Fatal("switchInteractiveTheme() error = nil, want persistence failure")
	}
	if cfg.VisualStyle != configpkg.VisualStylePlain || deps.Renderer != previous {
		t.Fatal("failed switch changed runtime state")
	}
}

func TestRunInteractiveThemeCommandListsAndSwitchesWithoutLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstyle = \"plain\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	cfg.ConfigPath = path
	cfg.NoColor = true
	cfg.VisualStyle = configpkg.VisualStylePlain
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "/theme\n/theme cards\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.StdoutIsTerminal = func(*os.File) bool { return true }
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})
	if fake.requestCount() != 0 {
		t.Fatalf("LLM requests = %d, want 0", fake.requestCount())
	}
	for _, text := range []string{"* plain", "guide", "bands", "cards", "Theme switched to cards."} {
		if !strings.Contains(output, text) {
			t.Fatalf("output = %q, missing %q", output, text)
		}
	}
	if !strings.Contains(output, "\nShellia Theme switched to cards.") {
		t.Fatalf("output = %q, want a blank line before the theme switch message", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `style = "cards"`) {
		t.Fatalf("config = %q, want cards", data)
	}
}

func TestRunInteractiveUnknownThemeKeepsCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\nstyle = \"guide\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	cfg.ConfigPath = path
	cfg.VisualStyle = configpkg.VisualStyleGuide
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "/theme missing\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})
	if !strings.Contains(output, `visual theme "missing" not found`) {
		t.Fatalf("output = %q, want unknown-theme warning", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `style = "guide"`) {
		t.Fatalf("config = %q, want unchanged guide", data)
	}
}
