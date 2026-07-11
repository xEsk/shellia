package executor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	configpkg "shellia/internal/config"
	"shellia/internal/core"
	safetypkg "shellia/internal/safety"
	tracepkg "shellia/internal/trace"
)

const (
	defaultPlanningMaxRounds = 4
	classificationSafe       = safetypkg.ClassificationSafe
)

var (
	defaultConfig   = configpkg.DefaultConfig
	withTraceTurnID = tracepkg.WithTurnID
)

func shouldRetryAfterExecutionError(err error, round int, maxRounds int) bool {
	if round >= maxRounds-1 {
		return false
	}

	var promptErr *interactivePromptError
	return errors.As(err, &promptErr)
}

func loopTestContext(t *testing.T) contextInfo {
	t.Helper()
	return core.ContextInfo{
		CWD:   t.TempDir(),
		User:  "test-user",
		OS:    "test-os",
		Shell: "/bin/sh",
	}
}

func openLoopTrace(t *testing.T) *traceLogger {
	t.Helper()
	cfg := defaultConfig()
	cfg.TraceEnabled = true
	cfg.TraceDir = t.TempDir()
	logger, err := tracepkg.OpenSession(cfg, loopTestContext(t))
	if err != nil {
		t.Fatalf("OpenSession() error = %v", err)
	}
	return logger
}

func closeLoopTraceAndRead(t *testing.T, logger *traceLogger) []map[string]any {
	t.Helper()
	path := logger.Path()
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid trace event %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func captureMainLoopIO(t *testing.T, input string, _ *http.Client, fn func(RuntimeDeps)) string {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	defer stdinRead.Close()
	defer stdoutRead.Close()

	if _, err := io.WriteString(stdinWrite, input); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	stdinWrite.Close()

	os.Stdout = stdoutWrite
	os.Stderr = stdoutWrite
	fn(RuntimeDeps{Stdin: stdinRead, Stdout: stdoutWrite, Stderr: stdoutWrite, HTTPClient: http.DefaultClient})
	stdoutWrite.Close()

	output, err := io.ReadAll(stdoutRead)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(output)
}

func traceEventsByName(events []map[string]any, name string) []map[string]any {
	matches := make([]map[string]any, 0)
	for _, event := range events {
		if event["event"] == name {
			matches = append(matches, event)
		}
	}
	return matches
}

func traceEventData(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object", event["data"])
	}
	return data
}
