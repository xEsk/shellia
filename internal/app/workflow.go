package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xEsk/shellia/internal/core"
	safetypkg "github.com/xEsk/shellia/internal/safety"
)

const maxObservationStrategyAttempts = 3

// workflowState owns the mutable state of one goal-oriented turn.
// executionAllowed is derived once at construction and intentionally has no setter.
type workflowState struct {
	objective                 string
	executionAllowed          bool
	operation                 string
	repairOperation           string
	successCriteria           string
	contractLocked            bool
	round                     int
	planningBudget            int
	evidenceRevision          int
	contextRevision           int
	contextRefs               []string
	retrievedContext          []historyEntry
	attempts                  []workflowAttempt
	completionEvidence        completionEvidence
	lastDecision              llmResponse
	stallCount                int
	structuralRepairsUsed     int
	semanticRepairUsed        bool
	decisionError             string
	retryObservationAvailable bool
	outcome                   turnOutcome
	blockerKind               string
	blockerReason             string
	lastSummary               string
	proposal                  pendingProposal
	lastPlans                 []commandPlan
	executions                []commandExecution
	skipped                   []skippedCommand
	latestBatchExecutionStart int
	latestBatchSkippedStart   int
}

// completionEvidence is the runtime-owned provenance for one admitted completion.
type completionEvidence struct {
	Source           string
	Freshness        string
	ContextRevision  int
	ContextRefs      []string
	EvidenceRevision int
	AttemptIDs       []int
}

// planRejection retains a rejected plan and the runtime-owned reason.
type planRejection struct {
	Plan   commandPlan
	Reason string
}

// validateDecision locks the first coherent operation and rejects terminal
// decisions that lack causal evidence for that runtime-owned contract.
func (state *workflowState) validateDecision(decision llmResponse) error {
	state.completionEvidence = completionEvidence{
		ContextRefs: make([]string, 0),
		AttemptIDs:  make([]int, 0),
	}
	if state.contractLocked {
		if decision.Operation != state.operation {
			return fmt.Errorf("decision contract changed: operation=%q", decision.Operation)
		}
	} else if state.repairOperation != "" && isExecutableOperation(state.repairOperation) != isExecutableOperation(decision.Operation) {
		return fmt.Errorf("semantic repair cannot change authority group from operation %q to %q", state.repairOperation, decision.Operation)
	}

	if err := state.validateDecisionMatrix(decision); err != nil {
		if state.repairOperation == "" {
			state.repairOperation = decision.Operation
		}
		return err
	}

	if !state.contractLocked {
		state.operation = decision.Operation
		state.contractLocked = true
	}
	return nil
}

func isExecutableOperation(operation string) bool {
	return operation == "act" || operation == "observe"
}

// validateDecisionMatrix enforces the closed operation and action matrix.
func (state *workflowState) validateDecisionMatrix(decision llmResponse) error {
	allowed := false
	switch decision.Operation {
	case "answer":
		allowed = decision.Action == "retrieve_context" || decision.Action == "complete" || decision.Action == "blocked"
	case "observe", "act":
		allowed = decision.Action == "execute" || decision.Action == "complete" || decision.Action == "blocked"
	case "capability":
		allowed = decision.Action == "complete" || decision.Action == "blocked"
	}
	if !allowed {
		return fmt.Errorf("decision is not allowed for operation=%q action=%q", decision.Operation, decision.Action)
	}
	if decision.Action == "complete" {
		evidence, err := state.resolveCompletionEvidence(decision.Operation)
		state.completionEvidence = evidence
		return err
	}
	return nil
}

// resolveCompletionEvidence derives causal provenance without interpreting command output.
func (state *workflowState) resolveCompletionEvidence(operation string) (completionEvidence, error) {
	evidence := completionEvidence{
		ContextRefs: make([]string, 0),
		AttemptIDs:  make([]int, 0),
	}
	switch operation {
	case "answer":
		if state.contextRevision == 0 {
			evidence.Source = "model_knowledge"
			evidence.Freshness = "not_applicable"
			return evidence, nil
		}
		evidence.Source = "session_result"
		evidence.Freshness = "snapshot"
		evidence.ContextRevision = state.contextRevision
		evidence.ContextRefs = append(evidence.ContextRefs, state.contextRefs...)
		if err := state.validateContextReferences(state.contextRevision, state.contextRefs); err != nil {
			return evidence, err
		}
		return evidence, nil
	case "capability":
		evidence.Source = "model_knowledge"
		evidence.Freshness = "not_applicable"
		return evidence, nil
	case "observe":
		evidence.Source = "current_observation"
		evidence.Freshness = "current"
		evidence.EvidenceRevision = state.evidenceRevision
		for _, attempt := range state.attempts {
			if hasObservedEvidence(attempt.Outcome) {
				evidence.AttemptIDs = append(evidence.AttemptIDs, attempt.ID)
			}
		}
		if len(evidence.AttemptIDs) > 0 {
			return evidence, nil
		}
		if len(state.attempts) == 0 && state.retryObservationAvailable {
			evidence.Source = "retry_observation"
			evidence.EvidenceRevision = 0
			return evidence, nil
		}
		return evidence, fmt.Errorf("current observation completion requires observed workflow evidence")
	case "act":
		evidence.Source = "current_execution"
		evidence.Freshness = "current"
		evidence.EvidenceRevision = state.evidenceRevision
		for _, attempt := range state.attempts {
			if attempt.EvidenceAfter == state.evidenceRevision && attempt.Outcome == "success" {
				evidence.AttemptIDs = append(evidence.AttemptIDs, attempt.ID)
			}
		}
		if state.evidenceRevision > 0 && len(evidence.AttemptIDs) > 0 {
			return evidence, nil
		}
		return evidence, fmt.Errorf("action completion requires a successful execution in the latest workflow batch")
	default:
		return evidence, fmt.Errorf("unknown completion operation %q", operation)
	}
}

// hasObservedEvidence reports outcomes that produced stdout, stderr, or an exit status.
func hasObservedEvidence(outcome string) bool {
	return outcome == "success" || outcome == "failed" || outcome == "timeout"
}

// newWorkflowState creates the single state owner for a turn.
func newWorkflowState(objective string, planOnly bool, planningBudget int) *workflowState {
	if planningBudget < 1 {
		planningBudget = 1
	}
	return &workflowState{
		objective:        strings.TrimSpace(objective),
		successCriteria:  strings.TrimSpace(objective),
		executionAllowed: !planOnly,
		planningBudget:   planningBudget,
		executions:       make([]commandExecution, 0, 4),
		skipped:          make([]skippedCommand, 0, 4),
		attempts:         make([]workflowAttempt, 0, 4),
	}
}

// canExecute reports the immutable authority established for this turn.
func (state *workflowState) canExecute() bool {
	return state != nil && state.executionAllowed
}

// recordDecision retains the latest structured decision and its normalized plan.
func (state *workflowState) recordDecision(decision llmResponse, summary string, plans []commandPlan) {
	state.lastDecision = decision
	state.lastSummary = strings.TrimSpace(summary)
	state.lastPlans = append(state.lastPlans[:0], plans...)
}

// beginDecisionBatch marks the shared projection boundary for every outcome of one execute decision.
func (state *workflowState) beginDecisionBatch() {
	state.latestBatchExecutionStart = len(state.executions)
	state.latestBatchSkippedStart = len(state.skipped)
}

// recordBatch adds real execution outcomes and advances the evidence revision once per batch.
func (state *workflowState) recordBatch(plans []commandPlan, batch commandBatchResult) {
	before := state.evidenceRevision
	if len(batch.Executions) > 0 || len(batch.Skipped) > 0 {
		state.evidenceRevision++
	}
	after := state.evidenceRevision

	for _, execution := range batch.Executions {
		plan := matchingPlan(plans, execution.Command, execution.Purpose)
		outcome := "success"
		if execution.TimedOut {
			outcome = "timeout"
		} else if execution.ExitCode != 0 {
			outcome = "failed"
		}
		state.attempts = append(state.attempts, workflowAttempt{
			ID:               len(state.attempts) + 1,
			Round:            state.round,
			PlannedCommand:   plan.Command,
			EffectiveCommand: execution.Command,
			Purpose:          execution.Purpose,
			Outcome:          outcome,
			ExitCode:         execution.ExitCode,
			EvidenceBefore:   before,
			EvidenceAfter:    after,
			RepeatReason:     plan.RepeatReason,
			RelatedAttemptID: state.latestAttemptID(execution.Command),
		})
	}
	for _, skipped := range batch.Skipped {
		plan := matchingPlan(plans, skipped.Command, skipped.Purpose)
		state.attempts = append(state.attempts, workflowAttempt{
			ID:               len(state.attempts) + 1,
			Round:            state.round,
			PlannedCommand:   plan.Command,
			EffectiveCommand: skipped.Command,
			Purpose:          skipped.Purpose,
			Outcome:          "skipped",
			EvidenceBefore:   before,
			EvidenceAfter:    after,
			RepeatReason:     plan.RepeatReason,
			RelatedAttemptID: state.latestAttemptID(skipped.Command),
		})
	}

	state.executions = append(state.executions, batch.Executions...)
	state.skipped = append(state.skipped, batch.Skipped...)
	if after > before {
		state.stallCount = 0
	}
}

// matchingPlan finds the plan metadata for an effective execution or skip.
func matchingPlan(plans []commandPlan, command string, purpose string) commandPlan {
	for _, plan := range plans {
		if strings.TrimSpace(plan.Command) == strings.TrimSpace(command) {
			return plan
		}
	}
	for _, plan := range plans {
		if plan.Purpose == purpose {
			return plan
		}
	}
	return commandPlan{Command: command, Purpose: purpose}
}

// latestAttemptID returns the most recent causal attempt for an effective command.
func (state *workflowState) latestAttemptID(command string) int {
	identity := strings.TrimSpace(command)
	for index := len(state.attempts) - 1; index >= 0; index-- {
		if strings.TrimSpace(state.attempts[index].EffectiveCommand) == identity {
			return state.attempts[index].ID
		}
	}
	return 0
}

// admitPlans rejects unexplained duplicate successes and exhausted observation strategies.
func (state *workflowState) admitPlans(plans []commandPlan) ([]commandPlan, []planRejection) {
	succeeded := make(map[string]bool, len(state.executions))
	for _, execution := range state.executions {
		identity := strings.TrimSpace(execution.Command)
		if execution.ExitCode == 0 && identity != "" {
			succeeded[identity] = true
		}
	}

	admitted := make([]commandPlan, 0, len(plans))
	rejected := make([]planRejection, 0)
	strategyAttempts := state.observationStrategyAttempts()
	authorizedRepeats := make(map[string]bool)
	for _, plan := range plans {
		strategy := observationStrategyKey(plan.Command)
		reason := ""
		if state.operation == "observe" && strategy != "" && strategyAttempts[strategy] >= maxObservationStrategyAttempts {
			reason = fmt.Sprintf("Observation strategy %q is exhausted after %d attempts; choose a different strategy or complete with the available evidence.", strategy, maxObservationStrategyAttempts)
		} else if identity := strings.TrimSpace(plan.Command); succeeded[identity] {
			if !authorizedRepeats[identity] && state.canAuthorizeRepeat(plan) {
				plan.AuthorizedRepeatCommand = identity
				authorizedRepeats[identity] = true
			} else {
				reason = repeatReasonRequired
			}
		}
		if reason != "" {
			rejected = append(rejected, planRejection{Plan: plan, Reason: reason})
			continue
		}
		admitted = append(admitted, plan)
		if state.operation == "observe" && strategy != "" {
			strategyAttempts[strategy]++
		}
	}
	return admitted, rejected
}

// canAuthorizeRepeat validates a model-proposed retry or verification against
// runtime attempt order instead of trusting repeat_reason as authority.
func (state *workflowState) canAuthorizeRepeat(plan commandPlan) bool {
	identity := strings.TrimSpace(plan.Command)
	latest := -1
	for index, attempt := range state.attempts {
		if strings.TrimSpace(attempt.EffectiveCommand) == identity {
			latest = index
		}
	}
	if latest < 0 {
		return false
	}
	if plan.RepeatReason == core.RepeatReasonRetry {
		return state.attempts[latest].Outcome == "failed" || state.attempts[latest].Outcome == "timeout"
	}
	if state.operation != "act" || plan.RepeatReason != core.RepeatReasonVerifyAfterChange || state.attempts[latest].Outcome != "success" {
		return false
	}
	for _, attempt := range state.attempts[latest+1:] {
		command := strings.TrimSpace(attempt.EffectiveCommand)
		if attempt.Outcome == "success" && command != "" && command != identity {
			return true
		}
	}
	return false
}

// observationStrategyAttempts counts evidence-producing attempts by primary executable.
func (state *workflowState) observationStrategyAttempts() map[string]int {
	attempts := make(map[string]int)
	for _, attempt := range state.attempts {
		if !hasObservedEvidence(attempt.Outcome) {
			continue
		}
		if strategy := observationStrategyKey(attempt.EffectiveCommand); strategy != "" {
			attempts[strategy]++
		}
	}
	return attempts
}

// observationStrategyKey identifies the primary evidence source before formatters in a pipeline.
func observationStrategyKey(command string) string {
	root := safetypkg.PrimaryCommandRoot(command)
	return strings.ToLower(filepath.Base(strings.TrimSpace(root)))
}

// recordPlanRejections retains rejected proposals as causal evidence without advancing evidenceRevision.
func (state *workflowState) recordPlanRejections(rejections []planRejection) {
	for _, rejection := range rejections {
		plan := rejection.Plan
		state.skipped = append(state.skipped, skippedCommand{
			Command: plan.Command,
			Purpose: plan.Purpose,
			Reason:  rejection.Reason,
		})
		state.attempts = append(state.attempts, workflowAttempt{
			ID:               len(state.attempts) + 1,
			Round:            state.round,
			PlannedCommand:   plan.Command,
			EffectiveCommand: plan.Command,
			Purpose:          plan.Purpose,
			Outcome:          "rejected",
			EvidenceBefore:   state.evidenceRevision,
			EvidenceAfter:    state.evidenceRevision,
			RepeatReason:     plan.RepeatReason,
		})
	}
}

// planningLimitReached reports whether another decision requires an explicit budget extension.
func (state *workflowState) planningLimitReached(round int) bool {
	return round >= state.planningBudget-1
}

// extendPlanningBudget adds a bounded number of rounds after user confirmation.
func (state *workflowState) extendPlanningBudget(rounds int) {
	if rounds > 0 {
		state.planningBudget += rounds
	}
}

// result projects the workflow state into the shared terminal turn result.
func (state *workflowState) result(outcome turnOutcome, blockerKind string, blockerReason string) turnResult {
	state.outcome = outcome
	state.blockerKind = strings.TrimSpace(blockerKind)
	state.blockerReason = strings.TrimSpace(blockerReason)
	return turnResult{
		Result:        strings.TrimSpace(state.lastSummary),
		Summary:       state.lastSummary,
		Outcome:       state.outcome,
		BlockerKind:   state.blockerKind,
		BlockerReason: state.blockerReason,
		Plans:         append([]commandPlan(nil), state.lastPlans...),
		Executions:    append([]commandExecution(nil), state.executions...),
		Skipped:       append([]skippedCommand(nil), state.skipped...),
		Proposal:      state.proposal,
	}
}
