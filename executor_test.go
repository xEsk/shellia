package main

import "testing"

// TestDetectInteractivePromptMatchesConfirmationPrompt checks that a trailing yes/no prompt is detected.
func TestDetectInteractivePromptMatchesConfirmationPrompt(t *testing.T) {
	prompt, ok := detectInteractivePrompt("WARNING!\nAre you sure you want to continue? [y/N] ")
	if !ok {
		t.Fatalf("detectInteractivePrompt() = false, want true")
	}
	if prompt != "Are you sure you want to continue? [y/N]" {
		t.Fatalf("detectInteractivePrompt() prompt = %q", prompt)
	}
}

// TestDetectInteractivePromptMatchesLooseConfirmationVariants checks common spacing and casing variants.
func TestDetectInteractivePromptMatchesLooseConfirmationVariants(t *testing.T) {
	cases := []string{
		"Are you sure? [ Y / n ] ",
		"continue? (YES / no)",
		"Proceed? [ n | Y ] ",
		"Install dependencies? [y    n] ",
	}

	for _, input := range cases {
		if prompt, ok := detectInteractivePrompt(input); !ok {
			t.Fatalf("detectInteractivePrompt(%q) = false, want true; prompt %q", input, prompt)
		}
	}
}

// TestDetectInteractivePromptMatchesLooseCredentialPrompt checks credential prompts with unusual spacing and casing.
func TestDetectInteractivePromptMatchesLooseCredentialPrompt(t *testing.T) {
	cases := []string{
		"Password:",
		"password : ",
		"Enter PASSPHRASE   :",
	}

	for _, input := range cases {
		if prompt, ok := detectInteractivePrompt(input); !ok {
			t.Fatalf("detectInteractivePrompt(%q) = false, want true; prompt %q", input, prompt)
		}
	}
}

// TestDetectInteractivePromptIgnoresCompletedQuestionLine checks that historical output lines are not treated as active prompts.
func TestDetectInteractivePromptIgnoresCompletedQuestionLine(t *testing.T) {
	if prompt, ok := detectInteractivePrompt("Overwrite existing file? [y/N]\n"); ok {
		t.Fatalf("detectInteractivePrompt() = true with prompt %q, want false", prompt)
	}
}

// TestInteractivePromptDetectorHandlesChunkedOutput checks prompts split across writes.
func TestInteractivePromptDetectorHandlesChunkedOutput(t *testing.T) {
	cancelled := false
	detector := newInteractivePromptDetector("docker image prune -a", func() {
		cancelled = true
	})

	if _, err := detector.Write([]byte("Are you sure you want")); err != nil {
		t.Fatalf("Write() first chunk error = %v", err)
	}
	if _, err := detector.Write([]byte(" to continue? [y/N] ")); err != nil {
		t.Fatalf("Write() second chunk error = %v", err)
	}

	if !cancelled {
		t.Fatalf("detector did not cancel the command after detecting a prompt")
	}
	if err := detector.promptError(); err == nil {
		t.Fatalf("promptError() = nil, want interactivePromptError")
	}
}

// TestShouldRetryAfterExecutionErrorForInteractivePrompt checks that prompt errors trigger one more planning round.
func TestShouldRetryAfterExecutionErrorForInteractivePrompt(t *testing.T) {
	err := &interactivePromptError{Command: "docker image prune -a", Prompt: "Are you sure? [y/N]"}

	if !shouldRetryAfterExecutionError(err, 0) {
		t.Fatalf("shouldRetryAfterExecutionError() = false, want true")
	}
	if shouldRetryAfterExecutionError(err, maxPlanRounds-1) {
		t.Fatalf("shouldRetryAfterExecutionError() = true on final round, want false")
	}
}
