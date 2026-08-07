package ui

import (
	"bytes"
	"strings"
	"testing"

	"shellia/internal/core"
)

func TestGuideRendererNestsTechnicalActivity(t *testing.T) {
	output := renderConversationFixture(t, newGuideRenderer, false)
	assertOrdered(t, output,
		"│ Tu",
		"│ you › quant d'espai queda al disc?",
		"│ Shellia",
		"│   plan",
		"│   step 1/1",
		"│     system output",
		"│   Shellia",
	)
}

func TestGuideNoColorKeepsGeometryWithoutANSI(t *testing.T) {
	output := renderConversationFixture(t, newGuideRenderer, false)
	if strings.Contains(output, "\033[") {
		t.Fatalf("guide without ANSI contains escape sequence: %q", output)
	}
	if !strings.Contains(output, "│") {
		t.Fatalf("guide without ANSI loses rails: %q", output)
	}
}

func TestGuideANSIUsesCyanForUserAndMagentaForShellia(t *testing.T) {
	output := renderConversationFixture(t, newGuideRenderer, true)
	if !strings.Contains(output, colorCyan+"│") {
		t.Fatalf("guide ANSI output lacks cyan user rail: %q", output)
	}
	if !strings.Contains(output, colorMagenta+"│") {
		t.Fatalf("guide ANSI output lacks magenta Shellia rail: %q", output)
	}
}

func TestGuideOutputIsVisibleBeforeCloseAndCloseIsIdempotent(t *testing.T) {
	var output bytes.Buffer
	renderer := newGuideRenderer(&output, false)
	turn := renderer.beginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	step := turn.beginStep(testConfig(), 1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("available now")

	beforeClose := output.String()
	if !strings.Contains(beforeClose, "available now") {
		t.Fatalf("output before Close() = %q, want incremental system output", beforeClose)
	}
	if strings.Contains(beforeClose, "─") {
		t.Fatalf("output before Close() contains a turn separator: %q", beforeClose)
	}

	step.Close()
	turn.close()
	afterFirstClose := output.String()
	turn.close()
	if got := output.String(); got != afterFirstClose {
		t.Fatalf("second Close() changed output:\n got: %q\nwant: %q", got, afterFirstClose)
	}
}

func TestGuideUserTurnPreservesSubmittedPromptWhitespace(t *testing.T) {
	var output bytes.Buffer
	renderer := newGuideRenderer(&output, false)
	renderer.userTurn(core.InteractiveModeAI, "  spaced  ")
	if !strings.Contains(output.String(), "│ you ›   spaced  ") {
		t.Fatalf("guide user turn = %q", output.String())
	}

}
