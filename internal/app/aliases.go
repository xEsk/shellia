package app

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	llmpkg "github.com/xEsk/shellia/internal/llm"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

type (
	config      = configpkg.Config
	modelConfig = configpkg.ModelConfig

	turnOutcome            = core.TurnOutcome
	contextInfo            = core.ContextInfo
	historyEntry           = core.HistoryEntry
	interactiveMode        = core.InteractiveMode
	pendingProposal        = core.PendingProposal
	sessionState           = core.SessionState
	turnResult             = core.TurnResult
	workflowAttempt        = core.WorkflowAttempt
	commandPlan            = core.CommandPlan
	commandExecution       = core.CommandExecution
	commandBatchResult     = core.CommandBatchResult
	skippedCommand         = core.SkippedCommand
	manualRenderMode       = executorpkg.ManualRenderMode
	interactivePromptError = executorpkg.InteractivePromptError
	traceLogger            = tracepkg.Logger
	presentation           = uipkg.Presentation

	llmPromptRequest = llmpkg.PromptRequest
	llmResponse      = llmpkg.Response
	capturedStream   = core.CapturedStream
)

const (
	turnOutcomeCompleted       = core.TurnOutcomeCompleted
	turnOutcomeBlocked         = core.TurnOutcomeBlocked
	turnOutcomePlanned         = core.TurnOutcomePlanned
	turnOutcomeDeclined        = core.TurnOutcomeDeclined
	turnOutcomeCancelled       = core.TurnOutcomeCancelled
	turnOutcomeTimeout         = core.TurnOutcomeTimeout
	turnOutcomePlanningLimit   = core.TurnOutcomePlanningLimit
	turnOutcomeStructuralError = core.TurnOutcomeStructuralError
	turnOutcomeNoProgress      = core.TurnOutcomeNoProgress
	repeatReasonRequired       = core.RepeatReasonRequired

	interactiveModeAI        = core.InteractiveModeAI
	interactiveModeShell     = core.InteractiveModeShell
	defaultPlanningMaxRounds = 4
)
