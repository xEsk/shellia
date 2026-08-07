package ui

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"

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

// TestBandsANSIRowsFillTerminalWidth catches backgrounds that stop after the visible content.
func TestBandsANSIRowsFillTerminalWidth(t *testing.T) {
	output := renderConversationFixture(t, newBandsRenderer, true)
	for _, content := range []string{"plan", "system output", "Queden 419Gi"} {
		row := bandsOutputRowContaining(t, output, content)
		if got, want := visibleWidth(row), 80; got != want {
			t.Fatalf("row containing %q has width %d, want %d: %q", content, got, want, row)
		}
	}
}

// TestBandsANSIRowsResumeBackgroundAfterNestedStyles catches background gaps caused by inner resets.
func TestBandsANSIRowsResumeBackgroundAfterNestedStyles(t *testing.T) {
	output := renderConversationFixture(t, newBandsRenderer, true)
	checks := []struct {
		content    string
		background string
	}{
		{content: "plan", background: bandsShelliaBackground},
		{content: "system output", background: bandsExecutionBackground},
		{content: "Queden 419Gi", background: bandsShelliaBackground},
	}

	for _, check := range checks {
		row := bandsOutputRowContaining(t, output, check.content)
		if !strings.HasPrefix(row, check.background) {
			t.Fatalf("row containing %q is outside its background: %q", check.content, row)
		}
		remaining := row
		for {
			reset := strings.Index(remaining, colorReset)
			if reset < 0 {
				break
			}
			remaining = remaining[reset+len(colorReset):]
			if stripANSISequences(remaining) != "" && !strings.HasPrefix(remaining, check.background) {
				t.Fatalf("row containing %q does not resume %q after reset: %q", check.content, check.background, row)
			}
		}
	}
}

// TestBandsFinalRepeatsShelliaLabel catches a final answer without the canonical answer identity.
func TestBandsFinalRepeatsShelliaLabel(t *testing.T) {
	output := stripANSISequences(renderConversationFixture(t, newBandsRenderer, true))
	assertOrdered(t, output, "system output", "Shellia", "Queden 419Gi")
}

// TestBandsSubmittedPromptPreservesLayoutAtFortyEightColumns catches prompt wrapping that loses user whitespace or ignores the prompt prefix width.
func TestBandsSubmittedPromptPreservesLayoutAtFortyEightColumns(t *testing.T) {
	output := renderBandsUserTurnAtWidth(t, 48, "alpha  beta\n1234567890123456789012345678901234567890\nsecond  line")
	for _, want := range []string{
		"▌   you › alpha  beta",
		"▌         12345678901234567890123456789012345678",
		"▌         90",
		"▌         second  line",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("48-column user turn lacks %q:\n%s", want, output)
		}
	}
	for _, row := range strings.Split(output, "\n") {
		row = strings.TrimSuffix(row, "\r")
		if strings.HasPrefix(row, bandsMarker) && visibleWidth(row) > 48 {
			t.Fatalf("user row width = %d, want <= 48: %q", visibleWidth(row), row)
		}
	}
}

// TestBandsMarkdownFinalRestoresBackgroundAfterBareReset catches Glamour reset sequences that expose the terminal background before row padding.
func TestBandsMarkdownFinalRestoresBackgroundAfterBareReset(t *testing.T) {
	var output bytes.Buffer
	renderer := newBandsRenderer(&output, true)
	turn := renderer.beginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	turn.final("Final with **bold text** and `inline code`.")
	turn.close()

	row := bandsOutputRowContaining(t, output.String(), "bold text")
	if !strings.Contains(row, "\033[m") {
		t.Fatalf("Markdown fixture did not emit the expected bare SGR reset: %q", row)
	}
	assertBandsBackgroundSurvivesResets(t, row, bandsShelliaBackground)
	if got, want := visibleWidth(row), 80; got != want {
		t.Fatalf("Markdown final row width = %d, want %d: %q", got, want, row)
	}
	lastBareReset := strings.LastIndex(row, "\033[m")
	afterReset := row[lastBareReset+len("\033[m"):]
	if !strings.HasPrefix(afterReset, bandsShelliaBackground) || strings.TrimSpace(stripANSISequences(afterReset)) != "" {
		t.Fatalf("Markdown background does not survive from bare reset through row padding: %q", row)
	}
}

// TestBandsWrapsNestedContentWithin48Columns catches plan rows sized before the Bands marker and indentation are applied.
func TestBandsWrapsNestedContentWithin48Columns(t *testing.T) {
	const width = 48
	output := renderNarrowBandsFixture(t, width)
	foundCommandContinuation := false

	for _, rendered := range strings.Split(output, "\n") {
		line := strings.TrimSuffix(stripANSISequences(rendered), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if visibleWidth(line) > width {
			t.Fatalf("bands row width = %d, want <= %d:\n%q\n\n%s", visibleWidth(line), width, line, output)
		}
		if !strings.HasPrefix(line, bandsMarker+" ") {
			t.Fatalf("bands continuation lost its marker: %q\n\n%s", line, output)
		}
		if strings.Contains(line, "continuation-token") {
			foundCommandContinuation = true
			if !strings.HasPrefix(line, bandsMarker+"           ") {
				t.Fatalf("command continuation lost nested indentation: %q", line)
			}
		}
	}

	if !foundCommandContinuation {
		t.Fatalf("long command did not produce the expected continuation:\n%s", output)
	}
	plain := stripANSISequences(output)
	for _, want := range []string{"plan", "risk", "safety", "confirm", "Shellia", "final answer"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("narrow bands output lacks %q:\n%s", want, output)
		}
	}
}

func bandsOutputRowContaining(t *testing.T, output string, content string) string {
	t.Helper()
	for _, row := range strings.Split(output, "\n") {
		row = strings.TrimSuffix(row, "\r")
		if strings.Contains(stripANSISequences(row), content) {
			return row
		}
	}
	t.Fatalf("output lacks row containing %q:\n%s", content, output)
	return ""
}

func assertBandsBackgroundSurvivesResets(t *testing.T, row string, background string) {
	t.Helper()
	remaining := row
	for {
		reset, resetLength := nextBandsSGRReset(remaining)
		if reset < 0 {
			return
		}
		remaining = remaining[reset+resetLength:]
		if stripANSISequences(remaining) != "" && !strings.HasPrefix(remaining, background) {
			t.Fatalf("row does not resume %q after SGR reset: %q", background, row)
		}
	}
}

func nextBandsSGRReset(text string) (int, int) {
	reset := strings.Index(text, colorReset)
	bareReset := strings.Index(text, "\033[m")
	if reset < 0 || (bareReset >= 0 && bareReset < reset) {
		return bareReset, len("\033[m")
	}
	return reset, len(colorReset)
}

func renderBandsUserTurnAtWidth(t *testing.T, width uint16, text string) string {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	defer ptmx.Close()
	if err := pty.Setsize(tty, &pty.Winsize{Cols: width + boxHorizontalMargin, Rows: 24}); err != nil {
		tty.Close()
		t.Fatalf("set pty size: %v", err)
	}

	read := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(ptmx)
		read <- output
	}()
	newBandsRenderer(tty, false).userTurn(core.InteractiveModeAI, text)
	if err := tty.Close(); err != nil {
		t.Fatalf("close pty: %v", err)
	}
	return string(<-read)
}

func renderNarrowBandsFixture(t *testing.T, width int) string {
	t.Helper()
	reader, terminal, err := pty.Open()
	if err != nil {
		t.Fatalf("open PTY: %v", err)
	}
	defer reader.Close()
	defer terminal.Close()

	physicalWidth := width + boxHorizontalMargin
	if err := pty.Setsize(terminal, &pty.Winsize{Cols: uint16(physicalWidth), Rows: 24}); err != nil {
		t.Fatalf("set PTY width: %v", err)
	}
	var output bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&output, reader)
		readDone <- copyErr
	}()

	cfg := testConfig()
	cfg.Verbose = true
	cfg.PlanOnly = true
	plan := core.CommandPlan{
		Command:              "inspect --alpha one --beta two --continuation-token omega",
		Purpose:              "Inspect a deliberately long purpose that must wrap inside the nested bands rows.",
		Risk:                 "medium",
		Classification:       "risky",
		RequiresConfirmation: true,
	}
	renderer := newBandsRenderer(terminal, true)
	turn := renderer.beginShelliaTurn(cfg, core.ContextInfo{CWD: "/tmp"})
	turn.plan(cfg, "This deliberately long summary must wrap without using the terminal's automatic wrapping.", []core.CommandPlan{plan}, false)
	turn.final("This **deliberately long final answer** must also wrap inside the band marker and indentation.")
	turn.close()

	if err := terminal.Close(); err != nil {
		t.Fatalf("close PTY terminal: %v", err)
	}
	if err := <-readDone; err != nil && !errors.Is(err, syscall.EIO) {
		t.Fatalf("read PTY output: %v", err)
	}
	return output.String()
}
