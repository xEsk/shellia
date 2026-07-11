package session

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
)

type (
	config             = configpkg.Config
	sessionState       = core.SessionState
	turnResult         = core.TurnResult
	commandExecution   = core.CommandExecution
	commandPlan        = core.CommandPlan
	capturedStream     = core.CapturedStream
	observationMemory  = core.ObservationMemory
	truncationStrategy = core.TruncationStrategy
)

const (
	truncationStart = core.TruncationStart
	truncationEnd   = core.TruncationEnd
	truncationMixed = core.TruncationMixed
)
