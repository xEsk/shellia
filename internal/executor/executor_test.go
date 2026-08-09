package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	configpkg "github.com/xEsk/shellia/internal/config"
	"github.com/xEsk/shellia/internal/core"
	safetypkg "github.com/xEsk/shellia/internal/safety"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// TestPrefixedWriterStreamsLinesAndFlushesPartial checks complete lines are
// rendered immediately while a trailing partial line waits for Flush.
func TestPrefixedWriterStreamsLinesAndFlushesPartial(t *testing.T) {
	var output bytes.Buffer
	cfg := configpkg.DefaultConfig()
	cfg.VisualStyle = configpkg.VisualStyleCards
	turn := uipkg.NewRenderer(&output, uipkg.Presentation{Style: cfg.VisualStyle}).BeginShelliaTurn(executorViewOptions(cfg), loopTestContext(t))
	defer turn.Close()
	box := &executorTestStepPresenter{box: turn.BeginStep(1, 1, commandPlan{Command: "printf stream", Purpose: "stream output"})}
	writer := &prefixedWriter{box: box}

	if _, err := writer.Write([]byte("one\ntwo")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	beforeFlush := output.String()
	if !strings.Contains(beforeFlush, "one") {
		t.Fatalf("complete line was not streamed before Flush: %q", beforeFlush)
	}
	if strings.Contains(beforeFlush, "two") {
		t.Fatalf("partial line was rendered before Flush: %q", beforeFlush)
	}

	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !strings.Contains(output.String(), "two") {
		t.Fatalf("partial line was not rendered by Flush: %q", output.String())
	}
}

// TestPrefixedWriterDefersPartialOutputState checks a partial write is not
// classified as visible output until it is flushed to the step surface.
func TestPrefixedWriterDefersPartialOutputState(t *testing.T) {
	box := &executorTestStepPresenter{box: uipkg.NewStepBox(io.Discard, false, "step 1/1")}
	writer := &prefixedWriter{box: box}

	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if prefixedWritersHadOutput(writer) {
		t.Fatal("prefixedWritersHadOutput() = true before partial line was flushed")
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !prefixedWritersHadOutput(writer) {
		t.Fatal("prefixedWritersHadOutput() = false after partial line was flushed")
	}
}

// TestPrefixedWriterPreservesSplitUTF8AndIndependentStreams checks chunk
// boundaries do not corrupt UTF-8 and stdout/stderr can share one step.
func TestPrefixedWriterPreservesSplitUTF8AndIndependentStreams(t *testing.T) {
	var output bytes.Buffer
	box := &executorTestStepPresenter{box: uipkg.NewStepBox(&output, false, "step 1/1")}
	stdout := &prefixedWriter{box: box}
	stderr := &prefixedWriter{box: box}
	payload := []byte("cafè ☕\n")

	if _, err := stdout.Write(payload[:4]); err != nil {
		t.Fatalf("stdout first Write() error = %v", err)
	}
	if _, err := stdout.Write(payload[4:]); err != nil {
		t.Fatalf("stdout second Write() error = %v", err)
	}
	if _, err := stderr.Write([]byte("warning")); err != nil {
		t.Fatalf("stderr Write() error = %v", err)
	}
	if err := stderr.Flush(); err != nil {
		t.Fatalf("stderr Flush() error = %v", err)
	}

	rendered := output.String()
	if !utf8.ValidString(rendered) {
		t.Fatalf("rendered output is not valid UTF-8: %q", rendered)
	}
	for _, want := range []string{"cafè ☕", "warning"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered output lacks %q: %q", want, rendered)
		}
	}
}

// TestPrefixedWriterHiddenModeEmitsNothing checks hidden streams remain
// invisible even after complete and partial writes are flushed.
func TestPrefixedWriterHiddenModeEmitsNothing(t *testing.T) {
	var output bytes.Buffer
	box := &executorTestStepPresenter{box: uipkg.NewStepBox(&output, false, "step 1/1")}
	baseline := output.String()
	writer := &prefixedWriter{box: box, hidden: true}

	if _, err := writer.Write([]byte("secret\npartial")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if got := output.String(); got != baseline {
		t.Fatalf("hidden writer changed output: got %q, want %q", got, baseline)
	}
	if prefixedWritersHadOutput(writer) {
		t.Fatal("hidden writer reported visible output")
	}
}

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

// TestExecuteCommandsUsesActiveTurn checks execution steps are created through
// the renderer turn supplied by the app boundary.
func TestExecuteCommandsUsesActiveTurn(t *testing.T) {
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp(stdout) error = %v", err)
	}
	t.Cleanup(func() { stdout.Close() }) //nolint:errcheck // best-effort test cleanup.

	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("CreateTemp(stdin) error = %v", err)
	}
	t.Cleanup(func() { stdin.Close() }) //nolint:errcheck // best-effort test cleanup.

	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = true
	cfg.ShowSystemOutput = true
	cfg.ShowCommandPopup = true
	ctxInfo := loopTestContext(t)
	renderer := uipkg.NewRenderer(stdout, uipkg.Presentation{Style: cfg.VisualStyle, ANSI: false})
	turn := renderer.BeginShelliaTurn(executorViewOptions(cfg), ctxInfo)
	defer turn.Close()
	plan := commandPlan{
		Command:        "printf '419Gi available\\n'",
		Purpose:        "Mostrar l'espai lliure.",
		Risk:           "safe",
		Classification: classificationSafe,
		LocalSafe:      true,
	}

	_, err = executeCommands(t.Context(), RuntimeDeps{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stdout,
		Presenter: newExecutorTestPresenter(stdout, stdout, false, turn),
	}, false, executorOptions(cfg), &ctxInfo, []commandPlan{plan}, nil)
	if err != nil {
		t.Fatalf("executeCommands() error = %v", err)
	}
	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	output := string(data)
	for _, want := range []string{"step 1/1", "system output", "419Gi available"} {
		if !strings.Contains(output, want) {
			t.Fatalf("executor output lacks %q: %q", want, output)
		}
	}
}

// TestExecuteManualCommandUsesActiveTurn checks inline manual commands use the
// renderer-owned step surface when the app has supplied a turn.
func TestExecuteManualCommandUsesActiveTurn(t *testing.T) {
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp(stdout) error = %v", err)
	}
	t.Cleanup(func() { stdout.Close() }) //nolint:errcheck // best-effort test cleanup.
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatalf("CreateTemp(stdin) error = %v", err)
	}
	t.Cleanup(func() { stdin.Close() }) //nolint:errcheck // best-effort test cleanup.

	cfg := configpkg.DefaultConfig()
	cfg.VisualStyle = configpkg.VisualStyleCards
	cfg.ShowSystemOutput = false
	ctxInfo := loopTestContext(t)
	turn := uipkg.NewRenderer(stdout, uipkg.Presentation{Style: cfg.VisualStyle}).BeginShelliaTurn(executorViewOptions(cfg), ctxInfo)
	_, err = executeManualCommand(t.Context(), RuntimeDeps{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stdout,
		Presenter: newExecutorTestPresenter(stdout, stdout, false, turn),
	}, false, executorOptions(cfg), &ctxInfo, "printf manual", manualRenderInline)
	if err != nil {
		t.Fatalf("executeManualCommand() error = %v", err)
	}
	turn.Close()

	output := string(readExecutorTestFile(t, stdout))
	for _, want := range []string{"│   ┌─ step 1/1", "run › printf manual", "completed"} {
		if !strings.Contains(output, want) {
			t.Fatalf("manual command output lacks %q: %q", want, output)
		}
	}
}

// TestExecuteInteractiveCommandSuspendsCardsAroundRawPTY checks a real PTY is
// written byte-for-byte outside renderer geometry and the turn is resumed on
// both command failure and cancellation.
func TestExecuteInteractiveCommandSuspendsCardsAroundRawPTY(t *testing.T) {
	tests := []struct {
		name    string
		command string
		marker  string
		ansi    string
		context func() (context.Context, context.CancelFunc)
	}{
		{
			name:    "command error",
			command: "printf '\\033[31mRAW-PTY\\033[0m\\n'; exit 7",
			marker:  "RAW-PTY",
			ansi:    "\033[31mRAW-PTY\033[0m",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(t.Context())
			},
		},
		{
			name:    "cancellation",
			command: "printf '\\033[32mRAW-CANCEL\\033[0m\\n'; exec sleep 5",
			marker:  "RAW-CANCEL",
			ansi:    "\033[32mRAW-CANCEL\033[0m",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(t.Context(), 150*time.Millisecond)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatalf("CreateTemp(stdout) error = %v", err)
			}
			t.Cleanup(func() { stdout.Close() }) //nolint:errcheck // best-effort test cleanup.
			stdin, err := os.CreateTemp(t.TempDir(), "stdin")
			if err != nil {
				t.Fatalf("CreateTemp(stdin) error = %v", err)
			}
			t.Cleanup(func() { stdin.Close() }) //nolint:errcheck // best-effort test cleanup.

			cfg := configpkg.DefaultConfig()
			cfg.VisualStyle = configpkg.VisualStyleCards
			ctxInfo := loopTestContext(t)
			turn := uipkg.NewRenderer(stdout, uipkg.Presentation{Style: cfg.VisualStyle}).BeginShelliaTurn(executorViewOptions(cfg), ctxInfo)
			box := &executorTestStepPresenter{box: turn.BeginStep(1, 1, commandPlan{Command: tt.command, Purpose: "raw PTY handoff", Interactive: true})}
			ctx, cancel := tt.context()
			defer cancel()

			result, runErr := executeOneCommand(ctx, commandRunRequest{
				Deps: RuntimeDeps{
					Stdin:     stdin,
					Stdout:    stdout,
					Stderr:    stdout,
					Presenter: newExecutorTestPresenter(stdout, stdout, false, turn),
				},
				Options:     executorOptions(cfg),
				ContextInfo: ctxInfo,
				Box:         box,
				Command:     tt.command,
				Interactive: true,
			})
			if runErr == nil {
				t.Fatal("executeOneCommand() error = nil, want command failure")
			}
			if !strings.Contains(result.Output.Stdout.Text, tt.ansi) {
				t.Fatalf("captured PTY stdout lacks raw ANSI bytes: %q", result.Output.Stdout.Text)
			}
			turn.Final("done")
			turn.Close()

			assertRawPTYHandoff(t, readExecutorTestFile(t, stdout), tt.marker, tt.ansi)
		})
	}
}

func readExecutorTestFile(t *testing.T, file *os.File) []byte {
	t.Helper()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(%s) error = %v", file.Name(), err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("ReadAll(%s) error = %v", file.Name(), err)
	}
	return data
}

func assertRawPTYHandoff(t *testing.T, output []byte, marker string, rawANSI string) {
	t.Helper()
	rawIndex := bytes.Index(output, []byte(rawANSI))
	if rawIndex == -1 {
		t.Fatalf("PTY output lacks exact raw bytes %q: %q", rawANSI, output)
	}
	if bytes.LastIndex(output[:rawIndex], []byte("\n╰")) == -1 {
		t.Fatalf("renderer card was not closed before PTY output: %q", output)
	}
	continuedIndex := bytes.Index(output[rawIndex:], []byte("Shellia · continued"))
	if continuedIndex == -1 {
		t.Fatalf("renderer turn was not resumed after PTY output: %q", output)
	}

	lineStart := bytes.LastIndexByte(output[:rawIndex], '\n') + 1
	lineEndOffset := bytes.IndexByte(output[rawIndex:], '\n')
	if lineEndOffset == -1 {
		t.Fatalf("PTY marker %q has no complete output line: %q", marker, output)
	}
	rawLine := output[lineStart : rawIndex+lineEndOffset]
	for _, geometry := range [][]byte{[]byte("│"), []byte("▌"), []byte("┌"), []byte("└")} {
		if bytes.Contains(rawLine, geometry) {
			t.Fatalf("raw PTY line contains renderer geometry %q: %q", geometry, rawLine)
		}
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
	cfg := configpkg.DefaultConfig()
	cfg.CommandTimeout = 10 * time.Millisecond
	cfg.ShowSystemOutput = false
	cfg.CaptureStdoutBytes = 1024
	cfg.CaptureStderrBytes = 1024

	result, err := executeOneCommand(t.Context(), commandRunRequest{
		Options: executorOptions(cfg),
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
	cfg := configpkg.DefaultConfig()
	cfg.CommandTimeout = 20 * time.Millisecond
	cfg.ShowSystemOutput = false
	cfg.CaptureStdoutBytes = 1024
	cfg.CaptureStderrBytes = 1024

	tempDir := t.TempDir()
	markerPath := filepath.Join(tempDir, "child-survived")
	command := fmt.Sprintf("(sleep 0.2; printf survived > %q) & wait", markerPath)

	result, err := executeOneCommand(t.Context(), commandRunRequest{
		Options: executorOptions(cfg),
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

// TestExecuteCommandsContinuesOnlyIndependentStepsAfterFailure checks dependent commands are skipped after an ordinary failure.
func TestExecuteCommandsContinuesOnlyIndependentStepsAfterFailure(t *testing.T) {
	cfg := configpkg.DefaultConfig()
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

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		deps.Trace = logger
		var err error
		batch, err = executeCommands(tracepkg.WithTurnID(t.Context(), turnID), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
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
	cfg := configpkg.DefaultConfig()
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

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		deps.Trace = logger
		var err error
		batch, err = executeCommands(tracepkg.WithTurnID(t.Context(), turnID), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if len(batch.Executions) != 1 || batch.Executions[0].Command != plans[0].Command {
		t.Fatalf("executions = %#v, want first command only", batch.Executions)
	}
	if len(batch.Skipped) != 1 || batch.Skipped[0].Command != plans[1].Command || batch.Skipped[0].Reason != core.RepeatReasonRequired {
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
	if len(skippedEvents) != 1 || traceEventData(t, skippedEvents[0])["reason"] != core.RepeatReasonRequired {
		t.Fatalf("command_skipped events = %#v, want duplicate success reason", skippedEvents)
	}
}

// TestExecuteCommandsDoesNotAutoRunCommandSubstitutionWithYesSafe checks local
// auto-safe execution cannot bypass confirmation for executable substitutions.
func TestExecuteCommandsDoesNotAutoRunCommandSubstitutionWithYesSafe(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	marker := filepath.Join(ctxInfo.CWD, "command-substitution-ran")
	command := fmt.Sprintf(`echo "$(touch %s)"`, marker)
	localSafety := safetypkg.ClassifyCommand(command)
	plans := []commandPlan{{
		Command:              command,
		Purpose:              "Print safe-looking output",
		Risk:                 safetypkg.HigherRisk("safe", localSafety.Risk),
		RequiresConfirmation: localSafety.RequiresConfirmation,
		Classification:       localSafety.Classification,
		LocalSafe:            localSafety.Classification == classificationSafe && !localSafety.RequiresConfirmation,
	}}

	var batch core.CommandBatchResult
	var runErr error
	captureMainLoopIO(t, "n\n", func(deps RuntimeDeps) {
		batch, runErr = executeCommands(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
	})

	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("command substitution ran despite rejected confirmation: marker error = %v, want os.ErrNotExist", err)
	}
	if !errors.Is(runErr, core.ErrAborted) {
		t.Fatalf("executeCommands() error = %v, want confirmation abort", runErr)
	}
	if len(batch.Executions) != 0 {
		t.Fatalf("batch.Executions = %#v, want no execution", batch.Executions)
	}
}

// TestExecuteCommandsConfirmsTypedRiskyRepeat checks repeat admission never bypasses normal confirmation.
func TestExecuteCommandsConfirmsTypedRiskyRepeat(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = false
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	marker := filepath.Join(t.TempDir(), "repeat-marker")
	plans := []commandPlan{
		{Command: "touch " + marker, Purpose: "Create marker", Risk: "medium", RequiresConfirmation: true},
		{Command: "touch " + marker, Purpose: "Repeat marker creation", Risk: "medium", RequiresConfirmation: true, RepeatReason: core.RepeatReasonUserRequested},
	}
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "y\ny\n", func(deps RuntimeDeps) {
		deps.Trace = logger
		var err error
		batch, err = executeCommands(tracepkg.WithTurnID(t.Context(), turnID), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if len(batch.Executions) != 2 || len(batch.Skipped) != 0 {
		t.Fatalf("batch = %#v, want both confirmed executions", batch)
	}
	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "command_confirmation")) != 2 || len(traceEventsByName(events, "command_start")) != 2 {
		t.Fatalf("repeat did not traverse both confirmation paths")
	}
}

// TestExecuteCommandsAllowsEditedDuplicateWithReason checks final effective-command admission uses the typed cause.
func TestExecuteCommandsAllowsEditedDuplicateWithReason(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = false
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	plans := []commandPlan{{
		Command:      "printf proposed",
		Purpose:      "Repeat edited inspection",
		RepeatReason: core.RepeatReasonUserRequested,
	}}
	prior := []commandExecution{{Command: "printf prior", ExitCode: 0}}

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "e\nprintf prior\ny\n", func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, plans, prior)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
	})

	if len(batch.Executions) != 1 || batch.Executions[0].Command != "printf prior" || len(batch.Skipped) != 0 {
		t.Fatalf("batch = %#v, want admitted edited repeat", batch)
	}
}

// TestExecuteCommandsReconfirmsRiskyEditedCommand checks an edit cannot inherit confirmation for a different command.
func TestExecuteCommandsReconfirmsRiskyEditedCommand(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = false
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	marker := filepath.Join(t.TempDir(), "must-remain")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	plans := []commandPlan{{Command: "printf original", Purpose: "Edit command"}}

	var batch core.CommandBatchResult
	var runErr error
	captureMainLoopIO(t, "e\nrm "+marker+"\n", func(deps RuntimeDeps) {
		batch, runErr = executeCommands(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
	})

	if !errors.Is(runErr, core.ErrAborted) {
		t.Fatalf("executeCommands() error = %v, want confirmation abort", runErr)
	}
	if len(batch.Executions) != 0 {
		t.Fatalf("batch.Executions = %#v, want no edited execution", batch.Executions)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("edited command ran without confirmation: %v", err)
	}
}

// TestExecuteCommandsDoesNotSuppressPreviouslyFailedEffectiveCommand checks
// a failed effective identity from an earlier turn batch remains retryable.
func TestExecuteCommandsDoesNotSuppressPreviouslyFailedEffectiveCommand(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	ctxInfo := loopTestContext(t)
	plans := []commandPlan{{Command: "false", Purpose: "Retry failure", Classification: classificationSafe, LocalSafe: true}}
	priorExecutions := []commandExecution{{Command: " false\t", Purpose: "Earlier failure", ExitCode: 1}}

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, plans, priorExecutions)
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
	cfg := configpkg.DefaultConfig()
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

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
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
	cfg := configpkg.DefaultConfig()
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

	var batch core.CommandBatchResult
	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		var err error
		batch, err = executeCommands(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
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
	if !batch.Executions[0].TimedOut || batch.Executions[1].TimedOut {
		t.Fatalf("execution timeout metadata = %#v, want only the first execution timed out", batch.Executions)
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
	cfg := configpkg.DefaultConfig()
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

	var batch core.CommandBatchResult
	var runErr error
	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		batch, runErr = executeCommands(ctx, deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
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
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = true
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CaptureStdoutBytes = 3
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		deps.Trace = logger
		plans := []commandPlan{{
			Command:        "printf abcdef",
			Purpose:        "Print marker",
			Risk:           "safe",
			Classification: classificationSafe,
			LocalSafe:      true,
		}}
		batch, err := executeCommands(tracepkg.WithTurnID(t.Context(), turnID), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
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
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = true
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		deps.Trace = logger
		plans := []commandPlan{{
			Command:        "exit 7",
			Purpose:        "Fail with status",
			Risk:           "safe",
			Classification: classificationSafe,
			LocalSafe:      true,
		}}
		batch, err := executeCommands(tracepkg.WithTurnID(t.Context(), turnID), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
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
	cfg := configpkg.DefaultConfig()
	cfg.YesSafe = false
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	turnID := logger.StartTurn(nil)

	captureMainLoopIO(t, "e\nprintf edited\ny\n", func(deps RuntimeDeps) {
		deps.Trace = logger
		plans := []commandPlan{{
			Command:        "printf original",
			Purpose:        "Print marker",
			Risk:           "safe",
			Classification: classificationSafe,
			LocalSafe:      true,
		}}
		batch, err := executeCommands(tracepkg.WithTurnID(t.Context(), turnID), deps, false, executorOptions(cfg), &ctxInfo, plans, nil)
		if err != nil {
			t.Fatalf("executeCommands() error = %v", err)
		}
		if len(batch.Executions) != 1 || batch.Executions[0].Command != "printf edited" {
			t.Fatalf("executions = %#v, want edited command", batch.Executions)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	confirmations := traceEventsByName(events, "command_confirmation")
	if len(confirmations) != 2 {
		t.Fatalf("command_confirmation events = %d, want edit and edited-command confirmation", len(confirmations))
	}
	confirmationData := traceEventData(t, confirmations[0])
	if confirmationData["decision"] != "edit" {
		t.Fatalf("decision = %#v, want edit", confirmationData["decision"])
	}
	if confirmationData["command"] != "printf original" || confirmationData["edited_command"] != "printf edited" {
		t.Fatalf("confirmation data = %#v, want original and edited commands", confirmationData)
	}
	editedConfirmation := traceEventData(t, confirmations[1])
	if editedConfirmation["decision"] != "run" || editedConfirmation["command"] != "printf edited" {
		t.Fatalf("edited confirmation data = %#v, want explicit run of edited command", editedConfirmation)
	}

	starts := traceEventsByName(events, "command_start")
	if len(starts) != 1 {
		t.Fatalf("command_start events = %d, want 1", len(starts))
	}
	startData := traceEventData(t, starts[0])
	if startData["command"] != "printf edited" || startData["original_command"] != "printf original" {
		t.Fatalf("command_start data = %#v, want effective and original commands", startData)
	}
	if startData["classification"] == classificationSafe || startData["requires_confirmation"] != true {
		t.Fatalf("command_start data = %#v, want edited command's stricter local safety", startData)
	}
}

// TestExecuteManualCommandTraceRecordsCommand checks direct shell commands are traced.
func TestExecuteManualCommandTraceRecordsCommand(t *testing.T) {
	cfg := configpkg.DefaultConfig()
	cfg.ShowCommandPopup = false
	cfg.ShowSystemOutput = false
	cfg.CommandTimeout = 2 * time.Second
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		deps.Trace = logger
		execution, err := executeManualCommand(t.Context(), deps, false, executorOptions(cfg), &ctxInfo, "printf manual", manualRenderInline)
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
