package session

import (
	"reflect"
	"strings"
	"testing"

	"github.com/xEsk/shellia/internal/core"
)

func TestMergeRecentUnique(t *testing.T) {
	t.Run("appends new items", func(t *testing.T) {
		got := mergeRecentUnique([]string{"a"}, []string{"b"}, 10)
		if len(got) != 2 || got[1] != "b" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("deduplicates by moving to end", func(t *testing.T) {
		got := mergeRecentUnique([]string{"a", "b", "c"}, []string{"a"}, 10)
		if got[len(got)-1] != "a" || len(got) != 3 {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		got := mergeRecentUnique([]string{"a", "b", "c"}, []string{"d"}, 3)
		if len(got) != 3 || got[0] != "b" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("skips empty strings", func(t *testing.T) {
		got := mergeRecentUnique(nil, []string{"", "  ", "a"}, 10)
		if len(got) != 1 || got[0] != "a" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("nil existing", func(t *testing.T) {
		got := mergeRecentUnique(nil, []string{"x"}, 5)
		if len(got) != 1 || got[0] != "x" {
			t.Fatalf("got %v", got)
		}
	})
}

func TestSanitizePathToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"src/main.go"`, "src/main.go"},
		{"'config.toml'", "config.toml"},
		{"(file.txt)", "file.txt"},
		{"path/to/file,", "path/to/file"},
		{"  spaced  ", "spaced"},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizePathToken(c.in); got != c.want {
			t.Errorf("sanitizePathToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractReferencedFiles(t *testing.T) {
	t.Run("finds dotted files", func(t *testing.T) {
		files := extractReferencedFiles("edit config.toml and main.go")
		if len(files) != 2 {
			t.Fatalf("got %v", files)
		}
	})

	t.Run("finds paths with slashes", func(t *testing.T) {
		files := extractReferencedFiles("open src/main.go please")
		found := false
		for _, f := range files {
			if strings.Contains(f, "src/main.go") {
				found = true
			}
		}
		if !found {
			t.Fatalf("src/main.go not found in %v", files)
		}
	})

	t.Run("ignores plain words", func(t *testing.T) {
		files := extractReferencedFiles("run git status")
		if len(files) != 0 {
			t.Fatalf("expected no files, got %v", files)
		}
	})
}

func TestNormalizeForMemory(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello, World!", "hello world"},
		{"it's OK", "its ok"},
		{"(do) it; now.", "do it now"},
		{"", ""},
		{"  multiple   spaces  ", "multiple spaces"},
	}
	for _, c := range cases {
		if got := normalizeForMemory(c.in); got != c.want {
			t.Errorf("normalizeForMemory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsProposalDecline(t *testing.T) {
	for _, instruction := range []string{"no", "no gràcies", "ara no", "cancel·la"} {
		if !isProposalDecline(instruction) {
			t.Errorf("isProposalDecline(%q) = false, want true", instruction)
		}
	}
	for _, instruction := range []string{"no sé", "no funciona", "fes una altra cosa"} {
		if isProposalDecline(instruction) {
			t.Errorf("isProposalDecline(%q) = true, want false", instruction)
		}
	}
}

func TestShouldPromotePendingIntent(t *testing.T) {
	t.Run("empty instruction returns false", func(t *testing.T) {
		if shouldPromotePendingIntent("", turnResult{}) {
			t.Error("expected false for empty instruction")
		}
	})

	t.Run("no plans promotes non-empty instruction", func(t *testing.T) {
		if !shouldPromotePendingIntent("deploy the app", turnResult{}) {
			t.Error("expected true for non-empty instruction")
		}
	})

	t.Run("with plans promotes non-empty instruction", func(t *testing.T) {
		turn := turnResult{Plans: []commandPlan{{Command: "make build"}}}
		if !shouldPromotePendingIntent("zeige den status", turn) {
			t.Error("expected true for non-empty instruction in any language")
		}
	})
}

func TestCollectCreatedFiles(t *testing.T) {
	execs := []commandExecution{
		{Command: "touch foo.txt"},
		{Command: "mkdir mydir"},
		{Command: "ls -la"},
		{Command: ""},
	}
	got := collectCreatedFiles(execs)
	if len(got) != 2 {
		t.Fatalf("expected 2 created files, got %v", got)
	}
	if got[0] != "foo.txt" || got[1] != "mydir" {
		t.Fatalf("unexpected files: %v", got)
	}
}

func TestCollectCreatedFilesIgnoresFailedCommands(t *testing.T) {
	executions := []commandExecution{
		{Command: "touch failed.txt", ExitCode: 1},
		{Command: "touch created.txt", ExitCode: 0},
	}
	if got := collectCreatedFiles(executions); !reflect.DeepEqual(got, []string{"created.txt"}) {
		t.Fatalf("collectCreatedFiles() = %v, want only successful file", got)
	}
}

func TestCollectObservationMemory(t *testing.T) {
	t.Run("empty executions returns empty", func(t *testing.T) {
		if got := collectObservationMemory(nil, 400, 4, truncationMixed); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("limits to maxObservationEntries", func(t *testing.T) {
		const limit = 4
		execs := make([]commandExecution, limit+2)
		for i := range execs {
			execs[i] = commandExecution{Command: "ls", Stdout: capturedStream{Text: "file.txt", TotalBytes: 8, KeptBytes: 8}}
		}
		got := collectObservationMemory(execs, 400, limit, truncationMixed)
		if len(got) > limit {
			t.Fatalf("expected at most %d, got %d", limit, len(got))
		}
	})

	t.Run("skips empty output", func(t *testing.T) {
		execs := []commandExecution{
			{Command: "ls", Stdout: capturedStream{}},
			{Command: "pwd", Stdout: capturedStream{Text: "/home/user", TotalBytes: 10, KeptBytes: 10}},
		}
		got := collectObservationMemory(execs, 400, 4, truncationMixed)
		if len(got) != 1 || got[0].Command != "pwd" {
			t.Fatalf("unexpected result: %v", got)
		}
	})
}

func TestCollectObservationMemoryIncludesFailedExitCode(t *testing.T) {
	got := collectObservationMemory([]commandExecution{{Command: "false", Purpose: "Fail", ExitCode: 7}}, 400, 4, truncationMixed)
	if len(got) != 1 || !strings.Contains(got[0].Transcript, "Exit code: 7") {
		t.Fatalf("observations = %#v, want failed exit code", got)
	}
}

func TestDetectRuntimeHint(t *testing.T) {
	t.Run("detects docker in instruction", func(t *testing.T) {
		if got := detectRuntimeHint("run docker compose up", turnResult{}); got == "" {
			t.Error("expected non-empty hint")
		}
	})

	t.Run("detects php8 in instruction", func(t *testing.T) {
		if got := detectRuntimeHint("run with php8", turnResult{}); got == "" {
			t.Error("expected non-empty hint")
		}
	})

	t.Run("ignores unexecuted docker plan", func(t *testing.T) {
		turn := turnResult{Plans: []commandPlan{{Command: "docker exec app php artisan"}}}
		if got := detectRuntimeHint("run migration", turn); got != "" {
			t.Fatalf("detectRuntimeHint() = %q, want no hint from unexecuted plan", got)
		}
	})

	t.Run("ignores skipped php plan", func(t *testing.T) {
		turn := turnResult{
			Plans:      []commandPlan{{Command: "php artisan migrate"}},
			Executions: []commandExecution{{Command: "git status --short", ExitCode: 0}},
		}
		if got := detectRuntimeHint("run migration", turn); got != "" {
			t.Fatalf("detectRuntimeHint() = %q, want no hint from skipped plan", got)
		}
	})

	t.Run("detects docker in successful execution", func(t *testing.T) {
		turn := turnResult{
			Plans:      []commandPlan{{Command: "docker exec app php artisan"}},
			Executions: []commandExecution{{Command: "docker exec app php artisan", ExitCode: 0}},
		}
		if got := detectRuntimeHint("run migration", turn); got == "" {
			t.Error("expected non-empty hint from successful execution")
		}
	})

	t.Run("returns empty for unrelated instruction", func(t *testing.T) {
		if got := detectRuntimeHint("run git status", turnResult{}); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestDetectRuntimeHintIgnoresFailedCommands(t *testing.T) {
	turn := turnResult{
		Plans: []commandPlan{{Command: "docker exec app php artisan migrate"}},
		Executions: []commandExecution{
			{Command: "docker exec app php artisan migrate", ExitCode: 1},
			{Command: "git status --short", ExitCode: 0},
		},
	}
	if got := detectRuntimeHint("run migration", turn); got != "" {
		t.Fatalf("detectRuntimeHint() = %q, want no hint from failed command", got)
	}
}

func TestResolveInstructionForPlanning(t *testing.T) {
	t.Run("returns instruction unchanged when no context", func(t *testing.T) {
		got := resolveInstructionForPlanning("deploy the app", sessionState{})
		if got != "deploy the app" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("accepts pending structured proposal", func(t *testing.T) {
		state := sessionState{PendingProposal: pendingProposal{Objective: "consulta l'espai disponible al disc", Summary: "Consultar disc"}}
		got := resolveInstructionForPlanning("sí", state)
		if got != "consulta l'espai disponible al disc" {
			t.Errorf("got %q, want pending proposal objective", got)
		}
	})

	t.Run("accepts Catalan execute pronoun", func(t *testing.T) {
		state := sessionState{PendingProposal: pendingProposal{Objective: "executa el canvi"}}
		if got := resolveInstructionForPlanning("executa’l", state); got != "executa el canvi" {
			t.Errorf("got %q, want pending proposal objective", got)
		}
	})

	t.Run("does not invent action without pending proposal", func(t *testing.T) {
		got := resolveInstructionForPlanning("sí", sessionState{})
		if got != "sí" {
			t.Errorf("got %q, want original instruction", got)
		}
	})

	t.Run("does not accept an ambiguous follow-up", func(t *testing.T) {
		state := sessionState{PendingProposal: pendingProposal{Objective: "executa el canvi"}}
		instruction := "ok, quins passos són?"
		if got := resolveInstructionForPlanning(instruction, state); got != instruction {
			t.Errorf("got %q, want ambiguous prompt unchanged", got)
		}
	})

	t.Run("does not expand reference follow-up locally", func(t *testing.T) {
		state := sessionState{PendingIntent: "deploy the app to production"}
		got := resolveInstructionForPlanning("weiter damit", state)
		if got != "weiter damit" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("trims empty instruction", func(t *testing.T) {
		got := resolveInstructionForPlanning("  ", sessionState{})
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

// TestUpdateSessionStateStoresStructuredProposal checks capability offers
// survive completed answers without relying on Markdown command extraction.
func TestUpdateSessionStateStoresStructuredProposal(t *testing.T) {
	state := &sessionState{}
	updateSessionState(state, "pots mirar el disc?", turnResult{
		Outcome:  turnOutcomeCompleted,
		Proposal: pendingProposal{Objective: "consulta l'espai disponible al disc", Summary: "Consultar disc"},
	}, MemoryOptions{})

	if state.PendingProposal.Objective != "consulta l'espai disponible al disc" {
		t.Fatalf("PendingProposal = %#v, want structured offer", state.PendingProposal)
	}
}

func TestUpdateSessionStatePromotesPlannedExecuteProposal(t *testing.T) {
	state := &sessionState{PendingIntent: "prepara el canvi", PendingIntentPlanOnly: true}
	updateSessionState(state, "prepara el canvi", turnResult{
		Outcome: turnOutcomePlanned,
		Proposal: pendingProposal{
			Mode:      core.ProposalModeExecute,
			Objective: "aplica el canvi",
			Summary:   "Aplicar el canvi",
		},
	}, MemoryOptions{PlanOnly: true})

	if state.PendingProposal.Mode != core.ProposalModeExecute || state.PendingIntent != "" || state.PendingIntentPlanOnly {
		t.Fatalf("state = %#v, want planned execute proposal with cleared plan-only continuation", state)
	}
}

func TestUpdateSessionStateReplacesPendingProposalOnNewCompletedInstruction(t *testing.T) {
	state := &sessionState{PendingProposal: pendingProposal{Objective: "consulta el disc", Summary: "Consultar disc"}}
	updateSessionState(state, "explica'm què és un inode", turnResult{Outcome: turnOutcomeCompleted}, MemoryOptions{})

	if state.PendingProposal != (pendingProposal{}) {
		t.Fatalf("PendingProposal = %#v, want cleared by new completed instruction", state.PendingProposal)
	}
}

func TestUpdateSessionStateRejectsProposalFromNonCompletedOutcome(t *testing.T) {
	state := &sessionState{PendingProposal: pendingProposal{Objective: "old objective", Summary: "Old offer"}}
	updateSessionState(state, "invalid capability turn", turnResult{
		Outcome:  turnOutcomeStructuralError,
		Proposal: pendingProposal{Objective: "hidden objective", Summary: "Hidden offer"},
	}, MemoryOptions{})

	if state.PendingProposal != (pendingProposal{}) {
		t.Fatalf("PendingProposal = %#v, want no offer from non-completed outcome", state.PendingProposal)
	}
}

func TestManualExecutionInvalidatesRetryObservationBinding(t *testing.T) {
	state := &sessionState{
		LastRetryInstruction:     "consulta el disc",
		LastObservationObjective: "consulta el disc",
		LastObservations:         []observationMemory{{Command: "df -h /", Purpose: "Inspect disk", Transcript: "20 GB"}},
	}
	updateSessionStateFromExecution(state, "pwd", commandExecution{
		Command: "pwd",
		Purpose: "Inspect directory",
		Stdout:  capturedStream{Text: "/tmp"},
	}, MemoryOptions{MemoryObservationChars: 200, MaxObservationEntries: 4})

	if state.LastObservationObjective != "" {
		t.Fatalf("LastObservationObjective = %q, want manual evidence detached from retry objective", state.LastObservationObjective)
	}
}

func TestOutputlessExecutionInvalidatesRetryObservationBinding(t *testing.T) {
	state := &sessionState{
		LastRetryInstruction:     "consulta el disc",
		LastObservationObjective: "consulta el disc",
		LastObservations:         []observationMemory{{Command: "df -h /", Purpose: "Inspect disk", Transcript: "20 GB"}},
	}
	updateSessionState(state, "mutate state", turnResult{
		Outcome:    turnOutcomeBlocked,
		Executions: []commandExecution{{Command: "touch marker", Purpose: "Mutate state", ExitCode: 0}},
	}, MemoryOptions{MemoryObservationChars: 200, MaxObservationEntries: 4})

	if state.LastObservationObjective != "" {
		t.Fatalf("LastObservationObjective = %q, want outputless execution to invalidate old evidence", state.LastObservationObjective)
	}
}

func TestUpdateSessionState(t *testing.T) {
	t.Run("nil state is a no-op", func(t *testing.T) {
		updateSessionState(nil, "anything", turnResult{}, MemoryOptions{})
	})

	t.Run("sets pending intent", func(t *testing.T) {
		state := &sessionState{}
		updateSessionState(state, "deploy to prod", turnResult{Outcome: turnOutcomeBlocked, BlockerKind: "missing_input", BlockerReason: "Need target"}, MemoryOptions{})
		if state.PendingIntent != "deploy to prod" {
			t.Errorf("got %q", state.PendingIntent)
		}
	})

	t.Run("stores referenced file from instruction", func(t *testing.T) {
		state := &sessionState{}
		updateSessionState(state, "edit src/main.go now", turnResult{}, MemoryOptions{})
		if state.LastReferencedFile == "" {
			t.Error("expected LastReferencedFile to be set")
		}
	})
}

// TestUpdateSessionStateCarriesMissingInputUntilCompletion checks follow-ups retain and resolve a blocker explicitly.
func TestUpdateSessionStateCarriesMissingInputUntilCompletion(t *testing.T) {
	state := &sessionState{LastObservations: []observationMemory{{Command: "df -h", Purpose: "Inspect disk", Transcript: "20 GB free"}}}
	updateSessionState(state, "restart it", turnResult{
		Outcome:       turnOutcomeBlocked,
		BlockerKind:   "missing_input",
		BlockerReason: "Specify the service name.",
	}, MemoryOptions{})

	if state.PendingIntent != "restart it" || state.LastBlockerKind != "missing_input" || state.LastBlockerReason != "Specify the service name." {
		t.Fatalf("blocked state = %#v", state)
	}
	if len(state.LastObservations) != 1 || state.LastObservations[0].Command != "df -h" {
		t.Fatalf("blocked state lost prior evidence = %#v", state.LastObservations)
	}

	updateSessionState(state, "restart nginx", turnResult{Outcome: turnOutcomeTimeout}, MemoryOptions{})
	if state.LastBlockerKind != "missing_input" || state.LastBlockerReason != "Specify the service name." {
		t.Fatalf("non-complete follow-up lost blocker = %#v", state)
	}

	updateSessionState(state, "restart nginx", turnResult{Outcome: turnOutcomeCompleted, Result: "nginx restarted"}, MemoryOptions{})
	if state.PendingIntent != "" || state.LastBlockerKind != "" || state.LastBlockerReason != "" {
		t.Fatalf("completed state retained blocker = %#v", state)
	}
}

// TestUpdateSessionStateKeepsNonSuccessfulOutcomePending checks deterministic stops are not projected as success.
func TestUpdateSessionStateKeepsNonSuccessfulOutcomePending(t *testing.T) {
	state := &sessionState{}
	updateSessionState(state, "restart nginx", turnResult{Outcome: turnOutcomeTimeout}, MemoryOptions{})
	if state.PendingIntent != "restart nginx" {
		t.Fatalf("PendingIntent = %q, want unfinished timeout instruction", state.PendingIntent)
	}
}
