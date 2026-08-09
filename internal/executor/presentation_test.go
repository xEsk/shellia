package executor

import (
	"bufio"
	"os"
	"testing"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
)

// TestExecuteCommandsUsesExecutorPresenter checks command execution depends on
// executor-owned presentation contracts rather than concrete UI types.
func TestExecuteCommandsUsesExecutorPresenter(t *testing.T) {
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("CreateTemp(stdin) error = %v", err)
	}
	t.Cleanup(func() { stdin.Close() }) //nolint:errcheck // best-effort test cleanup.

	presenter := &contractPresenter{step: &contractStepPresenter{}}
	ctxInfo := loopTestContext(t)
	_, err = executeCommands(t.Context(), RuntimeDeps{
		Stdin:     stdin,
		Presenter: presenter,
	}, false, Options{
		YesSafe:          true,
		ShowSystemOutput: true,
	}, &ctxInfo, []commandPlan{{
		Command:        "printf boundary",
		Purpose:        "Exercise presentation boundary",
		Classification: classificationSafe,
		LocalSafe:      true,
	}}, nil)
	if err != nil {
		t.Fatalf("executeCommands() error = %v", err)
	}
	if presenter.beginTurnCalls != 1 {
		t.Fatalf("BeginTurn() calls = %d, want 1", presenter.beginTurnCalls)
	}
	if presenter.turn.beginStepCalls != 1 {
		t.Fatalf("BeginStep() calls = %d, want 1", presenter.turn.beginStepCalls)
	}
	if len(presenter.step.outputLines) != 1 || presenter.step.outputLines[0] != "boundary" {
		t.Fatalf("output lines = %#v, want [boundary]", presenter.step.outputLines)
	}
}

type contractPresenter struct {
	turn           contractTurnPresenter
	step           *contractStepPresenter
	beginTurnCalls int
}

func (presenter *contractPresenter) BeginTurn(core.ContextInfo) TurnPresenter {
	presenter.beginTurnCalls++
	presenter.turn.step = presenter.step
	return &presenter.turn
}

func (*contractPresenter) ActiveTurn() TurnPresenter { return nil }

func (presenter *contractPresenter) BeginManualStep(string) StepPresenter {
	return presenter.step
}

func (*contractPresenter) Confirm(StepPresenter, *bufio.Reader, *os.File, string, string, configpkg.ConfirmationDefault) (ConfirmationDecision, string, error) {
	return ConfirmationDecisionRun, "", nil
}

func (*contractPresenter) InteractiveCommandStart() {}
func (*contractPresenter) Warning(string)           {}
func (*contractPresenter) StyleStart(Tone) string   { return "" }
func (*contractPresenter) StyleEnd() string         { return "" }

type contractTurnPresenter struct {
	step           StepPresenter
	beginStepCalls int
}

func (presenter *contractTurnPresenter) BeginStep(int, int, core.CommandPlan) StepPresenter {
	presenter.beginStepCalls++
	return presenter.step
}

func (*contractTurnPresenter) Suspend() {}
func (*contractTurnPresenter) Resume()  {}
func (*contractTurnPresenter) Close()   {}

type contractStepPresenter struct {
	closed      bool
	outputLines []string
}

func (presenter *contractStepPresenter) Close()     { presenter.closed = true }
func (*contractStepPresenter) Text(string, Tone)    {}
func (*contractStepPresenter) Section(string, Tone) {}
func (*contractStepPresenter) OutputLabel()         {}
func (presenter *contractStepPresenter) OutputLine(text string) {
	presenter.outputLines = append(presenter.outputLines, text)
}
func (presenter *contractStepPresenter) IsClosed() bool { return presenter.closed }
