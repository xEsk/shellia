package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestRenderCommandBoxPlainFallback checks the no-colour fallback remains compact.
func TestRenderCommandBoxPlainFallback(t *testing.T) {
	got := renderCommandBox(false, "ls", 80)
	want := []string{"run › ls"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderCommandBox() = %#v, want %#v", got, want)
	}
}

// TestRenderCommandBoxUsesCodexLikeBackground checks that the ANSI version renders a grey block.
func TestRenderCommandBoxUsesCodexLikeBackground(t *testing.T) {
	got := renderCommandBox(true, "exit", 80)
	if len(got) != 3 {
		t.Fatalf("renderCommandBox() returned %d rows, want %d", len(got), 3)
	}

	for _, line := range got {
		if !strings.Contains(line, commandBoxBackground) {
			t.Fatalf("renderCommandBox() row does not contain command box background: %q", line)
		}
		if width := visibleWidth(line); width != commandBoxMinPanelWidth {
			t.Fatalf("renderCommandBox() row width = %d, want %d", width, commandBoxMinPanelWidth)
		}
	}
	if !strings.Contains(got[1], commandBoxPromptForeground+commandBoxPrompt) {
		t.Fatalf("renderCommandBox() command row does not colour the prompt prefix: %q", got[1])
	}
}

// TestRenderCommandBoxLimitsLongCommands checks that long commands wrap inside the maximum width.
func TestRenderCommandBoxLimitsLongCommands(t *testing.T) {
	got := renderCommandBox(true, "docker image prune -a --filter label=shellia", 24)
	if len(got) <= 3 {
		t.Fatalf("renderCommandBox() returned %d rows, want wrapped content", len(got))
	}

	for _, line := range got {
		if width := visibleWidth(line); width > 24 {
			t.Fatalf("renderCommandBox() line width = %d, want <= 24: %q", width, line)
		}
	}
}

// TestRenderCommandBoxPreservesCommandSpacing checks that command spacing remains visible inside the panel.
func TestRenderCommandBoxPreservesCommandSpacing(t *testing.T) {
	got := strings.Join(renderCommandBox(false, "cmd  --flag", 80), "\n")
	if !strings.Contains(got, "run › cmd  --flag") {
		t.Fatalf("renderCommandBox() did not preserve command spacing: %q", got)
	}
}

// TestRenderCommandBoxPreservesExplicitNewlines checks multiline commands stay inside the panel.
func TestRenderCommandBoxPreservesExplicitNewlines(t *testing.T) {
	command := "cat <<'EOF' > hola.txt\nhola\nadeu\nfins\nEOF"
	got := renderCommandBox(true, command, 80)

	if len(got) != 7 {
		t.Fatalf("renderCommandBox() returned %d rows, want 7", len(got))
	}

	panelWidth := visibleWidth(got[0])
	for _, line := range got {
		if strings.Contains(line, "\n") {
			t.Fatalf("renderCommandBox() row contains embedded newline: %q", line)
		}
		if !strings.Contains(line, commandBoxBackground) {
			t.Fatalf("renderCommandBox() row does not contain command box background: %q", line)
		}
		if width := visibleWidth(line); width != panelWidth {
			t.Fatalf("renderCommandBox() row width = %d, want %d: %q", width, panelWidth, line)
		}
	}

	plainRows := make([]string, 0, len(got))
	for _, line := range got {
		plainRows = append(plainRows, stripANSISequences(line))
	}
	if !strings.Contains(plainRows[1], "  run › cat <<'EOF' > hola.txt") {
		t.Fatalf("renderCommandBox() first command row is not prefixed correctly: %q", plainRows[1])
	}
	for _, row := range plainRows[2:6] {
		if !strings.Contains(row, "        ") {
			t.Fatalf("renderCommandBox() multiline row is not indented to command level: %q", row)
		}
	}
}
