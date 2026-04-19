package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

// TestStepBoxCloseAvoidsDoubleSpacing checks that closing one step box and opening
// the next does not leave two blank rows before the separator.
func TestStepBoxCloseAvoidsDoubleSpacing(t *testing.T) {
	var buffer bytes.Buffer

	first := newStepBox(&buffer, false, "step 1/2")
	first.OutputLabel()
	first.OutputLine("hello")
	first.Close()

	second := newStepBox(&buffer, false, "step 2/2")
	second.Close()

	separator := strings.Repeat("─", boxWidth())
	doubleGap := "  hello\n\n\n" + separator
	if strings.Contains(buffer.String(), doubleGap) {
		t.Fatalf("step box output contains a double blank gap before the separator: %q", buffer.String())
	}

	singleGap := "  hello\n\n" + separator
	if !strings.Contains(buffer.String(), singleGap) {
		t.Fatalf("step box output does not contain the expected single blank gap before the separator: %q", buffer.String())
	}
}

// TestPrefixedWriterCanSuppressSystemOutput checks output can be captured elsewhere without rendering.
func TestPrefixedWriterCanSuppressSystemOutput(t *testing.T) {
	var buffer bytes.Buffer
	box := newStepBox(&buffer, false, "step 1/1")
	writer := &prefixedWriter{box: box, hidden: true}

	if _, err := writer.Write([]byte("hidden output\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}

	output := buffer.String()
	if strings.Contains(output, "system output") || strings.Contains(output, "hidden output") {
		t.Fatalf("hidden prefixedWriter rendered output: %q", output)
	}
}

// TestShowCompletedMarkerPrintsWithoutOutput checks the success marker used for silent commands.
func TestShowCompletedMarkerPrintsWithoutOutput(t *testing.T) {
	var buffer bytes.Buffer
	box := newStepBox(&buffer, false, "step 1/1")

	showCompletedMarker(box, false)
	box.Close()

	if !strings.Contains(buffer.String(), "• completed") {
		t.Fatalf("step box does not contain completed marker: %q", buffer.String())
	}
}

// TestShowCompletedMarkerSkipsWhenOutputWasShown checks output blocks are not followed by extra status noise.
func TestShowCompletedMarkerSkipsWhenOutputWasShown(t *testing.T) {
	var buffer bytes.Buffer
	box := newStepBox(&buffer, false, "step 1/1")

	box.OutputLabel()
	box.OutputLine("hello")
	showCompletedMarker(box, true)
	box.Close()

	if strings.Contains(buffer.String(), "• completed") {
		t.Fatalf("step box contains unexpected completed marker: %q", buffer.String())
	}
}

// TestPromptConfirmationUsesDefaultOnEnter checks Enter selects the configured default action.
func TestPromptConfirmationUsesDefaultOnEnter(t *testing.T) {
	var buffer bytes.Buffer
	box := newStepBox(&buffer, false, "step 1/1")
	reader := bufio.NewReader(strings.NewReader("\n"))

	decision, _, err := promptConfirmation(box, reader, "Run step 1/1?", "true", confirmationDefaultYes)
	if err != nil {
		t.Fatalf("promptConfirmation() error = %v", err)
	}
	if decision != confirmDecisionRun {
		t.Fatalf("promptConfirmation() decision = %v, want %v", decision, confirmDecisionRun)
	}
}

// TestPromptConfirmationRequiresChoiceWithoutDefault checks blank input is ignored without a default.
func TestPromptConfirmationRequiresChoiceWithoutDefault(t *testing.T) {
	var buffer bytes.Buffer
	box := newStepBox(&buffer, false, "step 1/1")
	reader := bufio.NewReader(strings.NewReader("\ny\n"))

	decision, _, err := promptConfirmation(box, reader, "Run step 1/1?", "true", confirmationDefaultNone)
	if err != nil {
		t.Fatalf("promptConfirmation() error = %v", err)
	}
	if decision != confirmDecisionRun {
		t.Fatalf("promptConfirmation() decision = %v, want %v", decision, confirmDecisionRun)
	}
}

// TestRenderConfirmationPromptHighlightsDefault checks the default option uses the confirm colour.
func TestRenderConfirmationPromptHighlightsDefault(t *testing.T) {
	var buffer bytes.Buffer
	box := newStepBox(&buffer, true, "step 1/1")

	renderConfirmationPrompt(box, "Run step 1/1?", confirmationDefaultNo)

	if !strings.Contains(buffer.String(), colorYellow+colorBold+"n"+colorReset) {
		t.Fatalf("confirmation prompt does not highlight default option: %q", buffer.String())
	}
}
