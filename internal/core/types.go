package core

import (
	"errors"
	"fmt"
	"strings"
)

// ErrAborted is returned when the user cancels execution.
var ErrAborted = errors.New("aborted by user")

// TruncationStrategy controls which part of long text is preserved.
type TruncationStrategy string

const (
	TruncationStart TruncationStrategy = "start"
	TruncationEnd   TruncationStrategy = "end"
	TruncationMixed TruncationStrategy = "mixed"
)

// ContextInfo contains local context sent to the model and shown in the UI.
type ContextInfo struct {
	CWD   string `json:"cwd"`
	User  string `json:"user"`
	OS    string `json:"os"`
	Shell string `json:"shell"`
}

// HistoryEntry stores a compact prior turn for prompt context.
type HistoryEntry struct {
	Instruction string
	Result      string
}

// InteractiveMode identifies the current interactive prompt mode.
type InteractiveMode string

const (
	InteractiveModeAI    InteractiveMode = "ai"
	InteractiveModeShell InteractiveMode = "shell"
)

// ObservationMemory stores reusable command observations for follow-up prompts.
type ObservationMemory struct {
	Command    string
	Purpose    string
	Transcript string
}

// SessionState stores lightweight follow-up memory for an interactive session.
type SessionState struct {
	LastRetryInstruction string
	PendingIntent        string
	LastCreatedFiles     []string
	LastRuntimeHint      string
	LastReferencedFile   string
	LastObservations     []ObservationMemory
	LastSuggestedCommand string
}

// TurnResult is the normalized outcome of one Shellia turn.
type TurnResult struct {
	Result     string
	Summary    string
	Actionable bool
	Plans      []CommandPlan
	Executions []CommandExecution
}

// CommandPlan is the combined result of LLM planning and local safety classification.
type CommandPlan struct {
	Command              string
	Purpose              string
	Risk                 string
	RequiresConfirmation bool
	Classification       string
	LocalSafe            bool
	Interactive          bool
	InteractiveReason    string
}

// CommandExecution stores the captured output and exit code for one command.
type CommandExecution struct {
	Command  string
	Purpose  string
	Stdout   CapturedStream
	Stderr   CapturedStream
	ExitCode int
}

// CapturedStream represents the captured part of a stream, with truncation information.
type CapturedStream struct {
	Text       string
	TotalBytes int
	KeptBytes  int
	Truncated  bool
}

// HasOutput reports whether the captured stream contains useful text.
func (stream CapturedStream) HasOutput() bool {
	return strings.TrimSpace(stream.Text) != ""
}

// RenderForPrompt prepares the stream to be sent to the model with a truncation notice.
func (stream CapturedStream) RenderForPrompt(label string, limit int, strategy TruncationStrategy) string {
	if !stream.HasOutput() {
		return ""
	}

	var body strings.Builder
	if stream.Truncated {
		_, _ = fmt.Fprintf(&body, "[%s truncated locally: kept %d of %d bytes]\n", label, stream.KeptBytes, stream.TotalBytes)
	}
	body.WriteString(TrimForSummary(stream.Text, limit, strategy))

	return fmt.Sprintf("%s:\n%s", label, body.String())
}

// TextForUser returns the captured text with a short notice if the stream was truncated.
func (stream CapturedStream) TextForUser() string {
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
func (execution CommandExecution) PreferredOutput() string {
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
func (execution CommandExecution) PromptTranscript(limit int, strategy TruncationStrategy) string {
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
		sections = append(sections, execution.Stderr.RenderForPrompt("stderr", stderrBudget, strategy))
	}
	if execution.Stdout.HasOutput() {
		sections = append(sections, execution.Stdout.RenderForPrompt("stdout", stdoutBudget, strategy))
	}

	return strings.Join(sections, "\n")
}

// TrimForSummary truncates long text for prompts or compact user-facing previews.
func TrimForSummary(text string, maxChars int, strategy TruncationStrategy) string {
	runes := []rune(strings.TrimSpace(text))
	if maxChars <= 0 {
		return ""
	}
	if len(runes) <= maxChars {
		return string(runes)
	}
	switch strategy {
	case TruncationStart:
		return "..." + string(runes[len(runes)-maxChars:])
	case TruncationEnd:
		return string(runes[:maxChars]) + "..."
	case TruncationMixed:
		if maxChars <= 8 {
			return string(runes[:maxChars]) + "..."
		}
		head := maxChars / 2
		tail := maxChars - head
		return string(runes[:head]) + "\n...\n" + string(runes[len(runes)-tail:])
	default:
		return string(runes[:maxChars]) + "..."
	}
}
