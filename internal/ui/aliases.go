package ui

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	interactivepkg "github.com/xEsk/shellia/internal/interactive"
	llmpkg "github.com/xEsk/shellia/internal/llm"
	safetypkg "github.com/xEsk/shellia/internal/safety"
)

type (
	config              = configpkg.Config
	modelConfig         = configpkg.ModelConfig
	confirmationDefault = configpkg.ConfirmationDefault
	contextInfo         = core.ContextInfo
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
