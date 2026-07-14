# Failure-Aware Replanning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ordinary command failures become bounded planning observations while preventing dependent commands from running after an earlier failure.

**Architecture:** Extend the existing model command contract with conservative per-command independence metadata. Return a structured batch result from the executor, let `runTurn` own bounded recovery, and keep skipped commands separate from real executions so summaries, traces, and session memory remain grounded.

**Tech Stack:** Go 1.26, standard `testing` package, existing OpenAI-compatible JSON contract, existing `runtimeDeps`, existing JSONL trace logger, GitHub-hosted documentation.

## Global Constraints

- Do not add dependencies, services, configuration keys, flags, persistence, or a new recovery controller.
- `independent_on_failure` is `false` when omitted and only concerns earlier failures in the same execution batch.
- Ordinary failures trigger one replan per failed batch; timeouts and cancellations never trigger automatic replanning.
- All recovery plans use the existing local safety classifier, plan confirmation, command confirmation, and `yes_safe` behavior.
- `continue_on_error=false` stops the current batch at the first failure or timeout.
- `continue_on_error=true` skips dependent commands and runs only commands explicitly marked independent.
- Manual `!` and `/shell` execution remain unchanged.
- Skipped commands are visible in the current turn and trace but never become fake executions or reusable session observations.
- Successful commands are not repeated in the same turn; failed and skipped commands remain retryable.
- Use `runtimeDeps` and injected readers/writers in app-loop tests; never replace process globals.

---

## File and Responsibility Map

- `internal/core/types.go`: shared plan, batch, skipped-command, turn-result, and observation transcript contracts.
- `internal/llm/llm.go`: JSON field, prompt rules, current-turn observations, and grounded summary input.
- `internal/llm/aliases.go`, `internal/llm/export.go`: shared aliases and exported summary signature.
- `internal/executor/executor.go`: dependency-aware batch execution, skipped rendering, and skipped trace events.
- `internal/executor/aliases.go`, `internal/executor/export.go`: batch aliases and exported runner signature.
- `internal/app/runtime.go`, `internal/app/aliases.go`: injectable batch runner contract.
- `internal/app/main.go`: accumulated outcomes, bounded failure replanning, timeout exclusion, and successful-command filtering.
- `internal/session/session_memory.go`: successful-effect filtering and reusable execution observations.
- `internal/trace/trace.go`: existing execution projection; no new abstraction required.
- `internal/*/*_test.go`: focal RED/GREEN coverage at each affected boundary.
- `README.md`, `internal/config/config.go`, `site/index.html`: active behavior and configuration documentation.

---

### Task 1: Add Conservative Failure-Dependency Contracts

**Files:**
- Modify: `internal/core/types.go:60-100`
- Modify: `internal/llm/llm.go:45-90,535-630,760-810`
- Modify: `internal/llm/aliases.go:10-25`
- Test: `internal/llm/llm_test.go`

**Interfaces:**
- Produces: `CommandPlan.IndependentOnFailure bool`
- Produces: `SkippedCommand { Command, Purpose, Reason string }`
- Produces: `CommandBatchResult { Executions, Skipped, HadOrdinaryFailure, HadTimeout }`
- Produces: JSON field `independent_on_failure`

- [ ] **Step 1: Write failing model-contract tests**

Add focused tests that prove omission is conservative and explicit independence survives normalization:

```go
func TestParseResponseDefaultsFailureIndependenceToFalse(t *testing.T) {
	parsed, err := parseResponse(`{"summary":"Run one command.","commands":[{"command":"pwd","purpose":"Print directory","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`)
	if err != nil {
		t.Fatalf("parseResponse() error = %v", err)
	}
	if parsed.Commands[0].IndependentOnFailure {
		t.Fatal("IndependentOnFailure = true, want conservative false default")
	}
}

func TestNormalizePlanPreservesFailureIndependence(t *testing.T) {
	_, plans, err := normalizePlan(Response{
		Summary: "Continue independent inspection.",
		Commands: []Command{{
			Command:              "pwd",
			Purpose:              "Print directory",
			Risk:                 "safe",
			IndependentOnFailure: true,
		}},
	})
	if err != nil {
		t.Fatalf("normalizePlan() error = %v", err)
	}
	if len(plans) != 1 || !plans[0].IndependentOnFailure {
		t.Fatalf("plans = %#v, want independent command", plans)
	}
}

func TestBuildSystemPromptDefinesFailureIndependence(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, snippet := range []string{"independent_on_failure", "same command batch", "false"} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("buildSystemPrompt() missing %q", snippet)
		}
	}
}
```

- [ ] **Step 2: Run the focal tests and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/llm -run 'Test(ParseResponseDefaultsFailureIndependenceToFalse|NormalizePlanPreservesFailureIndependence|BuildSystemPromptDefinesFailureIndependence)$'
```

Expected: FAIL because `Command` and `CommandPlan` do not contain `IndependentOnFailure` and the prompt does not define the field.

- [ ] **Step 3: Add the shared types and JSON field**

Extend `core.CommandPlan` and `core.TurnResult`, then add the new batch types:

```go
type TurnResult struct {
	Result     string
	Summary    string
	Actionable bool
	Plans      []CommandPlan
	Executions []CommandExecution
	Skipped    []SkippedCommand
}

type CommandPlan struct {
	Command              string
	Purpose              string
	Risk                 string
	RequiresConfirmation bool
	Classification       string
	LocalSafe            bool
	IndependentOnFailure bool
	Interactive          bool
	InteractiveReason    string
}

type SkippedCommand struct {
	Command string
	Purpose string
	Reason  string
}

type CommandBatchResult struct {
	Executions         []CommandExecution
	Skipped            []SkippedCommand
	HadOrdinaryFailure bool
	HadTimeout         bool
}
```

Extend `llm.Command` and preserve the field in `normalizePlan`:

```go
type Command struct {
	Command              string `json:"command"`
	Purpose              string `json:"purpose"`
	Risk                 string `json:"risk"`
	RequiresConfirmation bool   `json:"requires_confirmation"`
	IndependentOnFailure bool   `json:"independent_on_failure"`
	Interactive          bool   `json:"interactive"`
	InteractiveReason    string `json:"interactive_reason"`
}
```

```go
IndependentOnFailure: item.IndependentOnFailure,
```

Add the `skippedCommand` alias in `internal/llm/aliases.go`. Add executor/app batch aliases when those packages begin consuming the new types in Task 2.

- [ ] **Step 4: Update both planning JSON schemas and prompt rules**

Use this command object in the normal and plan-only schemas:

```json
{"command":"string","purpose":"string","risk":"safe|medium|high","requires_confirmation":true,"independent_on_failure":false,"interactive":false,"interactive_reason":""}
```

Add these exact rules to the system and user prompt contracts:

```text
Set independent_on_failure=true only when the command remains safe and useful if any earlier command in the same command batch fails.
When uncertain, set independent_on_failure=false. The field never lowers risk or confirmation requirements.
```

- [ ] **Step 5: Run the focal package and verify GREEN**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/llm
```

Expected: PASS.

- [ ] **Step 6: Commit the contract slice**

```bash
git add internal/core/types.go internal/llm/aliases.go internal/llm/llm.go internal/llm/llm_test.go
git commit -m "Add failure-aware plan contracts"
```

---

### Task 2: Return Dependency-Aware Command Batches

**Files:**
- Modify: `internal/executor/aliases.go:10-25`
- Modify: `internal/executor/export.go:35-65`
- Modify: `internal/executor/executor.go:332-455`
- Modify: `internal/executor/executor_test.go:380-535`
- Modify: `internal/app/aliases.go:20-55`
- Modify: `internal/app/runtime.go:8-75`
- Modify: `internal/app/main.go:890-985`
- Modify: `internal/app/main_loop_test.go` (all injected `ExecuteCommands` functions)

**Interfaces:**
- Consumes: `CommandPlan.IndependentOnFailure`
- Produces: `ExecuteCommands(...) (CommandBatchResult, error)`
- Produces: trace event `command_skipped`
- Invariant: Go `error` remains reserved for cancellation, user abort, the existing interactive-prompt repair path, and structural failures.

- [ ] **Step 1: Write failing executor tests for dependent and independent steps**

Add a table-driven test using real temporary marker files:

```go
func TestExecuteCommandsContinuesOnlyIndependentStepsAfterFailure(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ContinueOnError = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	dependent := filepath.Join(ctxInfo.CWD, "dependent")
	independent := filepath.Join(ctxInfo.CWD, "independent")
	plans := []commandPlan{
		{Command: "false", Purpose: "Fail", Classification: classificationSafe, LocalSafe: true},
		{Command: "touch " + dependent, Purpose: "Dependent", Classification: classificationSafe, LocalSafe: true},
		{Command: "touch " + independent, Purpose: "Independent", Classification: classificationSafe, LocalSafe: true, IndependentOnFailure: true},
	}

	var batch commandBatchResult
	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, cfg, &ctxInfo, plans)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if !batch.HadOrdinaryFailure || batch.HadTimeout {
		t.Fatalf("batch flags = %#v, want ordinary failure only", batch)
	}
	if len(batch.Executions) != 2 || len(batch.Skipped) != 1 {
		t.Fatalf("batch = %#v, want 2 executions and 1 skip", batch)
	}
	if batch.Skipped[0].Command != plans[1].Command {
		t.Fatalf("skipped = %#v, want dependent command", batch.Skipped)
	}
	if _, err := os.Stat(dependent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dependent marker error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(independent); err != nil {
		t.Fatalf("independent marker error = %v, want file", err)
	}
}
```

Add variants for `continue_on_error=false`, timeout behavior, and cancellation. The timeout variant must assert `HadTimeout=true`, `HadOrdinaryFailure=false`, dependent skipped, independent executed, and no recovery decision at this layer. The cancellation variant must cancel the injected context while the first command is running and assert `errors.Is(err, context.Canceled)`, no later command execution, and no ordinary-failure or timeout flag.

- [ ] **Step 2: Run the focal executor tests and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/executor -run 'TestExecuteCommands(ContinuesOnlyIndependentStepsAfterFailure|StopsBatchWhenContinueOnErrorIsFalse|TracksTimeoutWithoutOrdinaryFailure|StopsImmediatelyOnCancellation)$'
```

Expected: FAIL because the executor has no structured batch result or dependency-aware skip behavior.

- [ ] **Step 3: Change the injected and exported runner signatures**

Use this exact runner shape in `internal/app/runtime.go`:

```go
type commandRunner func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) (commandBatchResult, error)
```

Update the executor export similarly:

```go
func ExecuteCommands(ctx context.Context, deps RuntimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan) (core.CommandBatchResult, error) {
	return executeCommands(ctx, deps, ui, cfg, ctxInfo, plans)
}
```

Update each injected app test runner mechanically:

```go
return commandBatchResult{Executions: []commandExecution{{
	Command: plans[0].Command,
	Purpose: plans[0].Purpose,
	ExitCode: 0,
}}}, nil
```

- [ ] **Step 4: Implement dependency-aware execution**

Use one stable reason string:

```go
const skippedAfterFailureReason = "dependent on an earlier failed command"
```

At the start of `executeCommands`, replace the execution slice with:

```go
batch := commandBatchResult{
	Executions: make([]commandExecution, 0, len(plans)),
	Skipped:    make([]skippedCommand, 0, len(plans)),
}
blocked := false
```

At the top of each loop iteration, before constructing the normal command box or asking for confirmation:

```go
if blocked && !plan.IndependentOnFailure {
	skipped := skippedCommand{
		Command: plan.Command,
		Purpose: plan.Purpose,
		Reason:  skippedAfterFailureReason,
	}
	batch.Skipped = append(batch.Skipped, skipped)
	box := printCommandExecutionTo(deps.Stdout, ui, cfg, index+1, len(plans), plan)
	box.Section("skipped", colorDim)
	box.Text(skipped.Reason, colorDim)
	box.Close()
	deps.Trace.Record("command_skipped", turnID, "", -1, map[string]any{
		"step": index + 1, "total_steps": len(plans),
		"command": skipped.Command, "purpose": skipped.Purpose, "reason": skipped.Reason,
	})
	continue
}

box := printCommandExecutionTo(deps.Stdout, ui, cfg, index+1, len(plans), plan)
```

Remove the old unconditional `box := printCommandExecutionTo(...)` at the top of the loop so every executed or skipped step is rendered exactly once. Skipped steps must not emit `command_confirmation` or `command_start`.

Check `context.Canceled` before constructing and appending the current execution so a cancelled process cannot be represented with the zero-value exit code; return the already completed prefix of the batch immediately. Append every other real attempt (success, ordinary failure, timeout, or interactive-prompt attempt) to `batch.Executions`. Replace command-failure returns with:

```go
var promptErr *interactivePromptError
if errors.As(err, &promptErr) {
	return batch, err
}
if errors.Is(err, errAborted) {
	return batch, err
}

var runErr *commandRunError
if !errors.As(err, &runErr) {
	return batch, err
}
blocked = true
if runErr.TimedOut {
	batch.HadTimeout = true
} else {
	batch.HadOrdinaryFailure = true
}
if !cfg.ContinueOnError {
	return batch, nil
}
printWarningTo(deps.Stderr, ui, err.Error())
continue
```

Return `batch, nil` after the loop. Preserve the existing `command_start`, `command_end`, `command_error`, confirmation, edited-command, and session-directory behavior.

- [ ] **Step 5: Adapt `runTurn` without adding recovery yet**

Consume `batch.Executions`, accumulate `batch.Skipped`, and keep the current observation decision until Task 3:

```go
batch, err := deps.ExecuteCommands(ctx, deps, ui, cfg, ctxInfo, plans)
allExecutions = append(allExecutions, batch.Executions...)
allSkipped = append(allSkipped, batch.Skipped...)
```

Declare `allSkipped := make([]skippedCommand, 0, 4)` beside `allExecutions`.

Include `Skipped: allSkipped` in the final `TurnResult`. Do not yet force a failure-driven round in this task.

- [ ] **Step 6: Update trace assertions and run affected packages**

Update `TestExecuteCommandsTraceRecordsCommandErrors` to expect `batch.HadOrdinaryFailure=true` and `err=nil`. Add an assertion that exactly one `command_skipped` event contains the dependent command and reason.

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/executor ./internal/app
```

Expected: PASS.

- [ ] **Step 7: Commit the batch slice**

```bash
git add internal/executor internal/app/aliases.go internal/app/runtime.go internal/app/main.go internal/app/main_loop_test.go
git commit -m "Make command batches dependency aware"
```

---

### Task 3: Build the Failure-Replanning Walking Skeleton

**Files:**
- Modify: `internal/core/types.go:145-170`
- Modify: `internal/llm/llm.go:45-65,390-430,700-735`
- Modify: `internal/llm/export.go:45-75`
- Modify: `internal/llm/aliases.go:10-25`
- Modify: `internal/app/main.go:805-985`
- Modify: `internal/app/main_loop_test.go`
- Modify: `internal/app/aliases.go`

**Interfaces:**
- Consumes: `CommandBatchResult.HadOrdinaryFailure`, `.HadTimeout`, `.Skipped`
- Produces: `PromptRequest.Skipped []SkippedCommand`
- Produces: `CommandExecution.ObservationTranscript(limit, strategy)`
- Produces: one recovery planning round per batch with ordinary failure
- Produces: `shellia_decision=continue_after_execution_failure`

- [ ] **Step 1: Write the failing end-to-end app-loop test**

Use the existing fake LLM and injected runner:

```go
func TestRunTurnReplansOnceAfterOrdinaryFailure(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Run initial batch.","commands":[{"command":"false","purpose":"Trigger failure","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""},{"command":"touch blocked","purpose":"Blocked dependent step","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""},{"command":"pwd","purpose":"Independent inspection","risk":"safe","requires_confirmation":false,"independent_on_failure":true,"interactive":false,"interactive_reason":""}]}`},
		loopLLMResponse{content: `{"summary":"Run recovery.","commands":[{"command":"git status --short","purpose":"Verify repository state","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""}]}`},
		loopLLMResponse{content: "Recovery completed.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.ContinueOnError = true
	ctxInfo := loopTestContext(t)
	call := 0

	output := captureMainLoopIO(t, "y\ny\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
			call++
			if call == 1 {
				if len(plans) != 3 || !plans[2].IndependentOnFailure {
					t.Fatalf("initial plans = %#v, want failure, dependent, and independent steps", plans)
				}
				return commandBatchResult{
					Executions: []commandExecution{
						{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 7, Stderr: capturedStream{Text: "initial failure"}},
						{Command: plans[2].Command, Purpose: plans[2].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}},
					},
					Skipped: []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "dependent on an earlier failed command"}},
					HadOrdinaryFailure: true,
				}, nil
			}
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}}}}, nil
		}
		result, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover from a failed command"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
		if result.Result != "Recovery completed." || len(result.Executions) != 3 || len(result.Skipped) != 1 {
			t.Fatalf("result = %#v, want recovered result with three executions and one skip", result)
		}
	})

	bodies := fake.requestBodies()
	if len(bodies) != 3 {
		t.Fatalf("request count = %d, want two planning requests and one summary", len(bodies))
	}
	for _, snippet := range []string{"Exit code: 7", "initial failure", "Independent inspection", "Skipped commands from the current task:", "touch blocked", "dependent on an earlier failed command"} {
		if !strings.Contains(bodies[1], snippet) {
			t.Fatalf("recovery prompt missing %q: %q", snippet, bodies[1])
		}
	}
	if strings.Count(output, "Execute this plan?") != 2 {
		t.Fatalf("output = %q, want confirmation for initial and recovery plans", output)
	}
}
```

- [ ] **Step 2: Run the walking skeleton and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run TestRunTurnReplansOnceAfterOrdinaryFailure
```

Expected: FAIL because ordinary failures do not force a new round and skipped commands are not added to prompts or summaries.

- [ ] **Step 3: Add skipped observations and exit codes to planning prompts**

Add the shared current-turn transcript helper:

```go
func (execution CommandExecution) ObservationTranscript(limit int, strategy TruncationStrategy) string {
	return fmt.Sprintf("Exit code: %d\n%s", execution.ExitCode, execution.PromptTranscript(limit, strategy))
}
```

Extend `PromptRequest`:

```go
type PromptRequest struct {
	Config              config
	ContextInfo         contextInfo
	Instruction         string
	ResolvedInstruction string
	History             []historyEntry
	State               sessionState
	Observations        []commandExecution
	Skipped             []skippedCommand
}
```

Read `skipped := request.Skipped` beside the existing `observations := request.Observations` in `buildUserPrompt`.

Build the current-task block when either collection is non-empty:

```go
if cfg.IncludeRecentObservations && (len(observations) > 0 || len(skipped) > 0) {
	var b strings.Builder
	b.WriteString("\nObserved outputs from the current task:\n")
	for index, execution := range observations {
		fmt.Fprintf(&b, "%d. Purpose: %s\n", index+1, execution.Purpose)
		fmt.Fprintf(&b, "   Command: %s\n", execution.Command)
		fmt.Fprintf(&b, "%s\n", indentLines(execution.ObservationTranscript(cfg.ObservationOutputChars, cfg.TruncationStrategy), "   "))
	}
	if len(skipped) > 0 {
		b.WriteString("Skipped commands from the current task:\n")
		for index, item := range skipped {
			fmt.Fprintf(&b, "%d. Purpose: %s\n", index+1, item.Purpose)
			fmt.Fprintf(&b, "   Command: %s\n", item.Command)
			fmt.Fprintf(&b, "   Reason: %s\n", item.Reason)
		}
	}
	observationBlock = b.String()
}
```

- [ ] **Step 4: Force bounded recovery in `runTurn`**

Pass both accumulated collections to every planning request. Append the partial batch before inspecting `err`. Preserve the existing interactive-prompt repair by treating only `interactivePromptError` as a follow-up signal. A user abort returns immediately. A cancelled context records `execution_failure_replan_excluded` with reason `cancellation` and then returns immediately. Return every other structural error. Then decide follow-up with:

```go
if errors.Is(err, errAborted) {
	return turnResult{}, err
}
if errors.Is(err, context.Canceled) {
	deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
		"decision": "execution_failure_replan_excluded",
		"reason":   "cancellation",
	})
	return turnResult{}, err
}
var promptErr *interactivePromptError
interactiveRepair := errors.As(err, &promptErr)
if err != nil && !interactiveRepair {
	return turnResult{}, err
}

requiresFollowup := interactiveRepair || batch.HadOrdinaryFailure || (parsed.RequiresObservation && !batch.HadTimeout)
if !requiresFollowup {
	if batch.HadTimeout {
		deps.Trace.Record("shellia_decision", turnID, "planning", round, map[string]any{
			"decision": "execution_failure_replan_excluded",
			"reason":   "timeout",
		})
	}
	break
}

decision := "continue_after_observation"
if batch.HadOrdinaryFailure {
	decision = "continue_after_execution_failure"
}
```

Reuse the existing planning-limit prompt before `continue`; it must apply equally to ordinary failures, requested observations, and interactive-prompt repair. Do not create another counter. A batch with both timeout and ordinary failure must follow up because `HadOrdinaryFailure` is authoritative. Remove the obsolete error-branch control flow only after its interactive repair, cancellation, partial execution accumulation, and limit behavior are covered by focused tests.

Every return path reached after at least one execution batch—including a later planning response with an empty command array—must return the accumulated `Executions` and `Skipped` collections. Mark such a turn actionable because real execution occurred, so session memory and retry state receive the real outcomes.

- [ ] **Step 5: Include skipped commands in final summary input**

Change `streamSummarizeExecutions` and its export to accept `skipped []skippedCommand`. Append this separate section to the summary transcript:

```go
if len(skipped) > 0 {
	transcript.WriteString("Skipped commands\n")
	for index, item := range skipped {
		fmt.Fprintf(&transcript, "%d. Purpose: %s\n", index+1, item.Purpose)
		fmt.Fprintf(&transcript, "   Command: %s\n", item.Command)
		fmt.Fprintf(&transcript, "   Reason: %s\n", item.Reason)
	}
}
```

Add this grounding sentence to the summary system prompt:

```text
Commands listed as skipped were not executed; never describe them as completed or assign them output.
```

- [ ] **Step 6: Add recovery-boundary app tests**

Add focused `runtimeDeps` tests proving:

- a batch containing two ordinary failures still sends exactly one next planning request;
- a timeout-only batch sends no recovery planning request, records the timeout exclusion, and retains any independent execution returned in the batch;
- cancellation returns `context.Canceled`, sends no recovery planning request, and records the cancellation exclusion;
- accepting the existing planning-limit prompt extends by `PlanningMaxRounds`, while declining it preserves all accumulated executions and skips in the final result;
- a recovery plan uses the same plan confirmation and command confirmation path as the initial plan;
- a locally safe recovery command auto-runs only when `YesSafe=true`; with `YesSafe=false`, it still reaches the normal command prompt;
- a locally risky recovery command still requires command confirmation regardless of `YesSafe`.

Use injected files/streams and existing fake HTTP clients; do not replace process globals.

- [ ] **Step 7: Run the walking skeleton and affected packages**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run TestRunTurnReplansOnceAfterOrdinaryFailure
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app ./internal/llm
```

Expected: both commands PASS.

- [ ] **Step 8: Commit the walking skeleton**

```bash
git add internal/app internal/llm internal/core/types.go
git commit -m "Replan after ordinary command failures"
```

---

### Task 4: Filter Successful Repetitions Without Blocking Failed Retries

**Files:**
- Modify: `internal/app/main.go:840-865,1154-1175`
- Test: `internal/app/main_loop_test.go`

**Interfaces:**
- Consumes: effective `CommandExecution.Command` and `ExitCode`
- Produces: `filterPreviouslySuccessfulPlans(plans, executions) (kept, redundant []CommandPlan)`

- [ ] **Step 1: Write failing unit tests for mixed plans and failed retries**

```go
func TestFilterPreviouslySuccessfulPlansKeepsFailedRetries(t *testing.T) {
	plans := []commandPlan{{Command: "pwd"}, {Command: "false"}, {Command: "ls"}}
	executions := []commandExecution{{Command: "pwd", ExitCode: 0}, {Command: "false", ExitCode: 1}}

	kept, redundant := filterPreviouslySuccessfulPlans(plans, executions)
	if got := commandNames(kept); !reflect.DeepEqual(got, []string{"false", "ls"}) {
		t.Fatalf("kept = %v, want failed retry and new command", got)
	}
	if got := commandNames(redundant); !reflect.DeepEqual(got, []string{"pwd"}) {
		t.Fatalf("redundant = %v, want successful command", got)
	}
}
```

Add a second test proving trimmed effective command equality and a third proving a skipped command is not considered because it is absent from `executions`.

- [ ] **Step 2: Run the focal test and verify RED**

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app -run TestFilterPreviouslySuccessfulPlans
```

Expected: FAIL because only the all-or-nothing `shouldSkipRedundantRound` exists and it treats failures as executed.

- [ ] **Step 3: Implement individual successful-command filtering**

Replace `shouldSkipRedundantRound` with:

```go
func filterPreviouslySuccessfulPlans(plans []commandPlan, executions []commandExecution) ([]commandPlan, []commandPlan) {
	succeeded := make(map[string]bool, len(executions))
	for _, execution := range executions {
		command := strings.TrimSpace(execution.Command)
		if execution.ExitCode == 0 && command != "" {
			succeeded[command] = true
		}
	}

	kept := make([]commandPlan, 0, len(plans))
	redundant := make([]commandPlan, 0)
	for _, plan := range plans {
		if succeeded[strings.TrimSpace(plan.Command)] {
			redundant = append(redundant, plan)
			continue
		}
		kept = append(kept, plan)
	}
	return kept, redundant
}
```

Apply the filter after handling a genuinely empty model plan but before displaying or confirming commands. If all proposed commands are redundant, record `skip_redundant_successes` and proceed to the grounded final summary. If a mixed plan remains, display and execute only `kept`.

- [ ] **Step 4: Add an app-loop regression for retry after correction**

Use four deterministic planning responses with `PlanningMaxRounds=4`:

1. `[false]` fails;
2. `[touch corrected, false]` succeeds then fails;
3. `[touch corrected, false]` repeats the model proposal, but filtering sends only `false` to the executor;
4. an empty command array ends the bounded recovery with a grounded explanation.

Assert `false` reaches the executor three times, `touch corrected` reaches it once, the third displayed/confirmed plan contains only the failed retry, and the final `TurnResult` still contains all real executions when the fourth response has no commands. This proves filtering occurs before plan display and confirmation, not only at the executor boundary, and that a recovery answer does not discard accumulated outcomes.

- [ ] **Step 5: Run affected app tests and verify GREEN**

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app
```

Expected: PASS.

- [ ] **Step 6: Commit retry filtering**

```bash
git add internal/app/main.go internal/app/main_loop_test.go
git commit -m "Allow failed command retries"
```

---

### Task 5: Keep Summaries and Session Memory Grounded

**Files:**
- Modify: `internal/session/session_memory.go:130-270`
- Modify: `internal/session/session_memory_test.go`
- Modify: `internal/executor/executor.go:960-980`
- Modify: `internal/executor/export.go`
- Modify: `internal/executor/executor_test.go:350-405`
- Modify: `internal/app/main.go:790-980`
- Modify: `internal/app/main_loop_test.go`
- Modify: `internal/llm/llm.go:390-430`
- Test: `internal/trace/trace_test.go` or existing app trace helpers

**Interfaces:**
- Consumes: `CommandExecution.ObservationTranscript(limit, strategy)`
- Produces: `staticFallbackAnswer(summary, executions, skipped)`
- Invariant: only exit-code-zero executions create durable effect hints.

- [ ] **Step 1: Write failing session and fallback tests**

Add these cases:

```go
func TestCollectCreatedFilesIgnoresFailedCommands(t *testing.T) {
	executions := []commandExecution{
		{Command: "touch failed.txt", ExitCode: 1},
		{Command: "touch created.txt", ExitCode: 0},
	}
	if got := collectCreatedFiles(executions); !reflect.DeepEqual(got, []string{"created.txt"}) {
		t.Fatalf("collectCreatedFiles() = %v, want only successful file", got)
	}
}
```

Add `TestDetectRuntimeHintIgnoresFailedCommands` with a failed Docker/PHP command followed by an unrelated success, and assert no runtime hint is inferred from the failure.

```go
func TestCollectObservationMemoryIncludesFailedExitCode(t *testing.T) {
	got := collectObservationMemory([]commandExecution{{Command: "false", Purpose: "Fail", ExitCode: 7}}, 400, 4, truncationMixed)
	if len(got) != 1 || !strings.Contains(got[0].Transcript, "Exit code: 7") {
		t.Fatalf("observations = %#v, want failed exit code", got)
	}
}
```

Extend the fallback table with a recovered turn that contains an earlier failure, a later success, and one skipped command. Assert that the fallback reports the failure and omission without saying the task is done.

- [ ] **Step 2: Run focal tests and verify RED**

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/session -run 'Test(CollectCreatedFilesIgnoresFailedCommands|DetectRuntimeHintIgnoresFailedCommands|CollectObservationMemoryIncludesFailedExitCode)$'
env GOCACHE=/tmp/go-build go test -count=1 ./internal/executor -run TestStaticFallbackAnswer
```

Expected: FAIL because durable-effect detection ignores exit codes, empty failures are dropped, and fallback has no skipped-command input.

- [ ] **Step 3: Use the status-bearing transcript in reusable memory**

Use the `ObservationTranscript` helper added by Task 3 in reusable observation memory. Preserve the existing rule that successful empty executions are not stored while retaining failed executions even when both captured streams are empty:

```go
if execution.ExitCode == 0 && !execution.Stdout.HasOutput() && !execution.Stderr.HasOutput() {
	continue
}
transcript := strings.TrimSpace(execution.ObservationTranscript(chars, strategy))
```

- [ ] **Step 4: Restrict durable effect detection to successful executions**

At the start of the `collectCreatedFiles` loop:

```go
if execution.ExitCode != 0 {
	continue
}
```

Apply the same exit-code-zero condition before `detectRuntimeHint` infers a runtime from an execution. Do not add skipped commands to `TurnResult.Executions`, `collectCreatedFiles`, `detectRuntimeHint`, or `collectObservationMemory`.

- [ ] **Step 5: Make static fallback conservative**

Change the signature to:

```go
func staticFallbackAnswer(fallbackSummary string, executions []commandExecution, skipped []skippedCommand) string
```

Before the existing successful-last-command behavior, scan backward for a failure:

```go
for index := len(executions) - 1; index >= 0; index-- {
	execution := executions[index]
	if execution.ExitCode == 0 {
		continue
	}
	preferred := strings.TrimSpace(execution.PreferredOutput())
	status := fmt.Sprintf("The command `%s` failed with exit code %d.",
		strings.TrimSpace(execution.Command), execution.ExitCode)
	if len(skipped) > 0 {
		status += fmt.Sprintf(" %d command(s) were skipped and not executed.", len(skipped))
	}
	if preferred != "" {
		return preferred + "\n" + status
	}
	return status
}
```

When there is no failed execution but `len(skipped)>0`, return the trimmed fallback summary followed by `Some commands were skipped and were not executed.` If the summary is empty, return only that sentence. Never synthesize success for skipped work.

- [ ] **Step 6: Complete trace and turn-result assertions**

Add `skipped_count` to `turn_end`. Assert app traces include:

- `command_skipped` with command, purpose, and reason;
- `continue_after_execution_failure` once per failed batch;
- `execution_failure_replan_excluded` for timeout-only batches;
- `independent_on_failure` in `planner_result` command data.

Run:

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./internal/session ./internal/executor ./internal/app ./internal/llm ./internal/trace
```

Expected: PASS.

- [ ] **Step 7: Commit outcome grounding**

```bash
git add internal/session internal/executor internal/app internal/llm internal/trace
git commit -m "Keep recovery outcomes grounded"
```

---

### Task 6: Document the Behavior and Run Final Gates

**Files:**
- Modify: `README.md:45-60,285-325,415-430,475-490`
- Modify: `internal/config/config.go:530-550`
- Modify: `site/index.html:1135-1145,1173-1180`
- Review: `docs/superpowers/specs/2026-07-14-failure-aware-replanning-design.md`

**Interfaces:**
- Consumes: final implemented behavior and exact field/config names.
- Produces: active user and contributor documentation.

- [ ] **Step 1: Update README behavior and planning controls**

Add a feature bullet:

```markdown
- Failure-aware replanning: ordinary command failures become bounded observations, while dependent later steps are skipped
```

Document `continue_on_error` under planning controls:

```markdown
- `continue_on_error`
  - when `false`, the first failed or timed-out command stops the current batch
  - when `true`, Shellia skips dependent later commands and runs only commands the plan explicitly marks independent of earlier failures
  - ordinary failures trigger bounded replanning; timeouts, cancellations, `!`, and `/shell` do not
```

State that recovery plans retain all normal confirmation and `--yes-safe` rules.

- [ ] **Step 2: Update generated configuration comments**

Replace the current one-line comment with:

```text
# After a command fails, skip dependent steps and continue only steps explicitly marked independent.
# Ordinary failures can trigger bounded replanning; timeouts and cancellations do not.
continue_on_error = false
```

- [ ] **Step 3: Update the public site configuration explanation**

Add this list item beside the other execution controls:

```html
<li><b>continue_on_error</b><span>after a failure, skips dependent steps and runs only explicitly independent ones</span></li>
```

Keep the existing TOML example value `false`.

- [ ] **Step 4: Format and run the complete final verification sequence**

Run exactly:

```bash
gofmt -w ./cmd ./internal
git diff --check
env GOCACHE=/tmp/go-build go test -count=1 ./...
go vet ./...
go build -o shellia ./cmd/shellia
```

Expected: every command exits 0 and the test output contains no `FAIL` lines.

- [ ] **Step 5: Review the complete diff against the acceptance contract**

Verify all of these from code and tests:

- ordinary error forces exactly one round per failed batch;
- timeout-only batch does not replan;
- cancellation exits immediately;
- only same-batch commands marked independent continue;
- recovery plans retain normal confirmations and `yes_safe`;
- successful commands are filtered, failed/skipped commands retry;
- skipped commands appear in UI/trace/current turn/final summary but not session memory;
- manual commands remain unchanged;
- no dependency, config key, flag, persistence, or recovery service was added.

- [ ] **Step 6: Commit documentation and final adjustments**

```bash
git add README.md internal/config/config.go site/index.html
git commit -m "Document failure-aware replanning"
```

- [ ] **Step 7: Confirm clean implementation state**

Run:

```bash
git status --short --branch
```

Expected: no tracked implementation changes remain. Pre-existing untracked personal files may remain and must not be staged.

---

## Final Completion Proof

The implementation is complete only when:

1. Every task commit exists in order.
2. The walking-skeleton test proves failure → observation → recovery with two normal plan confirmations.
3. Executor tests prove dependent skip, independent continuation, timeout exclusion, and cancellation.
4. App tests prove one recovery round per failed batch, bounded continuation, and failed retry behavior.
5. Session and summary tests prove no fake execution or durable effect is inferred.
6. `go test -count=1 ./...`, `go vet ./...`, and `go build -o shellia ./cmd/shellia` all exit 0 after the final material change.
