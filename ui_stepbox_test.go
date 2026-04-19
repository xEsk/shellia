package main

import (
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

// TestPrefixedWriterCanHideSystemOutput checks output can be captured elsewhere without rendering.
func TestPrefixedWriterCanHideSystemOutput(t *testing.T) {
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
