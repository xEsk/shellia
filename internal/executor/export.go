package executor

import (
	"context"
	"net/http"
	"os"

	"github.com/xEsk/shellia/internal/core"

	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// RuntimeDeps groups process dependencies needed by the executor.
type RuntimeDeps struct {
	Stdin      *os.File
	Stdout     *os.File
	Stderr     *os.File
	HTTPClient *http.Client
	Trace      *tracepkg.Logger
	Turn       *uipkg.Turn
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
	if deps.HTTPClient == nil {
		deps.HTTPClient = http.DefaultClient
	}
	return deps
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
func GetContext(parentCtx context.Context, cfg config) (contextInfo, error) {
	return getContext(parentCtx, cfg)
}

// ExecuteCommands runs the sequential plan and returns its structured batch outcome.
func ExecuteCommands(ctx context.Context, deps RuntimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (core.CommandBatchResult, error) {
	return executeCommands(ctx, deps, ui, cfg, ctxInfo, plans, priorExecutions)
}

// ExecuteManualCommand executes a user-entered shell command.
func ExecuteManualCommand(ctx context.Context, deps RuntimeDeps, ui bool, cfg config, ctxInfo *contextInfo, command string, renderMode ManualRenderMode) (commandExecution, error) {
	return executeManualCommand(ctx, deps, ui, cfg, ctxInfo, command, renderMode)
}
