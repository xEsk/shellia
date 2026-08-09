package app

import (
	"context"
	"strings"
	"testing"

	"github.com/xEsk/shellia/internal/core"
	sessionpkg "github.com/xEsk/shellia/internal/session"
)

// TestRunTurnRepairsActionCompletionWithoutCurrentEvidence checks knowing how
// to perform a requested change cannot terminate the workflow as success.
func TestRunTurnRepairsActionCompletionWithoutCurrentEvidence(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Codex is updated","summary":"Use brew upgrade --cask codex.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"act","success_criteria":"Codex is updated","summary":"Update Codex.","commands":[{"command":"brew upgrade --cask codex","purpose":"Update Codex","risk":"high","requires_confirmation":true}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Codex is updated","summary":"Codex was updated.","completion_basis":{"type":"current_execution","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0

	var result turnResult
	captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "actualitza codex"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 1 || fake.requestCount() != 3 || result.Outcome != turnOutcomeCompleted {
		t.Fatalf("runs = %d, requests = %d, result = %#v; want repaired action, one execution, and causal completion", runs, fake.requestCount(), result)
	}
}

// TestRunTurnDoesNotRepairActByReclassifyingAsExplain checks semantic repair
// cannot escape an executable objective by changing its intent contract.
func TestRunTurnDoesNotRepairActByReclassifyingAsExplain(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Codex is updated","summary":"Use brew upgrade --cask codex.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Explain how to update Codex","summary":"Run brew upgrade --cask codex.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "actualitza codex"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeStructuralError || result.Result == "Run brew upgrade --cask codex." {
		t.Fatalf("result = %#v, want rejected intent drift without false success", result)
	}
}

// TestRunTurnCanRepairIntentWithinExecutableModes checks an incoherent first
// decision does not lock an incorrect objective contract.
func TestRunTurnCanRepairIntentWithinExecutableModes(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Disk changed","summary":"Disk is ready.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Inspect disk.","commands":[{"command":"df -h /","purpose":"Inspect disk","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"There are 18 GB free.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: "18 GB"}}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "consulta l'espai del disc"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 1 || result.Outcome != turnOutcomeCompleted || result.Result != "There are 18 GB free." {
		t.Fatalf("runs = %d, result = %#v, want coherent repaired observe workflow", runs, result)
	}
}

// TestRunTurnDoesNotExposeOfferFromRejectedDecision checks a failed semantic
// repair cannot create hidden executable authority in session memory.
func TestRunTurnDoesNotExposeOfferFromRejectedDecision(t *testing.T) {
	invalid := loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain capability","summary":"Sí, puc fer-ho.","completion_basis":{"type":"current_execution"},"offer":{"objective":"crea hidden-marker","summary":"Crear marcador"},"commands":[]}`}
	fake := newLoopLLMClient(t, invalid, invalid)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "pots crear un marcador?"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeStructuralError || result.Proposal != (pendingProposal{}) {
		t.Fatalf("result = %#v, want structural error without proposal", result)
	}
	if !strings.Contains(result.Result, "No commands were executed") || strings.Contains(output, "output remains available above") {
		t.Fatalf("result=%q output=%q, want an accurate no-execution fallback", result.Result, output)
	}
}

// TestRunTurnDoesNotRepairCapabilityIntoExecutableMode checks semantic repair
// cannot turn a non-authorizing capability question into an executable turn.
func TestRunTurnDoesNotRepairCapabilityIntoExecutableMode(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain marker capability","summary":"Sí, puc crear-lo.","completion_basis":{"type":"current_execution"},"offer":{"objective":"crea marker","summary":"Crear marker"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"act","success_criteria":"Marker exists","summary":"Create marker.","commands":[{"command":"touch marker","purpose":"Create marker","risk":"medium","requires_confirmation":true}]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "pots crear un marcador?"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed || result.Outcome != turnOutcomeStructuralError {
		t.Fatalf("executed = %t, result = %#v, want rejected authority-group drift", executed, result)
	}
}

// TestRunTurnRejectsStalePriorObservationForCurrentQuery checks reusable
// session output is not enough for a fresh mutable-state question unless this
// workflow is explicitly retrying the interrupted objective.
func TestRunTurnRejectsStalePriorObservationForCurrentQuery(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"There were 20 GB free.","completion_basis":{"type":"prior_session_evidence"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Refresh disk state.","commands":[{"command":"df -h /","purpose":"Inspect current disk space","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"There are 18 GB free now.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0
	state := sessionState{
		LastRetryInstruction:     "quant espai queda al disc?",
		LastObservationObjective: "una altra consulta",
		LastObservations:         []core.ObservationMemory{{Command: "df -h /", Purpose: "Old disk state", Transcript: "20 GB"}},
	}

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: "18 GB"}}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, turnRequest{Config: cfg, ContextInfo: &ctxInfo, Instruction: "quant espai queda al disc?", State: state})
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 1 || fake.requestCount() != 3 || result.Result != "There are 18 GB free now." {
		t.Fatalf("runs = %d, requests = %d, result = %#v; want refreshed current observation", runs, fake.requestCount(), result)
	}
}

// TestRunTurnCapabilityOffersWithoutExecuting checks a capability question
// answers and offers a later workflow without crossing the executor boundary.
func TestRunTurnCapabilityOffersWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain whether disk space can be inspected","summary":"Sí, puc consultar-ho amb df -h /.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"consulta l'espai disponible al disc","summary":"Consultar l'espai del disc"},"commands":[]}`})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "pots mirar quant espai queda al disc?")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatal("ExecuteCommands was called for a capability answer")
	}
	if !strings.Contains(output, "Vols que ho executi?") {
		t.Fatalf("output = %q, want canonical execution offer", output)
	}
}

func TestRunTurnCapabilityRepairUsesNonExecutingContractWithStaleHistory(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain whether a newer Codex version can be checked","summary":"I can check it.","completion_basis":{"type":"current_execution"},"offer":{"objective":"check whether a newer Codex version is available","summary":"Check for a Codex update"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain whether a newer Codex version can be checked","summary":"Yes. I can check Homebrew for a newer Codex cask without changing the system.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"check whether a newer Codex version is available","summary":"Check for a Codex update"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false
	request := loopTurnRequest(cfg, &ctxInfo, "pots mirar si hi ha un update nou?")
	request.History = []historyEntry{{Instruction: "actualitza codex", Result: "Codex is already up to date via Homebrew Cask."}}

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, request)
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed || result.Outcome != turnOutcomeCompleted || !strings.Contains(output, "Vols que ho executi?") {
		t.Fatalf("executed=%t result=%#v output=%q, want a completed capability offer", executed, result, output)
	}
	bodies := fake.requestBodies()
	if len(bodies) != 2 || !strings.Contains(bodies[1], "Capability repair contract") || !strings.Contains(bodies[1], "completion_basis.type=model_knowledge") {
		t.Fatalf("repair request bodies=%#v, want an explicit non-executing capability contract", bodies)
	}
}

func TestRunTurnObserveRepairRequiresFreshExecutionWithoutCurrentAttempts(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current Codex update availability observed","summary":"Codex was up to date.","completion_basis":{"type":"prior_session_evidence"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current Codex update availability observed","summary":"Check Homebrew now.","commands":[{"command":"brew outdated --cask codex","purpose":"Check current Codex update availability","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current Codex update availability observed","summary":"No newer Codex cask is available.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	request := loopTurnRequest(cfg, &ctxInfo, "comprova si hi ha una actualització nova")
	request.History = []historyEntry{{Instruction: "actualitza codex", Result: "Codex was up to date earlier."}}
	runs := 0

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, request)
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 1 || result.Outcome != turnOutcomeCompleted {
		t.Fatalf("runs=%d result=%#v, want one fresh observation and completion", runs, result)
	}
	bodies := fake.requestBodies()
	if len(bodies) != 3 || !strings.Contains(bodies[1], "Observe repair contract") || !strings.Contains(bodies[1], "Return action=execute") || !strings.Contains(bodies[1], "Do not use prior_session_evidence") {
		t.Fatalf("repair request bodies=%#v, want a fresh-execution observe contract", bodies)
	}
}

// TestRunInteractiveAcceptsStructuredCapabilityOffer checks an unequivocal
// follow-up starts a fresh workflow whose objective is the offered action.
func TestRunInteractiveAcceptsStructuredCapabilityOffer(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain disk inspection capability","summary":"Sí, puc consultar-ho amb df -h /.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"consulta l'espai disponible al disc","summary":"Consultar l'espai del disc"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Consultaré el disc.","commands":[{"command":"df -h /","purpose":"Consultar l'espai del disc","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Hi ha 20 GB disponibles.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	runs := 0

	captureMainLoopIO(t, "pots mirar quant espai queda al disc?\nsí\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, Stdout: capturedStream{Text: "20 GB"}, ExitCode: 0}}}, nil
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	bodies := fake.requestBodies()
	if runs != 1 || len(bodies) != 3 {
		t.Fatalf("runs = %d, requests = %d, want one accepted execution and three decisions", runs, len(bodies))
	}
	if !strings.Contains(bodies[1], `User instruction:\nconsulta l'espai disponible al disc`) || strings.Contains(bodies[1], `User instruction:\nsí`) {
		t.Fatalf("accepted offer did not become the workflow objective: %q", bodies[1])
	}
	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "pending_proposal_created")) != 1 || len(traceEventsByName(events, "pending_proposal_accepted")) != 1 {
		t.Fatalf("proposal lifecycle events = %#v, want one created and one accepted", events)
	}
}

// TestRunInteractiveAcceptedOfferPreservesRiskClassification checks accepting
// an offer grants only an objective, never pre-authorization for its command.
func TestRunInteractiveAcceptedOfferPreservesRiskClassification(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain file creation capability","summary":"Sí, puc crear el marcador.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"crea el fitxer accepted-risk-marker","summary":"Crear marcador"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"act","success_criteria":"Marker file exists","summary":"Crearé el marcador.","commands":[{"command":"touch accepted-risk-marker","purpose":"Create marker file","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Marker file exists","summary":"Marcador creat.","completion_basis":{"type":"current_execution","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	ctxInfo := loopTestContext(t)
	runs := 0

	captureMainLoopIO(t, "pots crear un marcador?\nsí\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			if len(plans) != 1 || !plans[0].RequiresConfirmation || plans[0].LocalSafe {
				t.Fatalf("accepted offer plan = %#v, want locally risky confirmed command", plans)
			}
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if runs != 1 {
		t.Fatalf("execution batches = %d, want one accepted workflow batch", runs)
	}
}

// TestRunInteractiveDeclinesStructuredCapabilityOffer checks a clear refusal
// consumes the pending offer without invoking either the model or executor.
func TestRunInteractiveDeclinesStructuredCapabilityOffer(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain disk inspection capability","summary":"Sí, puc consultar-ho amb df -h /.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"consulta l'espai disponible al disc","summary":"Consultar l'espai del disc"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	output := captureMainLoopIO(t, "pots mirar quant espai queda al disc?\nno\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if executed || fake.requestCount() != 1 {
		t.Fatalf("executed = %t, requests = %d, want no execution and one capability decision", executed, fake.requestCount())
	}
	if !strings.Contains(output, "D’acord. No ho executaré.") {
		t.Fatalf("output = %q, want proposal decline acknowledgement", output)
	}
}

// TestRunInteractiveRetriesAcceptedOfferObjective checks an accepted offer
// remains retryable as its executable objective rather than as the word "sí".
func TestRunInteractiveRetriesAcceptedOfferObjective(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain disk inspection capability","summary":"Sí, puc consultar-ho.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"consulta l'espai disponible al disc","summary":"Consultar disc"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Consultaré el disc.","commands":[{"command":"df -h /","purpose":"Consultar disc","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"He reutilitzat l'observació parcial.","completion_basis":{"type":"prior_session_evidence"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0

	output := captureMainLoopIO(t, "pots mirar el disc?\nsí\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: "20 GB"}}}}, context.Canceled
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if runs != 1 || fake.requestCount() != 3 {
		t.Fatalf("runs = %d, requests = %d, want cancelled accepted workflow and one retry", runs, fake.requestCount())
	}
	retryBody := fake.requestBodies()[2]
	if !strings.Contains(retryBody, `User instruction:\nconsulta l'espai disponible al disc`) || strings.Contains(retryBody, `User instruction:\nsí`) {
		t.Fatalf("retry prompt = %q, want offered objective", retryBody)
	}
	if !strings.Contains(output, "Retrying: consulta l'espai disponible al disc") {
		t.Fatalf("output = %q, want executable objective in retry status", output)
	}
}

// TestRunInteractiveRetriesAcceptedOfferAfterNilErrorFailure checks terminal
// non-success outcomes arm /retry even when runTurn itself returns nil.
func TestRunInteractiveRetriesAcceptedOfferAfterNilErrorFailure(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain update capability","summary":"Sí, puc actualitzar Codex.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"actualitza codex","summary":"Actualitzar Codex"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Codex is updated","summary":"Run brew upgrade.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Codex is updated","summary":"Run brew upgrade.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"blocked","objective_mode":"act","success_criteria":"Codex is updated","summary":"Package manager unavailable.","blocker_kind":"unavailable","blocker_reason":"No package manager is available.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "pots actualitzar Codex?\nsí\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	bodies := fake.requestBodies()
	if len(bodies) != 4 || !strings.Contains(bodies[3], `User instruction:\nactualitza codex`) {
		t.Fatalf("request bodies = %#v, want retry of accepted objective", bodies)
	}
	if !strings.Contains(output, "Retrying: actualitza codex") {
		t.Fatalf("output = %q, want accepted objective retry status", output)
	}
}

// TestRunInteractiveConsumesOldOfferWhenNewTurnFails checks a later provider
// error cannot leave unrelated executable authority waiting behind "sí".
func TestRunInteractiveConsumesOldOfferWhenNewTurnFails(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain disk capability","summary":"Sí, puc consultar el disc.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"consulta el disc","summary":"Consultar disc"},"commands":[]}`},
		loopLLMResponse{status: 400, content: `{"error":"bad request"}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Answer provided","summary":"No hi ha cap oferta pendent.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "pots mirar el disc?\nexplica els inodes\nsí\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	bodies := fake.requestBodies()
	if len(bodies) != 3 || !strings.Contains(bodies[2], `User instruction:\nsí`) || strings.Contains(bodies[2], `User instruction:\nconsulta el disc`) {
		t.Fatalf("request bodies = %#v, want unresolved yes after failed replacement turn", bodies)
	}
}

// TestRunInteractiveReplacesStructuredCapabilityOffer checks a new completed
// instruction clears the old offer while keeping it visible to the model.
func TestRunInteractiveReplacesStructuredCapabilityOffer(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"capability","success_criteria":"Explain disk inspection capability","summary":"Sí, puc consultar-ho.","completion_basis":{"type":"model_knowledge"},"offer":{"objective":"consulta l'espai disponible al disc","summary":"Consultar disc"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Explain inodes","summary":"Un inode descriu un objecte del filesystem.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "pots mirar el disc?\nquè és un inode?\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	bodies := fake.requestBodies()
	if len(bodies) != 2 || !strings.Contains(bodies[1], "pending_proposal_objective: consulta l'espai disponible al disc") {
		t.Fatalf("request bodies = %#v, want pending proposal visible to replacement decision", bodies)
	}
	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "pending_proposal_replaced")) != 1 {
		t.Fatalf("proposal lifecycle events = %#v, want one replacement", events)
	}
}

// TestRunTurnCompletesWithoutExecutor checks direct answers cannot accidentally
// cross the command-execution boundary.
func TestRunTurnCompletesWithoutExecutor(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"The answer is 42.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatal("ExecuteCommands was called for a complete decision")
	}
	if result.Outcome != turnOutcomeCompleted || result.Result != "The answer is 42." {
		t.Fatalf("runTurn() result = %#v, want completed direct answer", result)
	}
	if !strings.Contains(output, "The answer is 42.") {
		t.Fatalf("output = %q, want final answer", output)
	}
}

func TestRunTurnPlanOnlyKeepsExecutorClosedAcrossIntentModes(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		wantOutcome turnOutcome
	}{
		{name: "act", response: `{"action":"execute","objective_mode":"act","success_criteria":"Marker exists","summary":"Create marker.","commands":[{"command":"touch marker","purpose":"Create marker","risk":"medium","requires_confirmation":true}]}`, wantOutcome: turnOutcomePlanned},
		{name: "observe", response: `{"action":"execute","objective_mode":"observe","success_criteria":"Directory observed","summary":"Inspect directory.","commands":[{"command":"pwd","purpose":"Inspect directory","risk":"safe","requires_confirmation":false}]}`, wantOutcome: turnOutcomePlanned},
		{name: "capability", response: `{"action":"complete","objective_mode":"capability","success_criteria":"Capability explained","summary":"Sí, puc fer-ho.","completion_basis":{"type":"model_knowledge"},"commands":[]}`, wantOutcome: turnOutcomeCompleted},
		{name: "explain", response: `{"action":"complete","objective_mode":"explain","success_criteria":"Method explained","summary":"Així es faria.","completion_basis":{"type":"model_knowledge"},"commands":[]}`, wantOutcome: turnOutcomeCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newLoopLLMClient(t, loopLLMResponse{content: tt.response})
			cfg := loopTestConfig(fake.URL())
			cfg.PlanOnly = true
			ctxInfo := loopTestContext(t)

			var result turnResult
			captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
				deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
					t.Fatal("ExecuteCommands reached in plan-only mode")
					return commandBatchResult{}, nil
				}
				var err error
				result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "test intent"))
				if err != nil {
					t.Fatalf("runTurn() error = %v", err)
				}
			})

			if result.Outcome != tt.wantOutcome || len(result.Executions) != 0 {
				t.Fatalf("result = %#v, want outcome %q without execution", result, tt.wantOutcome)
			}
		})
	}
}

// TestRunTurnReevaluatesObjectiveAfterSuccessfulBatch checks command success
// feeds the next decision instead of ending the objective automatically.
func TestRunTurnReevaluatesObjectiveAfterSuccessfulBatch(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect disk.","commands":[{"command":"df -h","purpose":"Inspect disk space","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"There are 20 GB free.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				Stdout:   capturedStream{Text: "disk-free-marker: 20 GB"},
				ExitCode: 0,
			}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "how much disk is free?"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 1 || fake.requestCount() != 2 {
		t.Fatalf("runs = %d, LLM requests = %d, want 1 and 2", runs, fake.requestCount())
	}
	if result.Outcome != turnOutcomeCompleted || len(result.Executions) != 1 {
		t.Fatalf("runTurn() result = %#v, want completed result with one execution", result)
	}
	bodies := fake.requestBodies()
	if len(bodies) != 2 || !strings.Contains(bodies[1], "disk-free-marker: 20 GB") {
		t.Fatalf("second planning request missing observed evidence: %#v", bodies)
	}
}

// TestRunTurnReturnsActionableBlocker checks missing input remains distinct
// from successful completion and does not invoke the executor.
func TestRunTurnReturnsActionableBlocker(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"blocked","objective_mode":"act","success_criteria":"Test objective completed","summary":"I need the service name.","blocker_kind":"missing_input","blocker_reason":"Specify the service to restart.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			t.Fatal("ExecuteCommands was called for a blocked decision")
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "restart it"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeBlocked || result.BlockerKind != "missing_input" || result.BlockerReason != "Specify the service to restart." {
		t.Fatalf("runTurn() result = %#v, want missing-input blocker", result)
	}
}

// TestRunTurnStopsAfterOneStructuralRepair checks malformed model output has
// a finite repair budget and an explicit non-success outcome.
func TestRunTurnStopsAfterOneStructuralRepair(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"missing action","commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"still missing commands","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect"))
		if err == nil {
			t.Fatal("runTurn() error = nil, want structural repair failure")
		}
	})

	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want exactly one initial request and one repair", fake.requestCount())
	}
	if result.Outcome != turnOutcomeStructuralError {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, turnOutcomeStructuralError)
	}
}

// TestRunTurnUsesOneStructuralRepairForWholeWorkflow checks later rounds cannot reset the repair allowance.
func TestRunTurnUsesOneStructuralRepairForWholeWorkflow(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"missing action","commands":[]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect.","commands":[{"command":"pwd","purpose":"Inspect directory","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"missing action again","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect"))
		if err == nil {
			t.Fatal("runTurn() error = nil, want exhausted workflow repair budget")
		}
	})

	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want one repair total across both rounds", fake.requestCount())
	}
	if result.Outcome != turnOutcomeStructuralError || len(result.Executions) != 1 {
		t.Fatalf("result = %#v, want structural error with partial execution", result)
	}
}

// TestRunTurnAllowsTypedSuccessfulRepeat checks verification can repeat an exact prior success.
func TestRunTurnAllowsTypedSuccessfulRepeat(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect disk.","commands":[{"command":"df -h","purpose":"Inspect disk space","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Verify disk after cleanup.","commands":[{"command":"df -h","purpose":"Verify changed disk space","risk":"safe","requires_confirmation":false,"repeat_reason":"verify_after_change"}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Disk state verified.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[2]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "clean up and verify disk"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 2 || result.Outcome != turnOutcomeCompleted || len(result.Executions) != 2 {
		t.Fatalf("runs = %d, result = %#v, want two admitted executions", runs, result)
	}
}

func TestRunTurnDoesNotExposeMultiRevisionValidationFailure(t *testing.T) {
	invalidCompletion := loopLLMResponse{content: `{"action":"complete","objective_mode":"act","success_criteria":"Codex is updated","summary":"Codex updated.","completion_basis":{"type":"current_execution","evidence_revision":2,"attempt_ids":[1,2,3]},"commands":[]}`}
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"act","success_criteria":"Codex is updated","summary":"Inspect installation.","commands":[{"command":"brew info codex","purpose":"Inspect Codex installation","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"act","success_criteria":"Codex is updated","summary":"Update and verify Codex.","commands":[{"command":"brew upgrade --cask codex","purpose":"Update Codex","risk":"medium","requires_confirmation":true},{"command":"codex --version","purpose":"Verify installed version","risk":"safe","requires_confirmation":false}]}`},
		invalidCompletion,
		invalidCompletion,
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			executions := make([]commandExecution, 0, len(plans))
			for _, plan := range plans {
				executions = append(executions, commandExecution{Command: plan.Command, Purpose: plan.Purpose, ExitCode: 0})
			}
			return commandBatchResult{Executions: executions}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "actualitza codex"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	const internalError = "attempt 1 does not belong to evidence revision 2"
	if result.Outcome != turnOutcomeStructuralError || !strings.Contains(result.BlockerReason, internalError) {
		t.Fatalf("result = %#v, want structural error retaining technical reason", result)
	}
	if strings.Contains(result.Result, internalError) || strings.Contains(output, internalError) {
		t.Fatalf("internal validation error leaked to user: result=%q output=%q", result.Result, output)
	}
	if !strings.Contains(result.Result, "could not validate the model's final response") {
		t.Fatalf("result.Result = %q, want actionable validation failure", result.Result)
	}
}

// TestRunTurnRepairsThenStopsNoProgress checks an unexplained success duplicate gets one repair round.
func TestRunTurnRepairsThenStopsNoProgress(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect disk.","commands":[{"command":"df -h","purpose":"Inspect disk space","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect disk again.","commands":[{"command":"df -h","purpose":"Repeat without cause","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Still inspect disk again.","commands":[{"command":"df -h","purpose":"Repeat without cause","risk":"safe","requires_confirmation":false}]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runs := 0

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			runs++
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect disk once"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if runs != 1 || fake.requestCount() != 3 || result.Outcome != turnOutcomeNoProgress {
		t.Fatalf("runs = %d, requests = %d, result = %#v", runs, fake.requestCount(), result)
	}
	if !strings.Contains(fake.requestBodies()[2], repeatReasonRequired) {
		t.Fatalf("repair prompt missing repetition conflict: %q", fake.requestBodies()[2])
	}
}

// TestRunTurnTraceCapturesWorkflowLifecycle checks authority, attempts, evidence, decisions, and outcome are diagnosable.
func TestRunTurnTraceCapturesWorkflowLifecycle(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect.","commands":[{"command":"pwd","purpose":"Inspect directory","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspection complete.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	starts := traceEventsByName(events, "turn_start")
	if len(starts) != 1 || traceEventData(t, starts[0])["execution_allowed"] != true {
		t.Fatalf("turn_start = %#v, want execution authority", starts)
	}
	if len(traceEventsByName(events, "workflow_attempt")) != 1 {
		t.Fatalf("workflow_attempt events = %d, want 1", len(traceEventsByName(events, "workflow_attempt")))
	}
	revisions := traceEventsByName(events, "evidence_revision")
	if len(revisions) != 1 || traceEventData(t, revisions[0])["after"] != float64(1) {
		t.Fatalf("evidence_revision events = %#v, want revision 1", revisions)
	}
	ends := traceEventsByName(events, "turn_end")
	if len(ends) != 1 || traceEventData(t, ends[0])["outcome"] != string(turnOutcomeCompleted) {
		t.Fatalf("turn_end = %#v, want completed", ends)
	}
}

// TestMissingInputFollowUpCarriesBlockerUntilCompletion checks session projection preserves causal context across turns.
func TestMissingInputFollowUpCarriesBlockerUntilCompletion(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"blocked","objective_mode":"act","success_criteria":"Test objective completed","summary":"I need the service name.","blocker_kind":"missing_input","blocker_reason":"Specify the service to restart.","commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"nginx is the selected service.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	state := sessionState{}

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		first, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "restart it"))
		if err != nil {
			t.Fatalf("first runTurn() error = %v", err)
		}
		sessionpkg.UpdateState(&state, "restart it", first, cfg)
		second, err := runTurn(t.Context(), deps, false, turnRequest{
			Config:      cfg,
			ContextInfo: &ctxInfo,
			Instruction: "nginx",
			State:       state,
		})
		if err != nil {
			t.Fatalf("second runTurn() error = %v", err)
		}
		sessionpkg.UpdateState(&state, "nginx", second, cfg)
	})

	bodies := fake.requestBodies()
	if len(bodies) != 2 || !strings.Contains(bodies[1], "last_blocker_kind: missing_input") || !strings.Contains(bodies[1], "Specify the service to restart.") {
		t.Fatalf("follow-up prompt lost blocker context: %#v", bodies)
	}
	if state.PendingIntent != "" || state.LastBlockerKind != "" || state.LastBlockerReason != "" {
		t.Fatalf("completed follow-up retained blocker: %#v", state)
	}
}
