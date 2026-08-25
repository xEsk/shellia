package app

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"

	executorpkg "github.com/xEsk/shellia/internal/executor"
	uipkg "github.com/xEsk/shellia/internal/ui"
	updatepkg "github.com/xEsk/shellia/internal/update"
	"golang.org/x/term"
)

// commandRunner executes a model-generated command plan.
type commandRunner func(context.Context, runtimeDeps, bool, executorpkg.Options, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error)

// manualCommandRunner executes a user-entered shell command.
type manualCommandRunner func(context.Context, runtimeDeps, bool, executorpkg.Options, *contextInfo, string, manualRenderMode) (commandExecution, error)

// runtimeDeps groups process dependencies used by the core session loops.
type runtimeDeps struct {
	Stdin                 *os.File
	Stdout                *os.File
	Stderr                *os.File
	HTTPClient            *http.Client
	LatestReleaseURL      string
	ExecutablePath        func() (string, error)
	ExecuteCommands       commandRunner
	ExecuteManualCommand  manualCommandRunner
	ReadInteractivePrompt func(bool, *bufio.Reader, *os.File, io.Writer, interactiveMode, uipkg.ViewOptions, *uipkg.Renderer) (string, error)
	StdoutIsTerminal      func(*os.File) bool
	Trace                 *traceLogger
	Renderer              *uipkg.Renderer
	Turn                  *uipkg.Turn
}

// defaultRuntimeDeps returns the production dependencies for Shellia.
func defaultRuntimeDeps() runtimeDeps {
	return runtimeDeps{
		Stdin:                 os.Stdin,
		Stdout:                os.Stdout,
		Stderr:                os.Stderr,
		HTTPClient:            &http.Client{},
		LatestReleaseURL:      updatepkg.LatestReleaseURL,
		ExecutablePath:        os.Executable,
		ExecuteCommands:       executeCommands,
		ExecuteManualCommand:  executeManualCommand,
		ReadInteractivePrompt: uipkg.ReadInteractivePromptWithRenderer,
		StdoutIsTerminal:      stdoutIsTerminal,
	}
}

// withDefaults fills missing dependencies so tests can override only what they need.
func (deps runtimeDeps) withDefaults() runtimeDeps {
	defaults := defaultRuntimeDeps()
	if deps.Stdin == nil {
		deps.Stdin = defaults.Stdin
	}
	if deps.Stdout == nil {
		deps.Stdout = defaults.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = defaults.Stderr
	}
	if deps.HTTPClient == nil {
		deps.HTTPClient = defaults.HTTPClient
	}
	if deps.LatestReleaseURL == "" {
		deps.LatestReleaseURL = defaults.LatestReleaseURL
	}
	if deps.ExecutablePath == nil {
		deps.ExecutablePath = defaults.ExecutablePath
	}
	if deps.ExecuteCommands == nil {
		deps.ExecuteCommands = defaults.ExecuteCommands
	}
	if deps.ExecuteManualCommand == nil {
		deps.ExecuteManualCommand = defaults.ExecuteManualCommand
	}
	if deps.ReadInteractivePrompt == nil {
		deps.ReadInteractivePrompt = defaults.ReadInteractivePrompt
	}
	if deps.StdoutIsTerminal == nil {
		deps.StdoutIsTerminal = defaults.StdoutIsTerminal
	}
	return deps
}

// stdoutIsTerminal reports whether the injected output file is a terminal.
func stdoutIsTerminal(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}

func executorDeps(deps runtimeDeps, ui bool) executorpkg.RuntimeDeps {
	return executorpkg.RuntimeDeps{
		Stdin:     deps.Stdin,
		Stdout:    deps.Stdout,
		Stderr:    deps.Stderr,
		Trace:     deps.Trace,
		Presenter: newExecutorPresenter(deps, ui),
	}
}

func executeCommands(ctx context.Context, deps runtimeDeps, ui bool, options executorpkg.Options, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (commandBatchResult, error) {
	return executorpkg.ExecuteCommands(ctx, executorDeps(deps, ui), ui, options, ctxInfo, plans, priorExecutions)
}

func executeManualCommand(ctx context.Context, deps runtimeDeps, ui bool, options executorpkg.Options, ctxInfo *contextInfo, command string, renderMode manualRenderMode) (commandExecution, error) {
	return executorpkg.ExecuteManualCommand(ctx, executorDeps(deps, ui), ui, options, ctxInfo, command, renderMode)
}
