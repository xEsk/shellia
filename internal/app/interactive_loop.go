package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	interactivepkg "github.com/xEsk/shellia/internal/interactive"
	sessionpkg "github.com/xEsk/shellia/internal/session"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// interactiveSession owns mutable state for one persistent prompt session.
type interactiveSession struct {
	deps         runtimeDeps
	ui           bool
	cfg          config
	contextInfo  *contextInfo
	reader       *bufio.Reader
	mode         interactiveMode
	history      []historyEntry
	state        sessionState
	nextResultID int
}

// interactiveTurnRequest describes one runTurn invocation and how its result
// maps back into interactive-session state.
type interactiveTurnRequest struct {
	config              config
	instruction         string
	resolvedInstruction string
	historyInstruction  string
	retryInstruction    string
	priorProposal       pendingProposal
	acceptedProposal    bool
	reportCancelled     bool
}

// turnApplication contains the only inputs allowed to mutate session state
// after runTurn returns.
type turnApplication struct {
	historyInstruction string
	retryInstruction   string
	priorProposal      pendingProposal
	acceptedProposal   bool
	turn               turnResult
	err                error
	reportCancelled    bool
}

// runInteractive opens a persistent session where each prompt extends the conversation context.
// A fresh signal context is created per turn so Ctrl+C cancels only the current LLM call,
// allowing the loop to continue for the next request. It returns unrecoverable prompt read errors.
func runInteractive(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo) error {
	deps = deps.withDefaults()
	if deps.Renderer == nil {
		deps.Renderer = uipkg.NewRenderer(deps.Stdout, presentation{Style: configpkg.VisualStylePlain, ANSI: ui, User: promptPresentationUser(cfg, ctxInfo.User)})
	}

	session := interactiveSession{
		deps:        deps,
		ui:          ui,
		cfg:         cfg,
		contextInfo: ctxInfo,
		reader:      bufio.NewReader(deps.Stdin),
		mode:        interactiveModeAI,
		history:     make([]historyEntry, 0, maxHistoryEntries),
	}

	uipkg.PrintSessionBannerTo(session.deps.Stdout, ui, viewOptions(session.cfg))

	if strings.TrimSpace(session.cfg.Instruction) != "" {
		session.executeInteractiveTurn(ctx, interactiveTurnRequest{
			config:             session.cfg,
			instruction:        session.cfg.Instruction,
			historyInstruction: session.cfg.Instruction,
			retryInstruction:   session.cfg.Instruction,
		})
	}

	for {
		if ctx.Err() != nil {
			//nolint:nilerr // Parent context cancellation is normal interactive-session completion.
			return nil
		}

		input, err := session.deps.ReadInteractivePrompt(session.ui, session.reader, session.deps.Stdin, session.deps.Stdout, session.mode, viewOptions(session.cfg), session.deps.Renderer)
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(session.deps.Stdout)
				return nil
			}
			return fmt.Errorf("cannot read prompt: %w", err)
		}

		exit := session.routeInteractiveInput(ctx, input)
		if exit {
			return nil
		}
	}
}

// routeInteractiveInput dispatches one prompt to a local command, manual
// execution, or model-backed interactive turn.
func (session *interactiveSession) routeInteractiveInput(ctx context.Context, input string) bool {
	trimmed := strings.TrimSpace(input)
	forcePromptMode := false
	planOnly, plannedInstruction := interactivepkg.ParsePlanInstruction(input)
	if planOnly {
		if strings.TrimSpace(plannedInstruction) == "" {
			uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "Missing plan instruction.")
			return false
		}

		turnCfg := session.cfg
		turnCfg.PlanOnly = true
		session.executeInteractiveTurn(ctx, interactiveTurnRequest{
			config:             turnCfg,
			instruction:        plannedInstruction,
			historyInstruction: input,
			retryInstruction:   plannedInstruction,
			reportCancelled:    true,
		})
		return false
	}

	command := interactivepkg.ParseCommand(trimmed)
	if command != interactivepkg.CommandNone {
		switch command {
		case interactivepkg.CommandUnknown:
			uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "Unknown command: "+trimmed)
			return false
		case interactivepkg.CommandExit:
			fmt.Fprintln(session.deps.Stdout)
			uipkg.PrintInfoTo(session.deps.Stdout, session.ui, "Session closed.")
			return true
		case interactivepkg.CommandClear:
			uipkg.ClearScreenTo(session.deps.Stdout)
			return false
		case interactivepkg.CommandContext:
			uipkg.PrintContextTo(session.deps.Stdout, session.ui, viewOptions(session.cfg), *session.contextInfo)
			return false
		case interactivepkg.CommandShell:
			session.mode = interactiveModeShell
			uipkg.PrintModeStatusTo(session.deps.Stdout, session.ui, fmt.Sprintf("Shell mode enabled (%s).", session.cfg.ShellMode))
			return false
		case interactivepkg.CommandAI:
			session.mode = interactiveModeAI
			uipkg.PrintModeStatusTo(session.deps.Stdout, session.ui, "Prompt mode enabled.")
			return false
		case interactivepkg.CommandMode:
			uipkg.PrintModeStatusTo(session.deps.Stdout, session.ui, "Current mode: "+string(session.mode))
			return false
		case interactivepkg.CommandModel:
			modelName := interactivepkg.ParseModelCommandName(trimmed)
			if modelName == "" {
				printModelProfilesTo(session.deps.Stdout, session.ui, session.cfg)
				return false
			}
			if err := switchInteractiveModel(&session.cfg, modelName); err != nil {
				uipkg.PrintWarningTo(session.deps.Stderr, session.ui, err.Error())
				return false
			}
			uipkg.PrintModelSwitchTo(session.deps.Stdout, session.ui, viewOptions(session.cfg))
			return false
		case interactivepkg.CommandTheme:
			themeName := interactivepkg.ParseThemeCommandName(trimmed)
			if themeName == "" {
				printVisualThemesTo(session.deps.Stdout, session.ui, session.cfg)
				return false
			}
			if err := switchInteractiveTheme(&session.cfg, &session.deps, session.contextInfo.User, themeName); err != nil {
				uipkg.PrintWarningTo(session.deps.Stderr, session.ui, err.Error())
				return false
			}
			fmt.Fprintln(session.deps.Stdout)
			uipkg.PrintInfoTo(session.deps.Stdout, session.ui, "Theme switched to "+string(session.cfg.VisualStyle)+".")
			return false
		case interactivepkg.CommandPlan:
			uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "Missing plan instruction.")
			return false
		case interactivepkg.CommandRetry:
			if strings.TrimSpace(session.state.LastRetryInstruction) == "" {
				uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "No failed or cancelled request to retry.")
				return false
			}
			trimmed = session.state.LastRetryInstruction
			input = session.state.LastRetryInstruction
			forcePromptMode = true
			uipkg.PrintInfoTo(session.deps.Stdout, session.ui, fmt.Sprintf("Retrying: %s", input))
		case interactivepkg.CommandNew:
			session.history = make([]historyEntry, 0, maxHistoryEntries)
			session.state = sessionState{}
			uipkg.PrintNewSessionSeparatorTo(session.deps.Stdout, session.ui)
			return false
		}
	}

	if trimmed == "" {
		return false
	}

	if !forcePromptMode && session.mode == interactiveModeAI && strings.TrimSpace(session.state.PendingProposal.Objective) != "" && sessionpkg.IsProposalDecline(trimmed) {
		declined := session.state.PendingProposal
		session.state.PendingProposal = pendingProposal{}
		session.deps.Trace.Record("pending_proposal_declined", "", "session", -1, map[string]any{
			"objective": declined.Objective,
		})
		uipkg.PrintInfoTo(session.deps.Stdout, session.ui, "D’acord. No ho executaré.")
		return false
	}

	if !forcePromptMode && (session.mode == interactiveModeShell || strings.HasPrefix(trimmed, "!")) {
		session.executeManualInput(ctx, trimmed)
		return false
	}

	instruction := input
	priorProposal := session.state.PendingProposal
	resolvedInstruction := sessionpkg.ResolveInstructionForPlanning(instruction, session.state)
	acceptedProposal := resolvedInstruction != strings.TrimSpace(instruction) && strings.TrimSpace(priorProposal.Objective) != ""
	retryInstruction := instruction
	if acceptedProposal {
		retryInstruction = resolvedInstruction
	}

	session.executeInteractiveTurn(ctx, interactiveTurnRequest{
		config:              session.cfg,
		instruction:         instruction,
		resolvedInstruction: resolvedInstruction,
		historyInstruction:  instruction,
		retryInstruction:    retryInstruction,
		priorProposal:       priorProposal,
		acceptedProposal:    acceptedProposal,
		reportCancelled:     true,
	})
	return false
}

// executeInteractiveTurn runs one model-backed turn and immediately routes its
// result through applyTurnResult.
func (session *interactiveSession) executeInteractiveTurn(ctx context.Context, request interactiveTurnRequest) {
	turnState := session.state
	if request.acceptedProposal {
		turnState.PendingProposal = pendingProposal{}
	}

	turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	turn, err := runTurn(turnCtx, session.deps, session.ui, turnRequest{
		Config:              request.config,
		ContextInfo:         session.contextInfo,
		Instruction:         request.instruction,
		ResolvedInstruction: request.resolvedInstruction,
		AcceptedProposal:    request.acceptedProposal,
		History:             session.history,
		State:               turnState,
	})
	stop()

	session.applyTurnResult(turnApplication{
		historyInstruction: request.historyInstruction,
		retryInstruction:   request.retryInstruction,
		priorProposal:      request.priorProposal,
		acceptedProposal:   request.acceptedProposal,
		turn:               turn,
		err:                err,
		reportCancelled:    request.reportCancelled,
	})
}

// executeManualInput runs one explicit shell command and retains its reusable
// execution memory without adding model-turn history.
func (session *interactiveSession) executeManualInput(ctx context.Context, input string) {
	command := input
	renderMode := renderModeForShellSession(session.cfg)
	if session.mode != interactiveModeShell {
		command = strings.TrimSpace(strings.TrimPrefix(command, "!"))
		renderMode = renderModeForManualCommand(session.cfg)
	}
	if command == "" {
		uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "Missing shell command.")
		return
	}

	turnCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	session.state.LastObservationObjective = ""
	execution, err := session.deps.ExecuteManualCommand(turnCtx, session.deps, session.ui, executorOptions(session.cfg), session.contextInfo, command, renderMode)
	stop()

	if errors.Is(err, context.Canceled) {
		uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "Command cancelled.")
		return
	}
	if err != nil {
		uipkg.PrintWarningTo(session.deps.Stderr, session.ui, err.Error())
		return
	}

	sessionpkg.UpdateStateFromExecution(&session.state, command, execution, sessionMemoryOptions(session.cfg))
}

// applyTurnResult is the single post-runTurn owner of interactive history,
// retry state, unfinished instructions, partial executions, and proposals.
func (session *interactiveSession) applyTurnResult(application turnApplication) {
	if application.acceptedProposal || strings.TrimSpace(application.priorProposal.Objective) != "" {
		session.state.PendingProposal = pendingProposal{}
	}

	if application.err != nil && len(application.turn.Executions) > 0 {
		sessionpkg.UpdateState(&session.state, application.retryInstruction, application.turn, sessionMemoryOptions(session.cfg))
	}

	if errors.Is(application.err, core.ErrAborted) || errors.Is(application.err, context.Canceled) {
		session.state.LastRetryInstruction = application.retryInstruction
		sessionpkg.RememberUnfinishedInstruction(&session.state, application.retryInstruction)
		if application.reportCancelled {
			uipkg.PrintWarningTo(session.deps.Stderr, session.ui, "Request cancelled.")
			fmt.Fprintln(session.deps.Stdout)
			uipkg.PrintSeparator(session.deps.Stdout, session.ui)
		}
		return
	}
	if application.err != nil {
		uipkg.PrintWarningTo(session.deps.Stderr, session.ui, application.err.Error())
		session.state.LastRetryInstruction = application.retryInstruction
		sessionpkg.RememberUnfinishedInstruction(&session.state, application.retryInstruction)
		return
	}

	if !application.acceptedProposal && strings.TrimSpace(application.priorProposal.Objective) != "" && application.turn.Outcome == turnOutcomeCompleted && strings.TrimSpace(application.turn.Proposal.Objective) != strings.TrimSpace(application.priorProposal.Objective) {
		session.deps.Trace.Record("pending_proposal_replaced", "", "session", -1, map[string]any{
			"previous_objective":    application.priorProposal.Objective,
			"replacement_objective": application.turn.Proposal.Objective,
		})
	}

	session.nextResultID++
	session.history = append(session.history, historyEntry{
		ID:             fmt.Sprintf("result-%d", session.nextResultID),
		Instruction:    application.historyInstruction,
		Outcome:        application.turn.Outcome,
		Result:         application.turn.Result,
		CharacterCount: len([]rune(application.turn.Result)),
	})
	sessionpkg.UpdateState(&session.state, application.retryInstruction, application.turn, sessionMemoryOptions(session.cfg))
	if application.acceptedProposal && application.turn.Outcome != turnOutcomeCompleted && application.turn.Outcome != turnOutcomeDeclined {
		session.state.LastRetryInstruction = application.retryInstruction
		sessionpkg.RememberUnfinishedInstruction(&session.state, application.retryInstruction)
	}
	if application.turn.Outcome == turnOutcomeCompleted {
		session.state.LastRetryInstruction = ""
	}
	if len(session.history) > maxHistoryEntries {
		session.history = session.history[len(session.history)-maxHistoryEntries:]
	}
}

// renderModeForShellSession maps the configured shell mode to the executor mode.
func renderModeForShellSession(cfg config) manualRenderMode {
	if cfg.ShellMode == configpkg.CommandEnginePlain {
		return executorpkg.ManualRenderDirect
	}
	return executorpkg.ManualRenderShellInteractive
}

// renderModeForManualCommand maps the configured one-off command mode to the executor mode.
func renderModeForManualCommand(cfg config) manualRenderMode {
	if cfg.CommandMode == configpkg.CommandEngineInteractive {
		return executorpkg.ManualRenderInteractive
	}
	return executorpkg.ManualRenderInline
}
