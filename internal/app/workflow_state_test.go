package app

import (
	"testing"

	"github.com/xEsk/shellia/internal/core"
)

// TestWorkflowStateKeepsExecutionAuthorityImmutable checks decisions cannot grant authority to /plan.
func TestWorkflowStateKeepsExecutionAuthorityImmutable(t *testing.T) {
	tests := []struct {
		name           string
		planOnly       bool
		wantCanExecute bool
	}{
		{name: "normal", planOnly: false, wantCanExecute: true},
		{name: "plan only", planOnly: true, wantCanExecute: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newWorkflowState("inspect disk", tt.planOnly, 4)
			state.recordDecision(llmResponse{Action: "execute", Summary: "Run df."}, "Run df.", []commandPlan{{Command: "df -h", Purpose: "Inspect disk"}})

			if state.canExecute() != tt.wantCanExecute {
				t.Fatalf("canExecute() = %t, want %t", state.canExecute(), tt.wantCanExecute)
			}
			if state.objective != "inspect disk" {
				t.Fatalf("objective = %q, want stable objective", state.objective)
			}
		})
	}
}

// TestWorkflowStateRecordsBatchEvidence checks real outcomes advance evidence and preserve partial results.
func TestWorkflowStateRecordsBatchEvidence(t *testing.T) {
	state := newWorkflowState("inspect disk", false, 4)
	state.recordDecision(llmResponse{Action: "execute", Summary: "Run checks."}, "Run checks.", []commandPlan{
		{Command: "df -h", Purpose: "Inspect disk"},
		{Command: "du -sh .", Purpose: "Inspect folder"},
	})

	state.recordBatch(state.lastPlans, commandBatchResult{
		Executions: []commandExecution{{Command: "df -h", Purpose: "Inspect disk", ExitCode: 0}},
		Skipped:    []skippedCommand{{Command: "du -sh .", Purpose: "Inspect folder", Reason: "cancelled"}},
	})

	if state.evidenceRevision != 1 {
		t.Fatalf("evidenceRevision = %d, want 1", state.evidenceRevision)
	}
	if len(state.attempts) != 2 {
		t.Fatalf("attempts = %#v, want execution and skip", state.attempts)
	}
	result := state.result(turnOutcomeCancelled, "cancelled", "The operation was cancelled.")
	if result.Outcome != turnOutcomeCancelled || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v, want cancelled partial result", result)
	}
}

// TestWorkflowStateKeepsMixedDecisionEvidenceInLatestBatch checks rejected and executed plans share one projection boundary.
func TestWorkflowStateKeepsMixedDecisionEvidenceInLatestBatch(t *testing.T) {
	state := newWorkflowState("inspect disk", false, 4)
	state.skipped = append(state.skipped, skippedCommand{Command: "old", Reason: "older"})
	state.beginDecisionBatch()
	state.recordPlanRejections([]planRejection{
		{Plan: commandPlan{Command: "duplicate-one", Purpose: "Rejected one"}, Reason: repeatReasonRequired},
		{Plan: commandPlan{Command: "duplicate-two", Purpose: "Rejected two"}, Reason: repeatReasonRequired},
	})
	state.recordBatch([]commandPlan{{Command: "df -h", Purpose: "Inspect"}}, commandBatchResult{
		Executions: []commandExecution{{Command: "df -h", Purpose: "Inspect", ExitCode: 0}},
	})

	if state.latestBatchSkippedStart != 1 {
		t.Fatalf("latestBatchSkippedStart = %d, want boundary before both current conflicts", state.latestBatchSkippedStart)
	}
	if state.latestBatchExecutionStart != 0 {
		t.Fatalf("latestBatchExecutionStart = %d, want current execution boundary", state.latestBatchExecutionStart)
	}
}

// TestWorkflowStateUsesExplicitTimeoutMetadata checks a real exit 124 is not misclassified as a timeout.
func TestWorkflowStateUsesExplicitTimeoutMetadata(t *testing.T) {
	state := newWorkflowState("run command", false, 4)
	state.recordBatch([]commandPlan{{Command: "custom", Purpose: "Run"}}, commandBatchResult{
		Executions: []commandExecution{{Command: "custom", Purpose: "Run", ExitCode: 124}},
	})
	if state.attempts[0].Outcome != "failed" {
		t.Fatalf("ordinary exit 124 outcome = %q, want failed", state.attempts[0].Outcome)
	}

	state.recordBatch([]commandPlan{{Command: "slow", Purpose: "Wait"}}, commandBatchResult{
		Executions: []commandExecution{{Command: "slow", Purpose: "Wait", ExitCode: 124, TimedOut: true}},
	})
	if state.attempts[1].Outcome != "timeout" {
		t.Fatalf("explicit timeout outcome = %q, want timeout", state.attempts[1].Outcome)
	}
}

// TestWorkflowStateExtendsPlanningBudget checks bounded continuation stays explicit.
func TestWorkflowStateExtendsPlanningBudget(t *testing.T) {
	state := newWorkflowState("inspect disk", false, 2)
	if state.planningLimitReached(1) != true {
		t.Fatal("planningLimitReached(1) = false, want true for a two-round budget")
	}
	state.extendPlanningBudget(2)
	if state.planningLimitReached(1) {
		t.Fatal("planningLimitReached(1) = true after extension")
	}
}

// TestWorkflowStateAdmitsContextualRepetitions checks successful commands need a typed cause while failures remain retryable.
func TestWorkflowStateAdmitsContextualRepetitions(t *testing.T) {
	state := newWorkflowState("retry checks", false, 4)
	state.executions = []commandExecution{
		{Command: "pwd", ExitCode: 0},
		{Command: "false", ExitCode: 1},
	}
	plans := []commandPlan{
		{Command: "pwd", Purpose: "Duplicate without cause"},
		{Command: " false ", Purpose: "Retry failure"},
		{Command: "ls", Purpose: "New command"},
	}

	admitted, rejected := state.admitPlans(plans)
	if got := commandNames(admitted); !equalStrings(got, []string{" false ", "ls"}) {
		t.Fatalf("admitted = %v, want failure retry and new command", got)
	}
	if got := rejectedCommandNames(rejected); !equalStrings(got, []string{"pwd"}) {
		t.Fatalf("rejected = %v, want successful duplicate", got)
	}
}

func rejectedCommandNames(rejected []planRejection) []string {
	commands := make([]string, 0, len(rejected))
	for _, rejection := range rejected {
		commands = append(commands, rejection.Plan.Command)
	}
	return commands
}

// TestObservationStrategyKeyNormalizesWrappersAndQuotes checks cosmetic shell
// invocation changes cannot reset the observation attempt budget.
func TestObservationStrategyKeyNormalizesWrappersAndQuotes(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "env LC_ALL=C lsof -nP -iTCP", want: "lsof"},
		{command: "/usr/bin/env LC_ALL=C netstat -an", want: "netstat"},
		{command: `"/usr/sbin/lsof" -nP -iTCP`, want: "lsof"},
		{command: "env", want: "env"},
		{command: "env | grep PATH", want: "env"},
		{command: "env -P /usr/bin lsof -nP -iTCP", want: "lsof"},
		{command: `env -S 'lsof -nP -iTCP'`, want: "lsof"},
		{command: `env -S 'X=1 lsof -nP -iTCP'`, want: "lsof"},
		{command: `env -S '"/usr/sbin/lsof" -nP -iTCP'`, want: "lsof"},
		{command: "env TAG=$(date) lsof -nP -iTCP", want: "lsof"},
	}

	for _, test := range tests {
		if got := observationStrategyKey(test.command); got != test.want {
			t.Errorf("observationStrategyKey(%q) = %q, want %q", test.command, got, test.want)
		}
	}
}

// TestWorkflowStateRejectsModelRepeatReasonsWithoutRuntimeCause checks model
// labels alone cannot authorize an exact successful command again.
func TestWorkflowStateRejectsModelRepeatReasonsWithoutRuntimeCause(t *testing.T) {
	reasons := []core.RepeatReason{
		core.RepeatReasonUserRequested,
		core.RepeatReasonRetry,
		core.RepeatReasonVerifyAfterChange,
		core.RepeatReasonPollChangedState,
	}
	for _, reason := range reasons {
		t.Run(string(reason), func(t *testing.T) {
			state := newWorkflowState("repeat check", false, 4)
			state.executions = []commandExecution{{Command: "df -h", ExitCode: 0}}
			admitted, rejected := state.admitPlans([]commandPlan{{Command: " df -h ", RepeatReason: reason}})
			if len(admitted) != 0 || len(rejected) != 1 || rejected[0].Reason != repeatReasonRequired {
				t.Fatalf("admitted = %#v, rejected = %#v", admitted, rejected)
			}
		})
	}
}

// TestWorkflowStateAdmitsVerificationAfterInterveningSuccess preserves an
// exact verification when runtime history proves another command ran between.
func TestWorkflowStateAdmitsVerificationAfterInterveningSuccess(t *testing.T) {
	state := newWorkflowState("clean up and verify disk", false, 4)
	state.operation = "act"
	state.executions = []commandExecution{
		{Command: "df -h", ExitCode: 0},
		{Command: "cleanup", ExitCode: 0},
	}
	state.attempts = []workflowAttempt{
		{ID: 1, EffectiveCommand: "df -h", Outcome: "success"},
		{ID: 2, EffectiveCommand: "cleanup", Outcome: "success"},
	}

	admitted, rejected := state.admitPlans([]commandPlan{{Command: "df -h", RepeatReason: core.RepeatReasonVerifyAfterChange}})
	if len(admitted) != 1 || admitted[0].AuthorizedRepeatCommand != "df -h" || len(rejected) != 0 {
		t.Fatalf("admitted = %#v, rejected = %#v", admitted, rejected)
	}
}

// TestWorkflowStateConsumesRuntimeRepeatAuthorizationOnce checks one causal
// change cannot authorize multiple identical verifications in the same batch.
func TestWorkflowStateConsumesRuntimeRepeatAuthorizationOnce(t *testing.T) {
	state := newWorkflowState("clean up and verify disk", false, 4)
	state.operation = "act"
	state.executions = []commandExecution{{Command: "df -h", ExitCode: 0}, {Command: "cleanup", ExitCode: 0}}
	state.attempts = []workflowAttempt{
		{ID: 1, EffectiveCommand: "df -h", Outcome: "success"},
		{ID: 2, EffectiveCommand: "cleanup", Outcome: "success"},
	}
	plans := []commandPlan{
		{Command: "df -h", RepeatReason: core.RepeatReasonVerifyAfterChange},
		{Command: "df -h", RepeatReason: core.RepeatReasonVerifyAfterChange},
	}

	admitted, rejected := state.admitPlans(plans)
	if len(admitted) != 1 || len(rejected) != 1 || rejected[0].Reason != repeatReasonRequired {
		t.Fatalf("admitted = %#v, rejected = %#v", admitted, rejected)
	}
}

// TestWorkflowStateAdmitsRetryAfterLatestFailure checks an older success does
// not hide the runtime fact that the exact command most recently failed.
func TestWorkflowStateAdmitsRetryAfterLatestFailure(t *testing.T) {
	state := newWorkflowState("retry latest failure", false, 4)
	state.operation = "act"
	state.executions = []commandExecution{{Command: "update", ExitCode: 0}, {Command: "update", ExitCode: 1}}
	state.attempts = []workflowAttempt{
		{ID: 1, EffectiveCommand: "update", Outcome: "success"},
		{ID: 2, EffectiveCommand: "update", Outcome: "failed"},
	}

	admitted, rejected := state.admitPlans([]commandPlan{{Command: "update", RepeatReason: core.RepeatReasonRetry}})
	if len(admitted) != 1 || admitted[0].AuthorizedRepeatCommand != "update" || len(rejected) != 0 {
		t.Fatalf("admitted = %#v, rejected = %#v", admitted, rejected)
	}
}

func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
