package app

import (
	executorpkg "github.com/xEsk/shellia/internal/executor"
	llmpkg "github.com/xEsk/shellia/internal/llm"
	sessionpkg "github.com/xEsk/shellia/internal/session"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// executorContextOptions projects the application configuration used to collect local context.
func executorContextOptions(cfg config) executorpkg.ContextOptions {
	return executorpkg.ContextOptions{
		IncludeUser: cfg.IncludeUser,
	}
}

// executorOptions projects the application configuration used to execute commands.
func executorOptions(cfg config) executorpkg.Options {
	return executorpkg.Options{
		CommandTimeout:      cfg.CommandTimeout,
		YesSafe:             cfg.YesSafe,
		ContinueOnError:     cfg.ContinueOnError,
		ConfirmationDefault: cfg.ConfirmationDefault,
		CaptureStdoutBytes:  cfg.CaptureStdoutBytes,
		CaptureStderrBytes:  cfg.CaptureStderrBytes,
		ShowSystemOutput:    cfg.ShowSystemOutput,
	}
}

// viewOptions projects the current application configuration used to render the UI.
func viewOptions(cfg config) uipkg.ViewOptions {
	models := make([]uipkg.ModelOption, len(cfg.Models))
	for index, model := range cfg.Models {
		models[index] = uipkg.ModelOption{
			Name:  model.Name,
			Model: model.Model,
		}
	}

	return uipkg.ViewOptions{
		ModelName:        cfg.ModelName,
		Model:            cfg.Model,
		Models:           models,
		Verbose:          cfg.Verbose,
		AskConfirmPlan:   cfg.AskConfirmPlan,
		NoColor:          cfg.NoColor,
		ShowCommandPopup: cfg.ShowCommandPopup,
		IncludeCWD:       cfg.IncludeCWD,
		IncludeUser:      cfg.IncludeUser,
		IncludeOS:        cfg.IncludeOS,
		IncludeShell:     cfg.IncludeShell,
		VisualStyle:      cfg.VisualStyle,
		PlanOnly:         cfg.PlanOnly,
	}
}

// llmClientOptions projects provider and transport configuration for LLM calls.
func llmClientOptions(cfg config) llmpkg.ClientOptions {
	return llmpkg.ClientOptions{
		BaseURL:                cfg.BaseURL,
		APIKey:                 cfg.APIKey,
		Model:                  cfg.Model,
		RequestTimeout:         cfg.RequestTimeout,
		SupportsResponseFormat: cfg.SupportsResponseFormat,
	}
}

// llmPromptOptions projects the application configuration that controls prompt content.
func llmPromptOptions(cfg config) llmpkg.PromptOptions {
	return llmpkg.PromptOptions{
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

// sessionMemoryOptions projects the controls used to retain observation memory.
func sessionMemoryOptions(cfg config) sessionpkg.MemoryOptions {
	return sessionpkg.MemoryOptions{
		MaxObservationEntries:  cfg.MaxObservationEntries,
		MemoryObservationChars: cfg.MemoryObservationChars,
		TruncationStrategy:     cfg.TruncationStrategy,
	}
}

// traceOptions projects trace controls and non-secret session metadata.
func traceOptions(cfg config) tracepkg.Options {
	return tracepkg.Options{
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
