package trace

import (
	"context"
	"strings"
)

// Logger writes one JSONL diagnostic stream for a Shellia session.
type Logger = traceLogger

// SetVersion records the binary version used in session-start trace data.
func SetVersion(value string) {
	version = strings.TrimSpace(value)
	if version == "" {
		version = "dev"
	}
}

// OpenSession creates the JSONL trace file when tracing is enabled.
func OpenSession(options Options, ctxInfo contextInfo) (*Logger, error) {
	return openSessionTrace(options, ctxInfo)
}

// WithTurnID returns a context annotated with the trace turn id.
func WithTurnID(ctx context.Context, turnID string) context.Context {
	return withTraceTurnID(ctx, turnID)
}

// TurnID returns the trace turn id from context.
func TurnID(ctx context.Context) string {
	return traceTurnID(ctx)
}

// SessionStartData builds the trace payload for session start.
func SessionStartData(options Options, ctxInfo contextInfo) map[string]any {
	return traceSessionStartData(options, ctxInfo)
}

// StreamData builds the trace payload for one captured stream.
func StreamData(stream capturedStream) map[string]any {
	return traceStreamData(stream)
}

// ExecutionData builds the trace payload for one command execution.
func ExecutionData(execution commandExecution) map[string]any {
	return traceExecutionData(execution)
}
