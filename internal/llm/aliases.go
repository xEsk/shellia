package llm

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

type (
	config             = configpkg.Config
	contextInfo        = core.ContextInfo
	historyEntry       = core.HistoryEntry
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
