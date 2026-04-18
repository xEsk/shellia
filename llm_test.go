package main

import (
	"strings"
	"testing"
)

// TestShouldRetryWithDiscoveryRepairForRequiresInputEmptyPlan checks that an
// empty first plan marked as requires_input gets one discovery repair retry.
func TestShouldRetryWithDiscoveryRepairForRequiresInputEmptyPlan(t *testing.T) {
	response := llmResponse{
		Summary:       "Need more detail.",
		RequiresInput: true,
		InputReason:   "Installation method is unknown.",
	}

	if !shouldRetryWithDiscoveryRepair(response, 0, nil) {
		t.Fatalf("shouldRetryWithDiscoveryRepair() = false, want true")
	}
}

// TestShouldRetryWithDiscoveryRepairSkipsAnswerOnly checks that an answer-only
// empty response does not trigger the discovery repair fallback.
func TestShouldRetryWithDiscoveryRepairSkipsAnswerOnly(t *testing.T) {
	response := llmResponse{
		Summary: "Not enough detail.",
	}

	if shouldRetryWithDiscoveryRepair(response, 0, nil) {
		t.Fatalf("shouldRetryWithDiscoveryRepair() = true, want false")
	}
}

// TestShouldRetryWithDiscoveryRepairSkipsLaterRounds checks that the repair
// fallback runs only on the initial empty planning response.
func TestShouldRetryWithDiscoveryRepairSkipsLaterRounds(t *testing.T) {
	response := llmResponse{
		Summary:       "Need more detail.",
		RequiresInput: true,
	}

	if shouldRetryWithDiscoveryRepair(response, 1, nil) {
		t.Fatalf("shouldRetryWithDiscoveryRepair() = true, want false")
	}
}

// TestBuildSystemPromptGuidesInteractivePromptRecovery checks that the planner avoids redundant in-command prompts.
func TestBuildSystemPromptGuidesInteractivePromptRecovery(t *testing.T) {
	prompt := buildSystemPrompt()

	requiredSnippets := []string{
		"Shellia already asks the user to confirm risky commands",
		"prefer a known non-interactive confirmation flag only when you are confident",
		"If observed output shows a confirmation prompt or another terminal question",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildSystemPrompt() missing %q in %q", snippet, prompt)
		}
	}
}

// TestBuildDiscoveryRepairPromptIncludesContext checks that the discovery repair
// prompt keeps the normal planning context and adds the repair instructions.
func TestBuildDiscoveryRepairPromptIncludesContext(t *testing.T) {
	cfg := defaultConfig()
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
		Git: gitContext{
			IsRepo:      true,
			Branch:      "main",
			StatusShort: " M ui.go",
		},
	}
	history := []historyEntry{
		{Instruction: "check brew", Result: "brew is installed"},
	}
	state := sessionState{
		PendingIntent:        "actualitza el claude-code",
		LastRuntimeHint:      "homebrew",
		LastSuggestedCommand: "brew update",
	}
	previous := llmResponse{
		Summary:       "Cannot determine how Claude Code was installed.",
		RequiresInput: true,
		InputReason:   "Need the installation method.",
	}

	prompt := buildDiscoveryRepairPrompt(cfg, "actualitza el claude-code", "actualitza el claude-code", ctxInfo, history, state, nil, previous)

	requiredSnippets := []string{
		"User instruction:",
		"Current context:",
		"- cwd: /tmp/project",
		"Recent session context:",
		"Session memory:",
		"Discovery repair mode:",
		"Do not stop after one unsuccessful ownership or installation check if other plausible local discovery paths still exist.",
		"In your summary, briefly tell the user that the first verification was not conclusive and that you are continuing with another short investigation.",
		"Previous empty planning response:",
		"requires_input: true",
		"Need the installation method.",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildDiscoveryRepairPrompt() missing %q in %q", snippet, prompt)
		}
	}
}
