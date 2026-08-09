package executor

import (
	"bufio"
	"os"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

// Tone identifies the semantic tone of executor presentation text.
type Tone int

const (
	ToneDim Tone = iota
	ToneSuccess
	ToneWarning
)

// ConfirmationDecision identifies the user's command confirmation choice.
type ConfirmationDecision int

const (
	ConfirmationDecisionCancel ConfirmationDecision = iota
	ConfirmationDecisionRun
	ConfirmationDecisionEdit
	ConfirmationDecisionInteractive
)

// TraceValue returns the stable trace payload for a confirmation decision.
func (decision ConfirmationDecision) TraceValue() string {
	switch decision {
	case ConfirmationDecisionRun:
		return "run"
	case ConfirmationDecisionEdit:
		return "edit"
	case ConfirmationDecisionInteractive:
		return "interactive"
	case ConfirmationDecisionCancel:
		return "cancel"
	default:
		return "unknown"
	}
}

// Presenter provides process-level presentation operations used by executor.
type Presenter interface {
	BeginTurn(core.ContextInfo) TurnPresenter
	ActiveTurn() TurnPresenter
	BeginManualStep(string) StepPresenter
	Confirm(StepPresenter, *bufio.Reader, *os.File, string, string, configpkg.ConfirmationDefault) (ConfirmationDecision, string, error)
	InteractiveCommandStart()
	Warning(string)
	StyleStart(Tone) string
	StyleEnd() string
}

// TurnPresenter presents command steps within one Shellia turn.
type TurnPresenter interface {
	BeginStep(int, int, core.CommandPlan) StepPresenter
	Suspend()
	Resume()
	Close()
}

// StepPresenter presents one command's state, confirmation, and output.
type StepPresenter interface {
	Close()
	Text(string, Tone)
	Section(string, Tone)
	OutputLabel()
	OutputLine(string)
	IsClosed() bool
}
