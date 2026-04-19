package main

import (
	"strings"
	"testing"
)

// TestCommandMenuLinesPlain renders slash-command suggestions without ANSI.
func TestCommandMenuLinesPlain(t *testing.T) {
	got := commandMenuLines(false, "/sh")
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
	got := commandMenuLines(false, "/")
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
