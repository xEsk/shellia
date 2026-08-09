package app

import (
	"bufio"
	"fmt"
	"io"
	"os"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// executorPresenter adapts executor-owned presentation semantics to UI.
type executorPresenter struct {
	stdout io.Writer
	stderr io.Writer
	ui     bool
	turn   *uipkg.Turn
}

// newExecutorPresenter creates the presentation adapter for one executor call.
func newExecutorPresenter(deps runtimeDeps, ui bool) *executorPresenter {
	return &executorPresenter{
		stdout: deps.Stdout,
		stderr: deps.Stderr,
		ui:     ui,
		turn:   deps.Turn,
	}
}

// BeginTurn returns the active app turn or creates the executor's plain fallback turn.
func (presenter *executorPresenter) BeginTurn(ctxInfo core.ContextInfo) executorpkg.TurnPresenter {
	if presenter.turn != nil {
		return &executorTurnPresenter{turn: presenter.turn}
	}
	renderer := uipkg.NewRenderer(presenter.stdout, uipkg.Presentation{Style: configpkg.VisualStylePlain, ANSI: presenter.ui})
	presenter.turn = renderer.BeginShelliaTurn(uipkg.ViewOptions{VisualStyle: configpkg.VisualStylePlain}, ctxInfo)
	return &executorTurnPresenter{turn: presenter.turn, owned: true}
}

// ActiveTurn returns the app-owned turn available for terminal suspension.
func (presenter *executorPresenter) ActiveTurn() executorpkg.TurnPresenter {
	if presenter.turn == nil {
		return nil
	}
	return &executorTurnPresenter{turn: presenter.turn}
}

// BeginManualStep opens the current turn's manual step or a standalone shell step.
func (presenter *executorPresenter) BeginManualStep(command string) executorpkg.StepPresenter {
	if presenter.turn != nil {
		return &executorStepPresenter{box: presenter.turn.BeginStep(1, 1, commandPlan{
			Command: command,
			Purpose: "Manual shell command",
		})}
	}
	box := uipkg.NewStepBox(presenter.stdout, presenter.ui, "shell")
	box.Spacer()
	box.Command(command)
	return &executorStepPresenter{box: box}
}

// Confirm delegates command confirmation and editing to UI.
func (*executorPresenter) Confirm(box executorpkg.StepPresenter, reader *bufio.Reader, stdin *os.File, prompt string, command string, defaultChoice configpkg.ConfirmationDefault) (executorpkg.ConfirmationDecision, string, error) {
	step, ok := box.(*executorStepPresenter)
	if !ok {
		return executorpkg.ConfirmationDecisionCancel, "", fmt.Errorf("unsupported executor step presenter %T", box)
	}
	decision, edited, err := uipkg.PromptConfirmation(step.box, reader, stdin, prompt, command, defaultChoice)
	return executorConfirmationDecision(decision), edited, err
}

// InteractiveCommandStart presents the raw terminal handoff message.
func (presenter *executorPresenter) InteractiveCommandStart() {
	uipkg.PrintInteractiveCommandStartTo(presenter.stdout, presenter.ui)
}

// Warning presents a non-fatal executor warning.
func (presenter *executorPresenter) Warning(message string) {
	uipkg.PrintWarningTo(presenter.stderr, presenter.ui, message)
}

// StyleStart returns UI's opening sequence for an executor semantic tone.
func (presenter *executorPresenter) StyleStart(tone executorpkg.Tone) string {
	return uipkg.StyleStart(presenter.ui, executorToneColor(tone))
}

// StyleEnd returns UI's closing style sequence.
func (presenter *executorPresenter) StyleEnd() string {
	return uipkg.StyleEnd(presenter.ui)
}

type executorTurnPresenter struct {
	turn  *uipkg.Turn
	owned bool
}

func (presenter *executorTurnPresenter) BeginStep(index int, total int, plan core.CommandPlan) executorpkg.StepPresenter {
	return &executorStepPresenter{box: presenter.turn.BeginStep(index, total, plan)}
}

func (presenter *executorTurnPresenter) Suspend() { presenter.turn.Suspend() }
func (presenter *executorTurnPresenter) Resume()  { presenter.turn.Resume() }
func (presenter *executorTurnPresenter) Close() {
	if presenter.owned {
		presenter.turn.Close()
	}
}

type executorStepPresenter struct {
	box *uipkg.StepBox
}

func (presenter *executorStepPresenter) Close() { presenter.box.Close() }
func (presenter *executorStepPresenter) Text(text string, tone executorpkg.Tone) {
	presenter.box.Text(text, executorToneColor(tone))
}

func (presenter *executorStepPresenter) Section(text string, tone executorpkg.Tone) {
	presenter.box.Section(text, executorToneColor(tone))
}
func (presenter *executorStepPresenter) OutputLabel()           { presenter.box.OutputLabel() }
func (presenter *executorStepPresenter) OutputLine(text string) { presenter.box.OutputLine(text) }
func (presenter *executorStepPresenter) IsClosed() bool         { return presenter.box.IsClosed() }

func executorToneColor(tone executorpkg.Tone) string {
	switch tone {
	case executorpkg.ToneSuccess:
		return uipkg.ColorGreen
	case executorpkg.ToneWarning:
		return uipkg.ColorYellow
	default:
		return uipkg.ColorDim
	}
}

func executorConfirmationDecision(decision uipkg.ConfirmDecision) executorpkg.ConfirmationDecision {
	switch decision {
	case uipkg.ConfirmDecisionRun:
		return executorpkg.ConfirmationDecisionRun
	case uipkg.ConfirmDecisionEdit:
		return executorpkg.ConfirmationDecisionEdit
	case uipkg.ConfirmDecisionInteractive:
		return executorpkg.ConfirmationDecisionInteractive
	default:
		return executorpkg.ConfirmationDecisionCancel
	}
}
