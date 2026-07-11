package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func readTraceEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
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
