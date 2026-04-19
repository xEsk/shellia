package main

import (
	"reflect"
	"strings"
	"testing"
)

// TestSessionBannerLinesPlain checks the no-colour startup banner fallback.
func TestSessionBannerLinesPlain(t *testing.T) {
	previousVersion := version
	version = "v9.9.9"
	t.Cleanup(func() {
		version = previousVersion
	})

	got := sessionBannerLines(false, 80)
	want := []string{
		"shellia session · v9.9.9",
		"  !<cmd>  /shell  /ai  /mode  exit  /quit  /clear  /context",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sessionBannerLines(false) = %#v, want %#v", got, want)
	}
}

// TestSessionBannerLinesANSI checks that the ANSI startup banner uses a consistent grey panel.
func TestSessionBannerLinesANSI(t *testing.T) {
	previousVersion := version
	version = "dev"
	t.Cleanup(func() {
		version = previousVersion
	})

	got := sessionBannerLines(true, 80)
	if len(got) != 4 {
		t.Fatalf("sessionBannerLines(true) returned %d rows, want %d", len(got), 4)
	}

	width := visibleWidth(got[0])
	if width < sessionBannerMinWidth {
		t.Fatalf("sessionBannerLines(true) width = %d, want >= %d", width, sessionBannerMinWidth)
	}

	for _, line := range got {
		if !strings.Contains(line, commandBoxBackground) {
			t.Fatalf("sessionBannerLines(true) row does not contain the grey background: %q", line)
		}
		if visibleWidth(line) != width {
			t.Fatalf("sessionBannerLines(true) row width = %d, want %d: %q", visibleWidth(line), width, line)
		}
	}
	if !strings.Contains(got[1], "shell") || !strings.Contains(got[1], "dev") {
		t.Fatalf("sessionBannerLines(true) title row missing brand or version: %q", got[1])
	}
}
