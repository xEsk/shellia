package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"

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

func TestGuideWrapsNestedContentWithin48Columns(t *testing.T) {
	const width = 48
	output := renderNarrowGuideFixture(t, width)
	foundCommandContinuation := false

	for _, rendered := range strings.Split(output, "\n") {
		line := strings.TrimSuffix(stripANSISequences(rendered), "\r")
		if line == "" {
			continue
		}
		if visibleWidth(line) > width {
			t.Fatalf("guide row width = %d, want <= %d:\n%q\n\n%s", visibleWidth(line), width, line, output)
		}
		if strings.Trim(line, "─") == "" {
			continue
		}
		if !strings.HasPrefix(line, "│ ") {
			t.Fatalf("guide continuation lost its rail: %q\n\n%s", line, output)
		}
		if strings.Contains(line, "continuation-token") {
			foundCommandContinuation = true
			if !strings.HasPrefix(line, "│           ") {
				t.Fatalf("command continuation lost nested indentation: %q", line)
			}
		}
	}

	if !foundCommandContinuation {
		t.Fatalf("long command did not produce the expected continuation:\n%s", output)
	}
	for _, want := range []string{"plan", "confirm", "system output", "Shellia"} {
		if !strings.Contains(stripANSISequences(output), want) {
			t.Fatalf("narrow guide output lacks %q:\n%s", want, output)
		}
	}
}

func TestGuideWrapsSubmittedPromptWithin48Columns(t *testing.T) {
	const (
		width = 48
		text  = "alpha  beta\n12345678901234567890123456789012345678901234567890\nsecond  line"
	)
	wantLines := []string{
		"│ Tu",
		"│ you › alpha  beta",
		"│       1234567890123456789012345678901234567890",
		"│       1234567890",
		"│       second  line",
	}

	for _, ansi := range []bool{false, true} {
		t.Run(fmt.Sprintf("ansi_%t", ansi), func(t *testing.T) {
			output := renderGuidePTY(t, width, func(target io.Writer) {
				newGuideRenderer(target, ansi).userTurn(core.InteractiveModeAI, text)
			})
			plain := stripANSISequences(output)
			for _, want := range wantLines {
				if !strings.Contains(plain, want) {
					t.Fatalf("wrapped prompt lacks %q:\n%s", want, output)
				}
			}
			for _, rendered := range strings.Split(output, "\n") {
				line := strings.TrimSuffix(stripANSISequences(rendered), "\r")
				if visibleWidth(line) > width {
					t.Fatalf("prompt row width = %d, want <= %d: %q", visibleWidth(line), width, line)
				}
			}
			if ansi && !strings.Contains(output, colorCyan) {
				t.Fatalf("ANSI prompt lacks cyan identity: %q", output)
			}
			if !ansi && strings.Contains(output, "\033[") {
				t.Fatalf("no-color prompt contains ANSI: %q", output)
			}
		})
	}
}

func TestGuideWrapsStreamedOutputWithin48Columns(t *testing.T) {
	const (
		width   = 48
		payload = "alpha  beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron"
	)

	for _, ansi := range []bool{false, true} {
		t.Run(fmt.Sprintf("ansi_%t", ansi), func(t *testing.T) {
			output := renderGuidePTY(t, width, func(target io.Writer) {
				renderer := newGuideRenderer(target, ansi)
				turn := renderer.beginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
				step := turn.beginStep(testConfig(), 1, 1, testPlan())
				step.OutputLabel()
				step.OutputLine(payload)
				step.Close()
				turn.close()
			})

			plain := stripANSISequences(output)
			rows := strings.Split(plain, "\n")
			outputStarted := false
			chunks := make([]string, 0, 3)
			for _, rendered := range rows {
				line := strings.TrimSuffix(rendered, "\r")
				if strings.Contains(line, "system output") {
					outputStarted = true
					continue
				}
				if !outputStarted || line == "│     " {
					continue
				}
				if strings.Trim(line, "─") == "" {
					break
				}
				if visibleWidth(line) > width {
					t.Fatalf("streamed row width = %d, want <= %d: %q", visibleWidth(line), width, line)
				}
				if !strings.HasPrefix(line, "│     ") {
					t.Fatalf("streamed continuation lost nested guide: %q", line)
				}
				chunks = append(chunks, strings.TrimPrefix(line, "│     "))
			}

			if len(chunks) < 2 {
				t.Fatalf("long streamed output did not wrap:\n%s", output)
			}
			reconstructed := strings.TrimPrefix(strings.Join(chunks, ""), "  ")
			if reconstructed != payload {
				t.Fatalf("reconstructed payload = %q, want %q", reconstructed, payload)
			}
			if ansi && !strings.Contains(output, colorDim) {
				t.Fatalf("ANSI output lacks neutral guide style: %q", output)
			}
			if !ansi && strings.Contains(output, "\033[") {
				t.Fatalf("no-color output contains ANSI: %q", output)
			}
		})
	}
}

func renderGuidePTY(t *testing.T, width int, render func(io.Writer)) string {
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

	render(terminal)
	if err := terminal.Close(); err != nil {
		t.Fatalf("close PTY terminal: %v", err)
	}
	if err := <-readDone; err != nil && !errors.Is(err, syscall.EIO) {
		t.Fatalf("read PTY output: %v", err)
	}
	return output.String()
}

func renderNarrowGuideFixture(t *testing.T, width int) string {
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
		Purpose:              "Inspect a deliberately long purpose that must wrap inside the nested guide rows.",
		Risk:                 "medium",
		Classification:       "risky",
		RequiresConfirmation: true,
	}
	renderer := newGuideRenderer(terminal, true)
	turn := renderer.beginShelliaTurn(cfg, core.ContextInfo{CWD: "/tmp"})
	turn.plan(cfg, "This deliberately long summary must wrap without using the terminal's automatic wrapping.", []core.CommandPlan{plan}, false)
	step := turn.beginStep(cfg, 1, 1, plan)
	step.OutputLabel()
	step.OutputLine("short output")
	step.Close()
	turn.final("This deliberately long final answer must also wrap inside the guide rail and indentation.")
	turn.close()

	if err := terminal.Close(); err != nil {
		t.Fatalf("close PTY terminal: %v", err)
	}
	if err := <-readDone; err != nil && !errors.Is(err, syscall.EIO) {
		t.Fatalf("read PTY output: %v", err)
	}
	return output.String()
}
