package llm

import (
	configpkg "shellia/internal/config"
	"shellia/internal/core"
	safetypkg "shellia/internal/safety"
	sessionpkg "shellia/internal/session"
	tracepkg "shellia/internal/trace"
)

type (
	config             = configpkg.Config
	contextInfo        = core.ContextInfo
	gitContext         = core.GitContext
	historyEntry       = core.HistoryEntry
	observationMemory  = core.ObservationMemory
	sessionState       = core.SessionState
	commandExecution   = core.CommandExecution
	capturedStream     = core.CapturedStream
	commandPlan        = core.CommandPlan
	truncationStrategy = core.TruncationStrategy
	traceLogger        = tracepkg.Logger
)

const (
	truncationStart = core.TruncationStart
	truncationEnd   = core.TruncationEnd
	truncationMixed = core.TruncationMixed
)

const classificationSafe = safetypkg.ClassificationSafe

var (
	classifyCommand               = safetypkg.ClassifyCommand
	higherRisk                    = safetypkg.HigherRisk
	resolveInstructionForPlanning = sessionpkg.ResolveInstructionForPlanning
	defaultConfig                 = configpkg.DefaultConfig
)
