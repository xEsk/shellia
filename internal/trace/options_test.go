package trace

import (
	"testing"

	configpkg "github.com/xEsk/shellia/internal/config"
)

func traceOptionsForTest(cfg configpkg.Config) Options {
	return Options{
		TraceEnabled:              cfg.TraceEnabled,
		TraceDir:                  cfg.TraceDir,
		ModelName:                 cfg.ModelName,
		Model:                     cfg.Model,
		BaseURL:                   cfg.BaseURL,
		Interactive:               cfg.Interactive,
		PlanOnly:                  cfg.PlanOnly,
		YesSafe:                   cfg.YesSafe,
		AskConfirmPlan:            cfg.AskConfirmPlan,
		PlanningMaxRounds:         cfg.PlanningMaxRounds,
		IncludeSessionMemory:      cfg.IncludeSessionMemory,
		IncludeRecentObservations: cfg.IncludeRecentObservations,
		CaptureStdoutBytes:        cfg.CaptureStdoutBytes,
		CaptureStderrBytes:        cfg.CaptureStderrBytes,
	}
}

// TestOptionsPreserveSessionStartMetadata verifies the trace owner receives the
// enabled controls and non-secret metadata it emits.
func TestOptionsPreserveSessionStartMetadata(t *testing.T) {
	options := Options{
		TraceEnabled:              true,
		TraceDir:                  "/tmp/shellia-trace",
		ModelName:                 "local",
		Model:                     "test-model",
		BaseURL:                   "http://127.0.0.1:11434/v1",
		Interactive:               true,
		PlanOnly:                  true,
		YesSafe:                   true,
		AskConfirmPlan:            true,
		PlanningMaxRounds:         3,
		IncludeSessionMemory:      true,
		IncludeRecentObservations: true,
		CaptureStdoutBytes:        512,
		CaptureStderrBytes:        256,
	}

	data := SessionStartData(options, contextInfo{CWD: "/workspace"})
	if data["model"] != "test-model" || data["base_url"] != "http://127.0.0.1:11434/v1" {
		t.Fatalf("SessionStartData() = %#v, want trace metadata", data)
	}
}
