package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	configpkg "github.com/xEsk/shellia/internal/config"
)

func TestOpenSessionTraceDisabledCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg := configpkg.DefaultConfig()
	cfg.TraceDir = dir

	logger, err := openSessionTrace(traceOptionsForTest(cfg), contextInfo{})
	if err != nil {
		t.Fatalf("openSessionTrace() error = %v", err)
	}
	if logger != nil {
		t.Fatalf("openSessionTrace() = %#v, want nil", logger)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("trace dir entries = %d, want 0", len(entries))
	}
}

func TestOpenSessionTraceCreatesJSONLEvents(t *testing.T) {
	dir := t.TempDir()
	cfg := configpkg.DefaultConfig()
	cfg.TraceEnabled = true
	cfg.TraceDir = dir

	logger, err := openSessionTrace(traceOptionsForTest(cfg), contextInfo{})
	if err != nil {
		t.Fatalf("openSessionTrace() error = %v", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	logger.Record("session_start", "", "", -1, map[string]string{"version": "dev"})
	turnID := logger.StartTurn(map[string]string{"instruction": "answer directly"})
	logger.Record("llm_prompt", turnID, "planning", 0, map[string]string{"user_prompt": "User instruction"})

	events := readTraceEvents(t, logger.Path())
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	if events[0]["event"] != "session_start" || events[1]["event"] != "turn_start" || events[2]["event"] != "llm_prompt" {
		t.Fatalf("event order = %#v", []any{events[0]["event"], events[1]["event"], events[2]["event"]})
	}
	if events[2]["round"] != float64(0) {
		t.Fatalf("round = %#v, want 0", events[2]["round"])
	}
	if filepath.Ext(logger.Path()) != ".jsonl" {
		t.Fatalf("trace path = %q, want .jsonl extension", logger.Path())
	}
}

func TestTraceLoggerNilCallsDoNotPanic(t *testing.T) {
	var logger *traceLogger

	logger.Record("event", "", "", -1, nil)
	if turnID := logger.StartTurn(nil); turnID != "" {
		t.Fatalf("StartTurn() = %q, want empty", turnID)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if path := logger.Path(); path != "" {
		t.Fatalf("Path() = %q, want empty", path)
	}
}

func readTraceEvents(t *testing.T, path string) []map[string]any {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatalf("Close(%q) error = %v", path, err)
		}
	}()

	events := make([]map[string]any, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid JSONL line %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("Scan(%q) error = %v", path, err)
	}
	return events
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
		t.Fatalf("event data = %#v, want object", event["data"])
	}
	return data
}
