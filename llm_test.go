package main

import (
	"strings"
	"testing"
)

// TestParseResponseAcceptsExtraTrailingBrace checks local models that append one stray brace.
func TestParseResponseAcceptsExtraTrailingBrace(t *testing.T) {
	raw := `{"summary":"Showing files.","commands":[{"command":"ls","purpose":"List files","risk":"safe","requires_confirmation":false}]}}`

	response, err := parseResponse(raw)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Summary != "Showing files." {
		t.Fatalf("Summary = %q, want %q", response.Summary, "Showing files.")
	}
	if len(response.Commands) != 1 || response.Commands[0].Command != "ls" {
		t.Fatalf("Commands = %#v, want ls command", response.Commands)
	}
}

// TestTrimForSummaryHandlesNonPositiveLimit checks defensive callers cannot panic trimming output.
func TestTrimForSummaryHandlesNonPositiveLimit(t *testing.T) {
	if got := trimForSummary("abcdef", 0, truncationStart); got != "" {
		t.Fatalf("trimForSummary(limit 0) = %q, want empty", got)
	}
	if got := trimForSummary("abcdef", -1, truncationStart); got != "" {
		t.Fatalf("trimForSummary(limit -1) = %q, want empty", got)
	}
}

// TestParseResponseKeepsBracesInsideStrings checks JSON extraction respects quoted content.
func TestParseResponseKeepsBracesInsideStrings(t *testing.T) {
	raw := `prefix {"summary":"Use {literal} braces.","commands":[]} suffix`

	response, err := parseResponse(raw)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if response.Summary != "Use {literal} braces." {
		t.Fatalf("Summary = %q, want braces preserved", response.Summary)
	}
}

// TestParseResponseAllowsEmptyObservationForRepair checks repair can handle missing discovery commands.
func TestParseResponseAllowsEmptyObservationForRepair(t *testing.T) {
	raw := `{"summary":"Need to verify locally.","commands":[],"requires_observation":true,"observation_reason":"Check package availability."}`

	response, err := parseResponse(raw)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if !response.RequiresObservation {
		t.Fatalf("RequiresObservation = false, want true")
	}
	if len(response.Commands) != 0 {
		t.Fatalf("Commands = %#v, want empty", response.Commands)
	}
}

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

// TestShouldRetryWithDiscoveryRepairForRequiresObservationEmptyPlan checks
// local-model empty observation plans get one discovery repair retry.
func TestShouldRetryWithDiscoveryRepairForRequiresObservationEmptyPlan(t *testing.T) {
	response := llmResponse{
		Summary:             "Need to verify locally.",
		RequiresObservation: true,
		ObservationReason:   "Check package availability.",
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

// TestBuildSystemPromptAllowsExplicitReruns checks that the planner does not
// treat previous observations as a hard stop when the user asks to rerun work.
func TestBuildSystemPromptAllowsExplicitReruns(t *testing.T) {
	prompt := buildSystemPrompt()

	requiredSnippets := []string{
		"explicitly asks to repeat, rerun, retry, or execute an earlier action again",
		"treat it as a fresh execution request",
		"still propose the command again",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildSystemPrompt() missing %q in %q", snippet, prompt)
		}
	}
}

// TestBuildSystemPromptAvoidsFallbackJSONGuidance checks JSON-mode providers avoid extra noise.
func TestBuildSystemPromptAvoidsFallbackJSONGuidance(t *testing.T) {
	prompt := buildSystemPrompt()

	if strings.Contains(prompt, "Do not repeat the JSON object") {
		t.Fatalf("buildSystemPrompt() includes fallback JSON guidance: %q", prompt)
	}
}

// TestBuildPlanOnlySystemPromptDefinesOperationalPlan checks /plan has a dedicated non-executing contract.
func TestBuildPlanOnlySystemPromptDefinesOperationalPlan(t *testing.T) {
	prompt := buildPlanOnlySystemPrompt()

	requiredSnippets := []string{
		"non-executing plan mode",
		"preparation, inspection, decision, and manual execution",
		"Avoid redundant inspection steps",
		"write observation_reason as explicit branches",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildPlanOnlySystemPrompt() missing %q in %q", snippet, prompt)
		}
	}
}

// TestBuildDiscoveryRepairLLMPromptsAddsPreviousResponse checks repair prompts include the missing-detail context.
func TestBuildDiscoveryRepairLLMPromptsAddsPreviousResponse(t *testing.T) {
	cfg := defaultConfig()
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}

	systemPrompt, userPrompt := buildDiscoveryRepairLLMPrompts(discoveryPromptRequest{
		Prompt: llmPromptRequest{
			Config:      cfg,
			ContextInfo: ctxInfo,
			Instruction: "update the local tool",
		},
		Previous: llmResponse{
			Summary:       "Need install source.",
			RequiresInput: true,
			InputReason:   "The install source is unknown.",
		},
	})

	if !strings.Contains(systemPrompt, "You are a shell planning assistant.") {
		t.Fatalf("system prompt = %q, want standard planning prompt", systemPrompt)
	}
	requiredSnippets := []string{
		"Discovery repair mode",
		"Previous empty planning response",
		"Need install source.",
		"The install source is unknown.",
		"discovered locally from this machine",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(userPrompt, snippet) {
			t.Fatalf("buildDiscoveryRepairLLMPrompts() user prompt missing %q in %q", snippet, userPrompt)
		}
	}
}

// TestCallDiscoveryRepairLLMSendsRepairPrompt checks the repair LLM path uses the discovery prompt.
func TestCallDiscoveryRepairLLMSendsRepairPrompt(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Inspect locally.","requires_observation":true,"observation_reason":"Use command output.","commands":[{"command":"command -v shellia","purpose":"Find shellia","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}

	raw, err := callDiscoveryRepairLLM(t.Context(), fake.HTTPClient(), discoveryPromptRequest{
		Prompt: llmPromptRequest{
			Config:      cfg,
			ContextInfo: ctxInfo,
			Instruction: "update shellia",
		},
		Previous: llmResponse{
			Summary:       "Need install source.",
			RequiresInput: true,
			InputReason:   "The install source is unknown.",
		},
	})
	if err != nil {
		t.Fatalf("callDiscoveryRepairLLM() error = %v", err)
	}
	if !strings.Contains(raw, "command -v shellia") {
		t.Fatalf("callDiscoveryRepairLLM() = %q, want fake repair response", raw)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 || !strings.Contains(bodies[0], "Discovery repair mode") {
		t.Fatalf("repair request bodies = %#v, want discovery repair prompt", bodies)
	}
}

// TestBuildUserPromptAlwaysAddsJSONGuidance checks JSON instructions do not depend on response_format.
func TestBuildUserPromptAlwaysAddsJSONGuidance(t *testing.T) {
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}

	localCfg := defaultConfig()
	localCfg.BaseURL = "http://localhost:8080/v1"
	localCfg.APIKey = ""
	localPrompt := buildUserPrompt(llmPromptRequest{
		Config:              localCfg,
		ContextInfo:         ctxInfo,
		Instruction:         "list files",
		ResolvedInstruction: "list files",
	})
	if !strings.Contains(localPrompt, "Do not repeat the JSON object") {
		t.Fatalf("buildUserPrompt(local) missing JSON guidance: %q", localPrompt)
	}

	keyedCfg := defaultConfig()
	keyedCfg.APIKey = "test-key"
	keyedPrompt := buildUserPrompt(llmPromptRequest{
		Config:              keyedCfg,
		ContextInfo:         ctxInfo,
		Instruction:         "list files",
		ResolvedInstruction: "list files",
	})
	if !strings.Contains(keyedPrompt, "Do not repeat the JSON object") {
		t.Fatalf("buildUserPrompt(keyed) missing JSON guidance: %q", keyedPrompt)
	}
}

// TestBuildUserPromptIncludesGitContextByDefault checks Git context remains enabled by default.
func TestBuildUserPromptIncludesGitContextByDefault(t *testing.T) {
	cfg := defaultConfig()
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
		Git: gitContext{
			IsRepo:      true,
			Branch:      "main",
			StatusShort: " M llm.go",
		},
	}

	prompt := buildUserPrompt(llmPromptRequest{
		Config:              cfg,
		ContextInfo:         ctxInfo,
		Instruction:         "check status",
		ResolvedInstruction: "check status",
	})

	for _, snippet := range []string{"- git.is_repo: true", "- git.branch: main", "- git.status_short:\n M llm.go"} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildUserPrompt() missing %q in %q", snippet, prompt)
		}
	}
}

// TestBuildUserPromptOmitsGitContextWhenDisabled checks Git fields are not exposed when disabled.
func TestBuildUserPromptOmitsGitContextWhenDisabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.IncludeGit = false
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
		Git: gitContext{
			IsRepo:      true,
			Branch:      "main",
			StatusShort: " M llm.go",
		},
	}

	prompt := buildUserPrompt(llmPromptRequest{
		Config:              cfg,
		ContextInfo:         ctxInfo,
		Instruction:         "check status",
		ResolvedInstruction: "check status",
	})

	for _, snippet := range []string{"git.is_repo", "git.branch", "git.status_short"} {
		if strings.Contains(prompt, snippet) {
			t.Fatalf("buildUserPrompt() includes disabled Git field %q in %q", snippet, prompt)
		}
	}
}

// TestBuildUserPromptOmitsSessionMemoryWhenDisabled checks stored session context can be hidden.
func TestBuildUserPromptOmitsSessionMemoryWhenDisabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.IncludeSessionMemory = false
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}
	history := []historyEntry{{Instruction: "previous task", Result: "previous result"}}
	state := sessionState{
		PendingIntent:        "continue deployment",
		LastSuggestedCommand: "git push origin main",
		LastRuntimeHint:      "docker",
		LastCreatedFiles:     []string{"release.txt"},
		LastReferencedFile:   "main.go",
	}

	prompt := buildUserPrompt(llmPromptRequest{
		Config:              cfg,
		ContextInfo:         ctxInfo,
		Instruction:         "do it",
		ResolvedInstruction: "Resolved: do previous task",
		History:             history,
		State:               state,
	})

	for _, snippet := range []string{"Recent session context:", "Session memory:", "Resolved planning context:", "previous task", "last_suggested_command"} {
		if strings.Contains(prompt, snippet) {
			t.Fatalf("buildUserPrompt() includes disabled session memory %q in %q", snippet, prompt)
		}
	}
}

// TestBuildUserPromptOmitsRecentObservationsWhenDisabled checks command output context can be hidden.
func TestBuildUserPromptOmitsRecentObservationsWhenDisabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.IncludeRecentObservations = false
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}
	state := sessionState{
		LastObservations: []observationMemory{
			{Purpose: "Check status", Command: "git status --short", Transcript: "stdout:\n M llm.go"},
		},
	}
	observations := []commandExecution{
		{Purpose: "List files", Command: "ls", Stdout: capturedStream{Text: "main.go"}},
	}

	prompt := buildUserPrompt(llmPromptRequest{
		Config:              cfg,
		ContextInfo:         ctxInfo,
		Instruction:         "run it again",
		ResolvedInstruction: "run it again",
		State:               state,
		Observations:        observations,
	})

	for _, snippet := range []string{"Recent reusable observations:", "Observed outputs from the current task:", "git status --short", "main.go"} {
		if strings.Contains(prompt, snippet) {
			t.Fatalf("buildUserPrompt() includes disabled observations %q in %q", snippet, prompt)
		}
	}
}

// TestBuildUserPromptAddsPlanOnlyGuidance checks /plan requests human-readable branch guidance.
func TestBuildUserPromptAddsPlanOnlyGuidance(t *testing.T) {
	cfg := defaultConfig()
	cfg.PlanOnly = true
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}

	prompt := buildUserPrompt(llmPromptRequest{
		Config:              cfg,
		ContextInfo:         ctxInfo,
		Instruction:         "inspect docker",
		ResolvedInstruction: "inspect docker",
	})

	requiredSnippets := []string{
		"Plan-only mode:",
		"not an execution round",
		"no automatic discovery repair",
		"exact preparation commands",
		"manual decision branches",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildUserPrompt() missing %q in %q", snippet, prompt)
		}
	}
}

// TestBuildUserPromptMakesReusableObservationsNonBlocking checks that prior
// outputs remain context without blocking explicit fresh execution requests.
func TestBuildUserPromptMakesReusableObservationsNonBlocking(t *testing.T) {
	cfg := defaultConfig()
	ctxInfo := contextInfo{
		CWD:   "/tmp/project",
		User:  "xesc",
		OS:    "darwin/arm64",
		Shell: "/bin/zsh",
	}
	state := sessionState{
		LastObservations: []observationMemory{
			{
				Purpose:    "Check status",
				Command:    "git status --short",
				Transcript: "stdout:\n M llm.go",
			},
		},
	}

	prompt := buildUserPrompt(llmPromptRequest{
		Config:              cfg,
		ContextInfo:         ctxInfo,
		Instruction:         "run it again",
		ResolvedInstruction: "run it again",
		State:               state,
	})

	requiredSnippets := []string{
		"Recent reusable observations:",
		"task continuity only",
		"NOT a reason to skip a fresh execution request",
		"Never skip commands based solely on reusable observations from prior turns",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildUserPrompt() missing %q in %q", snippet, prompt)
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

	prompt := buildDiscoveryRepairPrompt(discoveryPromptRequest{
		Prompt: llmPromptRequest{
			Config:              cfg,
			ContextInfo:         ctxInfo,
			Instruction:         "actualitza el claude-code",
			ResolvedInstruction: "actualitza el claude-code",
			History:             history,
			State:               state,
		},
		Previous: previous,
	})

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
