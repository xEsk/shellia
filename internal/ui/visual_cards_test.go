package ui

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xEsk/shellia/internal/core"

	"github.com/creack/pty"
	configpkg "github.com/xEsk/shellia/internal/config"
)

func TestCardsRendererStreamsBeforeCloseAndClosesOnce(t *testing.T) {
	var out bytes.Buffer
	r := &Renderer{impl: newCardsRenderer(&out, false)}
	turn := r.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	step := turn.BeginStep(1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("first")
	if !strings.Contains(out.String(), "first") {
		t.Fatal("output was buffered until close")
	}

	step.Close()
	beforeTurnClose := out.String()
	turn.Final("done")
	turn.Close()
	afterFirstClose := out.String()
	turn.Close()

	if len(afterFirstClose) <= len(beforeTurnClose) {
		t.Fatal("turn close did not emit its final border")
	}
	if strings.Count(afterFirstClose, "\n╰") != 1 {
		t.Fatalf("outer bottom border count = %d, want 1:\n%s", strings.Count(afterFirstClose, "\n╰"), afterFirstClose)
	}
	if out.String() != afterFirstClose {
		t.Fatal("second Close changed card output")
	}
}

func TestCardsRendererShowsFlushedPartialOutput(t *testing.T) {
	var out bytes.Buffer
	turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
	step := turn.BeginStep(1, 1, testPlan())
	writer := &prefixedWriter{box: step}
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if strings.Contains(out.String(), "partial") {
		t.Fatalf("partial line rendered before Flush():\n%s", out.String())
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !strings.Contains(out.String(), "partial") {
		t.Fatalf("Flush() did not render partial output:\n%s", out.String())
	}
	step.Close()
	turn.Close()
}

func TestCardsRendererKeepsConversationHierarchy(t *testing.T) {
	output := renderCardsConversationFixture(t, false, "xesc")
	assertOrdered(
		t,
		output,
		"╭─ xesc",
		"│   quant d'espai queda al disc?",
		"╰",
		"╭─ Shellia · dev",
		"plan",
		"step 1/1",
		"system output",
		"419Gi available",
		"Queden 419Gi lliures al disc arrel (/).",
	)
	if strings.Count(output, "╭─ xesc") != 1 || strings.Count(output, "╭─ Shellia · dev") != 1 {
		t.Fatalf("actor cards were split into multiple cards:\n%s", output)
	}
	for _, unwanted := range []string{"Tu", "you ›", "What do you want Shellia to do?"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("cards transcript contains obsolete prompt identity %q:\n%s", unwanted, output)
		}
	}
}

func TestCardsRendererNoColorKeepsBordersWithoutANSI(t *testing.T) {
	output := renderCardsConversationFixture(t, false, "xesc")
	if strings.Contains(output, "\033[") {
		t.Fatalf("no-color cards emitted ANSI: %q", output)
	}
	for _, want := range []string{"╭─ xesc", "╭─ Shellia · dev", "│ ", " │", "╰"} {
		if !strings.Contains(output, want) {
			t.Fatalf("no-color cards lack %q:\n%s", want, output)
		}
	}
}

func TestCardsRendererPreservesSubmittedPromptWhitespace(t *testing.T) {
	var out bytes.Buffer
	renderer := NewRenderer(&out, Presentation{Style: configpkg.VisualStyleCards, User: "xesc"})
	renderer.UserTurn(core.InteractiveModeAI, "  first  line\n\n    indented  text  ")

	rows := cardsUserContentRows(t, out.String())
	if len(rows) != 5 {
		t.Fatalf("user content row count = %d, want top padding, 3 submitted rows and bottom padding:\n%s", len(rows), out.String())
	}
	if strings.TrimSpace(rows[0]) != "" || strings.TrimSpace(rows[4]) != "" {
		t.Fatalf("user card lacks vertical padding: %q", rows)
	}
	if !strings.HasPrefix(rows[1], "    first  line") {
		t.Fatalf("first submitted row lost whitespace: %q", rows[1])
	}
	if strings.TrimSpace(rows[2]) != "" {
		t.Fatalf("submitted blank line = %q, want blank", rows[2])
	}
	if !strings.HasPrefix(rows[3], "      indented  text  ") {
		t.Fatalf("indented submitted row lost whitespace: %q", rows[3])
	}
}

func TestCardsInteractivePromptUsesActiveUser(t *testing.T) {
	renderer := NewRenderer(io.Discard, Presentation{Style: configpkg.VisualStyleCards, ANSI: true, User: "xesc"})
	if got := stripANSISequences(renderer.interactivePromptPrefix(true, core.InteractiveModeAI)); got != "xesc › " {
		t.Fatalf("cards active prompt prefix = %q, want %q", got, "xesc › ")
	}
	if !renderer.ownsUserTurnQuestion(core.InteractiveModeAI) {
		t.Fatal("cards does not own the standalone prompt question")
	}
}

func TestCardsThinkingFrameContinuesOpenShelliaCard(t *testing.T) {
	var output bytes.Buffer
	turn := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleCards, ANSI: true, User: "xesc"}).
		BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	output.Reset()

	renderThinkingFrame(&output, true, 0, true, turn.ThinkingPrefix())

	frame := output.String()
	if strings.HasPrefix(frame, "\n") {
		t.Fatalf("cards thinking frame opened an unowned blank row: %q", frame)
	}
	if !strings.Contains(frame, cardsShelliaBackground) ||
		!strings.Contains(stripANSISequences(frame), "│   "+thinkingStatusBullet+" ") {
		t.Fatalf("cards thinking frame does not continue the open Shellia card: %q", frame)
	}
}

func TestCardsReusesOneSpacerBetweenHeaderThinkingAndPlan(t *testing.T) {
	var output bytes.Buffer
	turn := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleCards}).
		BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	turn.ThinkingPrefix()
	turn.Plan("Comprovar l'espai disponible.", nil, false)

	plain := strings.ReplaceAll(stripANSISequences(output.String()), "\r", "")
	between := textBetween(t, plain, "/tmp", "│   plan")
	if got := countBlankCardRows(between); got != 1 {
		t.Fatalf("cards spacer rows between header and plan = %d, want 1:\n%s", got, plain)
	}
}

func TestCardsThinkingAfterExecutionHasOneSpacerRow(t *testing.T) {
	var output bytes.Buffer
	turn := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleCards}).
		BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	step := turn.BeginStep(1, 1, testPlan())
	step.OutputLine("done")
	step.Close()
	output.Reset()

	renderThinkingFrame(&output, false, 0, true, turn.ThinkingPrefix())

	plain := strings.ReplaceAll(stripANSISequences(output.String()), "\r", "")
	rows := strings.Split(plain, "\n")
	if len(rows) < 2 || !blankCardRow(rows[0]) || !strings.HasPrefix(rows[1], "│   "+thinkingStatusLineText) {
		t.Fatalf("cards thinking lacks one leading spacer row: %q", plain)
	}
}

func countBlankCardRows(output string) int {
	count := 0
	for _, row := range strings.Split(output, "\n") {
		if blankCardRow(row) {
			count++
		}
	}
	return count
}

func blankCardRow(row string) bool {
	if !strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│") {
		return false
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(row, "│"), "│")) == ""
}

func TestCardsUsesPaddedActorSurfacesAndNestedExecution(t *testing.T) {
	output := renderCardsConversationFixture(t, true, "xesc")
	plain := stripANSISequences(output)
	for _, want := range []string{
		"╭─ xesc",
		"│   quant d'espai queda al disc?",
		"╭─ Shellia · dev",
		"│   plan",
		"│   ┌─ step 1/1",
		"│   │ ",
		"│   Shellia",
		"│     Queden 419Gi",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("cards template hierarchy lacks %q:\n%s", want, plain)
		}
	}
	for _, background := range []string{
		"\033[48;2;17;49;58m",
		"\033[48;2;64;45;70m",
		"\033[48;2;35;39;41m",
	} {
		if !strings.Contains(output, background) {
			t.Fatalf("cards output lacks background %q:\n%q", background, output)
		}
	}
}

func TestCardsRoundedBordersLeaveCornerCellsUnfilled(t *testing.T) {
	output := renderCardsConversationFixture(t, true, "xesc")
	var sawUserInterior bool
	var sawShelliaInterior bool
	var roundedBorders int

	for _, row := range strings.Split(output, "\r\n") {
		plain := stripANSISequences(row)
		switch {
		case strings.HasPrefix(plain, "╭─ xesc"),
			strings.HasPrefix(plain, "╭─ Shellia"),
			strings.HasPrefix(plain, "╰"):
			roundedBorders++
			if strings.Contains(row, cardsUserBackground) || strings.Contains(row, cardsShelliaBackground) {
				t.Fatalf("rounded border row painted its corner cells: %q", row)
			}
		case strings.Contains(plain, "quant d'espai queda al disc?"):
			sawUserInterior = strings.Contains(row, cardsUserBackground)
		case strings.Contains(plain, "Queden 419Gi lliures"):
			sawShelliaInterior = strings.Contains(row, cardsShelliaBackground)
		}
	}

	if roundedBorders != 4 {
		t.Fatalf("rounded border row count = %d, want 4:\n%s", roundedBorders, stripANSISequences(output))
	}
	if !sawUserInterior || !sawShelliaInterior {
		t.Fatalf("actor card interiors lost their backgrounds: user=%t Shellia=%t", sawUserInterior, sawShelliaInterior)
	}
}

func TestCardsRendererWrapsStreamedOutputWithinTerminalWidth(t *testing.T) {
	const terminalWidth = 48
	payload := strings.Repeat("0123456789", 6)
	for _, ansi := range []bool{false, true} {
		t.Run(map[bool]string{false: "no ANSI", true: "ANSI"}[ansi], func(t *testing.T) {
			output := renderCardsAtTerminalWidth(t, terminalWidth, ansi, func(renderer *Renderer) {
				turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{})
				step := turn.BeginStep(1, 1, testPlan())
				step.OutputLabel()
				step.OutputLine(payload)
				step.Close()
				turn.Close()
			})

			var renderedPayload strings.Builder
			payloadRows := 0
			for _, rawLine := range strings.Split(output, "\n") {
				line := strings.TrimRight(stripANSISequences(rawLine), "\r")
				if visibleWidth(line) > terminalWidth {
					t.Fatalf("visible line width = %d, want <= %d: %q\n%s", visibleWidth(line), terminalWidth, line, output)
				}
				if !strings.Contains(line, "0123456789") {
					continue
				}
				payloadRows++
				if !strings.HasPrefix(line, "│   │ ") || !strings.HasSuffix(line, " │   │") {
					t.Fatalf("stream continuation lacks nested rails: %q\n%s", line, output)
				}
				content := strings.TrimSuffix(strings.TrimPrefix(line, "│   │ "), " │   │")
				renderedPayload.WriteString(strings.TrimSpace(content))
			}
			if payloadRows < 2 {
				t.Fatalf("payload row count = %d, want at least 2:\n%s", payloadRows, output)
			}
			if renderedPayload.String() != payload {
				t.Fatalf("wrapped payload = %q, want %q", renderedPayload.String(), payload)
			}
		})
	}
}

func TestCardsRendererClosesFinalOnlyAndEarlyTurns(t *testing.T) {
	t.Run("final without step", func(t *testing.T) {
		var out bytes.Buffer
		turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
		turn.Final("blocked")
		turn.Close()
		if !strings.Contains(out.String(), "blocked") || strings.Count(out.String(), "\n╰") != 1 {
			t.Fatalf("final-only turn was not closed once:\n%s", out.String())
		}
	})

	t.Run("close before final", func(t *testing.T) {
		var out bytes.Buffer
		turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
		turn.Close()
		closed := out.String()
		turn.Final("must not render")
		turn.Close()
		if out.String() != closed {
			t.Fatalf("closed turn accepted more output:\n%s", out.String())
		}
		if strings.Count(closed, "\n╰") != 1 {
			t.Fatalf("early-close bottom border count = %d, want 1:\n%s", strings.Count(closed, "\n╰"), closed)
		}
	})
}

func TestCardsRendererSuspendResumeCreatesClosedContinuation(t *testing.T) {
	var out bytes.Buffer
	turn := (&Renderer{impl: newCardsRenderer(&out, false)}).BeginShelliaTurn(testConfig(), core.ContextInfo{})
	step := turn.BeginStep(1, 1, testPlan())
	step.OutputLine("before handoff")
	turn.Suspend()
	if strings.Count(out.String(), "\n╰") != 1 || strings.Count(out.String(), "│   └") != 1 {
		t.Fatalf("Suspend did not close the active card:\n%s", out.String())
	}

	turn.Resume()
	step.OutputLine("after handoff")
	step.Close()
	turn.Final("done")
	turn.Close()
	closed := out.String()
	turn.Close()
	if strings.Count(closed, "╭─ Shellia") != 2 || strings.Count(closed, "\n╰") != 2 {
		t.Fatalf("continuation geometry is unbalanced:\n%s", closed)
	}
	if strings.Count(closed, "│   ┌─ step 1/1") != 2 || strings.Count(closed, "│   └") != 2 {
		t.Fatalf("nested continuation geometry is unbalanced:\n%s", closed)
	}
	assertOrdered(t, closed, "before handoff", "╰", "Shellia · continued", "step 1/1 · continued", "after handoff")
	if out.String() != closed {
		t.Fatal("repeated Close changed resumed card output")
	}
}

func cardsUserContentRows(t *testing.T, output string) []string {
	t.Helper()
	lines := strings.Split(stripANSISequences(output), "\n")
	rows := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(line, "│ ") || !strings.HasSuffix(line, " │") {
			continue
		}
		rows = append(rows, strings.TrimSuffix(strings.TrimPrefix(line, "│ "), " │"))
	}
	return rows
}

func renderCardsConversationFixture(t *testing.T, ansi bool, user string) string {
	t.Helper()
	var output bytes.Buffer
	renderer := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleCards, ANSI: ansi, User: user})
	renderer.UserTurn(core.InteractiveModeAI, "quant d'espai queda al disc?")
	turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/Users/Xesc/Documents/Scripts"})
	turn.Plan("Cal consultar l'espai disponible.", []core.CommandPlan{testPlan()}, false)
	step := turn.BeginStep(1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("419Gi available")
	step.Close()
	turn.Final("Queden 419Gi lliures al disc arrel (/).")
	turn.Close()
	return output.String()
}

func renderCardsAtTerminalWidth(t *testing.T, width int, ansi bool, render func(*Renderer)) string {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open() error = %v", err)
	}
	t.Cleanup(func() { master.Close() }) //nolint:errcheck // best-effort test cleanup.
	if err := pty.Setsize(slave, &pty.Winsize{Cols: uint16(width), Rows: 24}); err != nil {
		slave.Close() //nolint:errcheck // best-effort cleanup after setup failure.
		t.Fatalf("pty.Setsize() error = %v", err)
	}
	type readResult struct {
		data []byte
		err  error
	}
	readDone := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(master)
		readDone <- readResult{data: data, err: err}
	}()

	renderer := &Renderer{impl: newCardsRenderer(slave, ansi), ansi: ansi}
	render(renderer)
	if err := slave.Close(); err != nil {
		t.Fatalf("slave.Close() error = %v", err)
	}
	var result readResult
	select {
	case result = <-readDone:
	case <-time.After(250 * time.Millisecond):
		master.Close() //nolint:errcheck // force the PTY reader to return after collecting output.
		result = <-readDone
	}
	if result.err != nil && len(result.data) == 0 {
		t.Fatalf("io.ReadAll(pty) error = %v", result.err)
	}
	return string(result.data)
}
