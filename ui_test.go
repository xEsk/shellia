package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

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

// TestPrintSeparatorUsesStandardLine checks that the shared separator matches the box width.
func TestPrintSeparatorUsesStandardLine(t *testing.T) {
	var buffer bytes.Buffer

	printSeparator(&buffer, false)

	want := strings.Repeat("─", boxWidth()) + "\n"
	if got := buffer.String(); got != want {
		t.Fatalf("printSeparator() = %q, want %q", got, want)
	}
}

// TestPromptHasTextIgnoresWhitespace checks that Enter on an empty prompt is ignored.
func TestPromptHasTextIgnoresWhitespace(t *testing.T) {
	for _, input := range []string{"", "   ", "\t"} {
		if promptHasText([]rune(input)) {
			t.Fatalf("promptHasText(%q) = true, want false", input)
		}
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
