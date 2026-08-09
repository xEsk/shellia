package executor

import (
	"strings"
	"testing"
	"time"

	configpkg "github.com/xEsk/shellia/internal/config"
)

// TestGetContextUsesContextOptions checks user discovery is controlled at the executor boundary.
func TestGetContextUsesContextOptions(t *testing.T) {
	ctxInfo, err := GetContext(t.Context(), ContextOptions{IncludeUser: false})
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}
	if ctxInfo.User != "" {
		t.Fatalf("GetContext() user = %q, want empty when IncludeUser is false", ctxInfo.User)
	}

	ctxInfo, err = GetContext(t.Context(), ContextOptions{IncludeUser: true})
	if err != nil {
		t.Fatalf("GetContext() with user error = %v", err)
	}
	if ctxInfo.User == "" {
		t.Fatal("GetContext() user is empty when IncludeUser is true")
	}
}

// TestOptionsRetainExecutionBehavior checks command execution consumes every executor option.
func TestOptionsRetainExecutionBehavior(t *testing.T) {
	ctxInfo := loopTestContext(t)
	opts := Options{
		CommandTimeout:      time.Second,
		YesSafe:             true,
		ContinueOnError:     true,
		ConfirmationDefault: configpkg.ConfirmationDefaultNo,
		CaptureStdoutBytes:  3,
		CaptureStderrBytes:  4,
		ShowSystemOutput:    false,
	}
	plans := []commandPlan{
		{Command: "printf '\\141\\142\\143\\144\\145\\146'; printf '\\147\\150\\151\\152\\153\\154' >&2", Purpose: "Capture output", Classification: classificationSafe, LocalSafe: true},
		{Command: "exit 7", Purpose: "Fail", Classification: classificationSafe, LocalSafe: true},
		{Command: "printf later", Purpose: "Continue", Classification: classificationSafe, LocalSafe: true, IndependentOnFailure: true},
	}

	var batchErr error
	var batchExecutions int
	output := captureMainLoopIO(t, "", func(deps RuntimeDeps) {
		batch, err := ExecuteCommands(t.Context(), deps, false, opts, &ctxInfo, plans, nil)
		batchErr = err
		batchExecutions = len(batch.Executions)
		if batch.Executions[0].Stdout.Text != "abc" || !batch.Executions[0].Stdout.Truncated {
			t.Fatalf("stdout capture = %#v, want truncated abc", batch.Executions[0].Stdout)
		}
		if batch.Executions[0].Stderr.Text != "ghij" || !batch.Executions[0].Stderr.Truncated {
			t.Fatalf("stderr capture = %#v, want truncated ghij", batch.Executions[0].Stderr)
		}
	})
	if batchErr != nil {
		t.Fatalf("ExecuteCommands() error = %v", batchErr)
	}
	if batchExecutions != 3 {
		t.Fatalf("executions = %d, want 3 when ContinueOnError is true", batchExecutions)
	}
	if strings.Contains(output, "abcdef") || strings.Contains(output, "ghijkl") {
		t.Fatalf("hidden system output leaked into renderer output: %q", output)
	}
}

// TestOptionsConfirmationDefaultRunsOnEnter checks the supplied confirmation default is used.
func TestOptionsConfirmationDefaultRunsOnEnter(t *testing.T) {
	ctxInfo := loopTestContext(t)
	opts := Options{CommandTimeout: time.Second, ConfirmationDefault: configpkg.ConfirmationDefaultYes}
	plans := []commandPlan{{Command: "printf confirmed", Purpose: "Confirm", Classification: classificationSafe, LocalSafe: true}}

	var batchErr error
	var batchExecutions int
	captureMainLoopIO(t, "\n", func(deps RuntimeDeps) {
		batch, err := ExecuteCommands(t.Context(), deps, false, opts, &ctxInfo, plans, nil)
		batchErr = err
		batchExecutions = len(batch.Executions)
	})
	if batchErr != nil {
		t.Fatalf("ExecuteCommands() error = %v", batchErr)
	}
	if batchExecutions != 1 {
		t.Fatalf("executions = %d, want 1 after the default yes confirmation", batchExecutions)
	}
}
