package session

import (
	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

type (
	config             = configpkg.Config
	sessionState       = core.SessionState
	turnResult         = core.TurnResult
	commandExecution   = core.CommandExecution
	commandPlan        = core.CommandPlan
	capturedStream     = core.CapturedStream
	observationMemory  = core.ObservationMemory
	pendingProposal    = core.PendingProposal
	truncationStrategy = core.TruncationStrategy
)

const (
	turnOutcomeCompleted       = core.TurnOutcomeCompleted
	turnOutcomeBlocked         = core.TurnOutcomeBlocked
	turnOutcomePlanned         = core.TurnOutcomePlanned
	turnOutcomeTimeout         = core.TurnOutcomeTimeout
	turnOutcomeStructuralError = core.TurnOutcomeStructuralError
	truncationStart            = core.TruncationStart
	truncationEnd              = core.TruncationEnd
	truncationMixed            = core.TruncationMixed
)
