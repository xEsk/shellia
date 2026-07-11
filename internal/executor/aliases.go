package executor

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
	safetypkg "shellia/internal/safety"
	tracepkg "shellia/internal/trace"
	uipkg "shellia/internal/ui"
)

type (
	config           = configpkg.Config
	contextInfo      = core.ContextInfo
	gitContext       = core.GitContext
	commandPlan      = core.CommandPlan
	commandExecution = core.CommandExecution
	capturedStream   = core.CapturedStream
	stepBox          = uipkg.StepBox
	confirmDecision  = uipkg.ConfirmDecision
	traceLogger      = tracepkg.Logger
)

const (
	colorDim    = uipkg.ColorDim
	colorGreen  = uipkg.ColorGreen
	colorYellow = uipkg.ColorYellow

	confirmDecisionCancel      = uipkg.ConfirmDecisionCancel
	confirmDecisionRun         = uipkg.ConfirmDecisionRun
	confirmDecisionEdit        = uipkg.ConfirmDecisionEdit
	confirmDecisionInteractive = uipkg.ConfirmDecisionInteractive
)

var (
	errAborted = core.ErrAborted

	traceTurnID               = tracepkg.TurnID
	traceExecutionData        = tracepkg.ExecutionData
	traceConfirmationDecision = uipkg.TraceConfirmationDecision

	printCommandExecutionTo        = uipkg.PrintCommandExecutionTo
	printInteractiveCommandStartTo = uipkg.PrintInteractiveCommandStartTo
	printWarningTo                 = uipkg.PrintWarningTo
	promptConfirmation             = uipkg.PromptConfirmation
	newStepBox                     = uipkg.NewStepBox
	styleStart                     = uipkg.StyleStart
	styleEnd                       = uipkg.StyleEnd
	hasShellOperators              = safetypkg.HasShellOperators
)
