package app

import (
	"context"
	"strings"
	"testing"

	executorpkg "github.com/xEsk/shellia/internal/executor"
	llmpkg "github.com/xEsk/shellia/internal/llm"
)

// TestWorkflowRetrieveContextLoadsMultipleCompleteResults checks selected results
// are loaded whole and in requested order under one new context revision.
func TestWorkflowRetrieveContextLoadsMultipleCompleteResults(t *testing.T) {
	state := newWorkflowState("compare results", false, 10)
	history := []historyEntry{
		{ID: "result-2", Result: "alpha", CharacterCount: 5},
		{ID: "result-7", Result: "beta", CharacterCount: 4},
	}

	kind, reason := state.retrieveContext(history, []string{"result-7", "result-2"})

	if kind != "" || reason != "" || state.contextRevision != 1 || len(state.retrievedContext) != 2 {
		t.Fatalf("kind=%q reason=%q state=%#v", kind, reason, state)
	}
	if state.retrievedContext[0].ID != "result-7" || state.retrievedContext[0].Result != "beta" || state.retrievedContext[1].ID != "result-2" || state.retrievedContext[1].Result != "alpha" {
		t.Fatalf("retrievedContext = %#v, want complete results in requested order", state.retrievedContext)
	}
	if got := retrievedContextCharacterCount(state.retrievedContext); got != 9 {
		t.Fatalf("retrievedContextCharacterCount() = %d, want 9", got)
	}
}

// TestWorkflowRetrieveContextRejectsUnavailableResult checks absent and evicted
// stable IDs block retrieval with the exact missing-input contract.
func TestWorkflowRetrieveContextRejectsUnavailableResult(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		history []historyEntry
	}{
		{name: "unknown", ref: "result-unknown", history: []historyEntry{{ID: "result-8", Result: "available"}}},
		{name: "evicted", ref: "result-0", history: []historyEntry{
			{ID: "result-0", Result: "evicted"},
			{ID: "result-1", Result: "available"},
			{ID: "result-2", Result: "available"},
			{ID: "result-3", Result: "available"},
			{ID: "result-4", Result: "available"},
			{ID: "result-5", Result: "available"},
			{ID: "result-6", Result: "available"},
			{ID: "result-7", Result: "available"},
			{ID: "result-8", Result: "available"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newWorkflowState("reuse result", false, 10)
			kind, reason := state.retrieveContext(tt.history, []string{tt.ref})

			if kind != "missing_input" || reason != "Session result "+tt.ref+" is no longer available." {
				t.Fatalf("kind=%q reason=%q", kind, reason)
			}
			if state.contextRevision != 0 || len(state.contextRefs) != 0 || len(state.retrievedContext) != 0 {
				t.Fatalf("failed retrieval mutated state: %#v", state)
			}
		})
	}
}

// TestWorkflowRetrieveContextEnforcesCharacterBudget checks the rune-counted
// boundary admits exactly 16,000 characters and rejects 16,001.
func TestWorkflowRetrieveContextEnforcesCharacterBudget(t *testing.T) {
	t.Run("exact limit", func(t *testing.T) {
		state := newWorkflowState("reuse result", false, 10)
		kind, reason := state.retrieveContext([]historyEntry{{ID: "result-1", Result: strings.Repeat("é", 16000)}}, []string{"result-1"})
		if kind != "" || reason != "" || state.contextRevision != 1 || retrievedContextCharacterCount(state.retrievedContext) != 16000 {
			t.Fatalf("kind=%q reason=%q state=%#v", kind, reason, state)
		}
	})

	t.Run("over limit", func(t *testing.T) {
		state := newWorkflowState("reuse result", false, 10)
		kind, reason := state.retrieveContext([]historyEntry{{ID: "result-1", Result: strings.Repeat("é", 16001)}}, []string{"result-1"})
		if kind != "unavailable" || reason != "Session results result-1 require 16001 characters; the retrieval limit is 16000." {
			t.Fatalf("kind=%q reason=%q", kind, reason)
		}
		if state.contextRevision != 0 || len(state.contextRefs) != 0 || len(state.retrievedContext) != 0 {
			t.Fatalf("failed retrieval mutated state: %#v", state)
		}
	})
}

// TestWorkflowRetrieveContextFailurePreservesLoadedRevision checks retrieval is
// all-or-nothing even after a prior successful load.
func TestWorkflowRetrieveContextFailurePreservesLoadedRevision(t *testing.T) {
	state := newWorkflowState("reuse result", false, 10)
	history := []historyEntry{
		{ID: "result-1", Result: "first"},
		{ID: "result-2", Result: strings.Repeat("x", 16001)},
	}
	if kind, reason := state.retrieveContext(history, []string{"result-1"}); kind != "" || reason != "" {
		t.Fatalf("initial retrieval kind=%q reason=%q", kind, reason)
	}

	kind, _ := state.retrieveContext(history, []string{"result-2"})

	if kind != "unavailable" {
		t.Fatalf("kind = %q, want unavailable", kind)
	}
	if state.contextRevision != 1 || len(state.contextRefs) != 1 || state.contextRefs[0] != "result-1" || len(state.retrievedContext) != 1 || state.retrievedContext[0].Result != "first" {
		t.Fatalf("failed retrieval changed loaded revision: %#v", state)
	}
}

// TestWorkflowRetrieveContextIncrementsRevision checks each successful load
// creates a distinct causal revision.
func TestWorkflowRetrieveContextIncrementsRevision(t *testing.T) {
	state := newWorkflowState("reuse results", false, 10)
	history := []historyEntry{{ID: "result-1", Result: "first"}, {ID: "result-2", Result: "second"}}
	for index, refs := range [][]string{{"result-1"}, {"result-2"}} {
		if kind, reason := state.retrieveContext(history, refs); kind != "" || reason != "" {
			t.Fatalf("retrieval %d kind=%q reason=%q", index+1, kind, reason)
		}
		if state.contextRevision != index+1 {
			t.Fatalf("retrieval %d contextRevision=%d, want %d", index+1, state.contextRevision, index+1)
		}
	}
}

// TestWorkflowValidateContextReferencesForCompletion checks session-result
// completion requires the current revision and exact ordered unique references.
func TestWorkflowValidateContextReferencesForCompletion(t *testing.T) {
	state := newWorkflowState("compare results", false, 10)
	history := []historyEntry{{ID: "result-2", Result: "alpha"}, {ID: "result-7", Result: "beta"}}
	for revision := 1; revision <= 2; revision++ {
		if kind, reason := state.retrieveContext(history, []string{"result-2", "result-7"}); kind != "" || reason != "" {
			t.Fatalf("retrieveContext(revision %d) kind=%q reason=%q", revision, kind, reason)
		}
	}

	valid := llmResponse{
		Action:          "complete",
		Operation:       "answer",
		EvidenceSource:  "session_result",
		Freshness:       "snapshot",
		SuccessCriteria: "Compare the results",
		ContextRefs:     []string{"result-2", "result-7"},
		CompletionBasis: llmpkg.CompletionBasis{Source: "session_result", Freshness: "snapshot", ContextRevision: 2},
	}
	if err := state.validateCompletion(valid); err != nil {
		t.Fatalf("validateCompletion(valid) error = %v", err)
	}

	tests := []struct {
		name     string
		revision int
		refs     []string
	}{
		{name: "missing revision", revision: 0, refs: []string{"result-2", "result-7"}},
		{name: "stale revision", revision: 1, refs: []string{"result-2", "result-7"}},
		{name: "reordered refs", revision: 2, refs: []string{"result-7", "result-2"}},
		{name: "missing ref", revision: 2, refs: []string{"result-2"}},
		{name: "extra ref", revision: 2, refs: []string{"result-2", "result-7", "result-8"}},
		{name: "duplicate ref", revision: 2, refs: []string{"result-2", "result-2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := valid
			decision.CompletionBasis.ContextRevision = tt.revision
			decision.ContextRefs = tt.refs
			if err := state.validateCompletion(decision); err == nil {
				t.Fatalf("validateCompletion(revision=%d, refs=%#v) error = nil", tt.revision, tt.refs)
			}
		})
	}
}

// TestRunTurnRetrievesContextBeforeExecutorPaths checks retrieval starts a new
// planning round without presenting, confirming, admitting, or executing a plan.
func TestRunTurnRetrievesContextBeforeExecutorPaths(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reuse the selected result","summary":"MODEL_RETRIEVAL_PLAN_PRESENTATION","completion_basis":{"source":"","freshness":""},"context_refs":["result-2"],"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reuse the selected result","summary":"Loaded result used.","completion_basis":{"source":"session_result","freshness":"snapshot","context_revision":1},"context_refs":["result-2"],"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	ctxInfo := loopTestContext(t)
	entry := historyEntry{
		ID:             "result-2",
		Instruction:    "Inspect the deployment",
		Outcome:        turnOutcomeCompleted,
		Result:         strings.Repeat("x", 300) + "RUNTIME_COMPLETE_TAIL",
		CharacterCount: 321,
	}
	request := loopTurnRequest(cfg, &ctxInfo, "reuse the deployment result")
	request.History = []historyEntry{entry}
	executorCalls := 0
	var result turnResult
	var runErr error

	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, executorpkg.Options, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executorCalls++
			return commandBatchResult{}, nil
		}
		result, runErr = runTurn(t.Context(), deps, false, request)
	})

	if runErr != nil {
		t.Fatalf("runTurn() error = %v", runErr)
	}
	if result.Outcome != turnOutcomeCompleted || result.Result != "Loaded result used." {
		t.Fatalf("runTurn() result = %#v", result)
	}
	if executorCalls != 0 {
		t.Fatalf("ExecuteCommands calls = %d, want 0", executorCalls)
	}
	if fake.requestCount() != 2 {
		t.Fatalf("LLM request count = %d, want 2", fake.requestCount())
	}
	bodies := fake.requestBodies()
	if strings.Contains(bodies[0], "RUNTIME_COMPLETE_TAIL") || !strings.Contains(bodies[1], "RUNTIME_COMPLETE_TAIL") {
		t.Fatalf("retrieved context tail appeared outside loaded revision: %#v", bodies)
	}
	if strings.Contains(output, "MODEL_RETRIEVAL_PLAN_PRESENTATION") {
		t.Fatalf("retrieval decision reached plan presentation: %q", output)
	}
}

// TestRunTurnRepeatedContextRetrievalHonorsPlanningLimit checks retrieval-only
// rounds cannot bypass the user-controlled planning budget extension boundary.
func TestRunTurnRepeatedContextRetrievalHonorsPlanningLimit(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reuse the selected result","summary":"Load the result.","completion_basis":{"source":"","freshness":""},"context_refs":["result-1"],"commands":[]}`},
		loopLLMResponse{content: `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reuse the selected result","summary":"Load the result again.","completion_basis":{"source":"","freshness":""},"context_refs":["result-1"],"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reuse the selected result","summary":"This response must require an approved extension.","completion_basis":{"source":"session_result","freshness":"snapshot","context_revision":2},"context_refs":["result-1"],"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.PlanningMaxRounds = 2
	ctxInfo := loopTestContext(t)
	request := loopTurnRequest(cfg, &ctxInfo, "reuse the selected result")
	request.History = []historyEntry{{ID: "result-1", Instruction: "earlier task", Outcome: turnOutcomeCompleted, Result: "earlier result", CharacterCount: 14}}

	var result turnResult
	output := captureMainLoopIO(t, "n\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, executorpkg.Options, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			t.Fatal("ExecuteCommands reached during context retrieval")
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, request)
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomePlanningLimit {
		t.Fatalf("runTurn() result = %#v, want planning-limit outcome", result)
	}
	if fake.requestCount() != 2 {
		t.Fatalf("LLM request count = %d, want 2 before declined extension", fake.requestCount())
	}
	if !strings.Contains(output, "Continue planning? [y/n]: no") {
		t.Fatalf("output missing declined planning continuation: %q", output)
	}
}

// TestRunTurnRejectsGuessedContextWhenSessionMemoryDisabled checks a model
// cannot retrieve a predictable result ID that the prompt catalog intentionally hides.
func TestRunTurnRejectsGuessedContextWhenSessionMemoryDisabled(t *testing.T) {
	const secret = "DISABLED_SESSION_MEMORY_SECRET"
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reveal a guessed result","summary":"Load the guessed result.","completion_basis":{"source":"","freshness":""},"context_refs":["result-1"],"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reveal a guessed result","summary":"The hidden result was loaded.","completion_basis":{"source":"session_result","freshness":"snapshot","context_revision":1},"context_refs":["result-1"],"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.IncludeSessionMemory = false
	ctxInfo := loopTestContext(t)
	request := loopTurnRequest(cfg, &ctxInfo, "use a guessed earlier result")
	request.History = []historyEntry{{ID: "result-1", Instruction: "private task", Outcome: turnOutcomeCompleted, Result: secret, CharacterCount: len([]rune(secret))}}

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, executorpkg.Options, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			t.Fatal("ExecuteCommands reached while session memory was disabled")
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, request)
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeBlocked || strings.TrimSpace(result.BlockerReason) == "" {
		t.Fatalf("runTurn() result = %#v, want explicit blocked outcome", result)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM request count = %d, want guessed retrieval rejected before another request", fake.requestCount())
	}
	for _, body := range fake.requestBodies() {
		if strings.Contains(body, secret) {
			t.Fatalf("disabled session result leaked into an LLM request: %q", body)
		}
	}
	if strings.Contains(output, secret) {
		t.Fatalf("disabled session result leaked into terminal output: %q", output)
	}
}

// TestRunTurnBlocksFailedContextRetrievalBeforeExecutorPaths checks missing and
// oversized selections stop without another model request or executor call.
func TestRunTurnBlocksFailedContextRetrievalBeforeExecutorPaths(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		history    []historyEntry
		wantKind   string
		wantReason string
	}{
		{
			name:       "missing result",
			ref:        "result-9",
			history:    []historyEntry{{ID: "result-2", Result: "available"}},
			wantKind:   "missing_input",
			wantReason: "Session result result-9 is no longer available.",
		},
		{
			name:       "oversized result",
			ref:        "result-2",
			history:    []historyEntry{{ID: "result-2", Result: strings.Repeat("é", 16001)}},
			wantKind:   "unavailable",
			wantReason: "Session results result-2 require 16001 characters; the retrieval limit is 16000.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newLoopLLMClient(t, loopLLMResponse{content: `{"action":"retrieve_context","operation":"answer","evidence_source":"session_result","freshness":"snapshot","success_criteria":"Reuse the selected result","summary":"MODEL_RETRIEVAL_PLAN_PRESENTATION","completion_basis":{"source":"","freshness":""},"context_refs":["` + tt.ref + `"],"commands":[]}`})
			cfg := loopTestConfig(fake.URL())
			cfg.AskConfirmPlan = true
			ctxInfo := loopTestContext(t)
			request := loopTurnRequest(cfg, &ctxInfo, "reuse the deployment result")
			request.History = tt.history
			executorCalls := 0
			var result turnResult
			var runErr error

			output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
				deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, executorpkg.Options, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
					executorCalls++
					return commandBatchResult{}, nil
				}
				result, runErr = runTurn(t.Context(), deps, false, request)
			})

			if runErr != nil {
				t.Fatalf("runTurn() error = %v", runErr)
			}
			if result.Outcome != turnOutcomeBlocked || result.BlockerKind != tt.wantKind || result.BlockerReason != tt.wantReason || result.Result != tt.wantReason {
				t.Fatalf("runTurn() result = %#v", result)
			}
			if fake.requestCount() != 1 {
				t.Fatalf("LLM request count = %d, want 1", fake.requestCount())
			}
			if executorCalls != 0 {
				t.Fatalf("ExecuteCommands calls = %d, want 0", executorCalls)
			}
			if strings.Contains(output, "MODEL_RETRIEVAL_PLAN_PRESENTATION") {
				t.Fatalf("failed retrieval reached plan presentation: %q", output)
			}
		})
	}
}
