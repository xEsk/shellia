package app

import (
	"io"
	"os"
	"strings"
	"testing"

	configpkg "github.com/xEsk/shellia/internal/config"
	executorpkg "github.com/xEsk/shellia/internal/executor"
	uipkg "github.com/xEsk/shellia/internal/ui"
)

// TestExecutorPresenterPreservesActiveTurnTranscript checks the app adapter
// routes executor semantics through the active UI turn without visual changes.
func TestExecutorPresenterPreservesActiveTurnTranscript(t *testing.T) {
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
	cfg.YesSafe = true
	cfg.ShowSystemOutput = true
	ctxInfo := loopTestContext(t)
	renderer := uipkg.NewRenderer(stdout, uipkg.Presentation{Style: cfg.VisualStyle})
	turn := renderer.BeginShelliaTurn(viewOptions(cfg), ctxInfo)
	presenter := newExecutorPresenter(runtimeDeps{
		Stdout: stdout,
		Stderr: stdout,
		Turn:   turn,
	}, false)

	_, err = executorpkg.ExecuteCommands(t.Context(), executorpkg.RuntimeDeps{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stdout,
		Presenter: presenter,
	}, false, executorOptions(cfg), &ctxInfo, []commandPlan{{
		Command:   "printf adapter-output",
		Purpose:   "Exercise app adapter",
		LocalSafe: true,
	}}, nil)
	if err != nil {
		t.Fatalf("ExecuteCommands() error = %v", err)
	}
	turn.Close()

	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	output := string(data)
	for _, want := range []string{"│   ┌─ step 1/1", "system output", "adapter-output"} {
		if !strings.Contains(output, want) {
			t.Fatalf("adapter output lacks %q: %q", want, output)
		}
	}
}

// TestExecutorPresenterInteractiveManualWithoutTurnDoesNotCreateTurn checks a
// raw manual command does not acquire an execution fallback turn to suspend.
func TestExecutorPresenterInteractiveManualWithoutTurnDoesNotCreateTurn(t *testing.T) {
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
	ctxInfo := loopTestContext(t)
	presenter := newExecutorPresenter(runtimeDeps{Stdout: stdout, Stderr: stdout}, false)
	_, err = executorpkg.ExecuteManualCommand(t.Context(), executorpkg.RuntimeDeps{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stdout,
		Presenter: presenter,
	}, false, executorOptions(cfg), &ctxInfo, "printf interactive-manual", executorpkg.ManualRenderInteractive)
	if err != nil {
		t.Fatalf("ExecuteManualCommand() error = %v", err)
	}
	if presenter.turn != nil {
		t.Fatal("interactive manual execution created an unexpected fallback turn")
	}

	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	output := string(data)
	if got := strings.Count(output, "Shellia"); got != 2 {
		t.Fatalf("Shellia labels = %d, want only interactive handoff text: %q", got, output)
	}
	if strings.Contains(output, " · v") {
		t.Fatalf("interactive manual output contains an unexpected turn header: %q", output)
	}
}

// TestExecutorPresenterInlineManualDecoratesActiveTurnOnce checks the active
// turn remains the sole owner of its command, purpose, and spacing rows.
func TestExecutorPresenterInlineManualDecoratesActiveTurnOnce(t *testing.T) {
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
	turn := uipkg.NewRenderer(stdout, uipkg.Presentation{Style: cfg.VisualStyle}).BeginShelliaTurn(viewOptions(cfg), ctxInfo)
	presenter := newExecutorPresenter(runtimeDeps{Stdout: stdout, Stderr: stdout, Turn: turn}, false)
	_, err = executorpkg.ExecuteManualCommand(t.Context(), executorpkg.RuntimeDeps{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stdout,
		Presenter: presenter,
	}, false, executorOptions(cfg), &ctxInfo, "printf inline-once", executorpkg.ManualRenderInline)
	if err != nil {
		t.Fatalf("ExecuteManualCommand() error = %v", err)
	}
	turn.Close()

	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	data, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	output := string(data)
	want := strings.Join([]string{
		"step 1/1",
		"",
		"run › printf inline-once",
		"",
		"• Manual shell command",
		"• system output",
		"",
		"  inline-once",
		"end step",
	}, "\n")
	if got := normalizedExecutorStepTranscript(t, output); got != want {
		t.Fatalf("normalized step transcript:\n%s\nwant:\n%s", got, want)
	}
}

func normalizedExecutorStepTranscript(t *testing.T, output string) string {
	t.Helper()

	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	rows := make([]string, 0)
	capturing := false
	for _, line := range lines {
		if !capturing {
			if strings.Contains(line, "┌─ step 1/1") {
				capturing = true
				rows = append(rows, "step 1/1")
			}
			continue
		}
		if strings.Contains(line, "└─") {
			rows = append(rows, "end step")
			return strings.Join(rows, "\n")
		}

		row := strings.TrimPrefix(line, "│   │")
		row = strings.TrimSuffix(row, "│   │")
		row = strings.TrimRight(row, " ")
		row = strings.TrimPrefix(row, " ")
		rows = append(rows, row)
	}

	t.Fatalf("output lacks a complete step transcript: %q", output)
	return ""
}
