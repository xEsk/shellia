package ui

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
	interactivepkg "shellia/internal/interactive"
	llmpkg "shellia/internal/llm"
	safetypkg "shellia/internal/safety"
)

type (
	config              = configpkg.Config
	modelConfig         = configpkg.ModelConfig
	confirmationDefault = configpkg.ConfirmationDefault
	contextInfo         = core.ContextInfo
	gitContext          = core.GitContext
	interactiveMode     = core.InteractiveMode
	commandPlan         = core.CommandPlan
	llmResponse         = llmpkg.Response
)

const (
	confirmationDefaultNone        = configpkg.ConfirmationDefaultNone
	confirmationDefaultYes         = configpkg.ConfirmationDefaultYes
	confirmationDefaultNo          = configpkg.ConfirmationDefaultNo
	confirmationDefaultEdit        = configpkg.ConfirmationDefaultEdit
	confirmationDefaultInteractive = configpkg.ConfirmationDefaultInteractive

	interactiveModeAI    = core.InteractiveModeAI
	interactiveModeShell = core.InteractiveModeShell

	riskSafe                = safetypkg.RiskSafe
	riskHigh                = safetypkg.RiskHigh
	classificationSafe      = safetypkg.ClassificationSafe
	classificationDangerous = safetypkg.ClassificationDangerous
)

var (
	defaultConfig                    = configpkg.DefaultConfig
	matchingInteractiveSlashCommands = interactivepkg.MatchingSlashCommands
	completeInteractiveSlashCommand  = interactivepkg.CompleteSlashCommand
)
