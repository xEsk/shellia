package app

import (
	"fmt"
	"strings"
)

// workflowState owns the mutable state of one goal-oriented turn.
// executionAllowed is derived once at construction and intentionally has no setter.
type workflowState struct {
	objective                 string
	executionAllowed          bool
	operation                 string
	evidenceSource            string
	freshness                 string
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
	lastDecision              llmResponse
	stallCount                int
	structuralRepairUsed      bool
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

// validateDecision locks the first coherent decision contract and rejects
// terminal decisions that lack causal evidence for the selected operation.
func (state *workflowState) validateDecision(decision llmResponse) error {
	if state.contractLocked {
		if decision.Operation != state.operation || decision.EvidenceSource != state.evidenceSource || decision.Freshness != state.freshness || decision.SuccessCriteria != state.successCriteria {
			return fmt.Errorf("decision contract changed: operation=%q evidence_source=%q freshness=%q criteria=%q", decision.Operation, decision.EvidenceSource, decision.Freshness, decision.SuccessCriteria)
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
		state.evidenceSource = decision.EvidenceSource
		state.freshness = decision.Freshness
		state.successCriteria = decision.SuccessCriteria
		state.contractLocked = true
	}
	return nil
}

func isExecutableOperation(operation string) bool {
	return operation == "act" || operation == "observe"
}

// validateDecisionMatrix enforces the closed operation, source, freshness, and action matrix.
func (state *workflowState) validateDecisionMatrix(decision llmResponse) error {
	allowed := false
	switch {
	case decision.Operation == "answer" && decision.EvidenceSource == "model_knowledge" && decision.Freshness == "not_applicable":
		allowed = decision.Action == "complete" || decision.Action == "blocked"
	case decision.Operation == "answer" && decision.EvidenceSource == "session_result" && decision.Freshness == "snapshot":
		allowed = decision.Action == "retrieve_context" || decision.Action == "complete" || decision.Action == "blocked"
	case decision.Operation == "observe" && decision.EvidenceSource == "current_observation" && decision.Freshness == "current":
		allowed = decision.Action == "execute" || decision.Action == "complete" || decision.Action == "blocked"
	case decision.Operation == "observe" && decision.EvidenceSource == "retry_observation" && decision.Freshness == "current":
		if !state.retryObservationAvailable {
			return fmt.Errorf("retry_observation is not eligible for this objective")
		}
		allowed = decision.Action == "execute" || decision.Action == "complete" || decision.Action == "blocked"
	case decision.Operation == "act" && decision.EvidenceSource == "current_execution" && decision.Freshness == "current":
		allowed = decision.Action == "execute" || decision.Action == "complete" || decision.Action == "blocked"
	case decision.Operation == "act" && decision.EvidenceSource == "current_observation" && decision.Freshness == "current":
		allowed = decision.Action == "execute" || decision.Action == "complete" || decision.Action == "blocked"
	case decision.Operation == "capability" && decision.EvidenceSource == "model_knowledge" && decision.Freshness == "not_applicable":
		allowed = decision.Action == "complete" || decision.Action == "blocked"
	}
	if !allowed {
		return fmt.Errorf("decision is not allowed for operation=%q evidence_source=%q freshness=%q action=%q", decision.Operation, decision.EvidenceSource, decision.Freshness, decision.Action)
	}
	if decision.Action == "complete" {
		return state.validateCompletion(decision)
	}
	return nil
}

// validateCompletion checks evidence provenance without interpreting arbitrary command output.
func (state *workflowState) validateCompletion(decision llmResponse) error {
	basis := decision.CompletionBasis
	switch decision.EvidenceSource {
	case "model_knowledge":
		return nil
	case "session_result":
		return state.validateContextReferences(basis.ContextRevision, decision.ContextRefs)
	case "retry_observation":
		if state.retryObservationAvailable {
			return nil
		}
		return fmt.Errorf("retry_observation is not eligible for this objective")
	case "current_observation":
		return state.validateEvidenceReferences(basis.EvidenceRevision, basis.AttemptIDs, false)
	case "current_execution":
		return state.validateEvidenceReferences(basis.EvidenceRevision, basis.AttemptIDs, true)
	default:
		return fmt.Errorf("unknown completion evidence source %q", decision.EvidenceSource)
	}
}

// validateEvidenceReferences ensures the model cites evidence produced by this workflow.
func (state *workflowState) validateEvidenceReferences(revision int, attemptIDs []int, requireSuccess bool) error {
	if revision < 1 || revision > state.evidenceRevision {
		return fmt.Errorf("completion evidence revision %d is outside current workflow revisions 1..%d", revision, state.evidenceRevision)
	}
	if len(attemptIDs) == 0 {
		return fmt.Errorf("completion evidence must reference at least one current attempt")
	}
	for _, attemptID := range attemptIDs {
		if attemptID < 1 || attemptID > len(state.attempts) {
			return fmt.Errorf("completion references unknown attempt %d", attemptID)
		}
		attempt := state.attempts[attemptID-1]
		if attempt.ID != attemptID || attempt.EvidenceAfter != revision {
			return fmt.Errorf("attempt %d does not belong to evidence revision %d", attemptID, revision)
		}
		if attempt.Outcome == "skipped" || attempt.Outcome == "rejected" || attempt.Outcome == "declined" || attempt.Outcome == "cancelled" {
			return fmt.Errorf("attempt %d has no observed execution evidence", attemptID)
		}
		if requireSuccess && attempt.Outcome != "success" {
			return fmt.Errorf("attempt %d did not complete successfully", attemptID)
		}
	}
	return nil
}

// newWorkflowState creates the single state owner for a turn.
func newWorkflowState(objective string, planOnly bool, planningBudget int) *workflowState {
	if planningBudget < 1 {
		planningBudget = 1
	}
	return &workflowState{
		objective:        strings.TrimSpace(objective),
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

// admitPlans rejects only exact prior successes that lack a recognized causal repeat reason.
func (state *workflowState) admitPlans(plans []commandPlan) ([]commandPlan, []commandPlan) {
	succeeded := make(map[string]bool, len(state.executions))
	for _, execution := range state.executions {
		identity := strings.TrimSpace(execution.Command)
		if execution.ExitCode == 0 && identity != "" {
			succeeded[identity] = true
		}
	}

	admitted := make([]commandPlan, 0, len(plans))
	rejected := make([]commandPlan, 0)
	for _, plan := range plans {
		if succeeded[strings.TrimSpace(plan.Command)] && !plan.RepeatReason.AllowsSuccessfulRepeat() {
			rejected = append(rejected, plan)
			continue
		}
		admitted = append(admitted, plan)
	}
	return admitted, rejected
}

// recordRepetitionConflicts retains rejected proposals as causal evidence without advancing evidenceRevision.
func (state *workflowState) recordRepetitionConflicts(plans []commandPlan) {
	for _, plan := range plans {
		state.skipped = append(state.skipped, skippedCommand{
			Command: plan.Command,
			Purpose: plan.Purpose,
			Reason:  repeatReasonRequired,
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
