package ui

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	safetypkg "github.com/xEsk/shellia/internal/safety"
)

type (
	config              = configpkg.Config
	modelConfig         = configpkg.ModelConfig
	confirmationDefault = configpkg.ConfirmationDefault
	contextInfo         = core.ContextInfo
	interactiveMode     = core.InteractiveMode
	commandPlan         = core.CommandPlan
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
