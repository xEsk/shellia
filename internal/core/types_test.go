package core

import (
	"strings"
	"testing"
)

// TestPromptTranscriptHonorsSingleCharacterBudget checks a mixed stream never
// exceeds its content budget and retains the higher-priority stderr evidence.
func TestPromptTranscriptHonorsSingleCharacterBudget(t *testing.T) {
	execution := CommandExecution{
		Stdout: CapturedStream{Text: "S"},
		Stderr: CapturedStream{Text: "E"},
	}

	transcript := execution.PromptTranscript(1, TruncationMixed)
	if !strings.Contains(transcript, "stderr:\nE") || strings.Contains(transcript, "stdout:\nS") {
		t.Fatalf("PromptTranscript(1) = %q, want only one stderr content character", transcript)
	}
}

// TestPromptTranscriptRedistributesUnusedStreamBudget checks a short stderr
// cannot waste budget while the same observation's stdout remains truncated.
func TestPromptTranscriptRedistributesUnusedStreamBudget(t *testing.T) {
	execution := CommandExecution{
		Stdout: CapturedStream{Text: "ABCDEFGHIJK"},
		Stderr: CapturedStream{Text: "E"},
	}

	transcript := execution.PromptTranscript(12, TruncationMixed)
	if !strings.Contains(transcript, "stderr:\nE") || !strings.Contains(transcript, "stdout:\nABCDEFGHIJK") {
		t.Fatalf("PromptTranscript(12) = %q, want all 12 available content characters", transcript)
	}
}
