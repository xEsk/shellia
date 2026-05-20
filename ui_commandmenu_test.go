package main

import (
	"strings"
	"testing"
)

// TestCommandMenuLinesPlain renders slash-command suggestions without ANSI.
func TestCommandMenuLinesPlain(t *testing.T) {
	got := commandMenuLines(false, "/sh", defaultConfig())
	if len(got) != 3 {
		t.Fatalf("commandMenuLines() returned %d lines, want %d", len(got), 3)
	}
	if !strings.Contains(got[1], "/shell") || !strings.Contains(got[1], "enter direct shell mode") {
		t.Fatalf("commandMenuLines() = %#v, want shell suggestion", got)
	}
	if !strings.HasPrefix(got[0], "╭") || !strings.HasSuffix(got[2], "╯") {
		t.Fatalf("commandMenuLines() = %#v, want compact box borders", got)
	}
}

// TestCommandMenuLinesKeepBoxWidth checks every rendered row has the same width.
func TestCommandMenuLinesKeepBoxWidth(t *testing.T) {
	got := commandMenuLines(false, "/", defaultConfig())
	if len(got) == 0 {
		t.Fatalf("commandMenuLines() returned no lines")
	}

	width := visibleWidth(got[0])
	for _, line := range got {
		if visibleWidth(line) != width {
			t.Fatalf("commandMenuLines() row width = %d, want %d: %q", visibleWidth(line), width, line)
		}
	}
}

// TestCommandMenuLinesShowsModelProfiles renders /model as a profile submenu.
func TestCommandMenuLinesShowsModelProfiles(t *testing.T) {
	cfg := defaultConfig()
	cfg.ModelName = "mlx"
	cfg.Models = []modelConfig{
		{Name: "openai", Model: "gpt-5.4-mini"},
		{Name: "mlx", Model: "mlx-community/qwen"},
	}

	got := commandMenuLines(false, "/model ", cfg)
	if len(got) != 4 {
		t.Fatalf("commandMenuLines(/model) returned %d lines, want 4", len(got))
	}
	if !strings.Contains(got[2], "* mlx") || !strings.Contains(got[2], "mlx-community/qwen") {
		t.Fatalf("commandMenuLines(/model) = %#v, want active mlx row", got)
	}
}

// TestCompleteInteractiveCommandCompletesModelProfile checks Tab completion for /model.
func TestCompleteInteractiveCommandCompletesModelProfile(t *testing.T) {
	cfg := defaultConfig()
	cfg.Models = []modelConfig{
		{Name: "openai"},
		{Name: "mlx"},
	}

	got, ok := completeInteractiveCommand("/model m", cfg)
	if !ok || got != "/model mlx" {
		t.Fatalf("completeInteractiveCommand(/model m) = %q, %t; want /model mlx, true", got, ok)
	}
}
