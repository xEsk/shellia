package executor

import (
	"context"
	"os"
	"time"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"

	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// RuntimeDeps groups process dependencies needed by the executor.
type RuntimeDeps struct {
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
	Trace  *tracepkg.Logger
	Turn   *uipkg.Turn
}

func (deps RuntimeDeps) withDefaults() RuntimeDeps {
	if deps.Stdin == nil {
		deps.Stdin = os.Stdin
	}
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	return deps
}

// ContextOptions contains the configuration consumed while collecting local context.
type ContextOptions struct {
	IncludeUser bool
}

// Options contains the configuration consumed while executing commands.
type Options struct {
	CommandTimeout      time.Duration
	YesSafe             bool
	ContinueOnError     bool
	ConfirmationDefault configpkg.ConfirmationDefault
	CaptureStdoutBytes  int
	CaptureStderrBytes  int
	ShowSystemOutput    bool
}

// ManualRenderMode controls how a user-entered shell command is rendered and executed.
type ManualRenderMode = manualRenderMode

const (
	ManualRenderInline           = manualRenderInline
	ManualRenderDirect           = manualRenderDirect
	ManualRenderInteractive      = manualRenderInteractive
	ManualRenderShellInteractive = manualRenderShellInteractive
)

// CommandRunError represents an executed command that finished with an error or timeout.
type CommandRunError = commandRunError

// InteractivePromptError reports that a non-interactive command asked for terminal input.
type InteractivePromptError = interactivePromptError

// GetContext collects the local context available to the model and UI.
func GetContext(parentCtx context.Context, options ContextOptions) (contextInfo, error) {
	return getContext(parentCtx, options)
}

// ExecuteCommands runs the sequential plan and returns its structured batch outcome.
func ExecuteCommands(ctx context.Context, deps RuntimeDeps, ui bool, options Options, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (core.CommandBatchResult, error) {
	return executeCommands(ctx, deps, ui, options, ctxInfo, plans, priorExecutions)
}

// ExecuteManualCommand executes a user-entered shell command.
func ExecuteManualCommand(ctx context.Context, deps RuntimeDeps, ui bool, options Options, ctxInfo *contextInfo, command string, renderMode ManualRenderMode) (commandExecution, error) {
	return executeManualCommand(ctx, deps, ui, options, ctxInfo, command, renderMode)
}
