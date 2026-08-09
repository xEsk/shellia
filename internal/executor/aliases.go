package executor

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	safetypkg "github.com/xEsk/shellia/internal/safety"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
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
	presentation       = uipkg.Presentation
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
	visualStylePlain          = configpkg.VisualStylePlain
)

var (
	errAborted = core.ErrAborted

	traceTurnID               = tracepkg.TurnID
	traceExecutionData        = tracepkg.ExecutionData
	traceConfirmationDecision = uipkg.TraceConfirmationDecision

	printInteractiveCommandStartTo = uipkg.PrintInteractiveCommandStartTo
	printWarningTo                 = uipkg.PrintWarningTo
	promptConfirmation             = uipkg.PromptConfirmation
	newStepBox                     = uipkg.NewStepBox
	newRenderer                    = uipkg.NewRenderer
	styleStart                     = uipkg.StyleStart
	styleEnd                       = uipkg.StyleEnd
	classifyCommand                = safetypkg.ClassifyCommand
	higherRisk                     = safetypkg.HigherRisk
	hasShellOperators              = safetypkg.HasShellOperators
)
