package executor

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
	safetypkg "shellia/internal/safety"
	tracepkg "shellia/internal/trace"
	uipkg "shellia/internal/ui"
)

type (
	config             = configpkg.Config
	contextInfo        = core.ContextInfo
	commandPlan        = core.CommandPlan
	commandExecution   = core.CommandExecution
	commandBatchResult = core.CommandBatchResult
	skippedCommand     = core.SkippedCommand
	repeatReason       = core.RepeatReason
	capturedStream     = core.CapturedStream
	stepBox            = uipkg.StepBox
	confirmDecision    = uipkg.ConfirmDecision
	traceLogger        = tracepkg.Logger
)

const (
	colorDim    = uipkg.ColorDim
	colorGreen  = uipkg.ColorGreen
	colorYellow = uipkg.ColorYellow

	confirmDecisionCancel      = uipkg.ConfirmDecisionCancel
	confirmDecisionRun         = uipkg.ConfirmDecisionRun
	confirmDecisionEdit        = uipkg.ConfirmDecisionEdit
	confirmDecisionInteractive = uipkg.ConfirmDecisionInteractive
	localClassificationSafe    = safetypkg.ClassificationSafe

	repeatReasonUserRequested = core.RepeatReasonUserRequested
	repeatReasonRequired      = core.RepeatReasonRequired
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
	classifyCommand                = safetypkg.ClassifyCommand
	higherRisk                     = safetypkg.HigherRisk
	hasShellOperators              = safetypkg.HasShellOperators
)
