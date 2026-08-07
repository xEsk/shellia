package ui

import (
	"bytes"
	"strings"
	"testing"

	"shellia/internal/core"
)

// TestBandsRendererOwnsWholeTurns catches a Bands renderer that leaves semantic turn content outside its band.
func TestBandsRendererOwnsWholeTurns(t *testing.T) {
	output := renderConversationFixture(t, newBandsRenderer, true)
	if !strings.Contains(output, "▌") {
		t.Fatalf("bands marker missing: %q", output)
	}
	assertOrdered(t, output, "Tu", "Shellia", "plan", "step 1/1", "system output")
}

// TestBandsRendererUsesSemanticColorsAndBackground catches a renderer without the distinct ANSI band treatment.
func TestBandsRendererUsesSemanticColorsAndBackground(t *testing.T) {
	output := renderConversationFixture(t, newBandsRenderer, true)
	for _, sequence := range []string{"\033[48;", colorCyan, colorMagenta} {
		if !strings.Contains(output, sequence) {
			t.Fatalf("bands output lacks %q: %q", sequence, output)
		}
	}
}

// TestBandsNoColorKeepsGeometryWithoutANSI catches fallback to plain output when ANSI is disabled.
func TestBandsNoColorKeepsGeometryWithoutANSI(t *testing.T) {
	output := renderConversationFixture(t, newBandsRenderer, false)
	if strings.Contains(output, "\033[") {
		t.Fatalf("no-color output contains ANSI: %q", output)
	}
	if !strings.Contains(output, "▌") {
		t.Fatalf("no-color bands marker missing: %q", output)
	}
}

// TestBandsRendererStreamsBeforeCloseAndClosesOnce catches buffered output and a non-idempotent turn close.
func TestBandsRendererStreamsBeforeCloseAndClosesOnce(t *testing.T) {
	var out bytes.Buffer
	r := &Renderer{impl: newBandsRenderer(&out, false)}
	turn := r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("first")
	if !strings.Contains(out.String(), "first") {
		t.Fatal("output was buffered")
	}
	step.Close()
	turn.Final("done")
	turn.Close()
	afterFirstClose := out.String()
	turn.Close()
	if out.String() != afterFirstClose {
		t.Fatal("second Close changed bands output")
	}
}
