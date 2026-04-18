package main

import (
	"strings"
	"testing"
)

// TestThinkingStatusFramePlainText checks that the non-ANSI fallback stays static and unpadded.
func TestThinkingStatusFramePlainText(t *testing.T) {
	frame := thinkingStatusFrame(false, 0)
	if frame != thinkingStatusText {
		t.Fatalf("thinkingStatusFrame(false, 0) = %q, want %q", frame, thinkingStatusText)
	}
	if strings.HasPrefix(frame, " ") {
		t.Fatalf("thinkingStatusFrame(false, 0) should not have left padding: %q", frame)
	}
	if strings.Contains(frame, "\n") {
		t.Fatalf("thinkingStatusFrame(false, 0) contains an unexpected newline: %q", frame)
	}
}

// TestThinkingStatusFrameShimmer checks that the ANSI version keeps the same visible text while animating its colour.
func TestThinkingStatusFrameShimmer(t *testing.T) {
	first := thinkingStatusFrame(true, 0)
	second := thinkingStatusFrame(true, 1)

	if first == second {
		t.Fatalf("thinkingStatusFrame(true, 0) and thinkingStatusFrame(true, 1) should differ to animate the shimmer")
	}
	if visibleWidth(first) != len(thinkingStatusText) {
		t.Fatalf("visibleWidth(thinkingStatusFrame(true, 0)) = %d, want %d", visibleWidth(first), len(thinkingStatusText))
	}
	if visibleWidth(second) != len(thinkingStatusText) {
		t.Fatalf("visibleWidth(thinkingStatusFrame(true, 1)) = %d, want %d", visibleWidth(second), len(thinkingStatusText))
	}
	if strings.Contains(first, "\n") || strings.Contains(second, "\n") {
		t.Fatalf("thinkingStatusFrame(true, frame) should not contain newlines")
	}
}
