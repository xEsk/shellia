package ui

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

// TestCommandMenuLinesShowsRetry checks the explicit retry command is suggested.
func TestCommandMenuLinesShowsRetry(t *testing.T) {
	got := commandMenuLines(false, "/re", defaultConfig())
	if len(got) != 3 {
		t.Fatalf("commandMenuLines(/re) returned %d lines, want 3", len(got))
	}
	if !strings.Contains(got[1], "/retry") || !strings.Contains(got[1], "retry the last cancelled or failed request") {
		t.Fatalf("commandMenuLines(/re) = %#v, want retry suggestion", got)
	}
}

// TestCommandMenuLinesShowsNew checks the fresh-session command is suggested.
func TestCommandMenuLinesShowsNew(t *testing.T) {
	got := commandMenuLines(false, "/ne", defaultConfig())
	if len(got) != 3 {
		t.Fatalf("commandMenuLines(/ne) returned %d lines, want 3", len(got))
	}
	if !strings.Contains(got[1], "/new") || !strings.Contains(got[1], "start a fresh context") {
		t.Fatalf("commandMenuLines(/ne) = %#v, want new-session suggestion", got)
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
