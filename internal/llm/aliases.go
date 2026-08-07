package llm

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
	safetypkg "shellia/internal/safety"
	sessionpkg "shellia/internal/session"
)

type (
	config             = configpkg.Config
	contextInfo        = core.ContextInfo
	historyEntry       = core.HistoryEntry
	observationMemory  = core.ObservationMemory
	pendingProposal    = core.PendingProposal
	sessionState       = core.SessionState
	commandExecution   = core.CommandExecution
	workflowAttempt    = core.WorkflowAttempt
	skippedCommand     = core.SkippedCommand
	capturedStream     = core.CapturedStream
	commandPlan        = core.CommandPlan
	repeatReason       = core.RepeatReason
	truncationStrategy = core.TruncationStrategy
)

const (
	repeatReasonUserRequested     = core.RepeatReasonUserRequested
	repeatReasonRetry             = core.RepeatReasonRetry
	repeatReasonVerifyAfterChange = core.RepeatReasonVerifyAfterChange
	repeatReasonPollChangedState  = core.RepeatReasonPollChangedState
	truncationStart               = core.TruncationStart
	truncationEnd                 = core.TruncationEnd
	truncationMixed               = core.TruncationMixed
)

const classificationSafe = safetypkg.ClassificationSafe

var (
	classifyCommand               = safetypkg.ClassifyCommand
	higherRisk                    = safetypkg.HigherRisk
	resolveInstructionForPlanning = sessionpkg.ResolveInstructionForPlanning
	defaultConfig                 = configpkg.DefaultConfig
)
