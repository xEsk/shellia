package main

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

// TestShouldRetryAfterExecutionErrorForInteractivePrompt checks that prompt errors trigger one more planning round.
func TestShouldRetryAfterExecutionErrorForInteractivePrompt(t *testing.T) {
	err := &interactivePromptError{Command: "docker image prune -a", Prompt: "Are you sure? [y/N]"}

	if !shouldRetryAfterExecutionError(err, 0) {
		t.Fatalf("shouldRetryAfterExecutionError() = false, want true")
	}
	if shouldRetryAfterExecutionError(err, maxPlanRounds-1) {
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
		Git: gitContext{IsRepo: true, Branch: "main"},
	}
	cfg := config{IncludeGit: false}

	applySessionState(ctxInfo, cfg, "cd docs", 0)

	if ctxInfo.CWD != nextDir {
		t.Fatalf("ctxInfo.CWD = %q, want %q", ctxInfo.CWD, nextDir)
	}
	if ctxInfo.Git != (gitContext{}) {
		t.Fatalf("ctxInfo.Git = %#v, want cleared git context", ctxInfo.Git)
	}
}

// TestApplySessionStateIgnoresFailedCommand checks failed commands do not mutate session state.
func TestApplySessionStateIgnoresFailedCommand(t *testing.T) {
	ctxInfo := &contextInfo{CWD: t.TempDir()}
	before := *ctxInfo

	applySessionState(ctxInfo, config{}, "cd docs", 1)

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
			want:       "No such file",
		},
		{
			name:       "failed without output",
			summary:    "Command failed.",
			executions: []commandExecution{{Command: "false", Purpose: "Fail", ExitCode: 1}},
			want:       "The command `false` failed with exit code 1.",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := staticFallbackAnswer(tt.summary, tt.executions); got != tt.want {
				t.Fatalf("staticFallbackAnswer() = %q, want %q", got, tt.want)
			}
		})
	}
}
