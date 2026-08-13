package executor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/xEsk/shellia/internal/core"
	safetypkg "github.com/xEsk/shellia/internal/safety"
	tracepkg "github.com/xEsk/shellia/internal/trace"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// commandRunRequest groups the inputs needed to launch a shell command.
type commandRunRequest struct {
	Deps         RuntimeDeps
	UI           bool
	Options      Options
	ContextInfo  contextInfo
	Box          StepPresenter
	Command      string
	Timeout      time.Duration
	DirectStream bool
	Interactive  bool
}

// commandRunResult carries command output plus rendering state.
type commandRunResult struct {
	Output    commandExecution
	ExitCode  int
	HadOutput bool
}

// confirmationInput prevents one batch's buffered confirmation reader from
// consuming answers intended for a later planning or execution round.
type confirmationInput struct {
	io.Reader
}

// Read limits confirmation input to one byte at a time.
func (input confirmationInput) Read(buffer []byte) (int, error) {
	if len(buffer) > 1 {
		buffer = buffer[:1]
	}
	return input.Reader.Read(buffer)
}

// commandRunError represents an executed command that finished with an error or timeout.
type commandRunError struct {
	Command  string
	ExitCode int
	TimedOut bool
	Err      error
}

// interactivePromptError reports that a non-interactive command asked for terminal input.
type interactivePromptError struct {
	Command string
	Prompt  string
}

// Error returns a short description of the command failure.
func (err *commandRunError) Error() string {
	if err == nil {
		return ""
	}
	if err.TimedOut {
		return fmt.Sprintf("command timed out: %s", err.Command)
	}
	return fmt.Sprintf("command failed: %s", err.Command)
}

// Unwrap returns the underlying process or context error when available.
func (err *commandRunError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// Error returns a short description of the interactive prompt failure.
func (err *interactivePromptError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("command requires interactive input or a known non-interactive flag: %s", err.Command)
}

type manualRenderMode int

const (
	manualRenderInline manualRenderMode = iota
	manualRenderDirect
	manualRenderInteractive
	manualRenderShellInteractive
)

const (
	interactivePromptTailBytes = 256
	ptyInputBufferSize         = 1024
	ptyPollInterval            = 10 * time.Millisecond
)

var (
	credentialPromptPattern       = regexp.MustCompile(`(?i)\b(pass(?:word|phrase))\s*:\s*$`)
	yesNoPromptPattern            = regexp.MustCompile(`(?i)([\[(]\s*(?:y(?:es)?\s*[/,| ]+\s*no?|no?\s*[/,| ]+\s*y(?:es)?|y\s*n|n\s*y)\s*[])])`)
	continuePromptPattern         = regexp.MustCompile(`(?i)\bpress\s+(?:enter|return|any\s+key|a\s+key)\s+to\s+continue\b`)
	textConfirmationPromptPattern = regexp.MustCompile(`(?i)\btype\s+['"]?[a-z0-9_-]{2,}['"]?\s+to\s+(?:confirm|continue|proceed|delete|destroy)\b`)
)

// interactivePromptDetector watches output streams for prompts that need a real terminal.
type interactivePromptDetector struct {
	command string
	cancel  context.CancelFunc

	mu      sync.Mutex
	tail    string
	prompt  string
	matched bool
}

// newInteractivePromptDetector creates a shared detector for stdout and stderr.
func newInteractivePromptDetector(command string, cancel context.CancelFunc) *interactivePromptDetector {
	return &interactivePromptDetector{
		command: strings.TrimSpace(command),
		cancel:  cancel,
	}
}

// Write inspects streamed command output and cancels the command when a prompt is detected.
func (detector *interactivePromptDetector) Write(data []byte) (int, error) {
	if detector == nil || len(data) == 0 {
		return len(data), nil
	}

	text := string(data)

	detector.mu.Lock()
	if detector.matched {
		detector.mu.Unlock()
		return len(data), nil
	}

	detector.tail = appendInteractivePromptTail(detector.tail, text, interactivePromptTailBytes)
	prompt, matched := detectInteractivePrompt(detector.tail)
	if matched {
		detector.prompt = prompt
		detector.matched = true
	}
	detector.mu.Unlock()

	if matched && detector.cancel != nil {
		detector.cancel()
	}

	return len(data), nil
}

// promptError returns the detected interactive prompt as an error when present.
func (detector *interactivePromptDetector) promptError() error {
	if detector == nil {
		return nil
	}

	detector.mu.Lock()
	defer detector.mu.Unlock()

	if !detector.matched {
		return nil
	}

	return &interactivePromptError{
		Command: detector.command,
		Prompt:  detector.prompt,
	}
}

// appendInteractivePromptTail keeps only the most recent bytes needed for prompt detection.
func appendInteractivePromptTail(current string, chunk string, limit int) string {
	combined := current + chunk
	if limit <= 0 || len(combined) <= limit {
		return combined
	}
	combined = combined[len(combined)-limit:]
	for !utf8.ValidString(combined) && len(combined) > 0 {
		_, size := utf8.DecodeRuneInString(combined)
		if size <= 0 {
			break
		}
		combined = combined[size:]
	}
	return combined
}

// detectInteractivePrompt checks whether the current trailing output line is waiting for terminal input.
func detectInteractivePrompt(tail string) (string, bool) {
	if strings.HasSuffix(tail, "\n") || strings.HasSuffix(tail, "\r") {
		return "", false
	}

	line := tail
	if index := strings.LastIndexAny(line, "\r\n"); index >= 0 {
		line = line[index+1:]
	}

	cleaned := strings.TrimSpace(stripANSISequences(line))
	if cleaned == "" {
		return "", false
	}

	normalized := normalizeInteractivePromptText(cleaned)
	if credentialPromptPattern.MatchString(normalized) {
		return cleaned, true
	}
	if strings.Contains(normalized, "?") && yesNoPromptPattern.MatchString(normalized) {
		return cleaned, true
	}
	if continuePromptPattern.MatchString(normalized) {
		return cleaned, true
	}
	if textConfirmationPromptPattern.MatchString(normalized) {
		return cleaned, true
	}

	return "", false
}

// normalizeInteractivePromptText makes prompt detection tolerant of casing and repeated whitespace.
func normalizeInteractivePromptText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// stripANSISequences removes CSI-style ANSI escape sequences from a string.
func stripANSISequences(text string) string {
	var b strings.Builder
	runes := []rune(text)

	for index := 0; index < len(runes); index++ {
		if runes[index] == '\033' && index+1 < len(runes) && runes[index+1] == '[' {
			index += 2
			for index < len(runes) && (runes[index] < '@' || runes[index] > '~') {
				index++
			}
			continue
		}
		if runes[index] == '\033' {
			continue
		}
		b.WriteRune(runes[index])
	}

	return b.String()
}

// limitedCaptureWriter keeps only the configured first bytes of a stream.
type limitedCaptureWriter struct {
	limit      int
	totalBytes int
	truncated  bool
	buffer     bytes.Buffer
}

// Write stores up to the configured limit and discards the rest while keeping the total counter.
func (writer *limitedCaptureWriter) Write(data []byte) (int, error) {
	writer.totalBytes += len(data)

	if writer.limit <= 0 || writer.buffer.Len() >= writer.limit {
		if len(data) > 0 {
			writer.truncated = true
		}
		return len(data), nil
	}

	remaining := writer.limit - writer.buffer.Len()
	if remaining <= 0 {
		writer.truncated = true
		return len(data), nil
	}

	if len(data) > remaining {
		if _, err := writer.buffer.Write(data[:remaining]); err != nil {
			return 0, err
		}
		writer.truncated = true
		return len(data), nil
	}

	if _, err := writer.buffer.Write(data); err != nil {
		return 0, err
	}
	return len(data), nil
}

// Stream converts the captured buffer into a portable structure for the rest of the tool.
func (writer *limitedCaptureWriter) Stream() core.CapturedStream {
	text := strings.ToValidUTF8(strings.TrimSpace(writer.buffer.String()), "?")
	return core.CapturedStream{
		Text:       text,
		TotalBytes: writer.totalBytes,
		KeptBytes:  writer.buffer.Len(),
		Truncated:  writer.truncated,
	}
}

// getContext collects the local context available to the model and UI.
func getContext(_ context.Context, options ContextOptions) (contextInfo, error) {
	wd, err := os.Getwd()
	if err != nil {
		return contextInfo{}, fmt.Errorf("cannot detect current directory: %w", err)
	}

	shellPath := os.Getenv("SHELL")
	if strings.TrimSpace(shellPath) == "" {
		shellPath = "/bin/sh"
	}

	ctx := contextInfo{
		CWD:   wd,
		OS:    fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Shell: shellPath,
	}
	if options.IncludeUser {
		currentUser, err := user.Current()
		if err != nil {
			return contextInfo{}, fmt.Errorf("cannot detect current user: %w", err)
		}
		ctx.User = currentUser.Username
	}
	return ctx, nil
}

const skippedAfterFailureReason = "dependent on an earlier failed command"

// executeCommands runs the sequential plan and returns its structured batch outcome.
func executeCommands(ctx context.Context, deps RuntimeDeps, ui bool, options Options, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (core.CommandBatchResult, error) {
	deps = deps.withDefaults()
	turn := deps.Presenter.BeginTurn(*ctxInfo)
	defer turn.Close()
	reader := bufio.NewReader(confirmationInput{Reader: deps.Stdin})
	batch := core.CommandBatchResult{
		Executions: make([]commandExecution, 0, len(plans)),
		Skipped:    make([]core.SkippedCommand, 0, len(plans)),
	}
	blocked := false
	turnID := tracepkg.TurnID(ctx)
	succeeded := successfulCommandIdentities(priorExecutions)
	priorSucceeded := successfulCommandIdentities(priorExecutions)
	consumedRepeatAuthorizations := make(map[string]bool)
	repeatAuthorized := func(plan commandPlan, command string) bool {
		identity := strings.TrimSpace(command)
		return priorSucceeded[identity] && !consumedRepeatAuthorizations[identity] && plan.AllowsSuccessfulRepeat(command)
	}

	for index, plan := range plans {
		if blocked && !plan.IndependentOnFailure {
			batch.Skipped = append(batch.Skipped, skipCommand(deps, turn, nil, turnID, index, len(plans), plan, plan.Command, skippedAfterFailureReason))
			continue
		}
		if succeeded[strings.TrimSpace(plan.Command)] && !repeatAuthorized(plan, plan.Command) {
			batch.Skipped = append(batch.Skipped, skipCommand(deps, turn, nil, turnID, index, len(plans), plan, plan.Command, core.RepeatReasonRequired))
			continue
		}

		box := turn.BeginStep(index+1, len(plans), plan)
		effectiveCommand := plan.Command
		interactive := plan.Interactive

		if !options.YesSafe || !plan.LocalSafe || plan.Interactive {
			for {
				decision, editedCommand, err := deps.Presenter.Confirm(box, reader, deps.Stdin, fmt.Sprintf("Run step %d/%d?", index+1, len(plans)), effectiveCommand, options.ConfirmationDefault)
				deps.Trace.Record("command_confirmation", turnID, "", -1, map[string]any{
					"step":           index + 1,
					"total_steps":    len(plans),
					"command":        effectiveCommand,
					"decision":       decision.TraceValue(),
					"edited_command": editedCommand,
				})
				if err != nil {
					deps.Trace.Record("command_error", turnID, "", -1, map[string]any{
						"step":    index + 1,
						"command": effectiveCommand,
						"error":   err.Error(),
					})
					box.Close()
					return batch, fmt.Errorf("cannot read confirmation: %w", err)
				}
				if decision == ConfirmationDecisionCancel {
					box.Close()
					return batch, core.ErrAborted
				}
				if decision == ConfirmationDecisionEdit {
					effectiveCommand = editedCommand
					localSafety := safetypkg.ClassifyCommand(effectiveCommand)
					plan.Risk = safetypkg.HigherRisk(plan.Risk, localSafety.Risk)
					plan.Classification = localSafety.Classification
					plan.RequiresConfirmation = plan.RequiresConfirmation || localSafety.RequiresConfirmation
					plan.LocalSafe = localSafety.Classification == safetypkg.ClassificationSafe
					if succeeded[strings.TrimSpace(effectiveCommand)] && !repeatAuthorized(plan, effectiveCommand) {
						break
					}
					if localSafety.RequiresConfirmation {
						continue
					}
				}
				if decision == ConfirmationDecisionInteractive {
					interactive = true
				}
				break
			}
		} else {
			deps.Trace.Record("command_confirmation", turnID, "", -1, map[string]any{
				"step":        index + 1,
				"total_steps": len(plans),
				"command":     plan.Command,
				"decision":    "auto_safe",
			})
		}
		if succeeded[strings.TrimSpace(effectiveCommand)] && !repeatAuthorized(plan, effectiveCommand) {
			batch.Skipped = append(batch.Skipped, skipCommand(deps, turn, box, turnID, index, len(plans), plan, effectiveCommand, core.RepeatReasonRequired))
			continue
		}
		if priorSucceeded[strings.TrimSpace(effectiveCommand)] {
			consumedRepeatAuthorizations[strings.TrimSpace(effectiveCommand)] = true
		}

		deps.Trace.Record("command_start", turnID, "", -1, map[string]any{
			"step":                  index + 1,
			"total_steps":           len(plans),
			"command":               effectiveCommand,
			"original_command":      plan.Command,
			"purpose":               plan.Purpose,
			"risk":                  plan.Risk,
			"classification":        plan.Classification,
			"requires_confirmation": plan.RequiresConfirmation,
			"repeat_reason":         plan.RepeatReason,
			"interactive":           interactive,
		})
		result, err := executeOneCommand(ctx, commandRunRequest{
			Deps:        deps,
			UI:          ui,
			Options:     options,
			ContextInfo: *ctxInfo,
			Box:         box,
			Command:     effectiveCommand,
			Timeout:     options.CommandTimeout,
			Interactive: interactive,
		})
		if errors.Is(ctx.Err(), context.Canceled) {
			deps.Trace.Record("command_error", turnID, "", -1, map[string]any{
				"step":      index + 1,
				"command":   effectiveCommand,
				"exit_code": result.ExitCode,
				"error":     context.Canceled.Error(),
			})
			if box != nil {
				box.Text("interrupted", ToneDim)
				box.Close()
			}
			return batch, context.Canceled
		}
		var executionError *commandRunError
		timedOut := errors.As(err, &executionError) && executionError.TimedOut
		batch.Executions = append(batch.Executions, commandExecution{
			Command:  effectiveCommand,
			Purpose:  plan.Purpose,
			Stdout:   result.Output.Stdout,
			Stderr:   result.Output.Stderr,
			ExitCode: result.ExitCode,
			TimedOut: timedOut,
		})
		deps.Trace.Record("command_end", turnID, "", -1, map[string]any{
			"step":        index + 1,
			"total_steps": len(plans),
			"execution":   tracepkg.ExecutionData(batch.Executions[len(batch.Executions)-1]),
		})
		if result.ExitCode == 0 {
			succeeded[strings.TrimSpace(effectiveCommand)] = true
		}
		if err != nil {
			deps.Trace.Record("command_error", turnID, "", -1, map[string]any{
				"step":      index + 1,
				"command":   effectiveCommand,
				"exit_code": result.ExitCode,
				"error":     err.Error(),
			})
			if box != nil {
				var runErr *commandRunError
				var promptErr *interactivePromptError
				switch {
				case errors.As(err, &promptErr):
					box.Text("interactive prompt detected", ToneDim)
				case errors.As(err, &runErr) && runErr.TimedOut:
					box.Text("timed out", ToneDim)
				case errors.As(err, &runErr):
					box.Text(fmt.Sprintf("exit code %d", runErr.ExitCode), ToneDim)
				default:
					box.Text("interrupted", ToneDim)
				}
				box.Close()
			}
			var promptErr *interactivePromptError
			if errors.As(err, &promptErr) {
				return batch, err
			}
			if errors.Is(err, core.ErrAborted) {
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
			if !options.ContinueOnError {
				return batch, nil
			}
			deps.Presenter.Warning(err.Error())
			continue
		}

		applySessionState(ctxInfo, effectiveCommand, result.ExitCode)
		showCompletedMarker(box, result.HadOutput)
		if box != nil {
			box.Close()
		}
	}

	return batch, nil
}

// successfulCommandIdentities returns exact trimmed identities for prior successful executions.
func successfulCommandIdentities(executions []commandExecution) map[string]bool {
	succeeded := make(map[string]bool, len(executions))
	for _, execution := range executions {
		identity := strings.TrimSpace(execution.Command)
		if execution.ExitCode == 0 && identity != "" {
			succeeded[identity] = true
		}
	}
	return succeeded
}

// skipCommand renders and traces one command that was deliberately not executed.
func skipCommand(deps RuntimeDeps, turn TurnPresenter, box StepPresenter, turnID string, index int, total int, plan commandPlan, effectiveCommand string, reason string) core.SkippedCommand {
	skipped := core.SkippedCommand{
		Command: effectiveCommand,
		Purpose: plan.Purpose,
		Reason:  reason,
	}
	if box == nil {
		box = turn.BeginStep(index+1, total, plan)
	}
	box.Section("skipped", ToneDim)
	box.Text(reason, ToneDim)
	box.Close()
	deps.Trace.Record("command_skipped", turnID, "", -1, map[string]any{
		"step":        index + 1,
		"total_steps": total,
		"command":     effectiveCommand,
		"purpose":     plan.Purpose,
		"reason":      reason,
	})
	return skipped
}

// executeManualCommand runs a direct shell command inside the current Shellia session.
func executeManualCommand(ctx context.Context, deps RuntimeDeps, ui bool, options Options, ctxInfo *contextInfo, command string, renderMode manualRenderMode) (commandExecution, error) {
	deps = deps.withDefaults()
	turnID := tracepkg.TurnID(ctx)
	var box StepPresenter
	switch renderMode {
	case manualRenderInline:
		box = deps.Presenter.BeginManualStep(command)
	case manualRenderInteractive:
		deps.Presenter.InteractiveCommandStart()
	case manualRenderDirect, manualRenderShellInteractive:
		// These modes render command output directly, so there is no inline preface.
	}

	deps.Trace.Record("manual_command_start", turnID, "", -1, map[string]any{
		"command":     command,
		"render_mode": int(renderMode),
	})
	result, err := executeOneCommand(ctx, commandRunRequest{
		Deps:         deps,
		UI:           ui,
		Options:      options,
		ContextInfo:  *ctxInfo,
		Box:          box,
		Command:      command,
		Timeout:      options.CommandTimeout,
		DirectStream: renderMode == manualRenderDirect,
		Interactive:  renderMode == manualRenderInteractive || renderMode == manualRenderShellInteractive,
	})
	execution := commandExecution{
		Command:  command,
		Purpose:  "Manual shell command",
		Stdout:   result.Output.Stdout,
		Stderr:   result.Output.Stderr,
		ExitCode: result.ExitCode,
	}
	deps.Trace.Record("manual_command_end", turnID, "", -1, tracepkg.ExecutionData(execution))

	if err != nil {
		deps.Trace.Record("manual_command_error", turnID, "", -1, map[string]any{
			"command":   command,
			"exit_code": result.ExitCode,
			"error":     err.Error(),
		})
		if box != nil {
			var runErr *commandRunError
			var promptErr *interactivePromptError
			switch {
			case errors.As(err, &promptErr):
				box.Text("interactive prompt detected", ToneDim)
			case errors.As(err, &runErr) && runErr.TimedOut:
				box.Text("timed out", ToneDim)
			case errors.As(err, &runErr):
				box.Text(fmt.Sprintf("exit code %d", runErr.ExitCode), ToneDim)
			default:
				box.Text("interrupted", ToneDim)
			}
			box.Close()
		}
		return execution, err
	}

	applySessionState(ctxInfo, command, result.ExitCode)
	showCompletedMarker(box, result.HadOutput)
	if box != nil {
		box.Close()
	}
	return execution, nil
}

// showCompletedMarker prints a compact success marker when the command produced no visible output.
func showCompletedMarker(box StepPresenter, hadOutput bool) {
	if box == nil || box.IsClosed() || hadOutput {
		return
	}
	box.Section("completed", ToneSuccess)
}

// executeOneCommand launches a command via the current shell with real-time output streaming.
func executeOneCommand(ctx context.Context, request commandRunRequest) (commandRunResult, error) {
	deps := request.Deps.withDefaults()
	options := request.Options
	ctxInfo := request.ContextInfo
	box := request.Box
	command := request.Command

	var cmdCtx context.Context
	var cancel context.CancelFunc

	if request.Timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, request.Timeout)
	} else {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	shellPath := ctxInfo.Shell
	if strings.TrimSpace(shellPath) == "" {
		shellPath = "/bin/sh"
	}

	if request.Interactive {
		if box != nil {
			box.Section("interactive session", ToneWarning)
			box.Text("Shellia will resume when the command exits.", ToneDim)
			box.Close()
		}
		if turn := deps.Presenter.ActiveTurn(); turn != nil {
			turn.Suspend()
			defer turn.Resume()
		}
		output, exitCode, hadOutput, err := executeInteractiveCommand(cmdCtx, deps, options, ctxInfo, shellPath, command)
		return commandRunResult{
			Output:    output,
			ExitCode:  exitCode,
			HadOutput: hadOutput,
		}, err
	}

	request.Deps = deps
	return executeNonInteractiveCommand(cmdCtx, ctx, request, shellPath, cancel)
}

// executeNonInteractiveCommand runs a shell command with captured stdout/stderr streams.
func executeNonInteractiveCommand(
	ctx context.Context,
	parentCtx context.Context,
	request commandRunRequest,
	shellPath string,
	cancel context.CancelFunc,
) (commandRunResult, error) {
	deps := request.Deps
	options := request.Options
	ctxInfo := request.ContextInfo
	box := request.Box
	command := request.Command

	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	configureProcessGroupCancellation(cmd)
	cmd.Dir = ctxInfo.CWD
	cmd.Stdin = nil

	stdoutCapture := &limitedCaptureWriter{limit: options.CaptureStdoutBytes}
	stderrCapture := &limitedCaptureWriter{limit: options.CaptureStderrBytes}
	detector := newInteractivePromptDetector(command, cancel)

	var stdoutStream io.Writer
	var stderrStream io.Writer
	var stdoutWriter *prefixedWriter
	var stderrWriter *prefixedWriter

	if request.DirectStream {
		stdoutStream = &directShellWriter{presenter: deps.Presenter, target: deps.Stdout, lineStart: true}
		stderrStream = &directShellWriter{presenter: deps.Presenter, target: deps.Stderr, lineStart: true}
	} else {
		stdoutWriter = &prefixedWriter{box: box, hidden: !options.ShowSystemOutput}
		stderrWriter = &prefixedWriter{box: box, hidden: !options.ShowSystemOutput}
		stdoutStream = stdoutWriter
		stderrStream = stderrWriter
	}

	cmd.Stdout = io.MultiWriter(stdoutStream, stdoutCapture, detector)
	cmd.Stderr = io.MultiWriter(stderrStream, stderrCapture, detector)

	if err := cmd.Start(); err != nil {
		return commandRunResult{ExitCode: 1}, fmt.Errorf("cannot start command %q: %w", command, err)
	}

	waitErr := cmd.Wait()
	if stdoutWriter != nil {
		if err := stdoutWriter.Flush(); err != nil {
			return commandRunResult{
				Output:    commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()},
				ExitCode:  1,
				HadOutput: stdoutWriter.started || stderrWriter.started,
			}, fmt.Errorf("cannot flush command stdout: %w", err)
		}
	}
	if stderrWriter != nil {
		if err := stderrWriter.Flush(); err != nil {
			return commandRunResult{
				Output:    commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()},
				ExitCode:  1,
				HadOutput: prefixedWritersHadOutput(stdoutWriter, stderrWriter),
			}, fmt.Errorf("cannot flush command stderr: %w", err)
		}
	}
	if request.DirectStream {
		if flusher, ok := stdoutStream.(*directShellWriter); ok {
			if err := flusher.Flush(); err != nil {
				return commandRunResult{
					Output:   commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()},
					ExitCode: 1,
				}, fmt.Errorf("cannot flush direct command stdout: %w", err)
			}
		}
		if flusher, ok := stderrStream.(*directShellWriter); ok {
			if err := flusher.Flush(); err != nil {
				return commandRunResult{
					Output:   commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()},
					ExitCode: 1,
				}, fmt.Errorf("cannot flush direct command stderr: %w", err)
			}
		}
	}
	hadOutput := false
	if request.DirectStream {
		hadOutput = stdoutCapture.totalBytes > 0 || stderrCapture.totalBytes > 0
	} else {
		hadOutput = prefixedWritersHadOutput(stdoutWriter, stderrWriter)
	}
	output := commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()}
	result := commandRunResult{
		Output:    output,
		HadOutput: hadOutput,
	}

	if promptErr := detector.promptError(); promptErr != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		result.ExitCode = code
		return result, promptErr
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
		return result, &commandRunError{Command: command, ExitCode: 124, TimedOut: true, Err: ctx.Err()}
	}
	if parentCtx.Err() != nil {
		// Parent context cancelled (Ctrl+C): propagate directly so callers can
		// detect context.Canceled with errors.Is and abort cleanly.
		return result, parentCtx.Err()
	}
	if waitErr != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		result.ExitCode = code
		return result, &commandRunError{Command: command, ExitCode: code, Err: waitErr}
	}

	return result, nil
}

// configureProcessGroupCancellation makes context cancellation stop the shell and its descendants.
func configureProcessGroupCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
		if errors.Is(err, unix.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// prefixedWritersHadOutput reports whether any prefixed stream rendered output.
func prefixedWritersHadOutput(writers ...*prefixedWriter) bool {
	for _, writer := range writers {
		if writer != nil && writer.started {
			return true
		}
	}
	return false
}

// executeInteractiveCommand temporarily hands over the terminal to a process that
// needs a real interactive session and captures part of its output.
func executeInteractiveCommand(ctx context.Context, deps RuntimeDeps, options Options, ctxInfo contextInfo, shellPath string, command string) (output commandExecution, exitCode int, hadOutput bool, err error) {
	deps = deps.withDefaults()
	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	cmd.Dir = ctxInfo.CWD

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return commandExecution{}, 1, false, fmt.Errorf("cannot start interactive command %q: %w", command, err)
	}
	defer func() {
		_ = ptmx.Close()
	}()

	stdoutCapture := &limitedCaptureWriter{limit: options.CaptureStdoutBytes}
	stream := io.MultiWriter(deps.Stdout, stdoutCapture)

	fd := int(deps.Stdin.Fd())
	restoreTerminal := func() {}
	restoreBlocking := func() {}
	if term.IsTerminal(fd) {
		state, rawErr := term.MakeRaw(fd)
		if rawErr == nil {
			restoreTerminal = func() {
				_ = term.Restore(fd, state)
			}
		}
	}
	if blockErr := unix.SetNonblock(fd, true); blockErr == nil {
		restoreBlocking = func() {
			_ = unix.SetNonblock(fd, false)
		}
	}
	defer restoreTerminal()
	defer restoreBlocking()

	if _, err := fmt.Fprintln(deps.Stdout); err != nil {
		return commandExecution{}, 1, false, fmt.Errorf("cannot prepare interactive command output: %w", err)
	}

	_ = pty.InheritSize(deps.Stdin, ptmx)

	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(stream, ptmx)
		copyDone <- copyErr
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	inputBuffer := make([]byte, ptyInputBufferSize)
	var waitErr error
	for {
		select {
		case waitErr = <-waitDone:
			_ = ptmx.Close()
			goto done
		default:
		}

		n, readErr := unix.Read(fd, inputBuffer)
		if n > 0 {
			if _, writeErr := ptmx.Write(inputBuffer[:n]); writeErr != nil {
				_ = ptmx.Close()
				waitErr = <-waitDone
				goto done
			}
			continue
		}

		if readErr == nil || errors.Is(readErr, unix.EAGAIN) || errors.Is(readErr, unix.EWOULDBLOCK) || errors.Is(readErr, unix.EINTR) {
			time.Sleep(ptyPollInterval)
			continue
		}

		_ = ptmx.Close()
		waitErr = <-waitDone
		break
	}

done:
	<-copyDone

	if stdoutCapture.totalBytes > 0 {
		hadOutput = true
	}
	output = commandExecution{Stdout: stdoutCapture.Stream()}

	if ctx.Err() == context.DeadlineExceeded {
		return output, 124, hadOutput, &commandRunError{Command: command, ExitCode: 124, TimedOut: true, Err: ctx.Err()}
	}
	if ctx.Err() != nil {
		return output, 0, hadOutput, ctx.Err()
	}
	if waitErr != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		return output, code, hadOutput, &commandRunError{Command: command, ExitCode: code, Err: waitErr}
	}

	return output, 0, hadOutput, nil
}

// applySessionState updates the persistent context when a command changes the session state.
func applySessionState(ctxInfo *contextInfo, command string, exitCode int) {
	if ctxInfo == nil || exitCode != 0 {
		return
	}

	nextCWD, changed := resolveDirectoryChange(ctxInfo.CWD, command)
	if !changed {
		return
	}

	ctxInfo.CWD = nextCWD
}

// resolveDirectoryChange detects a simple cd and computes the next session cwd.
func resolveDirectoryChange(currentCWD, command string) (string, bool) {
	target, ok := parseSimpleCDTarget(command)
	if !ok {
		return "", false
	}
	if safetypkg.HasShellOperators(command) {
		return "", false
	}

	if target == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		target = home
	}

	if strings.HasPrefix(target, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		switch {
		case target == "~":
			target = home
		case strings.HasPrefix(target, "~/"):
			target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
		default:
			return "", false
		}
	}

	if target == "-" {
		return "", false
	}

	if !filepath.IsAbs(target) {
		target = filepath.Join(currentCWD, target)
	}

	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}

	return resolved, true
}

// parseSimpleCDTarget extracts the directory argument from a standalone cd command.
func parseSimpleCDTarget(command string) (string, bool) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || !strings.HasPrefix(trimmed, "cd") {
		return "", false
	}

	// After "cd" the next character must be a space, tab, or end-of-string.
	// This prevents matching commands like cdup, cdrecord, cd_anything.
	after := trimmed[2:]
	if after != "" && after[0] != ' ' && after[0] != '\t' {
		return "", false
	}

	rest := strings.TrimSpace(after)
	if rest == "" {
		return "", true
	}

	if rest[0] == '"' || rest[0] == '\'' {
		if len(rest) < 2 || rest[len(rest)-1] != rest[0] {
			return "", false
		}
		if rest[0] == '"' {
			unquoted, err := strconv.Unquote(rest)
			if err != nil {
				return "", false
			}
			return unquoted, true
		}
		return rest[1 : len(rest)-1], true
	}

	if hasUnescapedWhitespace(rest) {
		return "", false
	}

	rest = strings.ReplaceAll(rest, "\\ ", " ")
	return rest, true
}

// hasUnescapedWhitespace reports whether a shell token contains plain whitespace.
func hasUnescapedWhitespace(value string) bool {
	escaped := false
	for _, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == ' ' || r == '\t' {
			return true
		}
	}
	return false
}
