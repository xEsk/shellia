package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	llmpkg "github.com/xEsk/shellia/internal/llm"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

var errStructuralResponse = errors.New("invalid structured model response")

const maxStructuralRepairs = 3

// planningRoundRequest groups one model planning attempt and its rendering dependencies.
type planningRoundRequest struct {
	Deps                  runtimeDeps
	UI                    bool
	TurnID                string
	Round                 int
	Client                llmpkg.ClientOptions
	Prompt                llmPromptRequest
	RawPrompt             bool
	RawResponse           bool
	AllowStructuralRepair bool
	EvidenceRevision      int
}

// planningRoundResult is the normalized output of one planning attempt.
type planningRoundResult struct {
	Parsed                    llmResponse
	Summary                   string
	Plans                     []commandPlan
	StructuralRepairUsed      bool
	StructuralRepairOperation string
}

// turnRequest groups the context needed to process one user instruction.
type turnRequest struct {
	Config               config
	ContextInfo          *contextInfo
	Instruction          string
	ResolvedInstruction  string
	AcceptedProposal     bool
	AcceptedProposalMode core.ProposalMode
	History              []historyEntry
	State                sessionState
}

// turnRoundContext carries immutable dependencies shared by one planning round.
type turnRoundContext struct {
	Deps        runtimeDeps
	UI          bool
	Config      config
	ContextInfo *contextInfo
	TurnID      string
	Round       int
	TurnUI      *uipkg.Turn
}

// runTurn executes a full plan → confirm → execute → answer cycle, or stops after planning in plan-only mode.
func runTurn(ctx context.Context, deps runtimeDeps, ui bool, request turnRequest) (result turnResult, err error) {
	deps = deps.withDefaults()
	cfg := request.Config
	ctxInfo := request.ContextInfo
	instruction := request.Instruction
	history := request.History
	state := request.State
	objective := strings.TrimSpace(request.ResolvedInstruction)
	if objective == "" {
		objective = instruction
	}
	workflow := newWorkflowState(objective, cfg.PlanOnly, cfg.PlanningMaxRounds)
	workflow.retryObservationAvailable = len(state.LastObservations) > 0 &&
		strings.EqualFold(strings.TrimSpace(state.LastRetryInstruction), strings.TrimSpace(objective)) &&
		strings.EqualFold(strings.TrimSpace(state.LastObservationObjective), strings.TrimSpace(objective))
	turnID := deps.Trace.StartTurn(map[string]any{
		"instruction":        instruction,
		"resolved_objective": objective,
		"execution_allowed":  workflow.canExecute(),
		"cwd":                ctxInfo.CWD,
		"history_count":      len(history),
		"state":              state,
	})
	ctx = tracepkg.WithTurnID(ctx, turnID)
	if request.AcceptedProposal {
		deps.Trace.Record("pending_proposal_accepted", turnID, "session", -1, map[string]any{
			"proposal_mode": request.AcceptedProposalMode,
			"objective":     objective,
		})
	}
	defer func() {
		if err != nil && result.Outcome == "" {
			switch {
			case errors.Is(err, context.Canceled):
				result.Outcome = turnOutcomeCancelled
				result.BlockerKind = "cancelled"
				result.BlockerReason = "The turn was cancelled."
			case errors.Is(err, context.DeadlineExceeded):
				result.Outcome = turnOutcomeTimeout
				result.BlockerKind = "timeout"
				result.BlockerReason = "The turn timed out."
			default:
				result.Outcome = turnOutcomeStructuralError
				result.BlockerKind = "structural_error"
				result.BlockerReason = err.Error()
			}
		}
		data := map[string]any{
			"result":           result.Result,
			"outcome":          result.Outcome,
			"blocker_kind":     result.BlockerKind,
			"blocker_reason":   result.BlockerReason,
			"plans_count":      len(result.Plans),
			"executions_count": len(result.Executions),
			"skipped_count":    len(result.Skipped),
		}
		if err != nil {
			data["error"] = err.Error()
		}
		deps.Trace.Record("turn_end", turnID, "", -1, data)
	}()

	if cfg.Debug || cfg.Verbose {
		uipkg.PrintContextTo(deps.Stdout, ui, viewOptions(cfg), *ctxInfo)
	}

	if deps.Renderer == nil {
		deps.Renderer = uipkg.NewRenderer(deps.Stdout, presentation{Style: configpkg.VisualStylePlain, ANSI: ui, User: promptPresentationUser(cfg, ctxInfo.User)})
	}
	turnUI := deps.Renderer.BeginShelliaTurn(viewOptions(cfg), *ctxInfo)
	defer turnUI.Close()
	deps.Turn = turnUI
	partialResult := func() turnResult {
		return workflow.result("", "", "")
	}

	for workflow.round = 0; ; workflow.round++ {
		round := turnRoundContext{
			Deps:        deps,
			UI:          ui,
			Config:      cfg,
			ContextInfo: ctxInfo,
			TurnID:      turnID,
			Round:       workflow.round,
			TurnUI:      turnUI,
		}
		roundResult, err := runPlanningRound(ctx, buildPlanningRoundRequest(round, workflow, history, state))
		if err != nil {
			failed := partialResult()
			var refusalErr *llmpkg.ModelRefusalError
			if errors.As(err, &refusalErr) {
				reason := strings.TrimSpace(refusalErr.Reason)
				failed.Outcome = turnOutcomeBlocked
				failed.BlockerKind = "unsafe_to_continue"
				failed.BlockerReason = reason
				failed.Result = reason
				turnUI.Final(reason)
				return failed, nil
			}
			switch {
			case errors.Is(err, context.Canceled):
				failed.Outcome = turnOutcomeCancelled
				failed.BlockerKind = "cancelled"
				failed.BlockerReason = "The planning request was cancelled."
			case errors.Is(err, context.DeadlineExceeded):
				failed.Outcome = turnOutcomeTimeout
				failed.BlockerKind = "timeout"
				failed.BlockerReason = "The planning request timed out."
			case errors.Is(err, errStructuralResponse):
				failed.Outcome = turnOutcomeStructuralError
				failed.BlockerKind = "structural_error"
				failed.BlockerReason = err.Error()
			default:
				failed.Outcome = turnOutcomeBlocked
				failed.BlockerKind = "unavailable"
				failed.BlockerReason = err.Error()
			}
			return failed, err
		}

		valid, validationFailure := routePlanningDecision(round, workflow, roundResult)
		if !valid {
			if validationFailure != nil {
				return *validationFailure, nil
			}
			continue
		}

		parsed := workflow.lastDecision
		summary := roundResult.Summary
		plans := roundResult.Plans
		if parsed.Action == "retrieve_context" {
			deps.Trace.Record("context_retrieval_requested", turnID, "planning", round.Round, map[string]any{"context_refs": parsed.ContextRefs})
			if !cfg.IncludeSessionMemory {
				const reason = "Session result retrieval is unavailable because session memory is disabled."
				turnUI.Final(reason)
				blocked := workflow.result(turnOutcomeBlocked, "unavailable", reason)
				blocked.Result = reason
				return blocked, nil
			}
			uipkg.PrintInfoTo(deps.Stdout, ui, fmt.Sprintf("Retrieving %d session result(s)…", len(parsed.ContextRefs)))
			kind, reason := workflow.retrieveContext(history, parsed.ContextRefs)
			if kind != "" {
				turnUI.Final(reason)
				blocked := workflow.result(turnOutcomeBlocked, kind, reason)
				blocked.Result = reason
				return blocked, nil
			}
			deps.Trace.Record("context_revision", turnID, "planning", round.Round, map[string]any{
				"context_revision": workflow.contextRevision,
				"context_refs":     workflow.contextRefs,
				"character_count":  retrievedContextCharacterCount(workflow.retrievedContext),
			})
			continuation, keepPlanning, continuationErr := handlePlanningContinuation(round, workflow, commandBatchResult{}, "context_retrieval")
			if continuationErr != nil {
				return partialResult(), continuationErr
			}
			if !keepPlanning {
				return continuation, nil
			}
			continue
		}
		if terminal, handled := handleTerminalDecision(round, workflow, parsed, summary); handled {
			return terminal, nil
		}

		workflow.beginDecisionBatch()
		plans, rejectedPlans := workflow.admitPlans(plans)
		workflow.lastPlans = append(workflow.lastPlans[:0], plans...)
		conflictAttemptStart := len(workflow.attempts)
		workflow.recordPlanRejections(rejectedPlans)
		traceWorkflowAttempts(deps, turnID, round.Round, workflow.attempts[conflictAttemptStart:])
		for _, rejection := range rejectedPlans {
			plan := rejection.Plan
			deps.Trace.Record("repeat_admission", turnID, "planning", round.Round, map[string]any{
				"command":           plan.Command,
				"purpose":           plan.Purpose,
				"repeat_reason":     plan.RepeatReason,
				"rejection_reason":  rejection.Reason,
				"admitted":          false,
				"evidence_revision": workflow.evidenceRevision,
			})
		}
		for _, plan := range plans {
			if plan.RepeatReason == "" {
				continue
			}
			deps.Trace.Record("repeat_admission", turnID, "planning", round.Round, map[string]any{
				"command":           plan.Command,
				"purpose":           plan.Purpose,
				"repeat_reason":     plan.RepeatReason,
				"admitted":          true,
				"evidence_revision": workflow.evidenceRevision,
			})
		}
		if len(plans) == 0 && len(rejectedPlans) > 0 {
			workflow.stallCount++
			if workflow.stallCount == 1 {
				deps.Trace.Record("shellia_decision", turnID, "planning", round.Round, map[string]any{
					"decision": "repair_repetition_conflict",
				})
				continue
			}
			deps.Trace.Record("shellia_decision", turnID, "planning", round.Round, map[string]any{
				"decision": "no_progress",
			})
			noProgressReason := "Shellia could not make progress because the same successful command was proposed again without a runtime-authorized repeat cause."
			if rejectedPlans[0].Reason != repeatReasonRequired {
				noProgressReason = "Shellia could not make progress because the current observation strategy was exhausted and no different strategy was proposed."
			}
			turnUI.Final(noProgressReason)
			stalled := workflow.result(turnOutcomeNoProgress, "no_progress", noProgressReason)
			stalled.Result = noProgressReason
			return stalled, nil
		}

		turnUI.Plan(summary, plans, false)
		if parsed.Action == "plan" {
			deps.Trace.Record("shellia_decision", turnID, "planning", round.Round, map[string]any{
				"decision": "plan",
				"action":   parsed.Action,
			})
			answer := proposalAnswer(parsed.Offer.Summary, workflow.proposal)
			traceProposalCreated(round, workflow.proposal)
			turnUI.Final(answer)
			planned := workflow.result(turnOutcomePlanned, "", "")
			planned.Result = answer
			return planned, nil
		}
		if !workflow.canExecute() {
			workflow.proposal = pendingProposal{
				Mode:      core.ProposalModeExecute,
				Objective: strings.TrimSpace(workflow.objective),
				Summary:   strings.TrimSpace(summary),
			}
			deps.Trace.Record("shellia_decision", turnID, "planning", round.Round, map[string]any{
				"decision": "planned_without_execution",
			})
			answer := proposalAnswer(workflow.proposal.Summary, workflow.proposal)
			traceProposalCreated(round, workflow.proposal)
			turnUI.Final(answer)
			planned := workflow.result(turnOutcomePlanned, "", "")
			planned.Result = answer
			return planned, nil
		}

		if cfg.AskConfirmPlan {
			executePlan, err := uipkg.PromptPlanExecution(deps.Stdout, ui, deps.Stdin)
			if err != nil {
				return partialResult(), fmt.Errorf("cannot read plan confirmation: %w", err)
			}
			deps.Trace.Record("plan_confirmation", turnID, "", -1, map[string]any{
				"accepted": executePlan,
			})
			if !executePlan {
				uipkg.PrintInfoTo(deps.Stdout, ui, "Plan not executed.")
				return workflow.result(turnOutcomeDeclined, "declined", "The user declined the plan."), nil
			}
		}

		batch, err := executeDecisionBatch(ctx, round, workflow, plans)
		if errors.Is(err, core.ErrAborted) {
			aborted := partialResult()
			aborted.Outcome = turnOutcomeDeclined
			return aborted, err
		}
		if errors.Is(err, context.Canceled) {
			deps.Trace.Record("shellia_decision", turnID, "planning", round.Round, map[string]any{
				"decision": "execution_failure_replan_excluded",
				"reason":   "cancellation",
			})
			cancelled := partialResult()
			cancelled.Outcome = turnOutcomeCancelled
			return cancelled, err
		}
		var promptErr *interactivePromptError
		interactiveRepair := errors.As(err, &promptErr)
		if err != nil && !interactiveRepair {
			return partialResult(), err
		}

		if batch.HadTimeout {
			deps.Trace.Record("shellia_decision", turnID, "planning", round.Round, map[string]any{
				"decision": "timeout",
			})
			timedOut := partialResult()
			timedOut.Outcome = turnOutcomeTimeout
			timedOut.BlockerKind = "timeout"
			timedOut.BlockerReason = "A command timed out; Shellia did not retry it automatically."
			return timedOut, nil
		}
		followupTrigger := "observation"
		if interactiveRepair {
			followupTrigger = "interactive_repair"
		}
		if batch.HadOrdinaryFailure {
			followupTrigger = "execution_failure"
		}

		continuation, keepPlanning, continuationErr := handlePlanningContinuation(round, workflow, batch, followupTrigger)
		if continuationErr != nil {
			return partialResult(), continuationErr
		}
		if !keepPlanning {
			return continuation, nil
		}
	}
}

// buildPlanningRoundRequest projects the current workflow into one planning request.
func buildPlanningRoundRequest(round turnRoundContext, workflow *workflowState, history []historyEntry, state sessionState) planningRoundRequest {
	var previousDecision *llmResponse
	if workflow.lastDecision.Action != "" {
		previousDecision = &workflow.lastDecision
	}
	promptRequest := llmPromptRequest{
		Config:                    llmPromptOptions(round.Config),
		ContextInfo:               *round.ContextInfo,
		Instruction:               workflow.objective,
		History:                   history,
		ContextRevision:           workflow.contextRevision,
		RetrievedContext:          workflow.retrievedContext,
		State:                     state,
		Observations:              workflow.executions,
		Skipped:                   workflow.skipped,
		LatestBatchExecutionStart: workflow.latestBatchExecutionStart,
		LatestBatchSkippedStart:   workflow.latestBatchSkippedStart,
		PlanningRoundsRemaining:   workflow.planningBudget - round.Round,
		Operation:                 workflow.operation,
		SuccessCriteria:           workflow.successCriteria,
		DecisionError:             workflow.decisionError,
		RetryObservationAvailable: workflow.retryObservationAvailable,
		PreviousDecision:          previousDecision,
		Attempts:                  workflow.attempts,
	}
	return planningRoundRequest{
		Deps:                  round.Deps,
		UI:                    round.UI,
		TurnID:                round.TurnID,
		Round:                 round.Round,
		Client:                llmClientOptions(round.Config),
		Prompt:                promptRequest,
		RawPrompt:             round.Config.RawPrompt,
		RawResponse:           round.Config.RawResponse,
		AllowStructuralRepair: workflow.structuralRepairsUsed < maxStructuralRepairs,
		EvidenceRevision:      workflow.evidenceRevision,
	}
}

// routePlanningDecision admits one coherent decision or routes semantic repair.
func routePlanningDecision(round turnRoundContext, workflow *workflowState, result planningRoundResult) (bool, *turnResult) {
	if result.StructuralRepairUsed {
		workflow.structuralRepairsUsed++
	}
	if result.StructuralRepairOperation != "" && workflow.repairOperation == "" {
		workflow.repairOperation = result.StructuralRepairOperation
	}
	parsed := result.Parsed
	parsed.SuccessCriteria = workflow.successCriteria
	if decisionErr := workflow.validateDecision(parsed); decisionErr != nil {
		workflow.recordDecision(parsed, result.Summary, result.Plans)
		workflow.decisionError = decisionErr.Error()
		traceCompletionValidation(round, workflow, parsed, false, decisionErr.Error())
		if workflow.semanticRepairUsed {
			failed := workflow.result(turnOutcomeStructuralError, "structural_error", decisionErr.Error())
			validationFailure := "Shellia could not validate the model's final response. The observed command output remains available above; retry the request if needed."
			if len(workflow.executions) == 0 {
				validationFailure = "Shellia could not validate the model's final response. No commands were executed; retry the request if needed."
			}
			failed.Result = validationFailure
			round.TurnUI.Final(validationFailure)
			return false, &failed
		}
		workflow.semanticRepairUsed = true
		return false, nil
	}
	workflow.decisionError = ""
	workflow.recordDecision(parsed, result.Summary, result.Plans)
	workflow.proposal = pendingProposal{
		Mode:      core.ProposalMode(parsed.Offer.Mode),
		Objective: strings.TrimSpace(parsed.Offer.Objective),
		Summary:   strings.TrimSpace(parsed.Offer.Summary),
	}
	if workflow.contractLocked {
		round.Deps.Trace.Record("objective_contract", round.TurnID, "planning", round.Round, map[string]any{
			"operation":        workflow.operation,
			"success_criteria": workflow.successCriteria,
		})
	}
	return true, nil
}

// handleTerminalDecision renders and returns complete or blocked decisions.
func handleTerminalDecision(round turnRoundContext, workflow *workflowState, parsed llmResponse, summary string) (turnResult, bool) {
	switch parsed.Action {
	case "complete":
		traceCompletionValidation(round, workflow, parsed, true, "")
		round.Deps.Trace.Record("shellia_decision", round.TurnID, "planning", round.Round, map[string]any{
			"decision":         "complete",
			"completion_basis": workflow.completionEvidence.Source,
		})
		answer := summary
		if workflow.proposal.Mode != "" {
			traceProposalCreated(round, workflow.proposal)
			answer = proposalAnswer(answer, workflow.proposal)
		}
		round.TurnUI.Final(answer)
		completed := workflow.result(turnOutcomeCompleted, "", "")
		completed.Result = answer
		return completed, true
	case "blocked":
		round.Deps.Trace.Record("shellia_decision", round.TurnID, "planning", round.Round, map[string]any{
			"decision":       "blocked",
			"blocker_kind":   parsed.BlockerKind,
			"blocker_reason": parsed.BlockerReason,
		})
		answer := strings.TrimSpace(summary)
		if answer == "" {
			answer = parsed.BlockerReason
		} else if reason := strings.TrimSpace(parsed.BlockerReason); reason != "" && reason != answer {
			answer += "\n" + reason
		}
		round.TurnUI.Final(answer)
		blocked := workflow.result(turnOutcomeBlocked, parsed.BlockerKind, parsed.BlockerReason)
		blocked.Result = answer
		return blocked, true
	default:
		return turnResult{}, false
	}
}

// proposalAnswer makes a typed proposal and its exact next authority visible.
func proposalAnswer(answer string, proposal pendingProposal) string {
	answer = strings.TrimSpace(answer)
	summary := strings.TrimSpace(proposal.Summary)
	if summary != "" && summary != answer {
		answer += "\n\nProposta: " + summary
	}
	switch proposal.Mode {
	case core.ProposalModePlan:
		return answer + "\n\nWould you like me to prepare an executable plan?"
	case core.ProposalModeExecute:
		return answer + "\n\nWould you like me to execute it?"
	default:
		return answer
	}
}

// traceProposalCreated records proposal authority without treating it as execution authority.
func traceProposalCreated(round turnRoundContext, proposal pendingProposal) {
	round.Deps.Trace.Record("pending_proposal_created", round.TurnID, "session", -1, map[string]any{
		"proposal_mode": proposal.Mode,
		"objective":     proposal.Objective,
		"summary":       proposal.Summary,
	})
}

// traceCompletionValidation records provenance derived from runtime-owned workflow state.
func traceCompletionValidation(round turnRoundContext, workflow *workflowState, parsed llmResponse, admitted bool, reason string) {
	evidence := workflow.completionEvidence
	data := map[string]any{
		"operation":         parsed.Operation,
		"evidence_source":   evidence.Source,
		"freshness":         evidence.Freshness,
		"success_criteria":  parsed.SuccessCriteria,
		"basis_source":      evidence.Source,
		"basis_freshness":   evidence.Freshness,
		"context_revision":  evidence.ContextRevision,
		"context_refs":      evidence.ContextRefs,
		"evidence_revision": evidence.EvidenceRevision,
		"attempt_ids":       evidence.AttemptIDs,
		"admitted":          admitted,
	}
	if strings.TrimSpace(reason) != "" {
		data["reason"] = reason
	}
	round.Deps.Trace.Record("completion_validation", round.TurnID, "planning", round.Round, data)
}

// executeDecisionBatch executes admitted plans and records their causal evidence.
func executeDecisionBatch(ctx context.Context, round turnRoundContext, workflow *workflowState, plans []commandPlan) (commandBatchResult, error) {
	attemptStart := len(workflow.attempts)
	evidenceBefore := workflow.evidenceRevision
	batch, err := round.Deps.ExecuteCommands(ctx, round.Deps, round.UI, executorOptions(round.Config), round.ContextInfo, plans, workflow.executions)
	workflow.recordBatch(plans, batch)
	traceWorkflowAttempts(round.Deps, round.TurnID, round.Round, workflow.attempts[attemptStart:])
	if workflow.evidenceRevision != evidenceBefore {
		round.Deps.Trace.Record("evidence_revision", round.TurnID, "execution", round.Round, map[string]any{
			"before": evidenceBefore,
			"after":  workflow.evidenceRevision,
		})
	}
	return batch, err
}

// handlePlanningContinuation routes the normal and planning-limit follow-up paths.
func handlePlanningContinuation(round turnRoundContext, workflow *workflowState, batch commandBatchResult, followupTrigger string) (turnResult, bool, error) {
	if workflow.planningLimitReached(round.Round) {
		keepGoing, err := uipkg.PromptPlanningLimitContinuation(round.Deps.Stdout, round.UI, round.Deps.Stdin, workflow.planningBudget)
		if err != nil {
			return turnResult{}, false, err
		}
		round.Deps.Trace.Record("shellia_decision", round.TurnID, "planning", round.Round, map[string]any{
			"decision": "planning_limit_continuation",
			"accepted": keepGoing,
			"limit":    workflow.planningBudget,
			"trigger":  followupTrigger,
		})
		if keepGoing {
			workflow.extendPlanningBudget(round.Config.PlanningMaxRounds)
			if batch.HadOrdinaryFailure {
				round.Deps.Trace.Record("shellia_decision", round.TurnID, "planning", round.Round, map[string]any{
					"decision": "continue_after_execution_failure",
				})
			}
			return turnResult{}, true, nil
		}
		limited := workflow.result(turnOutcomePlanningLimit, "planning_limit", "The planning limit was reached and continuation was declined.")
		return limited, false, nil
	}
	decision := "continue_after_observation"
	if batch.HadOrdinaryFailure {
		decision = "continue_after_execution_failure"
	}
	round.Deps.Trace.Record("shellia_decision", round.TurnID, "planning", round.Round, map[string]any{
		"decision": decision,
	})
	return turnResult{}, true, nil
}

// runPlanningRound asks the model for one workflow decision and repairs one malformed response.
func runPlanningRound(ctx context.Context, request planningRoundRequest) (planningRoundResult, error) {
	promptOptions := request.Prompt.Config
	clientOptions := request.Client
	deps := request.Deps
	ui := request.UI

	systemPrompt, userPrompt := llmpkg.BuildPrompts(request.Prompt)
	if request.RawPrompt {
		uipkg.PrintRawPromptsTo(deps.Stdout, ui, "Raw LLM prompt", systemPrompt, userPrompt)
	}

	deps.Trace.Record("llm_prompt", request.TurnID, "planning", request.Round, map[string]any{
		"model":         clientOptions.Model,
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
	})
	deps.Trace.Record("evidence_projection", request.TurnID, "planning", request.Round, map[string]any{
		"evidence_revision": request.EvidenceRevision,
		"executions_count":  len(request.Prompt.Observations),
		"skipped_count":     len(request.Prompt.Skipped),
		"output_budget":     promptOptions.ObservationOutputChars,
		"omitted": strings.Contains(userPrompt, "[older evidence omitted:") ||
			strings.Contains(userPrompt, "[older attempts omitted:") ||
			strings.Contains(userPrompt, "[omitted by configuration]"),
	})

	thinkingPrefix := ""
	if deps.Turn != nil {
		thinkingPrefix = deps.Turn.ThinkingPrefix()
	}
	thinking := uipkg.StartThinkingIndicator(ui, deps.Stdout, thinkingPrefix)
	rawResponse, err := llmpkg.CallPlanningPrompt(ctx, deps.HTTPClient, clientOptions, systemPrompt, userPrompt)
	if thinking != nil {
		thinking.Stop()
	}
	if err != nil {
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error": err.Error(),
		})
		return planningRoundResult{}, err
	}
	deps.Trace.Record("llm_response", request.TurnID, "planning", request.Round, map[string]any{
		"raw_response": rawResponse,
	})

	if request.RawResponse {
		uipkg.PrintSectionTo(deps.Stdout, ui, "Raw LLM response", uipkg.ColorBlue)
		fmt.Fprintln(deps.Stdout, rawResponse)
		fmt.Fprintln(deps.Stdout)
	}

	responseMode := llmpkg.ResponseModeCompatible
	if clientOptions.SupportsResponseFormat || clientOptions.SupportsJSONSchema {
		responseMode = llmpkg.ResponseModeStrict
	}
	parsed, err := llmpkg.ParseResponse(rawResponse, responseMode)
	structuralRepairUsed := false
	structuralRepairOperation := ""
	if err != nil {
		var validationErr *llmpkg.ResponseValidationError
		if errors.As(err, &validationErr) {
			switch validationErr.Response.Operation {
			case "answer", "observe", "act", "capability":
				structuralRepairOperation = validationErr.Response.Operation
			}
		}
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error":        err.Error(),
			"raw_response": rawResponse,
		})
		if !request.AllowStructuralRepair {
			return planningRoundResult{}, fmt.Errorf("%w: %w", errStructuralResponse, err)
		}
		structuralRepairUsed = true
		repairPrompt := userPrompt + "\n\nThe previous response was structurally invalid: " + err.Error() + "\nReturn exactly one valid JSON decision using the required schema."
		deps.Trace.Record("llm_prompt", request.TurnID, "structural_repair", request.Round, map[string]any{
			"model":         clientOptions.Model,
			"system_prompt": systemPrompt,
			"user_prompt":   repairPrompt,
		})
		repairedRaw, repairErr := llmpkg.CallPlanningPrompt(ctx, deps.HTTPClient, clientOptions, systemPrompt, repairPrompt)
		if repairErr != nil {
			return planningRoundResult{}, repairErr
		}
		deps.Trace.Record("llm_response", request.TurnID, "structural_repair", request.Round, map[string]any{
			"raw_response": repairedRaw,
		})
		parsed, err = llmpkg.ParseResponse(repairedRaw, responseMode)
		if err != nil {
			return planningRoundResult{}, fmt.Errorf("%w: %w", errStructuralResponse, err)
		}
	}

	summary, plans, err := llmpkg.NormalizePlan(parsed)
	if err != nil {
		deps.Trace.Record("llm_error", request.TurnID, "planning", request.Round, map[string]any{
			"error":  err.Error(),
			"parsed": parsed,
		})
		return planningRoundResult{}, err
	}
	deps.Trace.Record("planner_result", request.TurnID, "planning", request.Round, map[string]any{
		"action":         parsed.Action,
		"operation":      parsed.Operation,
		"summary":        summary,
		"blocker_kind":   parsed.BlockerKind,
		"blocker_reason": parsed.BlockerReason,
		"commands":       tracePlannerCommands(plans),
		"commands_count": len(plans),
	})

	return planningRoundResult{
		Parsed:                    parsed,
		Summary:                   summary,
		Plans:                     plans,
		StructuralRepairUsed:      structuralRepairUsed,
		StructuralRepairOperation: structuralRepairOperation,
	}, nil
}

// tracePlannerCommands renders normalized command plans with stable trace field names.
func tracePlannerCommands(plans []commandPlan) []map[string]any {
	commands := make([]map[string]any, 0, len(plans))
	for _, plan := range plans {
		commands = append(commands, map[string]any{
			"command":                plan.Command,
			"purpose":                plan.Purpose,
			"risk":                   plan.Risk,
			"requires_confirmation":  plan.RequiresConfirmation,
			"classification":         plan.Classification,
			"local_safe":             plan.LocalSafe,
			"independent_on_failure": plan.IndependentOnFailure,
			"repeat_reason":          plan.RepeatReason,
			"interactive":            plan.Interactive,
			"interactive_reason":     plan.InteractiveReason,
		})
	}
	return commands
}

// traceWorkflowAttempts records causal command outcomes without duplicating captured output.
func traceWorkflowAttempts(deps runtimeDeps, turnID string, round int, attempts []workflowAttempt) {
	for _, attempt := range attempts {
		deps.Trace.Record("workflow_attempt", turnID, "execution", round, map[string]any{
			"attempt_id":         attempt.ID,
			"planned_command":    attempt.PlannedCommand,
			"effective_command":  attempt.EffectiveCommand,
			"purpose":            attempt.Purpose,
			"outcome":            attempt.Outcome,
			"exit_code":          attempt.ExitCode,
			"repeat_reason":      attempt.RepeatReason,
			"related_attempt_id": attempt.RelatedAttemptID,
			"evidence_before":    attempt.EvidenceBefore,
			"evidence_after":     attempt.EvidenceAfter,
		})
	}
}
