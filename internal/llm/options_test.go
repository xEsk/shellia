package llm

import (
	"testing"

	configpkg "github.com/xEsk/shellia/internal/config"
)

func promptOptionsForTest(cfg configpkg.Config) PromptOptions {
	return PromptOptions{
		PlanOnly:                  cfg.PlanOnly,
		IncludeCWD:                cfg.IncludeCWD,
		IncludeOS:                 cfg.IncludeOS,
		IncludeShell:              cfg.IncludeShell,
		IncludeUser:               cfg.IncludeUser,
		IncludeSessionMemory:      cfg.IncludeSessionMemory,
		IncludeRecentObservations: cfg.IncludeRecentObservations,
		MaxObservationEntries:     cfg.MaxObservationEntries,
		ObservationOutputChars:    cfg.ObservationOutputChars,
		TruncationStrategy:        cfg.TruncationStrategy,
	}
}

func clientOptionsForTest(cfg configpkg.Config) ClientOptions {
	return ClientOptions{
		BaseURL:                cfg.BaseURL,
		APIKey:                 cfg.APIKey,
		Model:                  cfg.Model,
		RequestTimeout:         cfg.RequestTimeout,
		SupportsResponseFormat: cfg.SupportsResponseFormat,
	}
}

// TestOwnerOptionsSeparatePromptAndTransport verifies each LLM boundary accepts
// only the option view it consumes.
func TestOwnerOptionsSeparatePromptAndTransport(t *testing.T) {
	clientOptions := ClientOptions{
		BaseURL:                "http://127.0.0.1:11434/v1",
		APIKey:                 "test-key",
		Model:                  "test-model",
		SupportsResponseFormat: true,
	}
	promptOptions := PromptOptions{
		PlanOnly:                  true,
		IncludeCWD:                true,
		IncludeOS:                 true,
		IncludeShell:              true,
		IncludeUser:               true,
		IncludeSessionMemory:      true,
		IncludeRecentObservations: true,
		MaxObservationEntries:     2,
		ObservationOutputChars:    80,
		TruncationStrategy:        truncationMixed,
	}

	if !clientOptions.SupportsResponseFormat {
		t.Fatal("ClientOptions lost response format support")
	}
	if !promptOptions.IncludeSessionMemory {
		t.Fatal("PromptOptions lost session memory support")
	}

	BuildPrompts(PromptRequest{Config: promptOptions, Instruction: "inspect status"})
}
