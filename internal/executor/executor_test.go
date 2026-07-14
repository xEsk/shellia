package executor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"
)

// TestDetectInteractivePromptMatchesConfirmationPrompt checks that a trailing yes/no prompt is detected.
func TestDetectInteractivePromptMatchesConfirmationPrompt(t *testing.T) {
	prompt, ok := detectInteractivePrompt("WARNING!\nAre you sure you want to continue? [y/N] ")
	if !ok {
		t.Fatalf("detectInteractivePrompt() = false, want true")
	}
	if prompt != "Are you sure you want to continue? [y/N]" {
		t.Fatalf("detectInteractivePrompt() prompt = %q", prompt)
	}
}

// TestDetectInteractivePromptMatchesLooseConfirmationVariants checks common spacing and casing variants.
func TestDetectInteractivePromptMatchesLooseConfirmationVariants(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "spaced bracket choice", input: "Are you sure? [ Y / n ] "},
		{name: "parenthesized yes no", input: "continue? (YES / no)"},
		{name: "pipe separated choice", input: "Proceed? [ n | Y ] "},
		{name: "space separated choice", input: "Install dependencies? [y    n] "},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if prompt, ok := detectInteractivePrompt(tt.input); !ok {
				t.Fatalf("detectInteractivePrompt(%q) = false, want true; prompt %q", tt.input, prompt)
			}
		})
	}
}

// TestDetectInteractivePromptMatchesLooseCredentialPrompt checks credential prompts with unusual spacing and casing.
func TestDetectInteractivePromptMatchesLooseCredentialPrompt(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "password", input: "Password:"},
		{name: "spaced password", input: "password : "},
		{name: "passphrase", input: "Enter PASSPHRASE   :"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if prompt, ok := detectInteractivePrompt(tt.input); !ok {
				t.Fatalf("detectInteractivePrompt(%q) = false, want true; prompt %q", tt.input, prompt)
			}
		})
	}
}

// TestDetectInteractivePromptMatchesContinuePrompt checks common pause prompts.
func TestDetectInteractivePromptMatchesContinuePrompt(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "enter", input: "Press Enter to continue"},
		{name: "return", input: "press return to continue "},
		{name: "any key", input: "Press any key to continue..."},
		{name: "a key", input: "Press a key to continue"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if prompt, ok := detectInteractivePrompt(tt.input); !ok {
				t.Fatalf("detectInteractivePrompt(%q) = false, want true; prompt %q", tt.input, prompt)
			}
		})
	}
}

// TestDetectInteractivePromptMatchesTextConfirmationPrompt checks typed confirmation prompts.
func TestDetectInteractivePromptMatchesTextConfirmationPrompt(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "delete token", input: `Type DELETE to confirm`},
		{name: "quoted yes", input: `type "yes" to continue`},
		{name: "quoted destroy", input: `Type 'destroy' to proceed`},
		{name: "repo token", input: `Please type repo-name to delete`},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if prompt, ok := detectInteractivePrompt(tt.input); !ok {
				t.Fatalf("detectInteractivePrompt(%q) = false, want true; prompt %q", tt.input, prompt)
			}
		})
	}
}

// TestDetectInteractivePromptIgnoresCompletedQuestionLine checks that historical output lines are not treated as active prompts.
func TestDetectInteractivePromptIgnoresCompletedQuestionLine(t *testing.T) {
	if prompt, ok := detectInteractivePrompt("Overwrite existing file? [y/N]\n"); ok {
		t.Fatalf("detectInteractivePrompt() = true with prompt %q, want false", prompt)
	}
}

// TestDetectInteractivePromptIgnoresCompletedContinueLine checks completed pause prompts are not treated as active.
func TestDetectInteractivePromptIgnoresCompletedContinueLine(t *testing.T) {
	if prompt, ok := detectInteractivePrompt("Press Enter to continue\n"); ok {
		t.Fatalf("detectInteractivePrompt() = true with prompt %q, want false", prompt)
	}
}

// TestInteractivePromptDetectorHandlesChunkedOutput checks prompts split across writes.
func TestInteractivePromptDetectorHandlesChunkedOutput(t *testing.T) {
	cancelled := false
	detector := newInteractivePromptDetector("docker image prune -a", func() {
		cancelled = true
	})

	if _, err := detector.Write([]byte("Are you sure you want")); err != nil {
		t.Fatalf("Write() first chunk error = %v", err)
	}
	if _, err := detector.Write([]byte(" to continue? [y/N] ")); err != nil {
		t.Fatalf("Write() second chunk error = %v", err)
	}

	if !cancelled {
		t.Fatalf("detector did not cancel the command after detecting a prompt")
	}
	if err := detector.promptError(); err == nil {
		t.Fatalf("promptError() = nil, want interactivePromptError")
	}
}

// TestAppendInteractivePromptTailKeepsValidUTF8 checks byte trimming does not split runes.
func TestAppendInteractivePromptTailKeepsValidUTF8(t *testing.T) {
	got := appendInteractivePromptTail("prefix", "€ Password:", 12)
	if !utf8.ValidString(got) {
		t.Fatalf("appendInteractivePromptTail() returned invalid UTF-8: %q", got)
	}
	if _, ok := detectInteractivePrompt(got); !ok {
		t.Fatalf("detectInteractivePrompt(%q) = false, want true", got)
	}
}

// TestLimitedCaptureWriterStreamReplacesInvalidUTF8 checks truncated captures remain valid strings.
func TestLimitedCaptureWriterStreamReplacesInvalidUTF8(t *testing.T) {
	writer := &limitedCaptureWriter{limit: 2}
	if _, err := writer.Write([]byte("€")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	stream := writer.Stream()
	if !utf8.ValidString(stream.Text) {
		t.Fatalf("Stream().Text is invalid UTF-8: %q", stream.Text)
	}
	if stream.Text != "?" {
		t.Fatalf("Stream().Text = %q, want replacement", stream.Text)
	}
}

// TestShouldRetryAfterExecutionErrorForInteractivePrompt checks that prompt errors trigger one more planning round.
func TestShouldRetryAfterExecutionErrorForInteractivePrompt(t *testing.T) {
	err := &interactivePromptError{Command: "docker image prune -a", Prompt: "Are you sure? [y/N]"}

	if !shouldRetryAfterExecutionError(err, 0, defaultPlanningMaxRounds) {
		t.Fatalf("shouldRetryAfterExecutionError() = false, want true")
	}
	if shouldRetryAfterExecutionError(err, defaultPlanningMaxRounds-1, defaultPlanningMaxRounds) {
		t.Fatalf("shouldRetryAfterExecutionError() = true on final round, want false")
	}
}

// TestCommandRunErrorUnwrapsCause checks callers can inspect the underlying failure.
func TestCommandRunErrorUnwrapsCause(t *testing.T) {
	runErr := &commandRunError{Command: "false", ExitCode: 1, Err: exec.ErrNotFound}

	if !errors.Is(runErr, exec.ErrNotFound) {
		t.Fatalf("errors.Is(commandRunError, exec.ErrNotFound) = false, want true")
	}

	timeoutErr := &commandRunError{Command: "sleep 10", ExitCode: 124, TimedOut: true, Err: context.DeadlineExceeded}
	if !errors.Is(timeoutErr, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(commandRunError, context.DeadlineExceeded) = false, want true")
	}
}

// TestExecuteOneCommandRespectsTimeout checks command deadlines cancel subprocesses.
func TestExecuteOneCommandRespectsTimeout(t *testing.T) {
	cfg := defaultConfig()
	cfg.CommandTimeout = 10 * time.Millisecond
	cfg.ShowSystemOutput = false
	cfg.CaptureStdoutBytes = 1024
	cfg.CaptureStderrBytes = 1024

	result, err := executeOneCommand(t.Context(), commandRunRequest{
		Config: cfg,
		ContextInfo: contextInfo{
			CWD:   t.TempDir(),
			Shell: "/bin/sh",
		},
		Command: "sleep 1",
		Timeout: cfg.CommandTimeout,
	})

	var runErr *commandRunError
	if !errors.As(err, &runErr) {
		t.Fatalf("executeOneCommand() error = %T %[1]v, want commandRunError", err)
	}
	if !runErr.TimedOut || runErr.ExitCode != 124 || result.ExitCode != 124 {
		t.Fatalf("timeout error/result = %#v / %#v, want exit code 124 timeout", runErr, result)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(timeout, context.DeadlineExceeded) = false, want true")
	}
}

// TestExecuteOneCommandTimeoutStopsDescendants checks timed-out commands cannot leave child processes running.
func TestExecuteOneCommandTimeoutStopsDescendants(t *testing.T) {
	cfg := defaultConfig()
	cfg.CommandTimeout = 20 * time.Millisecond
	cfg.ShowSystemOutput = false
	cfg.CaptureStdoutBytes = 1024
	cfg.CaptureStderrBytes = 1024

	tempDir := t.TempDir()
	markerPath := filepath.Join(tempDir, "child-survived")
	command := fmt.Sprintf("(sleep 0.2; printf survived > %q) & wait", markerPath)

	result, err := executeOneCommand(t.Context(), commandRunRequest{
		Config: cfg,
		ContextInfo: contextInfo{
			CWD:   tempDir,
			Shell: "/bin/sh",
		},
		Command: command,
		Timeout: cfg.CommandTimeout,
	})

	var runErr *commandRunError
	if !errors.As(err, &runErr) || !runErr.TimedOut || result.ExitCode != 124 {
		t.Fatalf("executeOneCommand() error/result = %#v / %#v, want timeout with exit code 124", err, result)
	}
	time.Sleep(300 * time.Millisecond)
	if _, statErr := os.Stat(markerPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("descendant marker stat error = %v, want os.ErrNotExist", statErr)
	}
}

// TestParseSimpleCDTarget checks accepted and rejected standalone cd forms.
func TestParseSimpleCDTarget(t *testing.T) {
	cases := []struct {
		name       string
		command    string
		wantTarget string
		wantOK     bool
	}{
		{name: "bare cd", command: "cd", wantOK: true},
		{name: "relative path", command: "cd docs", wantTarget: "docs", wantOK: true},
		{name: "double quoted path", command: `cd "docs old"`, wantTarget: "docs old", wantOK: true},
		{name: "single quoted path", command: `cd 'docs old'`, wantTarget: "docs old", wantOK: true},
		{name: "escaped space", command: `cd docs\ old`, wantTarget: "docs old", wantOK: true},
		{name: "missing cd boundary", command: "cdrom", wantOK: false},
		{name: "multiple arguments", command: "cd docs old", wantOK: false},
		{name: "malformed quote", command: `cd "docs`, wantOK: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotOK := parseSimpleCDTarget(tt.command)
			if gotOK != tt.wantOK || gotTarget != tt.wantTarget {
				t.Fatalf("parseSimpleCDTarget(%q) = %q, %t; want %q, %t", tt.command, gotTarget, gotOK, tt.wantTarget, tt.wantOK)
			}
		})
	}
}

// TestResolveDirectoryChange checks session directory transitions without running a shell.
func TestResolveDirectoryChange(t *testing.T) {
	currentCWD := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := []struct {
		name    string
		command string
		want    string
		wantOK  bool
	}{
		{name: "relative cd", command: "cd docs", want: filepath.Join(currentCWD, "docs"), wantOK: true},
		{name: "escaped space cd", command: `cd docs\ old`, want: filepath.Join(currentCWD, "docs old"), wantOK: true},
		{name: "single quoted cd", command: `cd 'docs old'`, want: filepath.Join(currentCWD, "docs old"), wantOK: true},
		{name: "absolute cd", command: "cd /tmp", want: "/tmp", wantOK: true},
		{name: "home cd", command: "cd", want: home, wantOK: true},
		{name: "home child", command: "cd ~/project", want: filepath.Join(home, "project"), wantOK: true},
		{name: "previous directory unsupported", command: "cd -", wantOK: false},
		{name: "compound command ignored", command: "cd docs && pwd", wantOK: false},
		{name: "non cd command ignored", command: "pwd", wantOK: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := resolveDirectoryChange(currentCWD, tt.command)
			if gotOK != tt.wantOK {
				t.Fatalf("resolveDirectoryChange(%q) ok = %t, want %t", tt.command, gotOK, tt.wantOK)
			}
			if gotOK && got != tt.want {
				t.Fatalf("resolveDirectoryChange(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

// TestApplySessionStateUpdatesCWDOnSuccessfulCD checks successful cd commands update session state.
func TestApplySessionStateUpdatesCWDOnSuccessfulCD(t *testing.T) {
	currentCWD := t.TempDir()
	nextDir := filepath.Join(currentCWD, "docs")
	ctxInfo := &contextInfo{
		CWD: currentCWD,
	}

	applySessionState(ctxInfo, "cd docs", 0)

	if ctxInfo.CWD != nextDir {
		t.Fatalf("ctxInfo.CWD = %q, want %q", ctxInfo.CWD, nextDir)
	}
}

// TestApplySessionStateIgnoresFailedCommand checks failed commands do not mutate session state.
func TestApplySessionStateIgnoresFailedCommand(t *testing.T) {
	ctxInfo := &contextInfo{CWD: t.TempDir()}
	before := *ctxInfo

	applySessionState(ctxInfo, "cd docs", 1)

	if *ctxInfo != before {
		t.Fatalf("ctxInfo = %#v, want unchanged %#v", *ctxInfo, before)
	}
}

// TestStaticFallbackAnswer checks summarization fallback output from command executions.
func TestStaticFallbackAnswer(t *testing.T) {
	cases := []struct {
		name       string
		summary    string
		executions []commandExecution
		skipped    []skippedCommand
		want       string
	}{
		{name: "no executions", summary: "Nothing to run.", want: "Nothing to run."},
		{
			name:       "successful stdout",
			summary:    "Listed files.",
			executions: []commandExecution{{Command: "pwd", Purpose: "Print cwd", ExitCode: 0, Stdout: capturedStream{Text: "/tmp/project"}}},
			want:       "/tmp/project",
		},
		{
			name:       "successful no output",
			summary:    "Created marker.",
			executions: []commandExecution{{Command: "touch marker", Purpose: "Create marker", ExitCode: 0}},
			want:       "Create marker done.",
		},
		{
			name:       "failed with output",
			summary:    "Command failed.",
			executions: []commandExecution{{Command: "ls missing", Purpose: "List file", ExitCode: 2, Stderr: capturedStream{Text: "No such file"}}},
			want:       "No such file\nThe command `ls missing` failed with exit code 2.",
		},
		{
			name:       "failed without output",
			summary:    "Command failed.",
			executions: []commandExecution{{Command: "false", Purpose: "Fail", ExitCode: 1}},
			want:       "The command `false` failed with exit code 1.",
		},
		{
			name:    "recovered turn preserves failure and skipped omission",
			summary: "Recovery completed.",
			executions: []commandExecution{
				{Command: "false", Purpose: "Fail", ExitCode: 7, Stderr: capturedStream{Text: "initial failure"}},
				{Command: "pwd", Purpose: "Recover", ExitCode: 0, Stdout: capturedStream{Text: "/tmp/project"}},
			},
			skipped: []skippedCommand{{Command: "touch blocked", Purpose: "Blocked", Reason: skippedAfterFailureReason}},
			want:    "initial failure\nThe command `false` failed with exit code 7. 1 command(s) were skipped and not executed.",
		},
		{
			name:    "skipped work prevents synthesized success",
			summary: "Partial work.",
			skipped: []skippedCommand{{Command: "touch blocked", Purpose: "Blocked", Reason: skippedAfterFailureReason}},
			want:    "Partial work.\nSome commands were skipped and were not executed.",
		},
		{
			name:    "skipped work with empty summary",
			summary: "  \n\t",
			skipped: []skippedCommand{{Command: "touch blocked", Purpose: "Blocked", Reason: skippedAfterFailureReason}},
			want:    "Some commands were skipped and were not executed.",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := staticFallbackAnswer(tt.summary, tt.executions, tt.skipped); got != tt.want {
				t.Fatalf("staticFallbackAnswer() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExecuteCommandsContinuesOnlyIndependentStepsAfterFailure checks dependent commands are skipped after an ordinary failure.
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
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	var batch commandBatchResult
	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		deps.Trace = logger
		var err error
		batch, err = executeCommands(withTraceTurnID(t.Context(), turnID), deps, false, cfg, &ctxInfo, plans, nil)
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

	events := closeLoopTraceAndRead(t, logger)
	skippedEvents := traceEventsByName(events, "command_skipped")
	if len(skippedEvents) != 1 {
		t.Fatalf("command_skipped events = %d, want 1", len(skippedEvents))
	}
	data := traceEventData(t, skippedEvents[0])
	if data["command"] != plans[1].Command || data["purpose"] != plans[1].Purpose || data["reason"] != skippedAfterFailureReason {
		t.Fatalf("command_skipped data = %#v, want dependent command and reason", data)
	}
	if len(traceEventsByName(events, "command_confirmation")) != 2 || len(traceEventsByName(events, "command_start")) != 2 {
		t.Fatalf("skipped command emitted confirmation or start event")
	}
}

// TestExecuteCommandsSkipsDuplicateSuccessfulCommandInBatch checks a command
// completed successfully earlier in the batch is not confirmed or executed again.
func TestExecuteCommandsSkipsDuplicateSuccessfulCommandInBatch(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	plans := []commandPlan{
		{Command: "printf duplicate", Purpose: "Print once", Classification: classificationSafe, LocalSafe: true},
		{Command: "  printf duplicate\t", Purpose: "Do not print twice", Classification: classificationSafe, LocalSafe: true},
	}
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	var batch commandBatchResult
	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		deps.Trace = logger
		var err error
		batch, err = executeCommands(withTraceTurnID(t.Context(), turnID), deps, false, cfg, &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if len(batch.Executions) != 1 || batch.Executions[0].Command != plans[0].Command {
		t.Fatalf("executions = %#v, want first command only", batch.Executions)
	}
	if len(batch.Skipped) != 1 || batch.Skipped[0].Command != plans[1].Command || batch.Skipped[0].Reason != "already completed successfully in this turn" {
		t.Fatalf("skipped = %#v, want duplicate success skip", batch.Skipped)
	}
	if batch.HadOrdinaryFailure || batch.HadTimeout {
		t.Fatalf("batch flags = %#v, want duplicate skip without blocking", batch)
	}

	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "command_confirmation")) != 1 || len(traceEventsByName(events, "command_start")) != 1 {
		t.Fatalf("duplicate command was confirmed or started")
	}
	skippedEvents := traceEventsByName(events, "command_skipped")
	if len(skippedEvents) != 1 || traceEventData(t, skippedEvents[0])["reason"] != "already completed successfully in this turn" {
		t.Fatalf("command_skipped events = %#v, want duplicate success reason", skippedEvents)
	}
}

// TestExecuteCommandsDoesNotSuppressPreviouslyFailedEffectiveCommand checks
// a failed effective identity from an earlier turn batch remains retryable.
func TestExecuteCommandsDoesNotSuppressPreviouslyFailedEffectiveCommand(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	plans := []commandPlan{{Command: "false", Purpose: "Retry failure", Classification: classificationSafe, LocalSafe: true}}
	priorExecutions := []commandExecution{{Command: " false\t", Purpose: "Earlier failure", ExitCode: 1}}

	var batch commandBatchResult
	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, cfg, &ctxInfo, plans, priorExecutions)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if len(batch.Executions) != 1 || len(batch.Skipped) != 0 {
		t.Fatalf("batch = %#v, want failed retry executed", batch)
	}
	if !batch.HadOrdinaryFailure || batch.HadTimeout {
		t.Fatalf("batch flags = %#v, want ordinary failure only", batch)
	}
}

// TestExecuteCommandsStopsBatchWhenContinueOnErrorIsFalse checks a failure stops all later commands.
func TestExecuteCommandsStopsBatchWhenContinueOnErrorIsFalse(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ContinueOnError = false
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
		batch, err = executeCommands(t.Context(), deps, false, cfg, &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if !batch.HadOrdinaryFailure || batch.HadTimeout {
		t.Fatalf("batch flags = %#v, want ordinary failure only", batch)
	}
	if len(batch.Executions) != 1 || len(batch.Skipped) != 0 {
		t.Fatalf("batch = %#v, want one execution and no recorded skips", batch)
	}
	for _, marker := range []string{dependent, independent} {
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("marker %q error = %v, want os.ErrNotExist", marker, err)
		}
	}
}

// TestExecuteCommandsTracksTimeoutWithoutOrdinaryFailure checks timeouts block only dependent commands.
func TestExecuteCommandsTracksTimeoutWithoutOrdinaryFailure(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ContinueOnError = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	// Leave enough headroom for the independent command under parallel package load.
	cfg.CommandTimeout = 250 * time.Millisecond
	ctxInfo := loopTestContext(t)
	dependent := filepath.Join(ctxInfo.CWD, "dependent")
	independent := filepath.Join(ctxInfo.CWD, "independent")
	plans := []commandPlan{
		{Command: "sleep 1", Purpose: "Time out", Classification: classificationSafe, LocalSafe: true},
		{Command: "touch " + dependent, Purpose: "Dependent", Classification: classificationSafe, LocalSafe: true},
		{Command: "touch " + independent, Purpose: "Independent", Classification: classificationSafe, LocalSafe: true, IndependentOnFailure: true},
	}

	var batch commandBatchResult
	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, cfg, &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if batch.HadOrdinaryFailure || !batch.HadTimeout {
		t.Fatalf("batch flags = %#v, want timeout only", batch)
	}
	if len(batch.Executions) != 2 || len(batch.Skipped) != 1 {
		t.Fatalf("batch = %#v, want 2 executions and 1 skip", batch)
	}
	if _, err := os.Stat(dependent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dependent marker error = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(independent); err != nil {
		t.Fatalf("independent marker error = %v, want file", err)
	}
}

// TestExecuteCommandsStopsImmediatelyOnCancellation checks parent cancellation is returned without a fabricated execution.
func TestExecuteCommandsStopsImmediatelyOnCancellation(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ContinueOnError = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	cfg.CommandTimeout = 10 * time.Second
	ctxInfo := loopTestContext(t)
	later := filepath.Join(ctxInfo.CWD, "later")
	plans := []commandPlan{
		{Command: "sleep 10", Purpose: "Wait", Classification: classificationSafe, LocalSafe: true},
		{Command: "touch " + later, Purpose: "Later", Classification: classificationSafe, LocalSafe: true, IndependentOnFailure: true},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	var batch commandBatchResult
	var runErr error
	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		batch, runErr = executeCommands(ctx, deps, false, cfg, &ctxInfo, plans, nil)
	})

	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("executeCommands() error = %v, want context.Canceled", runErr)
	}
	if len(batch.Executions) != 0 || len(batch.Skipped) != 0 || batch.HadOrdinaryFailure || batch.HadTimeout {
		t.Fatalf("batch = %#v, want empty completed prefix", batch)
	}
	if _, err := os.Stat(later); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("later marker error = %v, want os.ErrNotExist", err)
	}
}

// TestExecuteCommandsTraceRecordsCapturedOutput checks command output metadata is traceable.
func TestExecuteCommandsTraceRecordsCapturedOutput(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CaptureStdoutBytes = 3
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		deps.Trace = logger
		plans := []commandPlan{{
			Command:        "printf abcdef",
			Purpose:        "Print marker",
			Risk:           "safe",
			Classification: classificationSafe,
			LocalSafe:      true,
		}}
		batch, err := executeCommands(withTraceTurnID(t.Context(), turnID), deps, false, cfg, &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
		if len(batch.Executions) != 1 {
			t.Fatalf("executions = %d, want 1", len(batch.Executions))
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	commandEnd := traceEventsByName(events, "command_end")
	if len(commandEnd) != 1 {
		t.Fatalf("command_end events = %d, want 1", len(commandEnd))
	}
	data := traceEventData(t, commandEnd[0])
	execution, ok := data["execution"].(map[string]any)
	if !ok {
		t.Fatalf("execution = %#v, want object", data["execution"])
	}
	stdout, ok := execution["stdout"].(map[string]any)
	if !ok {
		t.Fatalf("stdout = %#v, want object", execution["stdout"])
	}
	if stdout["text"] != "abc" || stdout["truncated"] != true {
		t.Fatalf("stdout trace = %#v, want truncated abc", stdout)
	}
	if stdout["kept_bytes"] != float64(3) || stdout["total_bytes"] != float64(6) {
		t.Fatalf("stdout byte counts = %#v, want kept 3 total 6", stdout)
	}
}

// TestExecuteCommandsTraceRecordsCommandErrors checks failed commands are diagnosable.
func TestExecuteCommandsTraceRecordsCommandErrors(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = true
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		deps.Trace = logger
		plans := []commandPlan{{
			Command:        "exit 7",
			Purpose:        "Fail with status",
			Risk:           "safe",
			Classification: classificationSafe,
			LocalSafe:      true,
		}}
		batch, err := executeCommands(withTraceTurnID(t.Context(), turnID), deps, false, cfg, &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
		if !batch.HadOrdinaryFailure || batch.HadTimeout {
			t.Fatalf("batch flags = %#v, want ordinary failure only", batch)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	commandErrors := traceEventsByName(events, "command_error")
	if len(commandErrors) != 1 {
		t.Fatalf("command_error events = %d, want 1", len(commandErrors))
	}
	data := traceEventData(t, commandErrors[0])
	if data["exit_code"] != float64(7) {
		t.Fatalf("exit_code = %#v, want 7", data["exit_code"])
	}
	if data["command"] != "exit 7" {
		t.Fatalf("command = %#v, want exit 7", data["command"])
	}
}

// TestExecuteCommandsTraceRecordsEditedCommand checks edited commands are traceable.
func TestExecuteCommandsTraceRecordsEditedCommand(t *testing.T) {
	cfg := defaultConfig()
	cfg.YesSafe = false
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	captureMainLoopIO(t, "e\nprintf edited\n", nil, func(deps RuntimeDeps) {
		deps.Trace = logger
		plans := []commandPlan{{
			Command:        "printf original",
			Purpose:        "Print marker",
			Risk:           "safe",
			Classification: classificationSafe,
			LocalSafe:      true,
		}}
		batch, err := executeCommands(withTraceTurnID(t.Context(), turnID), deps, false, cfg, &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
		if len(batch.Executions) != 1 || batch.Executions[0].Command != "printf edited" {
			t.Fatalf("executions = %#v, want edited command", batch.Executions)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	confirmations := traceEventsByName(events, "command_confirmation")
	if len(confirmations) != 1 {
		t.Fatalf("command_confirmation events = %d, want 1", len(confirmations))
	}
	confirmationData := traceEventData(t, confirmations[0])
	if confirmationData["decision"] != "edit" {
		t.Fatalf("decision = %#v, want edit", confirmationData["decision"])
	}
	if confirmationData["command"] != "printf original" || confirmationData["edited_command"] != "printf edited" {
		t.Fatalf("confirmation data = %#v, want original and edited commands", confirmationData)
	}

	starts := traceEventsByName(events, "command_start")
	if len(starts) != 1 {
		t.Fatalf("command_start events = %d, want 1", len(starts))
	}
	startData := traceEventData(t, starts[0])
	if startData["command"] != "printf edited" || startData["original_command"] != "printf original" {
		t.Fatalf("command_start data = %#v, want effective and original commands", startData)
	}
}

// TestExecuteManualCommandTraceRecordsCommand checks direct shell commands are traced.
func TestExecuteManualCommandTraceRecordsCommand(t *testing.T) {
	cfg := defaultConfig()
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "", nil, func(deps RuntimeDeps) {
		deps.Trace = logger
		execution, err := executeManualCommand(t.Context(), deps, false, cfg, &ctxInfo, "printf manual", manualRenderInline)
		if err != nil {
			t.Fatalf("executeManualCommand() error = %v", err)
		}
		if execution.Stdout.Text != "manual" {
			t.Fatalf("stdout = %q, want manual", execution.Stdout.Text)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "manual_command_start")) != 1 {
		t.Fatalf("manual_command_start events = %d, want 1", len(traceEventsByName(events, "manual_command_start")))
	}
	if len(traceEventsByName(events, "manual_command_end")) != 1 {
		t.Fatalf("manual_command_end events = %d, want 1", len(traceEventsByName(events, "manual_command_end")))
	}
}
