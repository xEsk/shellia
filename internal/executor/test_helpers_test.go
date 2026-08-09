package executor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/xEsk/shellia/internal/core"

	configpkg "github.com/xEsk/shellia/internal/config"

	safetypkg "github.com/xEsk/shellia/internal/safety"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

const (
	defaultPlanningMaxRounds = 4
	classificationSafe       = safetypkg.ClassificationSafe
)

func shouldRetryAfterExecutionError(err error, round int, maxRounds int) bool {
	if round >= maxRounds-1 {
		return false
	}

	var promptErr *interactivePromptError
	return errors.As(err, &promptErr)
}

func loopTestContext(t *testing.T) contextInfo {
	t.Helper()
	return core.ContextInfo{
		CWD:   t.TempDir(),
		User:  "test-user",
		OS:    "test-os",
		Shell: "/bin/sh",
	}
}

// executorOptions retains only the configuration consumed by command execution.
func executorOptions(cfg configpkg.Config) Options {
	return Options{
		CommandTimeout:      cfg.CommandTimeout,
		YesSafe:             cfg.YesSafe,
		ContinueOnError:     cfg.ContinueOnError,
		ConfirmationDefault: cfg.ConfirmationDefault,
		CaptureStdoutBytes:  cfg.CaptureStdoutBytes,
		CaptureStderrBytes:  cfg.CaptureStderrBytes,
		ShowSystemOutput:    cfg.ShowSystemOutput,
	}
}

// executorViewOptions retains the presentation settings used by executor tests.
func executorViewOptions(cfg configpkg.Config) uipkg.ViewOptions {
	return uipkg.ViewOptions{
		ShowCommandPopup: cfg.ShowCommandPopup,
		VisualStyle:      cfg.VisualStyle,
	}
}

type executorTestPresenter struct {
	stdout io.Writer
	stderr io.Writer
	ui     bool
	turn   *uipkg.Turn
}

func newExecutorTestPresenter(stdout io.Writer, stderr io.Writer, ui bool, turn *uipkg.Turn) *executorTestPresenter {
	return &executorTestPresenter{stdout: stdout, stderr: stderr, ui: ui, turn: turn}
}

func (presenter *executorTestPresenter) BeginTurn(ctxInfo core.ContextInfo) TurnPresenter {
	if presenter.turn != nil {
		return &executorTestTurnPresenter{turn: presenter.turn}
	}
	renderer := uipkg.NewRenderer(presenter.stdout, uipkg.Presentation{Style: configpkg.VisualStylePlain, ANSI: presenter.ui})
	presenter.turn = renderer.BeginShelliaTurn(uipkg.ViewOptions{VisualStyle: configpkg.VisualStylePlain}, ctxInfo)
	return &executorTestTurnPresenter{turn: presenter.turn, owned: true}
}

func (presenter *executorTestPresenter) ActiveTurn() TurnPresenter {
	if presenter.turn == nil {
		return nil
	}
	return &executorTestTurnPresenter{turn: presenter.turn}
}

func (presenter *executorTestPresenter) BeginManualStep(command string) StepPresenter {
	if presenter.turn != nil {
		return &executorTestStepPresenter{box: presenter.turn.BeginStep(1, 1, commandPlan{Command: command, Purpose: "Manual shell command"})}
	}
	box := uipkg.NewStepBox(presenter.stdout, presenter.ui, "shell")
	box.Spacer()
	box.Command(command)
	return &executorTestStepPresenter{box: box}
}

func (*executorTestPresenter) Confirm(box StepPresenter, reader *bufio.Reader, stdin *os.File, prompt string, command string, defaultChoice configpkg.ConfirmationDefault) (ConfirmationDecision, string, error) {
	step, ok := box.(*executorTestStepPresenter)
	if !ok {
		return ConfirmationDecisionCancel, "", fmt.Errorf("unsupported executor test step presenter %T", box)
	}
	decision, edited, err := uipkg.PromptConfirmation(step.box, reader, stdin, prompt, command, defaultChoice)
	return executorTestConfirmationDecision(decision), edited, err
}

func (presenter *executorTestPresenter) InteractiveCommandStart() {
	uipkg.PrintInteractiveCommandStartTo(presenter.stdout, presenter.ui)
}

func (presenter *executorTestPresenter) Warning(message string) {
	uipkg.PrintWarningTo(presenter.stderr, presenter.ui, message)
}

func (presenter *executorTestPresenter) StyleStart(tone Tone) string {
	return uipkg.StyleStart(presenter.ui, executorTestToneColor(tone))
}

func (presenter *executorTestPresenter) StyleEnd() string {
	return uipkg.StyleEnd(presenter.ui)
}

type executorTestTurnPresenter struct {
	turn  *uipkg.Turn
	owned bool
}

func (presenter *executorTestTurnPresenter) BeginStep(index int, total int, plan core.CommandPlan) StepPresenter {
	return &executorTestStepPresenter{box: presenter.turn.BeginStep(index, total, plan)}
}

func (presenter *executorTestTurnPresenter) Suspend() { presenter.turn.Suspend() }
func (presenter *executorTestTurnPresenter) Resume()  { presenter.turn.Resume() }
func (presenter *executorTestTurnPresenter) Close() {
	if presenter.owned {
		presenter.turn.Close()
	}
}

type executorTestStepPresenter struct {
	box *uipkg.StepBox
}

func (presenter *executorTestStepPresenter) Close() { presenter.box.Close() }
func (presenter *executorTestStepPresenter) Text(text string, tone Tone) {
	presenter.box.Text(text, executorTestToneColor(tone))
}

func (presenter *executorTestStepPresenter) Section(text string, tone Tone) {
	presenter.box.Section(text, executorTestToneColor(tone))
}
func (presenter *executorTestStepPresenter) OutputLabel()           { presenter.box.OutputLabel() }
func (presenter *executorTestStepPresenter) OutputLine(text string) { presenter.box.OutputLine(text) }
func (presenter *executorTestStepPresenter) IsClosed() bool         { return presenter.box.IsClosed() }

func executorTestToneColor(tone Tone) string {
	switch tone {
	case ToneSuccess:
		return uipkg.ColorGreen
	case ToneWarning:
		return uipkg.ColorYellow
	default:
		return uipkg.ColorDim
	}
}

func executorTestConfirmationDecision(decision uipkg.ConfirmDecision) ConfirmationDecision {
	switch decision {
	case uipkg.ConfirmDecisionRun:
		return ConfirmationDecisionRun
	case uipkg.ConfirmDecisionEdit:
		return ConfirmationDecisionEdit
	case uipkg.ConfirmDecisionInteractive:
		return ConfirmationDecisionInteractive
	default:
		return ConfirmationDecisionCancel
	}
}

func openLoopTrace(t *testing.T) *tracepkg.Logger {
	t.Helper()
	options := tracepkg.Options{
		TraceEnabled: true,
		TraceDir:     t.TempDir(),
	}
	logger, err := tracepkg.OpenSession(options, loopTestContext(t))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	return logger
}

func closeLoopTraceAndRead(t *testing.T, logger *tracepkg.Logger) []map[string]any {
	t.Helper()
	path := logger.Path()
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid trace event %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func captureMainLoopIO(t *testing.T, input string, fn func(RuntimeDeps)) string {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	defer stdinRead.Close()
	defer stdoutRead.Close()

	if _, err := io.WriteString(stdinWrite, input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	stdinWrite.Close()

	os.Stdout = stdoutWrite
	os.Stderr = stdoutWrite
	fn(RuntimeDeps{
		Stdin:     stdinRead,
		Stdout:    stdoutWrite,
		Stderr:    stdoutWrite,
		Presenter: newExecutorTestPresenter(stdoutWrite, stdoutWrite, false, nil),
	})
	stdoutWrite.Close()

	output, err := io.ReadAll(stdoutRead)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(output)
}

func traceEventsByName(events []map[string]any, name string) []map[string]any {
	matches := make([]map[string]any, 0)
	for _, event := range events {
		if event["event"] == name {
			matches = append(matches, event)
		}
	}
	return matches
}

func traceEventData(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object", event["data"])
	}
	return data
}
