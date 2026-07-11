package ui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// TestRawTurnPromptCtrlCCancels checks raw Ctrl+C is propagated as cancellation
// by prompts that are part of an active turn.
func TestRawTurnPromptCtrlCCancels(t *testing.T) {
	if err := rawTurnPromptCancellation(3); !errors.Is(err, context.Canceled) {
		t.Fatalf("rawTurnPromptCancellation(Ctrl+C) error = %v, want context.Canceled", err)
	}
	if err := rawTurnPromptCancellation('y'); err != nil {
		t.Fatalf("rawTurnPromptCancellation(y) error = %v, want nil", err)
	}
}

// TestShelliaVersionBadgeUsesConfiguredVersion checks that the UI shows the configured version.
func TestShelliaVersionBadgeUsesConfiguredVersion(t *testing.T) {
	previousVersion := version
	version = "v1.2.3"
	t.Cleanup(func() {
		version = previousVersion
	})

	if got := shelliaVersionBadge(false); got != "v1.2.3" {
		t.Fatalf("shelliaVersionBadge(false) = %q, want %q", got, "v1.2.3")
	}
}

// TestShelliaVersionBadgeFallsBackToDev checks that the UI falls back to "dev" when no version is defined.
func TestShelliaVersionBadgeFallsBackToDev(t *testing.T) {
	previousVersion := version
	version = "   "
	t.Cleanup(func() {
		version = previousVersion
	})

	if got := shelliaVersionBadge(false); got != "dev" {
		t.Fatalf("shelliaVersionBadge(false) = %q, want %q", got, "dev")
	}
}

// TestPrintHeaderOmitsImplicitGitContext checks compact headers never expose
// ambient repository state.
func TestPrintHeaderOmitsImplicitGitContext(t *testing.T) {
	cfg := defaultConfig()
	ctxInfo := contextInfo{
		CWD: "/tmp/project",
	}
	var buffer bytes.Buffer

	printHeaderTo(&buffer, false, cfg, ctxInfo)

	output := buffer.String()
	if strings.Contains(output, "main") || strings.Contains(output, "clean") || strings.Contains(output, "dirty") {
		t.Fatalf("printHeaderTo() includes implicit Git context: %q", output)
	}
	if !strings.Contains(output, "/tmp/project") {
		t.Fatalf("printHeaderTo() missing enabled cwd: %q", output)
	}
}

// TestPrintContextRespectsContextConfig checks /context only renders enabled fields.
func TestPrintContextRespectsContextConfig(t *testing.T) {
	cfg := defaultConfig()
	cfg.IncludeUser = false
	cfg.IncludeShell = false
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}
	var buffer bytes.Buffer

	printContextTo(&buffer, false, cfg, ctxInfo)

	output := buffer.String()
	for _, hidden := range []string{"user", "shell", "git", "branch", "status", "xesc", "/bin/zsh", "main", "M ui.go"} {
		if strings.Contains(output, hidden) {
			t.Fatalf("printContextTo() includes disabled context %q in %q", hidden, output)
		}
	}
	for _, visible := range []string{"cwd", "/tmp/project", "os", "darwin/arm64"} {
		if !strings.Contains(output, visible) {
			t.Fatalf("printContextTo() missing enabled context %q in %q", visible, output)
		}
	}
}

// TestPrintInteractiveCommandStartAddsLeadingBlankLine checks the interactive
// command handoff does not appear glued to the prompt that launched it.
func TestPrintInteractiveCommandStartAddsLeadingBlankLine(t *testing.T) {
	var buffer bytes.Buffer

	printInteractiveCommandStartTo(&buffer, false)

	want := "\nShellia Starting interactive command. Shellia will resume when it exits.\n"
	if got := buffer.String(); got != want {
		t.Fatalf("printInteractiveCommandStartTo() = %q, want %q", got, want)
	}
}

// TestPrintSeparatorUsesStandardLine checks that the shared separator matches the box width.
func TestPrintSeparatorUsesStandardLine(t *testing.T) {
	var buffer bytes.Buffer

	printSeparator(&buffer, false)

	want := strings.Repeat("─", boxWidthFor(&buffer)) + "\n"
	if got := buffer.String(); got != want {
		t.Fatalf("printSeparator() = %q, want %q", got, want)
	}
}

// TestPrintNewSessionSeparatorShowsContextBoundary checks /new has a visible marker.
func TestPrintNewSessionSeparatorShowsContextBoundary(t *testing.T) {
	var buffer bytes.Buffer

	printNewSessionSeparatorTo(&buffer, false)

	output := buffer.String()
	if !strings.Contains(output, "new session") {
		t.Fatalf("printNewSessionSeparatorTo() = %q, want new session title", output)
	}
	if !strings.Contains(output, "context cleared") {
		t.Fatalf("printNewSessionSeparatorTo() = %q, want context-cleared label", output)
	}
	if visibleWidth(strings.TrimSpace(output)) != boxWidthFor(&buffer) {
		t.Fatalf("printNewSessionSeparatorTo() width = %d, want %d: %q", visibleWidth(strings.TrimSpace(output)), boxWidthFor(&buffer), output)
	}
	if !strings.Contains(output, "─ new session · context cleared ") {
		t.Fatalf("printNewSessionSeparatorTo() = %q, want centered separator label", output)
	}
}

// TestCenteredSeparatorTextCentersLabel checks the text separator keeps a stable width.
func TestCenteredSeparatorTextCentersLabel(t *testing.T) {
	got := centeredSeparatorText("new session", 20)
	want := "─── new session ────"
	if got != want {
		t.Fatalf("centeredSeparatorText() = %q, want %q", got, want)
	}
}

// TestPromptHasTextIgnoresWhitespace checks that Enter on an empty prompt is ignored.
func TestPromptHasTextIgnoresWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "spaces", input: "   "},
		{name: "tab", input: "\t"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if promptHasText([]rune(tt.input)) {
				t.Fatalf("promptHasText(%q) = true, want false", tt.input)
			}
		})
	}

	if !promptHasText([]rune("actualitza el claude-code")) {
		t.Fatalf("promptHasText(non-empty) = false, want true")
	}
}

// TestReadFallbackPromptLineReturnsEOFOnEmptyInput checks closed stdin does not look like an empty prompt.
func TestReadFallbackPromptLineReturnsEOFOnEmptyInput(t *testing.T) {
	got, err := readFallbackPromptLine(bufio.NewReader(strings.NewReader("")))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("readFallbackPromptLine() error = %v, want io.EOF", err)
	}
	if got != "" {
		t.Fatalf("readFallbackPromptLine() = %q, want empty string", got)
	}
}

// TestReadFallbackPromptLineReturnsPartialLineOnEOF checks piped input without a newline is still accepted.
func TestReadFallbackPromptLineReturnsPartialLineOnEOF(t *testing.T) {
	got, err := readFallbackPromptLine(bufio.NewReader(strings.NewReader("answer without newline")))
	if err != nil {
		t.Fatalf("readFallbackPromptLine() error = %v, want nil", err)
	}
	if got != "answer without newline" {
		t.Fatalf("readFallbackPromptLine() = %q, want partial line", got)
	}
}

// TestApplyPromptEscapeSequenceInsertsAltEnterNewline checks Alt+Enter adds a
// soft newline to the editable prompt instead of exiting or submitting.
func TestApplyPromptEscapeSequenceInsertsAltEnterNewline(t *testing.T) {
	buffer := []rune("firstsecond")
	cursor := len([]rune("first"))
	affinity := cursorAffinityForward

	result, err := applyPromptEscapeSequenceFrom(strings.NewReader("\r"), &buffer, &cursor, 20, &affinity)
	if err != nil {
		t.Fatalf("applyPromptEscapeSequenceFrom() error = %v", err)
	}
	if result.exit {
		t.Fatalf("applyPromptEscapeSequenceFrom() exit = true, want false")
	}
	if got, want := string(buffer), "first\nsecond"; got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
	if cursor != len([]rune("first\n")) {
		t.Fatalf("cursor = %d, want after inserted newline", cursor)
	}
}

// TestReadBracketedPastePreservesMultilineText checks bracketed paste reads the
// full paste payload, including internal newlines, before returning.
func TestReadBracketedPastePreservesMultilineText(t *testing.T) {
	got, err := readBracketedPaste(strings.NewReader("one\n two\nthree\x1b[201~"))
	if err != nil {
		t.Fatalf("readBracketedPaste() error = %v", err)
	}
	if want := []rune("one\n two\nthree"); !reflect.DeepEqual(got, want) {
		t.Fatalf("readBracketedPaste() = %#v, want %#v", got, want)
	}
}

// TestApplyPromptEscapeSequenceInsertsBracketedPaste checks a multiline paste
// is inserted into the prompt buffer without treating its newlines as submits.
func TestApplyPromptEscapeSequenceInsertsBracketedPaste(t *testing.T) {
	buffer := []rune("ask: ")
	cursor := len(buffer)
	affinity := cursorAffinityForward

	result, err := applyPromptEscapeSequenceFrom(strings.NewReader("[200~one\n two\nthree\x1b[201~"), &buffer, &cursor, 20, &affinity)
	if err != nil {
		t.Fatalf("applyPromptEscapeSequenceFrom() error = %v", err)
	}
	if result.exit {
		t.Fatalf("applyPromptEscapeSequenceFrom() exit = true, want false")
	}

	want := "ask: one\n two\nthree"
	if got := string(buffer); got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
	if submitted := strings.TrimSpace(string(buffer)); submitted != want {
		t.Fatalf("submitted prompt = %q, want %q", submitted, want)
	}
}

// TestParseConfirmationChoiceAcceptsSupportedAnswers checks the shared parser for all accepted inputs.
func TestParseConfirmationChoiceAcceptsSupportedAnswers(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		choice confirmationDefault
		ok     bool
	}{
		{name: "yes short", input: "y", choice: confirmationDefaultYes, ok: true},
		{name: "yes long", input: "yes", choice: confirmationDefaultYes, ok: true},
		{name: "no short", input: "n", choice: confirmationDefaultNo, ok: true},
		{name: "no long", input: "no", choice: confirmationDefaultNo, ok: true},
		{name: "edit short", input: "e", choice: confirmationDefaultEdit, ok: true},
		{name: "edit long", input: "edit", choice: confirmationDefaultEdit, ok: true},
		{name: "interactive short", input: "i", choice: confirmationDefaultInteractive, ok: true},
		{name: "interactive long", input: "interactive", choice: confirmationDefaultInteractive, ok: true},
		{name: "unknown", input: "unknown", choice: confirmationDefaultNone, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotChoice, gotOK := parseConfirmationChoice(tt.input)
			if gotChoice != tt.choice || gotOK != tt.ok {
				t.Fatalf("parseConfirmationChoice(%q) = (%v, %t), want (%v, %t)", tt.input, gotChoice, gotOK, tt.choice, tt.ok)
			}
		})
	}
}

// TestWrapPromptRunesWithOffsetsPreservesWhitespace checks that wrapping keeps
// buffer spaces to preserve the exact caret mapping.
func TestWrapPromptRunesWithOffsetsPreservesWhitespace(t *testing.T) {
	buffer := []rune("alpha beta gamma")

	lines, offsets := wrapPromptRunesWithOffsets(buffer, 8)

	wantLines := []string{"alpha ", "beta ", "gamma"}
	wantOffsets := []int{0, 6, 11}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("wrapPromptRunesWithOffsets() lines = %#v, want %#v", lines, wantLines)
	}
	if !reflect.DeepEqual(offsets, wantOffsets) {
		t.Fatalf("wrapPromptRunesWithOffsets() offsets = %#v, want %#v", offsets, wantOffsets)
	}
}

// TestEditablePromptLayoutUsesFullWrappedLayout checks that the caret is placed
// on the real rendered row and not on the already wrapped prefix.
func TestEditablePromptLayoutUsesFullWrappedLayout(t *testing.T) {
	prompt := "you > "
	buffer := []rune("alpha beta gamma")
	width := 14

	lines, cursorRow, cursorCol := editablePromptLayout(prompt, buffer, 7, cursorAffinityForward, width)

	wantLines := []string{"alpha ", "beta ", "gamma"}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("editablePromptLayout() lines = %#v, want %#v", lines, wantLines)
	}
	if cursorRow != 1 {
		t.Fatalf("editablePromptLayout() cursorRow = %d, want %d", cursorRow, 1)
	}

	promptWidth := visibleWidth(prompt)
	if cursorCol != promptWidth+1 {
		t.Fatalf("editablePromptLayout() cursorCol = %d, want %d", cursorCol, promptWidth+1)
	}
}

// TestEditablePromptLayoutUsesExplicitNewlinesAsRows checks typed soft
// newlines create real prompt rows even when no wrapping is needed.
func TestEditablePromptLayoutUsesExplicitNewlinesAsRows(t *testing.T) {
	prompt := "you > "
	buffer := []rune("first\nsecond")

	lines, cursorRow, cursorCol := editablePromptLayout(prompt, buffer, len(buffer), cursorAffinityForward, 40)

	wantLines := []string{"first", "second"}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("editablePromptLayout() lines = %#v, want %#v", lines, wantLines)
	}
	if cursorRow != 1 {
		t.Fatalf("editablePromptLayout() cursorRow = %d, want %d", cursorRow, 1)
	}
	wantCol := visibleWidth(prompt) + len([]rune("second"))
	if cursorCol != wantCol {
		t.Fatalf("editablePromptLayout() cursorCol = %d, want %d", cursorCol, wantCol)
	}
}

// TestMoveCursorVerticalKeepsColumn checks that up and down keep the same
// visible column when the target row allows it.
func TestMoveCursorVerticalKeepsColumn(t *testing.T) {
	buffer := []rune("alpha beta gamma delta")
	lines, offsets := wrapPromptRunesWithOffsets(buffer, 8)
	if len(lines) != 4 {
		t.Fatalf("wrapPromptRunesWithOffsets() returned %d lines, want %d", len(lines), 4)
	}

	cursor := offsets[3] + 2
	gotUp, upAffinity := moveCursorVertical(buffer, cursor, 8, -1, cursorAffinityForward)
	if want := offsets[2] + 2; gotUp != want {
		t.Fatalf("moveCursorVertical(up) = %d, want %d", gotUp, want)
	}
	if upAffinity != cursorAffinityForward {
		t.Fatalf("moveCursorVertical(up) affinity = %d, want %d", upAffinity, cursorAffinityForward)
	}

	gotDown, downAffinity := moveCursorVertical(buffer, gotUp, 8, +1, upAffinity)
	if gotDown != cursor {
		t.Fatalf("moveCursorVertical(down) = %d, want %d", gotDown, cursor)
	}
	if downAffinity != cursorAffinityForward {
		t.Fatalf("moveCursorVertical(down) affinity = %d, want %d", downAffinity, cursorAffinityForward)
	}
}

// TestMoveCursorVerticalClipsToTargetLine checks that the caret is clipped to
// the end of the target row when that row is shorter.
func TestMoveCursorVerticalClipsToTargetLine(t *testing.T) {
	buffer := []rune("abcd efghij")
	lines, offsets := wrapPromptRunesWithOffsets(buffer, 6)
	if len(lines) != 2 {
		t.Fatalf("wrapPromptRunesWithOffsets() returned %d lines, want %d", len(lines), 2)
	}

	cursor := offsets[1] + len([]rune(lines[1]))
	got, gotAffinity := moveCursorVertical(buffer, cursor, 6, -1, cursorAffinityForward)
	want := offsets[0] + len([]rune(lines[0]))
	if got != want {
		t.Fatalf("moveCursorVertical(up) = %d, want %d", got, want)
	}
	if gotAffinity != cursorAffinityBackward {
		t.Fatalf("moveCursorVertical(up) affinity = %d, want %d", gotAffinity, cursorAffinityBackward)
	}
}

// TestMoveCursorVerticalHandlesBounds checks that if the user is already on the
// first or last row, the caret moves to the start or end of the buffer.
func TestMoveCursorVerticalHandlesBounds(t *testing.T) {
	buffer := []rune("alpha beta gamma")

	if got, _ := moveCursorVertical(buffer, 3, 8, -1, cursorAffinityForward); got != 0 {
		t.Fatalf("moveCursorVertical(top) = %d, want %d", got, 0)
	}
	if got, _ := moveCursorVertical(buffer, len(buffer)-1, 8, +1, cursorAffinityForward); got != len(buffer) {
		t.Fatalf("moveCursorVertical(bottom) = %d, want %d", got, len(buffer))
	}
}

// TestMoveCursorVerticalKeepsWrappedLineEnd checks that when a vertical movement
// lands exactly on a wrapped line boundary, the caret is rendered at the end of
// the target row instead of the start of the next one.
func TestMoveCursorVerticalKeepsWrappedLineEnd(t *testing.T) {
	prompt := "you > "
	buffer := []rune("alpha beta gamma")
	lines, offsets := wrapPromptRunesWithOffsets(buffer, 8)
	if len(lines) != 3 {
		t.Fatalf("wrapPromptRunesWithOffsets() returned %d lines, want %d", len(lines), 3)
	}

	cursor := offsets[0] + len([]rune(lines[0])) - 1
	gotCursor, gotAffinity := moveCursorVertical(buffer, cursor, 8, +1, cursorAffinityForward)
	wantCursor := offsets[1] + len([]rune(lines[1]))
	if gotCursor != wantCursor {
		t.Fatalf("moveCursorVertical(down) cursor = %d, want %d", gotCursor, wantCursor)
	}
	if gotAffinity != cursorAffinityBackward {
		t.Fatalf("moveCursorVertical(down) affinity = %d, want %d", gotAffinity, cursorAffinityBackward)
	}

	_, row, col := editablePromptLayout(prompt, buffer, gotCursor, gotAffinity, 14)
	if row != 1 {
		t.Fatalf("editablePromptLayout() row = %d, want %d", row, 1)
	}

	wantCol := visibleWidth(prompt) + len([]rune(lines[1]))
	if col != wantCol {
		t.Fatalf("editablePromptLayout() col = %d, want %d", col, wantCol)
	}
}

// TestLayoutAnswerLinesWrapsByWords checks that Shellia answers wrap by words instead of characters.
func TestLayoutAnswerLinesWrapsByWords(t *testing.T) {
	message := "Cal actualitzar Claude Code, pero falta saber com esta installat al teu Mac per donar la comanda correcta amb seguretat."

	got := layoutAnswerLines(message, 24)
	want := []string{
		"Cal actualitzar Claude",
		"Code, pero falta saber",
		"com esta installat al",
		"teu Mac per donar la",
		"comanda correcta amb",
		"seguretat.",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("layoutAnswerLines() = %#v, want %#v", got, want)
	}
}

// TestLayoutAnswerLinesPreservesExplicitBlankLines checks that explicit blank lines remain in the rendered answer.
func TestLayoutAnswerLinesPreservesExplicitBlankLines(t *testing.T) {
	message := "First paragraph with enough words to wrap.\n\nSecond paragraph."

	got := layoutAnswerLines(message, 18)
	want := []string{
		"First paragraph",
		"with enough words",
		"to wrap.",
		"",
		"Second paragraph.",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("layoutAnswerLines() = %#v, want %#v", got, want)
	}
}

// TestRenderAnswerMarkdownRemovesStrongMarkers checks Markdown markers do not affect visible width.
func TestRenderAnswerMarkdownRemovesStrongMarkers(t *testing.T) {
	lines := renderAnswerMarkdown("El consum és a **/var**.", 80, false)
	if len(lines) != 1 {
		t.Fatalf("renderAnswerMarkdown() lines = %#v, want one line", lines)
	}

	line := stripANSISequences(lines[0])
	if strings.Contains(line, "**") {
		t.Fatalf("renderAnswerMarkdown() kept strong markers: %q", line)
	}
	if !strings.Contains(line, "/var") {
		t.Fatalf("renderAnswerMarkdown() missing strong content: %q", line)
	}
	if got := visibleWidth(line); got != len([]rune(line)) {
		t.Fatalf("visible rendered width = %d, want plain rune width %d", got, len([]rune(line)))
	}
}

// TestRenderAnswerMarkdownWrapsAfterRendering checks removed Markdown markers do not cause short wraps.
func TestRenderAnswerMarkdownWrapsAfterRendering(t *testing.T) {
	message := "**/var** ocupa **9,7G**"

	lines := renderAnswerMarkdown(message, 16, false)
	want := []string{"/var ocupa 9,7G"}

	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("renderAnswerMarkdown() = %#v, want %#v", lines, want)
	}
}

// TestRenderAnswerMarkdownKeepsInlineCodeWidth checks inline code renders within the requested width.
func TestRenderAnswerMarkdownKeepsInlineCodeWidth(t *testing.T) {
	lines := renderAnswerMarkdown("Executa `du -sh /var` ara.", 24, true)
	if len(lines) == 0 {
		t.Fatal("renderAnswerMarkdown() returned no lines")
	}

	for _, line := range lines {
		if strings.Contains(stripANSISequences(line), "`") {
			t.Fatalf("renderAnswerMarkdown() kept inline code markers: %q", line)
		}
		if width := visibleWidth(line); width > 24 {
			t.Fatalf("renderAnswerMarkdown() line width = %d, want <= 24: %q", width, line)
		}
	}
}

// TestRenderAnswerMarkdownNoColorOmitsANSI checks plain output never emits terminal escapes.
func TestRenderAnswerMarkdownNoColorOmitsANSI(t *testing.T) {
	lines := renderAnswerMarkdown("Text amb **negreta** i `codi`.", 80, false)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "\033[") {
		t.Fatalf("renderAnswerMarkdown(false) emitted ANSI: %q", joined)
	}
	if strings.Contains(joined, "**") || strings.Contains(joined, "`") {
		t.Fatalf("renderAnswerMarkdown(false) kept Markdown markers: %q", joined)
	}
}

// TestRenderAnswerMarkdownMalformedInputFits checks malformed Markdown does not break layout.
func TestRenderAnswerMarkdownMalformedInputFits(t *testing.T) {
	lines := renderAnswerMarkdown("Aquest **markdown queda obert i s'ha de poder mostrar.", 18, true)
	if len(lines) == 0 {
		t.Fatal("renderAnswerMarkdown() returned no lines")
	}

	for _, line := range lines {
		if width := visibleWidth(line); width > 18 {
			t.Fatalf("renderAnswerMarkdown() line width = %d, want <= 18: %q", width, line)
		}
	}
}
