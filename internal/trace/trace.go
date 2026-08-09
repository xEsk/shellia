package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type traceTurnContextKey struct{}

var version = "dev"

// Options contains trace controls and the non-secret session metadata emitted in traces.
type Options struct {
	TraceEnabled              bool
	TraceDir                  string
	ModelName                 string
	Model                     string
	BaseURL                   string
	Interactive               bool
	PlanOnly                  bool
	YesSafe                   bool
	AskConfirmPlan            bool
	PlanningMaxRounds         int
	IncludeSessionMemory      bool
	IncludeRecentObservations bool
	CaptureStdoutBytes        int
	CaptureStderrBytes        int
}

// traceLogger writes one JSONL diagnostic stream for a Shellia session.
type traceLogger struct {
	mu        sync.Mutex
	file      *os.File
	sessionID string
	path      string
	turnSeq   int
	closed    bool
	failed    bool
}

func withTraceTurnID(ctx context.Context, turnID string) context.Context {
	if turnID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceTurnContextKey{}, turnID)
}

func traceTurnID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	turnID, _ := ctx.Value(traceTurnContextKey{}).(string)
	return turnID
}

type traceEvent struct {
	Time      string `json:"time"`
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id,omitempty"`
	Event     string `json:"event"`
	Phase     string `json:"phase,omitempty"`
	Round     *int   `json:"round,omitempty"`
	Data      any    `json:"data,omitempty"`
}

// openSessionTrace creates the JSONL trace file when tracing is enabled.
func openSessionTrace(options Options, _ contextInfo) (*traceLogger, error) {
	if !options.TraceEnabled {
		return nil, nil
	}

	dir, err := traceDir(options)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create trace directory: %w", err)
	}

	sessionID := newTraceSessionID()
	name := fmt.Sprintf("%s-%s.session.jsonl", time.Now().UTC().Format("20060102T150405Z"), sessionID)
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot create trace file: %w", err)
	}

	return &traceLogger{
		file:      file,
		sessionID: sessionID,
		path:      path,
	}, nil
}

// Record appends one diagnostic event. Once a write fails, later events are ignored.
func (logger *traceLogger) Record(event string, turnID string, phase string, round int, data any) {
	if logger == nil {
		return
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()

	if logger.closed || logger.failed || logger.file == nil {
		return
	}

	item := traceEvent{
		Time:      time.Now().UTC().Format(time.RFC3339Nano),
		SessionID: logger.sessionID,
		TurnID:    turnID,
		Event:     event,
		Phase:     phase,
		Data:      data,
	}
	if round >= 0 {
		item.Round = &round
	}

	encoded, err := json.Marshal(item)
	if err != nil {
		item.Data = map[string]string{"marshal_error": err.Error()}
		encoded, err = json.Marshal(item)
	}
	if err != nil {
		logger.failed = true
		return
	}
	encoded = append(encoded, '\n')
	if _, err := logger.file.Write(encoded); err != nil {
		logger.failed = true
	}
}

// StartTurn creates a stable turn id and records the turn_start event.
func (logger *traceLogger) StartTurn(data any) string {
	if logger == nil {
		return ""
	}

	logger.mu.Lock()
	logger.turnSeq++
	turnID := fmt.Sprintf("turn-%d", logger.turnSeq)
	logger.mu.Unlock()

	logger.Record("turn_start", turnID, "", -1, data)
	return turnID
}

// Close closes the trace file. It is safe to call more than once.
func (logger *traceLogger) Close() error {
	if logger == nil {
		return nil
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()

	if logger.closed || logger.file == nil {
		return nil
	}
	logger.closed = true
	return logger.file.Close()
}

// Path returns the trace file path, mainly for tests.
func (logger *traceLogger) Path() string {
	if logger == nil {
		return ""
	}
	return logger.path
}

func traceDir(options Options) (string, error) {
	if options.TraceDir != "" {
		return options.TraceDir, nil
	}

	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "shellia", "sessions"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot detect home directory for trace path: %w", err)
	}
	return filepath.Join(home, ".local", "state", "shellia", "sessions"), nil
}

func newTraceSessionID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

func traceSessionStartData(options Options, ctxInfo contextInfo) map[string]any {
	appVersion := strings.TrimSpace(version)
	if appVersion == "" {
		appVersion = "dev"
	}
	return map[string]any{
		"version":                     appVersion,
		"model_name":                  options.ModelName,
		"model":                       options.Model,
		"base_url":                    options.BaseURL,
		"cwd":                         ctxInfo.CWD,
		"interactive":                 options.Interactive,
		"plan_only":                   options.PlanOnly,
		"yes_safe":                    options.YesSafe,
		"ask_confirm_plan":            options.AskConfirmPlan,
		"planning_max_rounds":         options.PlanningMaxRounds,
		"include_session_memory":      options.IncludeSessionMemory,
		"include_recent_observations": options.IncludeRecentObservations,
		"capture_stdout_bytes":        options.CaptureStdoutBytes,
		"capture_stderr_bytes":        options.CaptureStderrBytes,
	}
}

func traceStreamData(stream capturedStream) map[string]any {
	return map[string]any{
		"text":        stream.Text,
		"total_bytes": stream.TotalBytes,
		"kept_bytes":  stream.KeptBytes,
		"truncated":   stream.Truncated,
	}
}

func traceExecutionData(execution commandExecution) map[string]any {
	return map[string]any{
		"command":   execution.Command,
		"purpose":   execution.Purpose,
		"exit_code": execution.ExitCode,
		"timed_out": execution.TimedOut,
		"stdout":    traceStreamData(execution.Stdout),
		"stderr":    traceStreamData(execution.Stderr),
	}
}
