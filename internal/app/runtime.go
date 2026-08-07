package app

import (
	"context"
	"net/http"
	"os"

	"golang.org/x/term"
	executorpkg "shellia/internal/executor"
)

// commandRunner executes a model-generated command plan.
type commandRunner func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error)

// manualCommandRunner executes a user-entered shell command.
type manualCommandRunner func(context.Context, runtimeDeps, bool, config, *contextInfo, string, manualRenderMode) (commandExecution, error)

// runtimeDeps groups process dependencies used by the core session loops.
type runtimeDeps struct {
	Stdin                *os.File
	Stdout               *os.File
	Stderr               *os.File
	HTTPClient           *http.Client
	ExecuteCommands      commandRunner
	ExecuteManualCommand manualCommandRunner
	StdoutIsTerminal     func(*os.File) bool
	Trace                *traceLogger
}

// defaultRuntimeDeps returns the production dependencies for Shellia.
func defaultRuntimeDeps() runtimeDeps {
	return runtimeDeps{
		Stdin:                os.Stdin,
		Stdout:               os.Stdout,
		Stderr:               os.Stderr,
		HTTPClient:           &http.Client{},
		ExecuteCommands:      executeCommands,
		ExecuteManualCommand: executeManualCommand,
		StdoutIsTerminal:     stdoutIsTerminal,
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
	if deps.ExecuteCommands == nil {
		deps.ExecuteCommands = defaults.ExecuteCommands
	}
	if deps.ExecuteManualCommand == nil {
		deps.ExecuteManualCommand = defaults.ExecuteManualCommand
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

func executorDeps(deps runtimeDeps) executorpkg.RuntimeDeps {
	return executorpkg.RuntimeDeps{
		Stdin:      deps.Stdin,
		Stdout:     deps.Stdout,
		Stderr:     deps.Stderr,
		HTTPClient: deps.HTTPClient,
		Trace:      deps.Trace,
	}
}

func executeCommands(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (commandBatchResult, error) {
	return executorpkg.ExecuteCommands(ctx, executorDeps(deps), ui, cfg, ctxInfo, plans, priorExecutions)
}

func executeManualCommand(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, command string, renderMode manualRenderMode) (commandExecution, error) {
	return executorpkg.ExecuteManualCommand(ctx, executorDeps(deps), ui, cfg, ctxInfo, command, renderMode)
}
