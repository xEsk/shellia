package main

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
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// commandPlan is the combined result of LLM planning and local safety classification.
// It flows from llm.go (normalizePlan) through executor.go (executeCommands) to ui.go (render).
type commandPlan struct {
	Command              string
	Purpose              string
	Risk                 string
	RequiresConfirmation bool
	Classification       string
	LocalSafe            bool
	Interactive          bool
	InteractiveReason    string
}

type commandExecution struct {
	Command  string
	Purpose  string
	Stdout   capturedStream
	Stderr   capturedStream
	ExitCode int
}

// commandRunError represents an executed command that finished with an error or timeout.
type commandRunError struct {
	Command  string
	ExitCode int
	TimedOut bool
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

const interactivePromptTailBytes = 256

var (
	credentialPromptPattern = regexp.MustCompile(`(?i)\b(pass(?:word|phrase))\s*:\s*$`)
	yesNoPromptPattern      = regexp.MustCompile(`(?i)([\[\(]\s*(?:y(?:es)?\s*[/,| ]+\s*n(?:o)?|n(?:o)?\s*[/,| ]+\s*y(?:es)?|y\s*n|n\s*y)\s*[\]\)])`)
)

// capturedStream represents the captured part of a stream, with truncation information.
type capturedStream struct {
	Text       string
	TotalBytes int
	KeptBytes  int
	Truncated  bool
}

// interactivePromptDetector watches output streams for prompts that need a real terminal.
type interactivePromptDetector struct {
	command string
	cancel  context.CancelFunc

	mu      sync.Mutex
	tail    string
	prompt  string
	matched bool
}

// HasOutput reports whether the captured stream contains useful text.
func (stream capturedStream) HasOutput() bool {
	return strings.TrimSpace(stream.Text) != ""
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
	return combined[len(combined)-limit:]
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
			for index < len(runes) && !(runes[index] >= '@' && runes[index] <= '~') {
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

// RenderForPrompt prepares the stream to be sent to the model with a truncation notice.
func (stream capturedStream) RenderForPrompt(label string, limit int) string {
	if !stream.HasOutput() {
		return ""
	}

	var body strings.Builder
	if stream.Truncated {
		fmt.Fprintf(&body, "[%s truncated locally: kept %d of %d bytes]\n", label, stream.KeptBytes, stream.TotalBytes)
	}
	body.WriteString(trimForSummary(stream.Text, limit))

	return fmt.Sprintf("%s:\n%s", label, body.String())
}

// TextForUser returns the captured text with a short notice if the stream was truncated.
func (stream capturedStream) TextForUser() string {
	if !stream.HasOutput() {
		return ""
	}

	text := strings.TrimSpace(stream.Text)
	if !stream.Truncated {
		return text
	}
	return text + fmt.Sprintf("\n...[output truncated locally: kept %d of %d bytes]", stream.KeptBytes, stream.TotalBytes)
}

// PreferredOutput returns the best available output for simple answers.
func (execution commandExecution) PreferredOutput() string {
	if execution.ExitCode != 0 && execution.Stderr.HasOutput() {
		return execution.Stderr.TextForUser()
	}
	if execution.Stdout.HasOutput() {
		return execution.Stdout.TextForUser()
	}
	if execution.Stderr.HasOutput() {
		return execution.Stderr.TextForUser()
	}
	return ""
}

// PromptTranscript builds a short transcript prioritising stderr over stdout.
func (execution commandExecution) PromptTranscript(limit int) string {
	if limit <= 0 {
		limit = 1
	}
	if !execution.Stdout.HasOutput() && !execution.Stderr.HasOutput() {
		return "Output: (empty)"
	}

	stderrBudget := limit
	stdoutBudget := limit
	if execution.Stderr.HasOutput() && execution.Stdout.HasOutput() {
		stderrBudget = (limit * 2) / 3
		if stderrBudget <= 0 {
			stderrBudget = 1
		}
		stdoutBudget = limit - stderrBudget
		if stdoutBudget <= 0 {
			stdoutBudget = 1
		}
	}

	sections := make([]string, 0, 2)
	if execution.Stderr.HasOutput() {
		sections = append(sections, execution.Stderr.RenderForPrompt("stderr", stderrBudget))
	}
	if execution.Stdout.HasOutput() {
		sections = append(sections, execution.Stdout.RenderForPrompt("stdout", stdoutBudget))
	}

	return strings.Join(sections, "\n")
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
func (writer *limitedCaptureWriter) Stream() capturedStream {
	return capturedStream{
		Text:       strings.TrimSpace(writer.buffer.String()),
		TotalBytes: writer.totalBytes,
		KeptBytes:  writer.buffer.Len(),
		Truncated:  writer.truncated,
	}
}

// getContext collects the local context sent to the model.
func getContext() (contextInfo, error) {
	wd, err := os.Getwd()
	if err != nil {
		return contextInfo{}, fmt.Errorf("cannot detect current directory: %w", err)
	}

	currentUser, err := user.Current()
	if err != nil {
		return contextInfo{}, fmt.Errorf("cannot detect current user: %w", err)
	}

	shellPath := os.Getenv("SHELL")
	if strings.TrimSpace(shellPath) == "" {
		shellPath = "/bin/sh"
	}

	return contextInfo{
		CWD:   wd,
		User:  currentUser.Username,
		OS:    fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		Shell: shellPath,
		Git:   getGitContext(wd),
	}, nil
}

// getGitContext detects whether the current directory belongs to a Git repository.
func getGitContext(cwd string) gitContext {
	if _, err := exec.LookPath("git"); err != nil {
		return gitContext{}
	}

	if code, _ := runCommandCapture(cwd, "git", "rev-parse", "--is-inside-work-tree"); code != 0 {
		return gitContext{}
	}

	ctx := gitContext{IsRepo: true}

	if code, output := runCommandCapture(cwd, "git", "branch", "--show-current"); code == 0 {
		if output == "" {
			ctx.Branch = "DETACHED"
		} else {
			ctx.Branch = output
		}
	}

	if code, output := runCommandCapture(cwd, "git", "status", "--short"); code == 0 {
		ctx.StatusShort = output
	}

	return ctx
}

// executeCommands runs the sequential plan, stopping on the first unrecoverable error.
func executeCommands(ctx context.Context, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan) ([]commandExecution, error) {
	reader := bufio.NewReader(os.Stdin)
	executions := make([]commandExecution, 0, len(plans))

	for index, plan := range plans {
		box := printCommandExecution(ui, cfg, index+1, len(plans), plan)
		effectiveCommand := plan.Command
		interactive := plan.Interactive

		if !(cfg.YesSafe && plan.LocalSafe && !plan.Interactive) {
			decision, editedCommand, err := promptConfirmation(box, reader, fmt.Sprintf("Run step %d/%d?", index+1, len(plans)), plan.Command, cfg.ConfirmationDefault)
			if err != nil {
				box.Close()
				return nil, fmt.Errorf("cannot read confirmation: %w", err)
			}
			if decision == confirmDecisionCancel {
				box.Close()
				return nil, errAborted
			}
			if decision == confirmDecisionEdit {
				effectiveCommand = editedCommand
			}
			if decision == confirmDecisionInteractive {
				interactive = true
			}
		}

		output, exitCode, hadOutput, err := executeOneCommand(ctx, ui, cfg, *ctxInfo, box, effectiveCommand, cfg.CommandTimeout, false, interactive)
		executions = append(executions, commandExecution{
			Command:  effectiveCommand,
			Purpose:  plan.Purpose,
			Stdout:   output.Stdout,
			Stderr:   output.Stderr,
			ExitCode: exitCode,
		})
		if err != nil {
			if box != nil {
				var runErr *commandRunError
				var promptErr *interactivePromptError
				switch {
				case errors.As(err, &promptErr):
					box.Text("interactive prompt detected", colorDim)
				case errors.As(err, &runErr) && runErr.TimedOut:
					box.Text("timed out", colorDim)
				case errors.As(err, &runErr):
					box.Text(fmt.Sprintf("exit code %d", runErr.ExitCode), colorDim)
				default:
					box.Text("interrupted", colorDim)
				}
				box.Close()
			}
			var promptErr *interactivePromptError
			if errors.As(err, &promptErr) {
				return executions, err
			}
			if cfg.ContinueOnError {
				printWarning(ui, err.Error())
				continue
			}
			return executions, err
		}

		applySessionState(ctxInfo, effectiveCommand, exitCode)
		showCompletedMarker(box, hadOutput)
		if box != nil {
			box.Close()
		}
	}

	return executions, nil
}

// executeManualCommand runs a direct shell command inside the current Shellia session.
func executeManualCommand(ctx context.Context, ui bool, cfg config, ctxInfo *contextInfo, command string, renderMode manualRenderMode) (commandExecution, error) {
	var box *stepBox
	if renderMode == manualRenderInline {
		box = newStepBox(os.Stdout, ui, "shell")
		box.Spacer()
		box.Command(command)
	} else if renderMode == manualRenderInteractive {
		printInfo(ui, "Starting interactive command. Shellia will resume when it exits.")
	}

	output, exitCode, hadOutput, err := executeOneCommand(
		ctx,
		ui,
		cfg,
		*ctxInfo,
		box,
		command,
		cfg.CommandTimeout,
		renderMode == manualRenderDirect,
		renderMode == manualRenderInteractive || renderMode == manualRenderShellInteractive,
	)
	execution := commandExecution{
		Command:  command,
		Purpose:  "Manual shell command",
		Stdout:   output.Stdout,
		Stderr:   output.Stderr,
		ExitCode: exitCode,
	}

	if err != nil {
		if box != nil {
			var runErr *commandRunError
			var promptErr *interactivePromptError
			switch {
			case errors.As(err, &promptErr):
				box.Text("interactive prompt detected", colorDim)
			case errors.As(err, &runErr) && runErr.TimedOut:
				box.Text("timed out", colorDim)
			case errors.As(err, &runErr):
				box.Text(fmt.Sprintf("exit code %d", runErr.ExitCode), colorDim)
			default:
				box.Text("interrupted", colorDim)
			}
			box.Close()
		}
		return execution, err
	}

	applySessionState(ctxInfo, command, exitCode)
	showCompletedMarker(box, hadOutput)
	if box != nil {
		box.Close()
	}
	return execution, nil
}

// showCompletedMarker prints a compact success marker when the command produced no visible output.
func showCompletedMarker(box *stepBox, hadOutput bool) {
	if box == nil || box.closed || hadOutput {
		return
	}
	box.Section("completed", colorGreen)
}

// executeOneCommand launches a command via the current shell with real-time output streaming.
// hadOutput is true when command output was rendered to the user.
func executeOneCommand(ctx context.Context, ui bool, cfg config, ctxInfo contextInfo, box *stepBox, command string, timeout time.Duration, directStream bool, interactive bool) (output commandExecution, exitCode int, hadOutput bool, err error) {
	var cmdCtx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	shellPath := ctxInfo.Shell
	if strings.TrimSpace(shellPath) == "" {
		shellPath = "/bin/sh"
	}

	if interactive {
		if box != nil {
			box.Spacer()
			box.Section("interactive session", colorYellow)
			box.Text("Shellia will resume when the command exits.", colorDim)
			box.Close()
		}
		return executeInteractiveCommand(cmdCtx, ui, cfg, ctxInfo, shellPath, command)
	}

	cmd := exec.CommandContext(cmdCtx, shellPath, "-c", command)
	cmd.Dir = ctxInfo.CWD
	cmd.Stdin = nil

	stdoutCapture := &limitedCaptureWriter{limit: cfg.CaptureStdoutBytes}
	stderrCapture := &limitedCaptureWriter{limit: cfg.CaptureStderrBytes}
	detector := newInteractivePromptDetector(command, cancel)

	var stdoutStream io.Writer
	var stderrStream io.Writer
	var stdoutWriter *prefixedWriter
	var stderrWriter *prefixedWriter

	if directStream {
		stdoutStream = &directShellWriter{ui: ui, target: os.Stdout, lineStart: true}
		stderrStream = &directShellWriter{ui: ui, target: os.Stderr, lineStart: true}
	} else {
		stdoutWriter = &prefixedWriter{box: box, hidden: !cfg.ShowSystemOutput}
		stderrWriter = &prefixedWriter{box: box, hidden: !cfg.ShowSystemOutput}
		stdoutStream = stdoutWriter
		stderrStream = stderrWriter
	}

	cmd.Stdout = io.MultiWriter(stdoutStream, stdoutCapture, detector)
	cmd.Stderr = io.MultiWriter(stderrStream, stderrCapture, detector)

	if err := cmd.Start(); err != nil {
		return commandExecution{}, 1, false, fmt.Errorf("cannot start command %q: %w", command, err)
	}

	waitErr := cmd.Wait()
	if stdoutWriter != nil {
		if err := stdoutWriter.Flush(); err != nil {
			return commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()}, 1, stdoutWriter.started || stderrWriter.started, err
		}
	}
	if stderrWriter != nil {
		if err := stderrWriter.Flush(); err != nil {
			return commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()}, 1, stdoutWriter.started || stderrWriter.started, err
		}
	}
	if directStream {
		if flusher, ok := stdoutStream.(*directShellWriter); ok {
			if err := flusher.Flush(); err != nil {
				return commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()}, 1, false, err
			}
		}
		if flusher, ok := stderrStream.(*directShellWriter); ok {
			if err := flusher.Flush(); err != nil {
				return commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()}, 1, false, err
			}
		}
	}
	if directStream {
		hadOutput = stdoutCapture.totalBytes > 0 || stderrCapture.totalBytes > 0
	} else {
		hadOutput = stdoutWriter.started || stderrWriter.started
	}
	output = commandExecution{Stdout: stdoutCapture.Stream(), Stderr: stderrCapture.Stream()}

	if promptErr := detector.promptError(); promptErr != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		return output, code, hadOutput, promptErr
	}

	if cmdCtx.Err() == context.DeadlineExceeded {
		return output, 124, hadOutput, &commandRunError{Command: command, ExitCode: 124, TimedOut: true}
	}
	if ctx.Err() != nil {
		// Parent context cancelled (Ctrl+C): propagate directly so callers can
		// detect context.Canceled with errors.Is and abort cleanly.
		return output, 0, hadOutput, ctx.Err()
	}
	if waitErr != nil {
		code := 1
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		return output, code, hadOutput, &commandRunError{Command: command, ExitCode: code}
	}

	return output, 0, hadOutput, nil
}

// executeInteractiveCommand temporarily hands over the terminal to a process that
// needs a real interactive session and captures part of its output.
func executeInteractiveCommand(ctx context.Context, ui bool, cfg config, ctxInfo contextInfo, shellPath string, command string) (output commandExecution, exitCode int, hadOutput bool, err error) {
	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	cmd.Dir = ctxInfo.CWD

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return commandExecution{}, 1, false, fmt.Errorf("cannot start interactive command %q: %w", command, err)
	}
	defer ptmx.Close() //nolint:errcheck

	stdoutCapture := &limitedCaptureWriter{limit: cfg.CaptureStdoutBytes}
	stream := io.MultiWriter(os.Stdout, stdoutCapture)

	fd := int(os.Stdin.Fd())
	restoreTerminal := func() {}
	restoreBlocking := func() {}
	if term.IsTerminal(fd) {
		state, rawErr := term.MakeRaw(fd)
		if rawErr == nil {
			restoreTerminal = func() {
				term.Restore(fd, state) //nolint:errcheck
			}
		}
	}
	if blockErr := unix.SetNonblock(fd, true); blockErr == nil {
		restoreBlocking = func() {
			unix.SetNonblock(fd, false) //nolint:errcheck
		}
	}
	defer restoreTerminal()
	defer restoreBlocking()

	if _, err := fmt.Fprintln(os.Stdout); err != nil {
		return commandExecution{}, 1, false, err
	}

	_ = pty.InheritSize(os.Stdin, ptmx)

	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(stream, ptmx)
		copyDone <- copyErr
	}()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	inputBuffer := make([]byte, 1024)
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
			time.Sleep(10 * time.Millisecond)
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
		return output, 124, hadOutput, &commandRunError{Command: command, ExitCode: 124, TimedOut: true}
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
		return output, code, hadOutput, &commandRunError{Command: command, ExitCode: code}
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
	ctxInfo.Git = getGitContext(nextCWD)
}

// resolveDirectoryChange detects a simple cd and computes the next session cwd.
func resolveDirectoryChange(currentCWD, command string) (string, bool) {
	target, ok := parseSimpleCDTarget(command)
	if !ok {
		return "", false
	}
	if hasShellOperators(command) {
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

	if len(rest) >= 2 {
		if (rest[0] == '"' && rest[len(rest)-1] == '"') || (rest[0] == '\'' && rest[len(rest)-1] == '\'') {
			unquoted, err := strconv.Unquote(rest)
			if err != nil {
				return "", false
			}
			return unquoted, true
		}
	}

	if strings.Contains(rest, "\\ ") {
		rest = strings.ReplaceAll(rest, "\\ ", " ")
	}

	if strings.ContainsAny(rest, " \t") {
		return "", false
	}

	return rest, true
}

// staticFallbackAnswer returns a plain-text answer when the streaming summarizer is unavailable.
func staticFallbackAnswer(fallbackSummary string, executions []commandExecution) string {
	if len(executions) == 0 {
		return fallbackSummary
	}

	last := executions[len(executions)-1]
	preferred := strings.TrimSpace(last.PreferredOutput())

	if last.ExitCode == 0 && preferred != "" {
		return preferred
	}

	if last.ExitCode == 0 {
		return fmt.Sprintf("%s done.", strings.TrimSpace(last.Purpose))
	}

	if preferred != "" {
		return preferred
	}
	return fmt.Sprintf("The command `%s` failed with exit code %d.", strings.TrimSpace(last.Command), last.ExitCode)
}

// runCommandCapture runs a simple command and returns its combined output.
func runCommandCapture(cwd, name string, args ...string) (int, string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), strings.TrimSpace(string(output))
		}
		return 1, strings.TrimSpace(string(output))
	}
	return 0, strings.TrimSpace(string(output))
}
