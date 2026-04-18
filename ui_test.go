package main

import (
	"reflect"
	"testing"
)

// TestShelliaVersionBadgeUsesConfiguredVersion comprova que la UI mostra la versió configurada.
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

// TestShelliaVersionBadgeFallsBackToDev comprova que la UI cau a "dev" si no hi ha versió definida.
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

// TestWrapPromptRunesWithOffsetsPreservesWhitespace comprova que el wrap manté
// els espais del buffer per conservar la correspondència exacta del caret.
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

// TestEditablePromptLayoutUsesFullWrappedLayout comprova que el caret es
// col·loca sobre la fila real renderitzada i no sobre el prefix ja embolicat.
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

// TestMoveCursorVerticalKeepsColumn comprova que amunt i avall mantenen la
// mateixa columna visible quan la fila objectiu ho permet.
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

// TestMoveCursorVerticalClipsToTargetLine comprova que el caret es retalla al
// final de la fila objectiu si aquesta és més curta.
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

// TestMoveCursorVerticalHandlesBounds comprova que si l'usuari ja és a la
// primera o última fila, el caret va a l'inici o al final del buffer.
func TestMoveCursorVerticalHandlesBounds(t *testing.T) {
	buffer := []rune("alpha beta gamma")

	if got, _ := moveCursorVertical(buffer, 3, 8, -1, cursorAffinityForward); got != 0 {
		t.Fatalf("moveCursorVertical(top) = %d, want %d", got, 0)
	}
	if got, _ := moveCursorVertical(buffer, len(buffer)-1, 8, +1, cursorAffinityForward); got != len(buffer) {
		t.Fatalf("moveCursorVertical(bottom) = %d, want %d", got, len(buffer))
	}
}

// TestMoveCursorVerticalKeepsWrappedLineEnd comprova que si un moviment vertical
// cau exactament al límit d'una línia embolicada, el caret es pinta al final de
// la línia objectiu i no a l'inici de la següent.
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
