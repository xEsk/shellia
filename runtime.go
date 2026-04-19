package main

import (
	"context"
	"net/http"
	"os"
)

// commandRunner executes a model-generated command plan.
type commandRunner func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) ([]commandExecution, error)

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
	return deps
}
