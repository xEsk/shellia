package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	executorpkg "github.com/xEsk/shellia/internal/executor"
)

// TestInteractiveSessionExecuteTurnAppliesCompletedResultOnce checks the turn
// route appends and trims one completed result while clearing retry state.
func TestInteractiveSessionExecuteTurnAppliesCompletedResultOnce(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Answer provided","summary":"completed result","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	history := make([]historyEntry, maxHistoryEntries)
	for index := range history {
		history[index] = historyEntry{Instruction: fmt.Sprintf("old-%d", index)}
	}

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		session := interactiveSession{
			deps:        deps,
			cfg:         cfg,
			contextInfo: &ctxInfo,
			history:     history,
			state:       sessionState{LastRetryInstruction: "previous failure"},
		}

		session.executeInteractiveTurn(t.Context(), interactiveTurnRequest{
			config:             cfg,
			instruction:        "completed instruction",
			historyInstruction: "completed instruction",
			retryInstruction:   "completed instruction",
		})

		if len(session.history) != maxHistoryEntries {
			t.Fatalf("history length = %d, want %d", len(session.history), maxHistoryEntries)
		}
		if session.history[0].Instruction != "old-1" || session.history[maxHistoryEntries-1] != (historyEntry{Instruction: "completed instruction", Result: "completed result"}) {
			t.Fatalf("history = %#v, want exactly one routed result", session.history)
		}
		if session.state.LastRetryInstruction != "" || session.state.PendingIntent != "" {
			t.Fatalf("state after completion = %#v, want cleared retry and pending intent", session.state)
		}
	})
}

// TestInteractiveSessionExecuteTurnAppliesBlockedResultOnce checks the turn
// route applies one blocker to both bounded history and session memory.
func TestInteractiveSessionExecuteTurnAppliesBlockedResultOnce(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"blocked","objective_mode":"act","success_criteria":"Service restarted","summary":"service name required","blocker_kind":"missing_input","blocker_reason":"Specify the service.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		session := interactiveSession{deps: deps, cfg: cfg, contextInfo: &ctxInfo}

		session.executeInteractiveTurn(t.Context(), interactiveTurnRequest{
			config:             cfg,
			instruction:        "restart the service",
			historyInstruction: "restart the service",
			retryInstruction:   "restart the service",
		})

		if len(session.history) != 1 || session.history[0] != (historyEntry{Instruction: "restart the service", Result: "service name required\nSpecify the service."}) {
			t.Fatalf("history = %#v, want exactly one routed blocked result", session.history)
		}
		if session.state.PendingIntent != "restart the service" || session.state.LastBlockerKind != "missing_input" || session.state.LastBlockerReason != "Specify the service." {
			t.Fatalf("state after blocker = %#v, want one blocker application", session.state)
		}
	})
}

// TestInteractiveSessionExecuteTurnAppliesCancellationOnce checks the turn
// route retains partial execution memory and renders one cancellation warning.
func TestInteractiveSessionExecuteTurnAppliesCancellationOnce(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Working directory observed","summary":"Inspect once.","commands":[{"command":"pwd","purpose":"Inspect before cancellation","risk":"safe","requires_confirmation":false}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ executorpkg.Options, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{
				Command: plans[0].Command, Purpose: plans[0].Purpose, Stdout: capturedStream{Text: "observed-before-cancel"},
			}}}, context.Canceled
		}
		session := interactiveSession{deps: deps, cfg: cfg, contextInfo: &ctxInfo}

		session.executeInteractiveTurn(t.Context(), interactiveTurnRequest{
			config:             cfg,
			instruction:        "inspect once",
			historyInstruction: "inspect once",
			retryInstruction:   "inspect once",
			reportCancelled:    true,
		})

		if len(session.history) != 0 {
			t.Fatalf("history = %#v, want no cancelled result", session.history)
		}
		if session.state.LastRetryInstruction != "inspect once" || session.state.PendingIntent != "inspect once" || len(session.state.LastObservations) != 1 {
			t.Fatalf("state after cancellation = %#v, want one partial application", session.state)
		}
	})

	if strings.Count(output, "Request cancelled.") != 1 {
		t.Fatalf("output = %q, want exactly one routed cancellation warning", output)
	}
}

// TestInteractiveSessionExecuteTurnAppliesPartialErrorOnce checks the turn
// route retains partial execution memory and renders one provider error.
func TestInteractiveSessionExecuteTurnAppliesPartialErrorOnce(t *testing.T) {
	turnErr := errors.New("provider failed after execution")
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Working directory observed","summary":"Inspect before explaining.","commands":[{"command":"pwd","purpose":"Inspect before provider error","risk":"safe","requires_confirmation":false}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ executorpkg.Options, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{
				Command: plans[0].Command, Purpose: plans[0].Purpose, Stdout: capturedStream{Text: "/tmp/project"},
			}}}, turnErr
		}
		session := interactiveSession{deps: deps, cfg: cfg, contextInfo: &ctxInfo}

		session.executeInteractiveTurn(t.Context(), interactiveTurnRequest{
			config:             cfg,
			instruction:        "inspect then explain",
			historyInstruction: "inspect then explain",
			retryInstruction:   "inspect then explain",
		})

		if len(session.history) != 0 {
			t.Fatalf("history = %#v, want no failed result", session.history)
		}
		if session.state.LastRetryInstruction != "inspect then explain" || session.state.PendingIntent != "inspect then explain" || len(session.state.LastObservations) != 1 {
			t.Fatalf("state after partial error = %#v, want one partial application", session.state)
		}
	})

	if strings.Count(output, turnErr.Error()) != 1 {
		t.Fatalf("output = %q, want error exactly once", output)
	}
}

// TestInteractiveSessionExecuteTurnAppliesAcceptedProposalOnce checks the turn
// route consumes the offer, records acceptance, and applies the result once.
func TestInteractiveSessionExecuteTurnAppliesAcceptedProposalOnce(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Accepted objective explained","summary":"offer completed","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	proposal := pendingProposal{Objective: "offered objective", Summary: "Offer"}

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		session := interactiveSession{
			deps:        deps,
			cfg:         cfg,
			contextInfo: &ctxInfo,
			state: sessionState{
				LastRetryInstruction: "offered objective",
				PendingProposal:      proposal,
			},
		}

		session.executeInteractiveTurn(t.Context(), interactiveTurnRequest{
			config:              cfg,
			instruction:         "yes",
			resolvedInstruction: "offered objective",
			historyInstruction:  "yes",
			retryInstruction:    "offered objective",
			priorProposal:       proposal,
			acceptedProposal:    true,
		})

		if len(session.history) != 1 || session.history[0] != (historyEntry{Instruction: "yes", Result: "offer completed"}) {
			t.Fatalf("history = %#v, want accepted result exactly once", session.history)
		}
		if session.state.PendingProposal != (pendingProposal{}) || session.state.LastRetryInstruction != "" || session.state.PendingIntent != "" {
			t.Fatalf("state after accepted proposal = %#v, want consumed proposal and cleared retry state", session.state)
		}
	})

	bodies := fake.requestBodies()
	if len(bodies) != 1 || !strings.Contains(bodies[0], `User instruction:\noffered objective`) || strings.Contains(bodies[0], `User instruction:\nyes`) {
		t.Fatalf("request bodies = %#v, want one resolved accepted objective", bodies)
	}
	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "pending_proposal_accepted")) != 1 {
		t.Fatalf("proposal lifecycle events = %#v, want one accepted event", events)
	}
}
