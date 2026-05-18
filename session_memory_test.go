package main

import (
	"strings"
	"testing"
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

func TestLooksPreparatory(t *testing.T) {
	preparatory := []string{
		"show the files",
		"list all dirs",
		"cat the config",
		"create a test file",
		"check the logs",
	}
	for _, s := range preparatory {
		if !looksPreparatory(s) {
			t.Errorf("expected looksPreparatory(%q) = true", s)
		}
	}

	notPreparatory := []string{
		"deploy the app",
		"run the build",
		"install dependencies",
	}
	for _, s := range notPreparatory {
		if looksPreparatory(s) {
			t.Errorf("expected looksPreparatory(%q) = false", s)
		}
	}
}

func TestLooksLikeAffirmativeFollowUp(t *testing.T) {
	affirmatives := []string{"ok", "yes", "si", "do it", "vale", "fesho", "llista"}
	for _, s := range affirmatives {
		if !looksLikeAffirmativeFollowUp(s) {
			t.Errorf("expected looksLikeAffirmativeFollowUp(%q) = true", s)
		}
	}

	not := []string{"", "not now", "stop", "cancel"}
	for _, s := range not {
		if looksLikeAffirmativeFollowUp(s) {
			t.Errorf("expected looksLikeAffirmativeFollowUp(%q) = false", s)
		}
	}
}

func TestLooksLikeReferenceFollowUp(t *testing.T) {
	refs := []string{"do it now", "before we start", "yes ok", "continue"}
	for _, s := range refs {
		if !looksLikeReferenceFollowUp(s) {
			t.Errorf("expected looksLikeReferenceFollowUp(%q) = true", s)
		}
	}

	not := []string{"deploy to production", "run the tests", "build the binary"}
	for _, s := range not {
		if looksLikeReferenceFollowUp(s) {
			t.Errorf("expected looksLikeReferenceFollowUp(%q) = false", s)
		}
	}
}

func TestShouldPromotePendingIntent(t *testing.T) {
	t.Run("empty instruction returns false", func(t *testing.T) {
		if shouldPromotePendingIntent("", turnResult{}) {
			t.Error("expected false for empty instruction")
		}
	})

	t.Run("no plans always promotes", func(t *testing.T) {
		if !shouldPromotePendingIntent("deploy the app", turnResult{}) {
			t.Error("expected true when no plans")
		}
	})

	t.Run("with plans: non-preparatory promotes", func(t *testing.T) {
		turn := turnResult{Plans: []commandPlan{{Command: "make build"}}}
		if !shouldPromotePendingIntent("deploy to production", turn) {
			t.Error("expected true for non-preparatory instruction")
		}
	})

	t.Run("with plans: preparatory does not promote", func(t *testing.T) {
		turn := turnResult{Plans: []commandPlan{{Command: "ls"}}}
		if shouldPromotePendingIntent("list the files", turn) {
			t.Error("expected false for preparatory instruction")
		}
	})
}

func TestLooksLikeShellCommand(t *testing.T) {
	shell := []string{"git status", "ls -la", "docker ps", "go build ./...", "brew install jq"}
	for _, s := range shell {
		if !looksLikeShellCommand(s) {
			t.Errorf("expected looksLikeShellCommand(%q) = true", s)
		}
	}

	not := []string{"", "some text", "unknown_cmd arg", "multi\nline"}
	for _, s := range not {
		if looksLikeShellCommand(s) {
			t.Errorf("expected looksLikeShellCommand(%q) = false", s)
		}
	}
}

func TestDetectSuggestedCommand(t *testing.T) {
	t.Run("extracts backtick shell command from result", func(t *testing.T) {
		turn := turnResult{Result: "You can run `git status` to check."}
		if got := detectSuggestedCommand(turn); got != "git status" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("extracts from summary if not in result", func(t *testing.T) {
		turn := turnResult{Summary: "Try `docker ps` to list containers."}
		if got := detectSuggestedCommand(turn); got != "docker ps" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("ignores non-shell backtick content", func(t *testing.T) {
		turn := turnResult{Result: "The variable `foo` is undefined."}
		if got := detectSuggestedCommand(turn); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("empty turn returns empty", func(t *testing.T) {
		if got := detectSuggestedCommand(turnResult{}); got != "" {
			t.Errorf("expected empty, got %q", got)
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

func TestCollectObservationMemory(t *testing.T) {
	t.Run("empty executions returns empty", func(t *testing.T) {
		if got := collectObservationMemory(nil); len(got) != 0 {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("limits to maxObservationEntries", func(t *testing.T) {
		execs := make([]commandExecution, maxObservationEntries+2)
		for i := range execs {
			execs[i] = commandExecution{Command: "ls", Stdout: capturedStream{Text: "file.txt", TotalBytes: 8, KeptBytes: 8}}
		}
		got := collectObservationMemory(execs)
		if len(got) > maxObservationEntries {
			t.Fatalf("expected at most %d, got %d", maxObservationEntries, len(got))
		}
	})

	t.Run("skips empty output", func(t *testing.T) {
		execs := []commandExecution{
			{Command: "ls", Stdout: capturedStream{}},
			{Command: "pwd", Stdout: capturedStream{Text: "/home/user", TotalBytes: 10, KeptBytes: 10}},
		}
		got := collectObservationMemory(execs)
		if len(got) != 1 || got[0].Command != "pwd" {
			t.Fatalf("unexpected result: %v", got)
		}
	})
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

	t.Run("detects docker in plan commands", func(t *testing.T) {
		turn := turnResult{Plans: []commandPlan{{Command: "docker exec app php artisan"}}}
		if got := detectRuntimeHint("run migration", turn); got == "" {
			t.Error("expected non-empty hint from plan")
		}
	})

	t.Run("returns empty for unrelated instruction", func(t *testing.T) {
		if got := detectRuntimeHint("run git status", turnResult{}); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestResolveInstructionForPlanning(t *testing.T) {
	t.Run("returns instruction unchanged when no context", func(t *testing.T) {
		got := resolveInstructionForPlanning("deploy the app", sessionState{})
		if got != "deploy the app" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("expands affirmative follow-up to suggested command", func(t *testing.T) {
		state := sessionState{LastSuggestedCommand: "git push origin main"}
		got := resolveInstructionForPlanning("ok", state)
		if !strings.Contains(got, "git push origin main") {
			t.Errorf("expected suggested command in result, got %q", got)
		}
	})

	t.Run("expands reference follow-up to pending intent", func(t *testing.T) {
		state := sessionState{PendingIntent: "deploy the app to production"}
		got := resolveInstructionForPlanning("yes do it", state)
		if !strings.Contains(got, "deploy the app to production") {
			t.Errorf("expected pending intent in result, got %q", got)
		}
	})

	t.Run("trims empty instruction", func(t *testing.T) {
		got := resolveInstructionForPlanning("  ", sessionState{})
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestUpdateSessionState(t *testing.T) {
	t.Run("nil state is a no-op", func(t *testing.T) {
		updateSessionState(nil, "anything", turnResult{})
	})

	t.Run("sets pending intent", func(t *testing.T) {
		state := &sessionState{}
		updateSessionState(state, "deploy to prod", turnResult{Actionable: true})
		if state.PendingIntent != "deploy to prod" {
			t.Errorf("got %q", state.PendingIntent)
		}
	})

	t.Run("clears suggested command on actionable turn", func(t *testing.T) {
		state := &sessionState{LastSuggestedCommand: "git status"}
		updateSessionState(state, "run build", turnResult{Actionable: true})
		if state.LastSuggestedCommand != "" {
			t.Errorf("expected cleared command, got %q", state.LastSuggestedCommand)
		}
	})

	t.Run("stores referenced file from instruction", func(t *testing.T) {
		state := &sessionState{}
		updateSessionState(state, "edit src/main.go now", turnResult{})
		if state.LastReferencedFile == "" {
			t.Error("expected LastReferencedFile to be set")
		}
	})
}
