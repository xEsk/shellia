package ui

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/term"

	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

// TestBandsRendererOwnsWholeTurns catches a Bands renderer that leaves semantic turn content outside its band.
func TestBandsRendererOwnsWholeTurns(t *testing.T) {
	output := renderBandsConversationFixture(t, true, "xesc")
	if !strings.Contains(output, "▌") {
		t.Fatalf("bands marker missing: %q", output)
	}
	assertOrdered(t, stripANSISequences(output), "xesc", "Shellia · dev", "plan", "step 1/1", "system output")
	for _, unwanted := range []string{"Tu", "you ›", "What do you want Shellia to do?"} {
		if strings.Contains(stripANSISequences(output), unwanted) {
			t.Fatalf("bands transcript contains obsolete prompt identity %q:\n%s", unwanted, output)
		}
	}
}

func TestBandsInteractivePromptUsesActiveUser(t *testing.T) {
	renderer := NewRenderer(io.Discard, Presentation{Style: configpkg.VisualStyleBands, ANSI: true, User: "xesc"})
	if got := stripANSISequences(renderer.interactivePromptPrefix(true, core.InteractiveModeAI)); got != "xesc › " {
		t.Fatalf("bands active prompt prefix = %q, want %q", got, "xesc › ")
	}
	if !renderer.ownsUserTurnQuestion(core.InteractiveModeAI) {
		t.Fatal("bands does not own the standalone prompt question")
	}
}

func TestBandsSubmittedUserTurnUsesFullWidthPaddedSurface(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands, ANSI: true, User: "xesc"})
	renderer.UserTurn(core.InteractiveModeAI, "quant espai queda al disc?")

	rows := bandsNonEmptyRenderedRows(output.String())
	if len(rows) != 4 {
		t.Fatalf("bands user surface rows = %d, want top padding, identity, prompt and bottom padding: %q", len(rows), output.String())
	}
	for _, row := range rows {
		if !strings.HasPrefix(row, bandsUserBackground) {
			t.Fatalf("bands user row lacks the user background: %q", row)
		}
		if got := visibleWidth(row); got != 80 {
			t.Fatalf("bands user row width = %d, want 80: %q", got, row)
		}
	}
	if strings.TrimSpace(stripANSISequences(rows[0])) != bandsMarker ||
		strings.TrimSpace(stripANSISequences(rows[3])) != bandsMarker {
		t.Fatalf("bands user surface lacks vertical padding rows: %q", output.String())
	}
	assertOrdered(t, stripANSISequences(output.String()), "xesc", "quant espai queda al disc?")
	if !strings.Contains(output.String(), "\r\n") {
		t.Fatalf("bands user rows do not return the cursor to column zero: %q", output.String())
	}
}

func TestBandsShelliaSurfacesUseSameVerticalPaddingAsUser(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands, ANSI: true, User: "xesc"})

	turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	assertBandsShelliaSegmentHasVerticalPadding(t, output.String(), "Shellia · dev", "/tmp")

	start := output.Len()
	turn.Plan(testConfig(), "Cal consultar l'espai disponible.", nil, false)
	assertBandsShelliaContinuationHasBottomPadding(t, output.String()[start:], "plan", "Cal consultar l'espai disponible.")

	start = output.Len()
	turn.Final("Queden 419Gi lliures.")
	assertBandsShelliaContinuationHasBottomPadding(t, output.String()[start:], "Shellia", "Queden 419Gi lliures.")
}

func TestBandsUserTurnReturnsToColumnZeroWhilePromptTerminalIsRaw(t *testing.T) {
	reader, terminal, err := pty.Open()
	if err != nil {
		t.Fatalf("open PTY: %v", err)
	}
	defer reader.Close()
	defer terminal.Close()

	state, err := term.MakeRaw(int(terminal.Fd()))
	if err != nil {
		t.Fatalf("make PTY raw: %v", err)
	}
	defer term.Restore(int(terminal.Fd()), state) //nolint:errcheck // best-effort test cleanup.

	var output bytes.Buffer
	readDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&output, reader)
		readDone <- copyErr
	}()

	NewRenderer(terminal, Presentation{Style: configpkg.VisualStyleBands, ANSI: true, User: "xesc"}).
		UserTurn(core.InteractiveModeAI, "quant espai queda al disc?")
	if err := terminal.Close(); err != nil {
		t.Fatalf("close PTY terminal: %v", err)
	}
	if err := <-readDone; err != nil && !errors.Is(err, syscall.EIO) {
		t.Fatalf("read PTY output: %v", err)
	}

	raw := output.Bytes()
	for index, value := range raw {
		if value == '\n' && (index == 0 || raw[index-1] != '\r') {
			t.Fatalf("bands raw row %d ends with LF without carriage return: %q", index, output.String())
		}
	}
	for _, row := range strings.Split(stripANSISequences(output.String()), "\r\n") {
		if strings.TrimSpace(row) != "" && !strings.HasPrefix(row, bandsMarker) {
			t.Fatalf("bands raw row does not start at column zero: %q", row)
		}
	}
}

func TestBandsUsesSubtleTrueColorSurfaces(t *testing.T) {
	output := renderBandsConversationFixture(t, true, "xesc")
	checks := []struct {
		content    string
		background string
	}{
		{content: "xesc", background: "\033[48;2;17;49;58m"},
		{content: "Shellia · dev", background: "\033[48;2;64;45;70m"},
		{content: "step 1/1", background: "\033[48;2;35;39;41m"},
	}
	for _, check := range checks {
		row := bandsOutputRowContaining(t, output, check.content)
		if !strings.HasPrefix(row, check.background) {
			t.Fatalf("bands row containing %q has background %q, want %q", check.content, row, check.background)
		}
	}
}

func TestBandsUserSurfaceUsesGuidePalette(t *testing.T) {
	const guideUserSurface = "\033[48;2;17;49;58m"
	outputs := map[string]string{
		"bands": renderBandsConversationFixture(t, true, "xesc"),
		"guide": renderConversationFixture(t, newGuideRenderer, true),
	}
	for name, output := range outputs {
		if !strings.Contains(output, guideUserSurface) {
			t.Fatalf("%s user surface does not use the shared background color: %q", name, output)
		}
	}
}

func TestBandsShelliaSurfacesMatchTemplateHierarchy(t *testing.T) {
	output := renderBandsConversationFixture(t, true, "xesc")
	plain := stripANSISequences(output)

	brandRows := 0
	for _, row := range strings.Split(output, "\n") {
		if !strings.Contains(stripANSISequences(row), "Shellia") {
			continue
		}
		brandRows++
		if !strings.Contains(row, colorWhite+colorBold+"Shell") || !strings.Contains(row, colorCyan+colorBold+"ia") {
			t.Fatalf("bands Shellia row does not preserve the multicolor brand: %q", row)
		}
	}
	if brandRows != 2 {
		t.Fatalf("bands multicolor Shellia brand rows = %d, want header and final response:\n%q", brandRows, output)
	}
	checks := []struct {
		content    string
		background string
	}{
		{content: "Shellia · dev", background: bandsShelliaBackground},
		{content: "plan", background: bandsShelliaBackground},
		{content: "step 1/1", background: bandsExecutionBackground},
		{content: "419Gi available", background: bandsExecutionBackground},
		{content: "Queden 419Gi", background: bandsShelliaBackground},
	}
	for _, check := range checks {
		row := bandsOutputRowContaining(t, output, check.content)
		if !strings.HasPrefix(row, check.background) {
			t.Fatalf("bands row containing %q has the wrong surface: %q", check.content, row)
		}
	}
	assertBandsNoTerminalBlankBetween(t, plain, "/Users/Xesc/Documents/Scripts", "plan")
	assertBandsNoTerminalBlankBetween(t, plain, "Cal consultar l'espai disponible.", "step 1/1")
	assertBandsNoTerminalBlankBetween(t, plain, "419Gi available", "Shellia")
}

func TestBandsHeaderAndPlanShareSinglePaddingRow(t *testing.T) {
	output := stripANSISequences(renderBandsConversationFixture(t, false, "xesc"))
	if got := bandsPaddingRowsBetween(output, "/Users/Xesc/Documents/Scripts", "plan"); got != 1 {
		t.Fatalf("bands padding rows between header and plan = %d, want 1:\n%s", got, output)
	}
}

func TestBandsExecutionAlignsWithPlanBody(t *testing.T) {
	output := stripANSISequences(renderBandsConversationFixture(t, false, "xesc"))
	for _, row := range strings.Split(output, "\r\n") {
		if !strings.Contains(row, "step 1/1") {
			continue
		}
		if !strings.HasPrefix(row, bandsMarker+"    step 1/1") {
			t.Fatalf("bands step is not aligned with the plan body: %q", row)
		}
		return
	}
	t.Fatalf("bands output lacks step row:\n%s", output)
}

func TestBandsLeavesSingleTerminalGapBeforeNextPrompt(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands, User: "xesc"})
	turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{})
	turn.Final("final answer")
	turn.Close()

	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("create non-terminal stdin: %v", err)
	}
	defer stdin.Close()
	if _, err := readInteractivePromptWithRenderer(
		false,
		bufio.NewReader(strings.NewReader("exit\n")),
		stdin,
		&output,
		core.InteractiveModeAI,
		testConfig(),
		renderer,
	); err != nil {
		t.Fatalf("read next prompt: %v", err)
	}

	plain := strings.ReplaceAll(stripANSISequences(output.String()), "\r\n", "\n")
	wantBoundary := bandsMarker + "  \n\nWhat do you want Shellia to do?"
	if !strings.Contains(plain, wantBoundary) {
		t.Fatalf("bands turn does not leave exactly one terminal row before the next prompt:\n%q", plain)
	}
}

func TestBandsThinkingFrameContinuesShelliaSurface(t *testing.T) {
	var output bytes.Buffer
	turn := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands, ANSI: true, User: "xesc"}).
		BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
	prefix := turn.ThinkingPrefix()
	if !strings.Contains(prefix, bandsShelliaBackground) || stripANSISequences(prefix) != bandsMarker+"  " {
		t.Fatalf("bands thinking prefix = %q, want Shellia band prefix", prefix)
	}
}

func TestBandsThinkingKeepsSinglePaddingRow(t *testing.T) {
	t.Run("after header", func(t *testing.T) {
		var output bytes.Buffer
		turn := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands}).
			BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/tmp"})
		renderThinkingFrame(&output, false, 0, true, turn.ThinkingPrefix())

		plain := stripANSISequences(output.String())
		if got := bandsPaddingRowsBetween(plain, "/tmp", thinkingStatusLineText); got != 1 {
			t.Fatalf("bands padding rows between header and Thinking = %d, want 1:\n%s", got, plain)
		}
	})

	t.Run("after execution", func(t *testing.T) {
		var output bytes.Buffer
		turn := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands}).
			BeginShelliaTurn(testConfig(), core.ContextInfo{})
		step := turn.BeginStep(testConfig(), 1, 1, testPlan())
		step.OutputLine("last output")
		step.Close()
		output.Reset()

		renderThinkingFrame(&output, false, 0, true, turn.ThinkingPrefix())
		plain := strings.ReplaceAll(stripANSISequences(output.String()), "\r", "")
		if want := bandsMarker + "  \n" + bandsMarker + "  " + thinkingStatusLineText; !strings.HasPrefix(plain, want) {
			t.Fatalf("bands Thinking lacks one leading padding row:\n%q", plain)
		}
	})
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

// TestBandsRendererWrapsStreamedOutputWithinTerminalWidth catches terminal
// auto-wrap that would leave continuation bytes outside the execution band.
func TestBandsRendererWrapsStreamedOutputWithinTerminalWidth(t *testing.T) {
	const terminalWidth = 48
	payload := strings.Repeat("0123456789", 6)
	for _, ansi := range []bool{false, true} {
		t.Run(map[bool]string{false: "no ANSI", true: "ANSI"}[ansi], func(t *testing.T) {
			output := renderBandsAtTerminalWidth(t, terminalWidth, ansi, func(renderer *Renderer) {
				turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{})
				step := turn.BeginStep(testConfig(), 1, 1, testPlan())
				step.OutputLabel()
				step.OutputLine(payload)
				step.Close()
				turn.Close()
			})

			var renderedPayload strings.Builder
			payloadRows := 0
			for _, rawLine := range strings.Split(output, "\n") {
				rawLine = strings.TrimRight(rawLine, "\r")
				line := stripANSISequences(rawLine)
				if visibleWidth(line) > terminalWidth {
					t.Fatalf("visible line width = %d, want <= %d: %q\n%s", visibleWidth(line), terminalWidth, line, output)
				}
				if !strings.Contains(line, "0123456789") {
					continue
				}
				payloadRows++
				if !strings.HasPrefix(line, bandsMarker+"    ") {
					t.Fatalf("stream continuation lacks nested band marker: %q\n%s", line, output)
				}
				content := strings.TrimPrefix(line, bandsMarker+"    ")
				renderedPayload.WriteString(strings.TrimSpace(content))

				if ansi {
					if got := visibleWidth(rawLine); got != terminalWidth {
						t.Fatalf("ANSI stream row width = %d, want %d: %q", got, terminalWidth, rawLine)
					}
					if !strings.HasPrefix(rawLine, bandsExecutionBackground) {
						t.Fatalf("stream continuation lacks execution background: %q", rawLine)
					}
					assertBandsBackgroundSurvivesResets(t, rawLine, bandsExecutionBackground)
				}
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
		"▌  xesc",
		"▌    alpha  beta",
		"▌    1234567890123456789012345678901234567890",
		"▌    second  line",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("48-column user turn lacks %q:\n%s", want, output)
		}
	}
	for _, row := range strings.Split(output, "\n") {
		row = strings.TrimRight(row, "\r")
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
		line := strings.TrimRight(stripANSISequences(rendered), "\r")
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
			if !strings.HasPrefix(line, bandsMarker+"          ") {
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
		row = strings.TrimRight(row, "\r")
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
	NewRenderer(tty, Presentation{Style: configpkg.VisualStyleBands, User: "xesc"}).UserTurn(core.InteractiveModeAI, text)
	if err := tty.Close(); err != nil {
		t.Fatalf("close pty: %v", err)
	}
	return string(<-read)
}

func renderBandsConversationFixture(t *testing.T, ansi bool, user string) string {
	t.Helper()
	var output bytes.Buffer
	renderer := NewRenderer(&output, Presentation{Style: configpkg.VisualStyleBands, ANSI: ansi, User: user})
	renderer.UserTurn(core.InteractiveModeAI, "quant d'espai queda al disc?")
	turn := renderer.BeginShelliaTurn(testConfig(), core.ContextInfo{CWD: "/Users/Xesc/Documents/Scripts"})
	turn.Plan(testConfig(), "Cal consultar l'espai disponible.", []core.CommandPlan{testPlan()}, false)
	step := turn.BeginStep(testConfig(), 1, 1, testPlan())
	step.OutputLabel()
	step.OutputLine("419Gi available")
	step.Close()
	turn.Final("Queden 419Gi lliures al disc arrel (/).")
	turn.Close()
	return output.String()
}

func bandsNonEmptyRenderedRows(output string) []string {
	rows := make([]string, 0)
	for _, row := range strings.Split(output, "\n") {
		row = strings.TrimRight(row, "\r")
		if strings.TrimSpace(stripANSISequences(row)) != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

func assertBandsShelliaSegmentHasVerticalPadding(t *testing.T, output string, contents ...string) {
	t.Helper()
	rows := bandsNonEmptyRenderedRows(output)
	if len(rows) < len(contents)+2 {
		t.Fatalf("bands Shellia segment has %d rendered rows, want content plus top and bottom padding: %q", len(rows), output)
	}
	for _, index := range []int{0, len(rows) - 1} {
		if !strings.HasPrefix(rows[index], bandsShelliaBackground) ||
			strings.TrimSpace(stripANSISequences(rows[index])) != bandsMarker {
			t.Fatalf("bands Shellia segment lacks vertical padding at row %d: %q", index, output)
		}
	}
	plain := stripANSISequences(output)
	for _, content := range contents {
		if !strings.Contains(plain, content) {
			t.Fatalf("bands Shellia segment lacks %q: %q", content, output)
		}
	}
}

func assertBandsShelliaContinuationHasBottomPadding(t *testing.T, output string, contents ...string) {
	t.Helper()
	rows := bandsNonEmptyRenderedRows(output)
	if len(rows) < len(contents)+1 {
		t.Fatalf("bands Shellia continuation has %d rendered rows, want content plus bottom padding: %q", len(rows), output)
	}
	last := rows[len(rows)-1]
	if !strings.HasPrefix(last, bandsShelliaBackground) ||
		strings.TrimSpace(stripANSISequences(last)) != bandsMarker {
		t.Fatalf("bands Shellia continuation lacks bottom padding: %q", output)
	}
	plain := stripANSISequences(output)
	for _, content := range contents {
		if !strings.Contains(plain, content) {
			t.Fatalf("bands Shellia continuation lacks %q: %q", content, output)
		}
	}
}

func bandsPaddingRowsBetween(output string, before string, after string) int {
	rows := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	beforeRow := -1
	for index, row := range rows {
		if beforeRow < 0 && strings.Contains(row, before) {
			beforeRow = index
			continue
		}
		if beforeRow < 0 || !strings.Contains(row, after) {
			continue
		}

		padding := 0
		for _, between := range rows[beforeRow+1 : index] {
			if strings.TrimSpace(between) == bandsMarker {
				padding++
			}
		}
		return padding
	}
	return -1
}

func assertBandsBlankRowBetween(t *testing.T, output string, before string, after string) {
	t.Helper()
	rows := strings.Split(output, "\n")
	beforeRow := -1
	for index, row := range rows {
		if beforeRow < 0 && strings.Contains(row, before) {
			beforeRow = index
			continue
		}
		if beforeRow >= 0 && strings.Contains(row, after) {
			for _, between := range rows[beforeRow+1 : index] {
				if strings.TrimSpace(between) == "" {
					return
				}
			}
			t.Fatalf("bands lacks a blank row between %q and %q:\n%s", before, after, output)
		}
	}
	t.Fatalf("bands output lacks ordered boundary %q -> %q:\n%s", before, after, output)
}

func assertBandsNoTerminalBlankBetween(t *testing.T, output string, before string, after string) {
	t.Helper()
	rows := strings.Split(output, "\n")
	beforeRow := -1
	for index, row := range rows {
		if beforeRow < 0 && strings.Contains(row, before) {
			beforeRow = index
			continue
		}
		if beforeRow >= 0 && strings.Contains(row, after) {
			for _, between := range rows[beforeRow+1 : index] {
				if strings.TrimSpace(between) == "" {
					t.Fatalf("bands has a terminal-background gap between %q and %q:\n%s", before, after, output)
				}
			}
			return
		}
	}
	t.Fatalf("bands output lacks ordered boundary %q -> %q:\n%s", before, after, output)
}

func renderBandsAtTerminalWidth(t *testing.T, width int, ansi bool, render func(*Renderer)) string {
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

	renderer := &Renderer{impl: newBandsRenderer(terminal, ansi), ansi: ansi}
	render(renderer)
	if err := terminal.Close(); err != nil {
		t.Fatalf("close PTY terminal: %v", err)
	}
	if err := <-readDone; err != nil && !errors.Is(err, syscall.EIO) {
		t.Fatalf("read PTY output: %v", err)
	}
	return output.String()
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
