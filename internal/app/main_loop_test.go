package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"reflect"
	"regexp"
	"shellia/internal/core"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	configpkg "shellia/internal/config"

	uipkg "shellia/internal/ui"
)

type loopLLMResponse struct {
	content           string
	status            int
	cancellationStart chan struct{}
}

type loopLLMRequest struct {
	body          string
	authorization string
}

type loopLLMClient struct {
	responses []loopLLMResponse
	mu        sync.Mutex
	requests  []loopLLMRequest
}

type contextErrorTransport struct{}

type blockingContextTransport struct {
	started chan struct{}
	once    sync.Once
}

func (transport *blockingContextTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.once.Do(func() {
		close(transport.started)
	})
	<-request.Context().Done()
	return nil, request.Context().Err()
}

// newLoopLLMClient builds an OpenAI-compatible fake transport for main loop tests.
func newLoopLLMClient(t *testing.T, responses ...loopLLMResponse) *loopLLMClient {
	t.Helper()
	return &loopLLMClient{responses: responses}
}

// RoundTrip serves fake LLM responses without opening a local network listener.
func (fake *loopLLMClient) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/chat/completions" {
		return loopHTTPResponse(r, http.StatusNotFound, "unexpected path", nil), nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var request chatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("invalid fake llm request: %w", err)
	}

	fake.mu.Lock()
	index := len(fake.requests)
	fake.requests = append(fake.requests, loopLLMRequest{
		body:          string(body),
		authorization: r.Header.Get("Authorization"),
	})
	fake.mu.Unlock()

	if index >= len(fake.responses) {
		return loopHTTPResponse(r, http.StatusInternalServerError, "unexpected LLM request", nil), nil
	}

	response := fake.responses[index]
	if response.status != 0 && response.status != http.StatusOK {
		return loopHTTPResponse(r, response.status, response.content, nil), nil
	}
	if response.cancellationStart != nil {
		close(response.cancellationStart)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	payload := map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"content": response.content}},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return loopHTTPResponse(r, http.StatusOK, string(encoded), map[string]string{"Content-Type": "application/json"}), nil
}

// HTTPClient returns an isolated client backed by the fake transport.
func (fake *loopLLMClient) HTTPClient() *http.Client {
	return &http.Client{Transport: fake}
}

// RoundTrip waits for request cancellation and returns the context cause.
func (transport contextErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// loopHTTPResponse builds a minimal HTTP response for the fake LLM transport.
func loopHTTPResponse(request *http.Request, status int, body string, headers map[string]string) *http.Response {
	response := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
	for key, value := range headers {
		response.Header.Set(key, value)
	}
	return response
}

// URL returns the fake LLM base URL.
func (fake *loopLLMClient) URL() string {
	return "http://shellia.test"
}

// requestCount returns how many LLM requests reached the fake transport.
func (fake *loopLLMClient) requestCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return len(fake.requests)
}

// requestBodies returns the JSON request bodies captured by the fake transport.
func (fake *loopLLMClient) requestBodies() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	bodies := make([]string, 0, len(fake.requests))
	for _, request := range fake.requests {
		bodies = append(bodies, request.body)
	}
	return bodies
}

// requestAuthorizations returns the Authorization headers captured by the fake transport.
func (fake *loopLLMClient) requestAuthorizations() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	headers := make([]string, 0, len(fake.requests))
	for _, request := range fake.requests {
		headers = append(headers, request.authorization)
	}
	return headers
}

// loopTestConfig returns a minimal config that points every model call at the fake transport.
func loopTestConfig(baseURL string) config {
	cfg := defaultConfig()
	cfg.BaseURL = baseURL
	cfg.APIKey = "test-key"
	cfg.Model = "test-model"
	cfg.RequestTimeout = 2 * time.Second
	cfg.CommandTimeout = 2 * time.Second
	cfg.YesSafe = true
	cfg.ShowSystemOutput = false
	cfg.ShowCommandPopup = false
	return cfg
}

// loopTestContext returns an isolated shell context for main loop tests.
func loopTestContext(t *testing.T) contextInfo {
	t.Helper()

	return contextInfo{
		CWD:   t.TempDir(),
		User:  "test-user",
		OS:    "test-os",
		Shell: "/bin/sh",
	}
}

// loopTurnRequest returns a minimal turn request for main loop tests.
func loopTurnRequest(cfg config, ctxInfo *contextInfo, instruction string) turnRequest {
	return turnRequest{
		Config:      cfg,
		ContextInfo: ctxInfo,
		Instruction: instruction,
	}
}

var visualAcceptanceANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// renderVisualWidthFixture presents one fixed semantic turn through a selected
// renderer on a sized PTY. The runApp acceptance matrix separately owns wiring.
func renderVisualWidthFixture(t *testing.T, style configpkg.VisualStyle, ansi bool, terminalWidth int) string {
	t.Helper()

	cfg := defaultConfig()
	cfg.Model = "test-model"
	cfg.VisualStyle = style
	cfg.NoColor = !ansi
	cfg.Verbose = true
	cfg.AskConfirmPlan = true
	ctxInfo := core.ContextInfo{CWD: "/srv/demo", User: "test-user", OS: "test-os", Shell: "/bin/zsh"}
	plan := core.CommandPlan{
		Command:              "df -h /",
		Purpose:              "Mostrar l'espai lliure.",
		Risk:                 "safe",
		Classification:       "safe",
		LocalSafe:            true,
		RequiresConfirmation: false,
	}

	render := func(target *os.File) {
		renderer := newRenderer(target, presentation{Style: style, ANSI: ansi, User: ctxInfo.User})
		renderer.UserTurn(core.InteractiveModeAI, "quant d'espai queda al disc?")
		turn := renderer.BeginShelliaTurn(cfg, ctxInfo)
		turn.Plan(cfg, "Cal consultar l'espai disponible.", []core.CommandPlan{plan}, false)
		step := turn.BeginStep(cfg, 1, 1, plan)
		if step == nil {
			t.Fatal("BeginStep() returned nil")
		}
		step.OutputLabel()
		step.OutputLine("419Gi available")
		step.Close()
		turn.Final("Queden 419Gi lliures al disc arrel (/).")
		turn.Close()
	}

	reader, terminal, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open() error = %v", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup of the PTY reader.
	if err := pty.Setsize(terminal, &pty.Winsize{Cols: uint16(terminalWidth + 4), Rows: 24}); err != nil {
		terminal.Close() //nolint:errcheck // best-effort cleanup after setup failure.
		t.Fatalf("pty.Setsize() error = %v", err)
	}

	readDone := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(reader)
		readDone <- output
	}()
	render(terminal)
	if err := terminal.Close(); err != nil {
		t.Fatalf("terminal.Close() error = %v", err)
	}
	return string(<-readDone)
}

type visualRunOptions struct {
	noColor     bool
	noColorFlag bool
	interactive bool
	tty         bool
}

// runAppVisualAcceptanceTranscript drives real config parsing, app renderer
// installation, interactive/one-shot routing and turn/executor facade wiring.
func runAppVisualAcceptanceTranscript(t *testing.T, style configpkg.VisualStyle, options visualRunOptions) string {
	t.Helper()
	writeShelliaConfig(t, fmt.Sprintf(`
default_model = "test"

[[models]]
name = "test"
base_url = "http://shellia.test"
model = "test-model"
api_key = "test-key"

[execution]
ask_confirm_plan = false

[ui]
style = %q
no_color = %t
verbose = true
show_system_output = true
`, style, options.noColor))

	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Cal consultar l'espai disponible.","commands":[{"command":"df -h /","purpose":"Mostrar l'espai lliure.","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Queden 419Gi lliures al disc arrel (/).","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	input := ""
	args := []string{"quant d'espai queda al disc?"}
	var inputController *os.File
	var inputTerminal *os.File
	if options.interactive {
		args = nil
		var err error
		inputController, inputTerminal, err = pty.Open()
		if err != nil {
			t.Fatalf("pty.Open(stdin) error = %v", err)
		}
		defer inputController.Close() //nolint:errcheck // best-effort cleanup of test PTY input.
		defer inputTerminal.Close()   //nolint:errcheck // best-effort cleanup of test PTY input.
		if _, err := inputController.WriteString("quant d'espai queda al disc?\r/exit\r"); err != nil {
			t.Fatalf("WriteString(stdin PTY) error = %v", err)
		}
	}
	if options.noColorFlag {
		args = append([]string{"--no-color"}, args...)
	}

	output := captureMainLoopIO(t, input, fake.HTTPClient(), func(deps runtimeDeps) {
		if options.interactive {
			deps.Stdin = inputTerminal
		}
		deps.StdoutIsTerminal = func(*os.File) bool { return options.tty }
		deps.ExecuteCommands = func(_ context.Context, turnDeps runtimeDeps, _ bool, turnCfg config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			if len(plans) != 1 {
				t.Fatalf("ExecuteCommands plans = %d, want 1", len(plans))
			}
			if turnDeps.Turn == nil {
				t.Fatal("ExecuteCommands received nil Turn")
			}
			step := turnDeps.Turn.BeginStep(turnCfg, 1, 1, plans[0])
			if step == nil {
				t.Fatal("Turn.BeginStep() returned nil")
			}
			step.OutputLabel()
			step.OutputLine("419Gi available")
			step.Close()
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "419Gi available", TotalBytes: 15, KeptBytes: 15},
			}}}, nil
		}
		if code := runApp(t.Context(), args, deps); code != 0 {
			t.Fatalf("runApp() code = %d, want 0", code)
		}
	})
	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want 2", fake.requestCount())
	}
	return output
}

func stripVisualAcceptanceANSI(text string) string {
	return visualAcceptanceANSI.ReplaceAllString(text, "")
}

func stableInteractiveTurnOutput(t *testing.T, output string) string {
	t.Helper()
	start := strings.Index(output, "\ncontext\n")
	if start < 0 {
		t.Fatalf("interactive output lacks stable turn boundary:\n%s", output)
	}
	end := strings.Index(output[start+1:], "\nWhat do you want Shellia to do?")
	if end < 0 {
		t.Fatalf("interactive output lacks next-prompt boundary:\n%s", output)
	}
	return output[start : start+1+end]
}

func assertVisualAcceptanceOrder(t *testing.T, output string, interactive bool, contextValue string) {
	t.Helper()
	values := []string{"context", contextValue, "Shellia", "plan", "Cal consultar l'espai disponible.", "step 1/1", "df -h /", "Mostrar l'espai lliure.", "system output", "419Gi available", "Queden 419Gi lliures"}
	if interactive {
		values = append([]string{"quant d'espai queda al disc?"}, values...)
	}
	position := 0
	for _, value := range values {
		next := strings.Index(output[position:], value)
		if next < 0 {
			t.Fatalf("output lacks ordered value %q:\n%s", value, output)
		}
		position += next + len(value)
	}
}

func assertVisualAcceptanceGeometry(t *testing.T, style configpkg.VisualStyle, output string, interactive bool) {
	t.Helper()
	var values []string
	switch style {
	case configpkg.VisualStylePlain:
		values = []string{"steps", "  1. Mostrar", "step 1/1", "• system output", "──"}
	case configpkg.VisualStyleGuide:
		values = []string{"┃ Shellia · dev", "┃ plan", "┃   │ step 1/1", "┃   │ • system output"}
	case configpkg.VisualStyleBands:
		values = []string{"▌  Shellia · dev", "▌  plan", "▌    step 1/1", "▌    • system output"}
	case configpkg.VisualStyleCards:
		values = []string{"╭─ Shellia · dev", "│   plan", "│   ┌─ step 1/1", "│   │ • system output", "╰"}
	default:
		t.Fatalf("unsupported visual style %q", style)
	}
	if interactive {
		switch style {
		case configpkg.VisualStyleGuide:
			currentUser, err := user.Current()
			if err != nil {
				t.Fatalf("user.Current() error = %v", err)
			}
			values = append(values, "┃ "+currentUser.Username, currentUser.Username+" ›")
		case configpkg.VisualStyleBands:
			currentUser, err := user.Current()
			if err != nil {
				t.Fatalf("user.Current() error = %v", err)
			}
			values = append(values, "▌  "+currentUser.Username, currentUser.Username+" ›")
		case configpkg.VisualStyleCards:
			currentUser, err := user.Current()
			if err != nil {
				t.Fatalf("user.Current() error = %v", err)
			}
			values = append(values, "╭─ "+currentUser.Username, currentUser.Username+" ›")
		}
	}
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("%q output lacks structural value %q:\n%s", style, value, output)
		}
	}
}

// TestVisualStylesPreserveOneSemanticTranscript checks interactive and one-shot
// rendering across every configured style with and without ANSI colours.
func TestVisualStylesPreserveOneSemanticTranscript(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	styles := []configpkg.VisualStyle{
		configpkg.VisualStylePlain,
		configpkg.VisualStyleGuide,
		configpkg.VisualStyleBands,
		configpkg.VisualStyleCards,
	}
	modes := []struct {
		name        string
		interactive bool
	}{
		{name: "interactive", interactive: true},
		{name: "one-shot", interactive: false},
	}

	for _, style := range styles {
		for _, mode := range modes {
			t.Run(string(style)+"/"+mode.name, func(t *testing.T) {
				withANSI := runAppVisualAcceptanceTranscript(t, style, visualRunOptions{interactive: mode.interactive, tty: true})
				withoutANSI := runAppVisualAcceptanceTranscript(t, style, visualRunOptions{noColor: true, interactive: mode.interactive, tty: true})
				if !strings.Contains(withANSI, "\x1b[") {
					t.Fatalf("ANSI output contains no escape sequence: %q", withANSI)
				}
				stableWithoutANSI := withoutANSI
				if mode.interactive {
					stableWithoutANSI = stableInteractiveTurnOutput(t, withoutANSI)
				}
				if strings.Contains(stableWithoutANSI, "\x1b[") {
					t.Fatalf("stable no-color turn contains ANSI: %q", stableWithoutANSI)
				}
				assertVisualAcceptanceGeometry(t, style, stripVisualAcceptanceANSI(withANSI), mode.interactive)
				assertVisualAcceptanceGeometry(t, style, withoutANSI, mode.interactive)
				assertVisualAcceptanceOrder(t, withoutANSI, mode.interactive, cwd)
				if !mode.interactive && strings.Contains(withoutANSI, "you ›") {
					t.Fatalf("one-shot output contains interactive user turn:\n%s", withoutANSI)
				}
			})
		}
	}
}

// TestVisualStylesFitEffectiveTerminalWidths checks the integrated semantic
// fixture at the three acceptance widths in ANSI and no-color modes.
func TestVisualStylesFitEffectiveTerminalWidths(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	styles := []configpkg.VisualStyle{
		configpkg.VisualStylePlain,
		configpkg.VisualStyleGuide,
		configpkg.VisualStyleBands,
		configpkg.VisualStyleCards,
	}
	for _, width := range []int{120, 80, 48} {
		for _, style := range styles {
			for _, ansi := range []bool{true, false} {
				name := fmt.Sprintf("%s/%d/ansi=%t", style, width, ansi)
				t.Run(name, func(t *testing.T) {
					output := renderVisualWidthFixture(t, style, ansi, width)
					for _, rawLine := range strings.Split(output, "\n") {
						line := strings.TrimRight(stripVisualAcceptanceANSI(rawLine), "\r")
						if got := utf8.RuneCountInString(line); got > width {
							t.Fatalf("visible line width = %d, want <= %d: %q\n%s", got, width, line, output)
						}
					}
				})
			}
		}
	}
}

// TestRunAppNoColorFlagPreservesConfiguredGeometry checks the public CLI flag
// reaches presentation selection without replacing the configured theme.
func TestRunAppNoColorFlagPreservesConfiguredGeometry(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	output := runAppVisualAcceptanceTranscript(t, configpkg.VisualStyleCards, visualRunOptions{noColorFlag: true, tty: true})
	if strings.Contains(output, "\x1b[") {
		t.Fatalf("--no-color output contains ANSI: %q", output)
	}
	assertVisualAcceptanceGeometry(t, configpkg.VisualStyleCards, output, false)
}

// TestVisualStylesFallBackToPlainWithoutTerminalCapability checks redirected
// output and TERM=dumb suppress both visual selection and Shellia-owned ANSI.
func TestVisualStylesFallBackToPlainWithoutTerminalCapability(t *testing.T) {
	tests := []struct {
		name string
		term string
		tty  bool
	}{
		{name: "redirected", term: "xterm-256color", tty: false},
		{name: "dumb terminal", term: "dumb", tty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM", tt.term)
			options := visualRunOptions{interactive: true, tty: tt.tty}
			got := runAppVisualAcceptanceTranscript(t, configpkg.VisualStyleCards, options)
			want := runAppVisualAcceptanceTranscript(t, configpkg.VisualStylePlain, options)
			if stable := stableInteractiveTurnOutput(t, got); strings.Contains(stable, "\x1b[") {
				t.Fatalf("stable fallback turn contains ANSI: %q", stable)
			}
			if got != want {
				t.Fatalf("fallback output differs from plain:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func openLoopTrace(t *testing.T) *traceLogger {
	t.Helper()

	cfg := defaultConfig()
	cfg.TraceEnabled = true
	cfg.TraceDir = t.TempDir()
	logger, err := openSessionTrace(cfg, contextInfo{})
	if err != nil {
		t.Fatalf("openSessionTrace() error = %v", err)
	}
	t.Cleanup(func() {
		_ = logger.Close()
	})
	return logger
}

func closeLoopTraceAndRead(t *testing.T, logger *traceLogger) []map[string]any {
	t.Helper()

	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return readTraceEvents(t, logger.Path())
}

func commandNames(plans []commandPlan) []string {
	commands := make([]string, 0, len(plans))
	for _, plan := range plans {
		commands = append(commands, plan.Command)
	}
	return commands
}

func TestOneShotExitCodeForOutcome(t *testing.T) {
	for _, tt := range []struct {
		outcome turnOutcome
		want    int
	}{
		{outcome: turnOutcomeCompleted, want: 0},
		{outcome: turnOutcomePlanned, want: 0},
		{outcome: turnOutcomeDeclined, want: 0},
		{outcome: turnOutcomeBlocked, want: 1},
		{outcome: turnOutcomeStructuralError, want: 1},
		{outcome: turnOutcomeNoProgress, want: 1},
		{outcome: turnOutcomePlanningLimit, want: 1},
		{outcome: turnOutcomeTimeout, want: 1},
	} {
		if got := oneShotExitCode(tt.outcome); got != tt.want {
			t.Errorf("oneShotExitCode(%q) = %d, want %d", tt.outcome, got, tt.want)
		}
	}
}

func TestRunAppReturnsNonZeroForBlockedOneShot(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"action":"blocked","objective_mode":"act","success_criteria":"Service restarted","summary":"I need the service name.","blocker_kind":"missing_input","blocker_reason":"Specify the service.","commands":[]}`})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_BASE_URL", fake.URL())
	t.Setenv("SHELLIA_MODEL", "test-model")
	t.Setenv("SHELLIA_API_KEY", "test-key")

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if code := runApp(t.Context(), []string{"restart it"}, deps); code != 1 {
			t.Fatalf("runApp() code = %d, want 1 for blocked one-shot", code)
		}
	})
}

// TestRunTurnSkipsCommandEditedIntoPriorSuccessfulEffectiveCommand checks the
// executor rechecks effective identity after editing and reports a real skip.
func TestRunTurnSkipsCommandEditedIntoPriorSuccessfulEffectiveCommand(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect once.","commands":[{"command":"printf prior","purpose":"Capture prior output","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect another value.","commands":[{"command":"printf proposed","purpose":"Capture another output","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Kept the first result.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = false
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	var result turnResult
	captureMainLoopIO(t, "y\ne\nprintf prior\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect without repeating work"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if len(result.Executions) != 1 || result.Executions[0].Command != "printf prior" {
		t.Fatalf("executions = %#v, want first effective command only", result.Executions)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Command != "printf prior" || result.Skipped[0].Reason != repeatReasonRequired {
		t.Fatalf("skipped = %#v, want edited duplicate outcome", result.Skipped)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want two planning requests and summary", fake.requestCount())
	}

	events := closeLoopTraceAndRead(t, logger)
	starts := traceEventsByName(events, "command_start")
	if len(starts) != 1 || traceEventData(t, starts[0])["command"] != "printf prior" {
		t.Fatalf("command_start events = %#v, want only first command", starts)
	}
	skippedEvents := traceEventsByName(events, "command_skipped")
	if len(skippedEvents) != 1 || traceEventData(t, skippedEvents[0])["command"] != "printf prior" {
		t.Fatalf("command_skipped events = %#v, want edited command", skippedEvents)
	}
}

// TestSwitchInteractiveModelAppliesAndPersistsDefault checks /model changes the runtime profile and config default.
func TestSwitchInteractiveModelAppliesAndPersistsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"openai\"\n\n[[models]]\nname = \"openai\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"Model switched.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})

	cfg := defaultConfig()
	cfg.ConfigPath = path
	cfg.ModelName = "openai"
	cfg.BaseURL = "http://localhost:8080/v1"
	cfg.Model = "openai-model"
	cfg.Models = []modelConfig{
		{Name: "openai", BaseURL: "http://localhost:8080/v1", Model: "openai-model", SupportsResponseFormat: true},
		{Name: "mlx", BaseURL: fake.URL(), Model: "mlx-model", APIKey: "test-key", SupportsResponseFormat: false},
	}

	if err := switchInteractiveModel(&cfg, "mlx"); err != nil {
		t.Fatalf("switchInteractiveModel() error = %v", err)
	}
	if cfg.ModelName != "mlx" || cfg.BaseURL != fake.URL() || cfg.Model != "mlx-model" || cfg.SupportsResponseFormat {
		t.Fatalf("cfg after switch = %#v, want mlx profile without response_format", cfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `default_model = "mlx"`) {
		t.Fatalf("config after switch = %q, want persisted default", string(data))
	}

	ctxInfo := loopTestContext(t)
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})
	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("LLM request bodies = %d, want 1", len(bodies))
	}
	var body struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if body.Model != "mlx-model" {
		t.Fatalf("request model = %q, want mlx-model", body.Model)
	}
}

// TestRunAppTraceWritesSingleSessionFile checks the main application flow owns trace lifecycle.
func TestRunAppTraceWritesSingleSessionFile(t *testing.T) {
	traceDir := t.TempDir()
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"No command needed.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SHELLIA_BASE_URL", fake.URL())
	t.Setenv("SHELLIA_MODEL", "test-model")
	t.Setenv("SHELLIA_API_KEY", "test-key")

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		code := runApp(t.Context(), []string{
			"--trace",
			"--trace-dir", traceDir,
			"answer directly",
		}, deps)
		if code != 0 {
			t.Fatalf("runApp() code = %d, want 0", code)
		}
	})

	entries, err := os.ReadDir(traceDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", traceDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("trace files = %d, want 1", len(entries))
	}
	events := readTraceEvents(t, filepath.Join(traceDir, entries[0].Name()))
	if len(traceEventsByName(events, "session_start")) != 1 {
		t.Fatalf("session_start events = %d, want 1", len(traceEventsByName(events, "session_start")))
	}
	if len(traceEventsByName(events, "session_end")) != 1 {
		t.Fatalf("session_end events = %d, want 1", len(traceEventsByName(events, "session_end")))
	}
	if len(traceEventsByName(events, "turn_start")) != 1 || len(traceEventsByName(events, "turn_end")) != 1 {
		t.Fatalf("turn events start=%d end=%d, want 1 and 1", len(traceEventsByName(events, "turn_start")), len(traceEventsByName(events, "turn_end")))
	}
}

// TestRunAppInteractiveReadErrorReturnsOneAndClosesTrace checks interactive prompt failures return to the caller and finalize tracing.
func TestRunAppInteractiveReadErrorReturnsOneAndClosesTrace(t *testing.T) {
	traceDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("CreateTemp(stderr) error = %v", err)
	}
	t.Cleanup(func() {
		stderr.Close() //nolint:errcheck // best-effort cleanup of the temporary error output.
	})

	var code int
	captureMainLoopIO(t, "", &http.Client{}, func(deps runtimeDeps) {
		deps.Stderr = stderr
		deps.ReadInteractivePrompt = func(bool, *bufio.Reader, *os.File, io.Writer, interactiveMode, config, *uipkg.Renderer) (string, error) {
			return "", errors.New("injected read failure")
		}
		code = runApp(t.Context(), []string{
			"--interactive",
			"--base-url", "http://localhost:8080/v1",
			"--model", "test-model",
			"--trace",
			"--trace-dir", traceDir,
		}, deps)
	})

	if code != 1 {
		t.Errorf("runApp() code = %d, want 1", code)
	}
	if _, err := stderr.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stderr) error = %v", err)
	}
	stderrOutput, err := io.ReadAll(stderr)
	if err != nil {
		t.Fatalf("ReadAll(stderr) error = %v", err)
	}
	if !strings.Contains(string(stderrOutput), "cannot read prompt: injected read failure") {
		t.Errorf("stderr = %q, want prompt read error", stderrOutput)
	}

	entries, err := os.ReadDir(traceDir)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", traceDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("trace files = %d, want 1", len(entries))
	}
	events := readTraceEvents(t, filepath.Join(traceDir, entries[0].Name()))
	if len(traceEventsByName(events, "session_end")) != 1 {
		t.Errorf("session_end events = %d, want 1", len(traceEventsByName(events, "session_end")))
	}
}

// TestInteractiveSIGINTCancelsTurnWithoutClosingSession checks an interrupt during
// a turn returns to the main prompt instead of cancelling the interactive session.
func TestInteractiveSIGINTCancelsTurnWithoutClosingSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	stdin, stdinWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdin) error = %v", err)
	}
	t.Cleanup(func() {
		stdin.Close()       //nolint:errcheck // best-effort cleanup of test pipes.
		stdinWriter.Close() //nolint:errcheck // best-effort cleanup of test pipes.
	})

	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp(stdout) error = %v", err)
	}
	t.Cleanup(func() {
		stdout.Close() //nolint:errcheck // best-effort cleanup of the temporary output.
	})

	transport := &blockingContextTransport{started: make(chan struct{})}
	deps := defaultRuntimeDeps()
	deps.Stdin = stdin
	deps.Stdout = stdout
	deps.Stderr = stdout
	deps.HTTPClient = &http.Client{Transport: transport}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	t.Cleanup(func() {
		signal.Stop(interrupts)
	})

	done := make(chan int, 1)
	go func() {
		done <- runApp(context.Background(), []string{
			"--interactive",
			"--base-url", "http://localhost:8080/v1",
			"--model", "test-model",
		}, deps)
	}()

	if _, err := io.WriteString(stdinWriter, "answer something\n"); err != nil {
		t.Fatalf("WriteString(prompt) error = %v", err)
	}

	select {
	case <-transport.started:
	case <-time.After(2 * time.Second):
		t.Fatal("LLM request did not start")
	}

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess() error = %v", err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatalf("Signal(os.Interrupt) error = %v", err)
	}

	select {
	case <-interrupts:
	case <-time.After(2 * time.Second):
		t.Fatal("test guard did not observe os.Interrupt")
	}

	if _, err := io.WriteString(stdinWriter, "/exit\n"); err != nil {
		t.Fatalf("WriteString(/exit) error = %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("runApp() code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interactive session did not exit after /exit")
	}

	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	if !strings.Contains(string(output), "Request cancelled.") {
		t.Fatalf("output missing turn cancellation: %q", output)
	}
	if !strings.Contains(string(output), "Session closed.") {
		t.Fatalf("output missing session close after /exit: %q", output)
	}
}

// TestSwitchInteractiveModelMissingKeepsCurrent checks bad model names do not mutate the session.
func TestSwitchInteractiveModelMissingKeepsCurrent(t *testing.T) {
	cfg := defaultConfig()
	cfg.ModelName = "openai"
	cfg.BaseURL = "http://localhost:8080/v1"
	cfg.Model = "openai-model"
	cfg.Models = []modelConfig{
		{Name: "openai", BaseURL: "http://localhost:8080/v1", Model: "openai-model", SupportsResponseFormat: true},
	}
	before := cfg

	err := switchInteractiveModel(&cfg, "missing")
	if err == nil {
		t.Fatalf("switchInteractiveModel() error = nil, want missing profile")
	}
	if cfg.ModelName != before.ModelName || cfg.Model != before.Model || cfg.BaseURL != before.BaseURL {
		t.Fatalf("cfg changed after missing profile: %#v, want %#v", cfg, before)
	}
}

// TestPrintModelProfilesToListsActiveModel checks /model without args lists configured profiles.
func TestPrintModelProfilesToListsActiveModel(t *testing.T) {
	cfg := defaultConfig()
	cfg.ModelName = "mlx"
	cfg.Models = []modelConfig{
		{Name: "openai", Model: "gpt"},
		{Name: "mlx", Model: "qwen"},
	}

	var output strings.Builder
	printModelProfilesTo(&output, false, cfg)
	got := output.String()
	if !strings.Contains(got, "* mlx · qwen") || !strings.Contains(got, "openai · gpt") {
		t.Fatalf("printModelProfilesTo() = %q, want active model list", got)
	}
}

// captureMainLoopIO replaces stdin/stdout with temporary files for deterministic loop tests.
func captureMainLoopIO(t *testing.T, input string, client *http.Client, fn func(runtimeDeps)) string {
	t.Helper()

	dir := t.TempDir()
	stdinFile, err := os.CreateTemp(dir, "stdin")
	if err != nil {
		t.Fatalf("CreateTemp(stdin) error = %v", err)
	}
	if _, err := stdinFile.WriteString(input); err != nil {
		t.Fatalf("WriteString(stdin) error = %v", err)
	}
	if _, err := stdinFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdin) error = %v", err)
	}

	stdoutFile, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatalf("CreateTemp(stdout) error = %v", err)
	}

	defer func() {
		stdinFile.Close()  //nolint:errcheck // best-effort cleanup of temporary test files.
		stdoutFile.Close() //nolint:errcheck // best-effort cleanup of temporary test files.
	}()

	deps := defaultRuntimeDeps()
	deps.Stdin = stdinFile
	deps.Stdout = stdoutFile
	deps.Stderr = stdoutFile
	deps.HTTPClient = client

	fn(deps)

	if _, err := stdoutFile.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Seek(stdout) error = %v", err)
	}
	output, err := io.ReadAll(stdoutFile)
	if err != nil {
		t.Fatalf("ReadAll(stdout) error = %v", err)
	}
	return string(output)
}

// TestDoLLMRequestOmitsAuthorizationWhenAPIKeyEmpty checks local no-key endpoints get no auth header.
func TestDoLLMRequestOmitsAuthorizationWhenAPIKeyEmpty(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig("http://localhost")
	cfg.APIKey = ""

	_, err := doLLMRequest(t.Context(), fake.HTTPClient(), cfg, chatCompletionRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Messages:    []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("doLLMRequest() error = %v", err)
	}

	headers := fake.requestAuthorizations()
	if len(headers) != 1 || headers[0] != "" {
		t.Fatalf("Authorization headers = %#v, want empty header", headers)
	}
}

// TestDoLLMRequestSendsAuthorizationWhenAPIKeySet checks keyed endpoints keep bearer auth.
func TestDoLLMRequestSendsAuthorizationWhenAPIKeySet(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig(fake.URL())
	cfg.APIKey = "test-key"

	_, err := doLLMRequest(t.Context(), fake.HTTPClient(), cfg, chatCompletionRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Messages:    []chatMessage{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("doLLMRequest() error = %v", err)
	}

	headers := fake.requestAuthorizations()
	if len(headers) != 1 || headers[0] != "Bearer test-key" {
		t.Fatalf("Authorization headers = %#v, want bearer token", headers)
	}
}

// TestRunPlanningRoundPreservesStructuralParseCause checks callers can inspect
// both the structural category and the JSON parsing failure on either error path.
func TestRunPlanningRoundPreservesStructuralParseCause(t *testing.T) {
	tests := []struct {
		name        string
		allowRepair bool
		responses   []loopLLMResponse
	}{
		{
			name:        "repair disabled",
			allowRepair: false,
			responses:   []loopLLMResponse{{content: `{"action":}`}},
		},
		{
			name:        "repair exhausted",
			allowRepair: true,
			responses: []loopLLMResponse{
				{content: `{"action":}`},
				{content: `{"action":}`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newLoopLLMClient(t, tt.responses...)
			cfg := loopTestConfig(fake.URL())

			captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
				deps.Trace = openLoopTrace(t)
				_, err := runPlanningRound(t.Context(), planningRoundRequest{
					Deps: deps,
					Prompt: llmPromptRequest{
						Config:      cfg,
						Instruction: "inspect",
					},
					AllowStructuralRepair: tt.allowRepair,
				})
				if !errors.Is(err, errStructuralResponse) {
					t.Fatalf("runPlanningRound() error = %v, want structural response category", err)
				}
				var syntaxError *json.SyntaxError
				if !errors.As(err, &syntaxError) {
					t.Fatalf("runPlanningRound() error = %v, want JSON syntax cause", err)
				}
			})
		})
	}
}

// TestDoLLMRequestPropagatesContextCancellation checks LLM calls preserve cancellation identity.
func TestDoLLMRequestPropagatesContextCancellation(t *testing.T) {
	cfg := loopTestConfig("http://shellia.test")
	client := &http.Client{Transport: contextErrorTransport{}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := doLLMRequest(ctx, client, cfg, chatCompletionRequest{
		Model:    cfg.Model,
		Messages: []chatMessage{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("doLLMRequest() error = %v, want context.Canceled", err)
	}
}

// TestCallPlanningPromptUsesResponseFormatForLocalNoKey checks JSON mode is profile-driven.
func TestCallPlanningPromptUsesResponseFormatForLocalNoKey(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig("http://localhost")
	cfg.APIKey = ""

	if _, err := callPlanningPrompt(t.Context(), fake.HTTPClient(), cfg, "system", "user"); err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("request bodies = %#v, want one body", bodies)
	}

	var body struct {
		ResponseFormat *responseFormat `json:"response_format"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", body.ResponseFormat)
	}
}

// TestCallPlanningPromptOmitsResponseFormatWhenUnsupported checks profile capability disables JSON mode.
func TestCallPlanningPromptOmitsResponseFormatWhenUnsupported(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig("http://localhost")
	cfg.SupportsResponseFormat = false

	if _, err := callPlanningPrompt(t.Context(), fake.HTTPClient(), cfg, "system", "user"); err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("request bodies = %#v, want one body", bodies)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if _, ok := body["response_format"]; ok {
		t.Fatalf("request body includes response_format: %s", bodies[0])
	}
}

// TestCallPlanningPromptKeepsResponseFormatWithKey checks JSON mode remains for compatible providers.
func TestCallPlanningPromptKeepsResponseFormatWithKey(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: "ok"})
	cfg := loopTestConfig(fake.URL())

	if _, err := callPlanningPrompt(t.Context(), fake.HTTPClient(), cfg, "system", "user"); err != nil {
		t.Fatalf("callPlanningPrompt() error = %v", err)
	}

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("request bodies = %#v, want one body", bodies)
	}

	var body struct {
		ResponseFormat *responseFormat `json:"response_format"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &body); err != nil {
		t.Fatalf("Unmarshal(request body) error = %v", err)
	}
	if body.ResponseFormat == nil || body.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", body.ResponseFormat)
	}
}

// TestRunTurnPrintsRawPrompt checks --raw-prompt exposes the exact model prompt pair.
func TestRunTurnPrintsRawPrompt(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"No command needed.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.RawPrompt = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	required := []string{
		"Raw LLM prompt",
		"system:",
		"You are Shellia's goal-oriented planning layer.",
		"return exactly one decision",
		"user:",
		"Execution authority: allowed",
		"User instruction:\nanswer directly",
	}
	for _, snippet := range required {
		if !strings.Contains(output, snippet) {
			t.Fatalf("runTurn() output missing %q in %q", snippet, output)
		}
	}
}

// TestRunTurnReturnsFinalAnswerWithoutCommands checks the answer-only path of the main turn loop.
func TestRunTurnReturnsFinalAnswerWithoutCommands(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"No command needed.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeCompleted {
		t.Fatalf("runTurn() Outcome = %q, want completed", result.Outcome)
	}
	if result.Result != "No command needed." {
		t.Fatalf("runTurn() Result = %q, want %q", result.Result, "No command needed.")
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "No command needed.") {
		t.Fatalf("runTurn() output does not contain final answer: %q", output)
	}
}

// TestRunTurnTraceRecordsFinalAnswerWithoutCommands checks empty plans are diagnosable.
func TestRunTurnTraceRecordsFinalAnswerWithoutCommands(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"No command needed.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "answer directly")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	if len(traceEventsByName(events, "turn_start")) != 1 {
		t.Fatalf("turn_start events = %d, want 1", len(traceEventsByName(events, "turn_start")))
	}
	if len(traceEventsByName(events, "llm_prompt")) != 1 {
		t.Fatalf("llm_prompt events = %d, want 1", len(traceEventsByName(events, "llm_prompt")))
	}
	plannerEvents := traceEventsByName(events, "planner_result")
	if len(plannerEvents) != 1 {
		t.Fatalf("planner_result events = %d, want 1", len(plannerEvents))
	}
	plannerData := traceEventData(t, plannerEvents[0])
	if plannerData["commands_count"] != float64(0) {
		t.Fatalf("commands_count = %#v, want 0", plannerData["commands_count"])
	}
	decisionEvents := traceEventsByName(events, "shellia_decision")
	if len(decisionEvents) != 1 {
		t.Fatalf("shellia_decision events = %d, want 1", len(decisionEvents))
	}
	decisionData := traceEventData(t, decisionEvents[0])
	if decisionData["decision"] != "complete" {
		t.Fatalf("decision = %#v, want complete", decisionData["decision"])
	}
}

// TestRunTurnExecutesSafePlanAndCompletes checks execution evidence feeds the next decision.
func TestRunTurnExecutesSafePlanAndCompletes(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Printed shellia-loop.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeCompleted {
		t.Fatalf("runTurn() Outcome = %q, want completed", result.Outcome)
	}
	if len(result.Executions) != 1 {
		t.Fatalf("runTurn() executions = %d, want 1", len(result.Executions))
	}
	if result.Executions[0].ExitCode != 0 {
		t.Fatalf("execution exit code = %d, want 0", result.Executions[0].ExitCode)
	}
	if result.Executions[0].Stdout.Text != "shellia-loop" {
		t.Fatalf("execution stdout = %q, want %q", result.Executions[0].Stdout.Text, "shellia-loop")
	}
	if result.Result != "Printed shellia-loop." {
		t.Fatalf("runTurn() Result = %q, want %q", result.Result, "Printed shellia-loop.")
	}
}

// TestRunTurnPropagatesParentCancellationDuringSummary checks Ctrl+C is not
// converted into a successful fallback response after commands have run.
func TestRunTurnPropagatesParentCancellationDuringSummary(t *testing.T) {
	summaryStarted := make(chan struct{})
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{cancellationStart: summaryStarted},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	ctx, cancel := context.WithCancel(t.Context())

	type turnOutcome struct {
		result turnResult
		err    error
	}
	done := make(chan turnOutcome, 1)
	go func() {
		var result turnResult
		var runErr error
		captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
			result, runErr = runTurn(ctx, deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker"))
		})
		done <- turnOutcome{result: result, err: runErr}
	}()

	select {
	case <-summaryStarted:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("summary request did not start")
	}

	select {
	case outcome := <-done:
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("runTurn() error = %v, want context.Canceled", outcome.err)
		}
		if len(outcome.result.Executions) != 1 {
			t.Fatalf("runTurn() result = %#v, want partial execution", outcome.result)
		}
		if outcome.result.Outcome != turnOutcomeCancelled {
			t.Fatalf("runTurn() outcome = %q, want cancelled", outcome.result.Outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runTurn() did not return after cancellation")
	}
}

// TestRunTurnTraceRecordsFollowUpDecision checks the decision after execution is traced.
func TestRunTurnTraceRecordsFollowUpDecision(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Printed shellia-loop.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "shellia-loop", TotalBytes: 12, KeptBytes: 12},
			}}}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	var planningPrompts int
	var planningResponses int
	for _, event := range events {
		if event["phase"] != "planning" {
			continue
		}
		switch event["event"] {
		case "llm_prompt":
			planningPrompts++
		case "llm_response":
			planningResponses++
		}
	}
	if planningPrompts != 2 || planningResponses != 2 {
		t.Fatalf("planning events = prompts %d responses %d, want 2 and 2", planningPrompts, planningResponses)
	}
}

// TestRunTurnDeclinesPlanWithoutExecuting checks a rejected plan does not run or summarize commands.
func TestRunTurnDeclinesPlanWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "print marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatalf("ExecuteCommands was called after declining the plan")
	}
	if result.Outcome != turnOutcomeDeclined || len(result.Plans) != 1 || len(result.Executions) != 0 {
		t.Fatalf("runTurn() result = %#v, want declined plan without executions", result)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "Plan not executed.") {
		t.Fatalf("declined plan output does not contain cancellation message: %q", output)
	}
}

// TestRunTurnPlanOnlyPrintsCommandsWithoutExecuting checks -p style turns stop after planning.
func TestRunTurnPlanOnlyPrintsCommandsWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatalf("ExecuteCommands was called in plan-only mode")
	}
	if result.Outcome != turnOutcomePlanned || len(result.Plans) != 1 || len(result.Executions) != 0 {
		t.Fatalf("runTurn() result = %#v, want one planned command and no executions", result)
	}
	if !strings.Contains(output, "touch marker.txt") {
		t.Fatalf("plan-only output does not contain command: %q", output)
	}
	if !strings.Contains(output, "run › touch marker.txt") {
		t.Fatalf("plan-only output does not render command box: %q", output)
	}
	for _, hidden := range []string{"risk", "safety"} {
		if strings.Contains(output, hidden) {
			t.Fatalf("plan-only output contains verbose metadata %q: %q", hidden, output)
		}
	}
}

// TestRunTurnPlanOnlyCannotReachExecutor checks plan-only authority is final
// even when the model returns an executable command plan.
func TestRunTurnPlanOnlyCannotReachExecutor(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, errors.New("executor reached in plan-only mode")
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatal("ExecuteCommands was called in plan-only mode")
	}
	if result.Outcome != turnOutcomePlanned || len(result.Plans) != 1 || len(result.Executions) != 0 {
		t.Fatalf("runTurn() result = %#v, want planned outcome without executions", result)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "touch marker.txt") {
		t.Fatalf("plan-only output does not contain command: %q", output)
	}
}

// TestRunTurnPlanOnlyReturnsBlockerWithoutExecuting checks /plan keeps its immutable authority on blocked decisions.
func TestRunTurnPlanOnlyReturnsBlockerWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"blocked","objective_mode":"act","success_criteria":"Test objective completed","summary":"Need the target container.","blocker_kind":"missing_input","blocker_reason":"No Docker container or image was specified.","commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "run php in docker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if executed {
		t.Fatal("ExecuteCommands was called for a blocked /plan decision")
	}
	if result.Outcome != turnOutcomeBlocked || result.BlockerKind != "missing_input" {
		t.Fatalf("runTurn() result = %#v, want missing_input blocker", result)
	}
	if fake.requestCount() != 1 || !strings.Contains(output, "No Docker container or image was specified.") {
		t.Fatalf("requests = %d, output = %q", fake.requestCount(), output)
	}
}

// TestRunTurnUsesBoundedExplicitGitObservation checks repository state reaches
// the model only through the normal command observation path.
func TestRunTurnUsesBoundedExplicitGitObservation(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect current repository state.","commands":[{"command":"git status --short","purpose":"Inspect Git status","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Repository state inspected.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.ObservationOutputChars = 12
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout: capturedStream{
					Text:       " M first.go\n?? second.go",
					TotalBytes: 500,
					KeptBytes:  20,
					Truncated:  true,
				},
			}}}, nil
		}

		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "show me the Git status")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	bodies := fake.requestBodies()
	if len(bodies) != 2 {
		t.Fatalf("request bodies = %d, want initial and observation rounds", len(bodies))
	}
	for index, body := range bodies {
		for _, ambient := range []string{"git.is_repo", "git.branch", "git.status_short"} {
			if strings.Contains(body, ambient) {
				t.Fatalf("request %d contains ambient Git field %q: %q", index+1, ambient, body)
			}
		}
	}
	if !strings.Contains(bodies[1], "git status --short") || !strings.Contains(bodies[1], "stdout truncated locally: kept 20 of 500 bytes") {
		t.Fatalf("observation request does not contain bounded explicit Git output: %q", bodies[1])
	}
	if strings.Contains(bodies[1], "?? second.go") {
		t.Fatalf("observation request contains output beyond the prompt budget: %q", bodies[1])
	}
}

// TestRunTurnReplansOnceAfterOrdinaryFailure checks failed execution becomes
// grounded input for one confirmed recovery planning round.
func TestRunTurnReplansOnceAfterOrdinaryFailure(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run initial batch.","commands":[{"command":"false","purpose":"Trigger failure","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""},{"command":"touch blocked","purpose":"Blocked dependent step","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""},{"command":"pwd","purpose":"Independent inspection","risk":"safe","requires_confirmation":false,"independent_on_failure":true,"interactive":false,"interactive_reason":""}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run recovery.","commands":[{"command":"git status --short","purpose":"Verify repository state","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovery completed.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[4]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.ContinueOnError = true
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)
	call := 0

	output := captureMainLoopIO(t, "y\ny\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			call++
			if call == 1 {
				if len(plans) != 3 || !plans[2].IndependentOnFailure {
					t.Fatalf("initial plans = %#v, want failure, dependent, and independent steps", plans)
				}
				return commandBatchResult{
					Executions: []commandExecution{
						{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 7, Stderr: capturedStream{Text: "initial failure"}},
						{Command: plans[2].Command, Purpose: plans[2].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}},
					},
					Skipped:            []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "dependent on an earlier failed command"}},
					HadOrdinaryFailure: true,
				}, nil
			}
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}}}}, nil
		}
		result, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover from a failed command"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
		if result.Result != "Recovery completed." || len(result.Executions) != 3 || len(result.Skipped) != 1 {
			t.Fatalf("result = %#v, want recovered result with three executions and one skip", result)
		}
	})

	bodies := fake.requestBodies()
	if len(bodies) != 3 {
		t.Fatalf("request count = %d, want three workflow decisions", len(bodies))
	}
	for _, snippet := range []string{"Exit code: 7", "initial failure", "Independent inspection", "Skipped commands from the current task:", "touch blocked", "dependent on an earlier failed command"} {
		if !strings.Contains(bodies[1], snippet) {
			t.Fatalf("recovery prompt missing %q: %q", snippet, bodies[1])
		}
	}
	for _, snippet := range []string{"Skipped commands", "touch blocked", "dependent on an earlier failed command"} {
		if !strings.Contains(bodies[2], snippet) {
			t.Fatalf("completion prompt missing %q: %q", snippet, bodies[2])
		}
	}
	if strings.Count(output, "Execute this plan? [y/n]: yes") != 2 {
		t.Fatalf("output = %q, want confirmation for initial and recovery plans", output)
	}

	events := closeLoopTraceAndRead(t, logger)
	continueAfterFailure := 0
	for _, event := range traceEventsByName(events, "shellia_decision") {
		if traceEventData(t, event)["decision"] == "continue_after_execution_failure" {
			continueAfterFailure++
		}
	}
	if continueAfterFailure != 1 {
		t.Fatalf("continue_after_execution_failure events = %d, want 1", continueAfterFailure)
	}

	turnEnds := traceEventsByName(events, "turn_end")
	if len(turnEnds) != 1 || traceEventData(t, turnEnds[0])["skipped_count"] != float64(1) {
		t.Fatalf("turn_end events = %#v, want skipped_count 1", turnEnds)
	}

	plannerEvents := traceEventsByName(events, "planner_result")
	if len(plannerEvents) != 3 {
		t.Fatalf("planner_result events = %d, want 3", len(plannerEvents))
	}
	commands, ok := traceEventData(t, plannerEvents[0])["commands"].([]any)
	if !ok || len(commands) != 3 {
		t.Fatalf("planner commands = %#v, want three command objects", commands)
	}
	first, firstOK := commands[0].(map[string]any)
	independent, independentOK := commands[2].(map[string]any)
	if !firstOK || !independentOK || first["independent_on_failure"] != false || independent["independent_on_failure"] != true {
		t.Fatalf("planner commands = %#v, want explicit conservative and independent failure fields", commands)
	}
}

// TestRunTurnFiltersSuccessfulCorrectionsButRetriesFailures checks repeated
// mixed proposals show, confirm, and execute only commands that have not succeeded.
func TestRunTurnFiltersSuccessfulCorrectionsButRetriesFailures(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run failing command.","commands":[{"command":"false","purpose":"Trigger failure","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Apply correction and retry.","commands":[{"command":"touch corrected","purpose":"Apply correction","risk":"safe","requires_confirmation":false,"independent_on_failure":true},{"command":"false","purpose":"Retry failure","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Retry the remaining failure.","commands":[{"command":"touch corrected","purpose":"Apply correction","risk":"safe","requires_confirmation":false,"independent_on_failure":true},{"command":"false","purpose":"Retry failure","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Retries exhausted after grounded outcomes.","completion_basis":{"type":"current_observation","evidence_revision":3,"attempt_ids":[5]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.PlanningMaxRounds = 4
	ctxInfo := loopTestContext(t)
	var executionBatches [][]string

	var result turnResult
	output := captureMainLoopIO(t, "y\ny\ny\n", fake.HTTPClient(), func(deps runtimeDeps) { //nolint:dupword // Each response confirms a distinct execution proposal.
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			executionBatches = append(executionBatches, commandNames(plans))
			batch := commandBatchResult{}
			for _, plan := range plans {
				exitCode := 0
				if plan.Command == "false" {
					exitCode = 1
					batch.HadOrdinaryFailure = true
				}
				batch.Executions = append(batch.Executions, commandExecution{
					Command:  plan.Command,
					Purpose:  plan.Purpose,
					ExitCode: exitCode,
				})
			}
			return batch, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "correct and retry a failure"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	wantBatches := [][]string{{"false"}, {"touch corrected", "false"}, {"false"}}
	if !reflect.DeepEqual(executionBatches, wantBatches) {
		t.Fatalf("execution batches = %v, want %v", executionBatches, wantBatches)
	}
	wantExecutions := []commandExecution{
		{Command: "false", Purpose: "Trigger failure", ExitCode: 1},
		{Command: "touch corrected", Purpose: "Apply correction", ExitCode: 0},
		{Command: "false", Purpose: "Retry failure", ExitCode: 1},
		{Command: "false", Purpose: "Retry failure", ExitCode: 1},
	}
	if !reflect.DeepEqual(result.Executions, wantExecutions) {
		t.Fatalf("result executions = %#v, want %#v", result.Executions, wantExecutions)
	}
	if result.Result != "Retries exhausted after grounded outcomes." {
		t.Fatalf("result = %q, want grounded final answer", result.Result)
	}
	if strings.Count(output, "Execute this plan? [y/n]: yes") != 3 {
		t.Fatalf("output = %q, want three filtered plan confirmations", output)
	}
	thirdStart := strings.Index(output, "Retry the remaining failure.")
	if thirdStart < 0 {
		t.Fatalf("output = %q, want third plan", output)
	}
	thirdEndOffset := strings.Index(output[thirdStart:], "Execute this plan? [y/n]: yes")
	if thirdEndOffset < 0 {
		t.Fatalf("output = %q, want third plan confirmation", output)
	}
	thirdPlan := output[thirdStart : thirdStart+thirdEndOffset]
	if !strings.Contains(thirdPlan, "false") || strings.Contains(thirdPlan, "touch corrected") {
		t.Fatalf("third displayed plan = %q, want only failed retry", thirdPlan)
	}
}

// TestRunTurnMultipleFailuresTriggerOneRecoveryRound checks failure count does
// not create more than one follow-up planning request for a batch.
func TestRunTurnMultipleFailuresTriggerOneRecoveryRound(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run failures.","commands":[{"command":"false","purpose":"First failure","risk":"safe","requires_confirmation":false},{"command":"exit 2","purpose":"Second failure","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover once.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovered once.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[3]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	calls := 0

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			calls++
			if calls == 1 {
				return commandBatchResult{
					Executions: []commandExecution{
						{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1},
						{Command: plans[1].Command, Purpose: plans[1].Purpose, ExitCode: 2},
					},
					HadOrdinaryFailure: true,
				}, nil
			}
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover once")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if calls != 2 || fake.requestCount() != 3 {
		t.Fatalf("execution calls = %d, LLM requests = %d, want two batches and planning/recovery/summary", calls, fake.requestCount())
	}
}

// TestRunTurnTimeoutDoesNotReplan checks timeout-only batches stop after
// execution while retaining every execution returned by the runner.
func TestRunTurnTimeoutDoesNotReplan(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run timeout batch.","commands":[{"command":"slow","purpose":"Timed operation","risk":"safe","requires_confirmation":false},{"command":"pwd","purpose":"Independent inspection","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Stopped after timeout.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{
				Executions: []commandExecution{
					{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 124},
					{Command: plans[1].Command, Purpose: plans[1].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}},
				},
				HadTimeout: true,
			}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "run timeout batch"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if fake.requestCount() != 1 || result.Outcome != turnOutcomeTimeout || len(result.Executions) != 2 {
		t.Fatalf("requests = %d, result = %#v, want one decision and timeout with both executions", fake.requestCount(), result)
	}
	events := closeLoopTraceAndRead(t, logger)
	exclusionCount := 0
	for _, event := range traceEventsByName(events, "shellia_decision") {
		data := traceEventData(t, event)
		if data["decision"] == "timeout" {
			exclusionCount++
		}
	}
	if exclusionCount != 1 {
		t.Fatalf("timeout decisions = %d, want 1", exclusionCount)
	}
}

// TestRunTurnCancellationDoesNotReplan checks cancellation returns immediately
// and records why execution failure recovery was excluded.
func TestRunTurnCancellationDoesNotReplan(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run commands.","commands":[{"command":"pwd","purpose":"Completed inspection","risk":"safe","requires_confirmation":false},{"command":"wait","purpose":"Cancelled step","risk":"safe","requires_confirmation":false}]}`})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{
				Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}}},
				Skipped:    []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "cancelled"}},
			}, context.Canceled
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "cancel command"))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runTurn() error = %v, want context.Canceled", err)
		}
	})

	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want initial planning only", fake.requestCount())
	}
	if result.Outcome != turnOutcomeCancelled || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("runTurn() result = %#v, want actionable partial cancellation result", result)
	}
	events := closeLoopTraceAndRead(t, logger)
	found := false
	for _, event := range traceEventsByName(events, "shellia_decision") {
		data := traceEventData(t, event)
		if data["decision"] == "execution_failure_replan_excluded" && data["reason"] == "cancellation" {
			found = true
		}
	}
	if !found {
		t.Fatal("trace missing cancellation execution_failure_replan_excluded decision")
	}
}

// TestRunTurnStructuralExecutionErrorReturnsPartialResult checks a runner error
// stops immediately without discarding real attempts or skipped commands.
func TestRunTurnStructuralExecutionErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run batch.","commands":[{"command":"pwd","purpose":"Inspect","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runnerErr := errors.New("runner transport failed")

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{
				Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}}},
				Skipped:    []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "runner stopped"}},
			}, runnerErr
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "run structural failure"))
		if !errors.Is(err, runnerErr) {
			t.Fatalf("runTurn() error = %v, want runner error", err)
		}
	})

	if fake.requestCount() != 1 || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("requests = %d, result = %#v, want one plan and partial result", fake.requestCount(), result)
	}
	if result.Outcome != turnOutcomeStructuralError {
		t.Fatalf("result.Outcome = %q, want structural_error", result.Outcome)
	}
}

// TestRunTurnLaterPlanningErrorReturnsPartialResult checks a failed follow-up
// model request retains the batch that required that request.
func TestRunTurnLaterPlanningErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect first.","commands":[{"command":"pwd","purpose":"Inspect","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{status: http.StatusBadRequest, content: "bad follow-up request"},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{
				Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0, Stdout: capturedStream{Text: ctxInfo.CWD}}},
				Skipped:    []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "not reached"}},
			}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect before planning error"))
		if err == nil || !strings.Contains(err.Error(), "status 400") {
			t.Fatalf("runTurn() error = %v, want follow-up HTTP error", err)
		}
	})

	if fake.requestCount() != 2 || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("requests = %d, result = %#v, want partial result after failed follow-up", fake.requestCount(), result)
	}
	if result.Outcome != turnOutcomeBlocked || result.BlockerKind != "unavailable" {
		t.Fatalf("result terminal cause = %#v, want blocked/unavailable", result)
	}
}

// TestRunTurnRecoveryConfirmationErrorReturnsPartialResult checks failure to
// read a later plan confirmation does not erase earlier outcomes.
func TestRunTurnRecoveryConfirmationErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, deps runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			if err := deps.Stdin.Close(); err != nil {
				t.Fatalf("Close(stdin) error = %v", err)
			}
			return commandBatchResult{
				Executions:         []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}},
				Skipped:            []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "dependent"}},
				HadOrdinaryFailure: true,
			}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "confirm recovery"))
		if err == nil || !strings.Contains(err.Error(), "cannot read plan confirmation") {
			t.Fatalf("runTurn() error = %v, want recovery confirmation error", err)
		}
	})

	if len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v, want partial result after confirmation error", result)
	}
}

// TestRunTurnPlanningLimitPromptErrorReturnsPartialResult checks the existing
// limit prompt's error path preserves accumulated execution state.
func TestRunTurnPlanningLimitPromptErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, deps runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			if err := deps.Stdin.Close(); err != nil {
				t.Fatalf("Close(stdin) error = %v", err)
			}
			return commandBatchResult{
				Executions:         []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}},
				Skipped:            []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "dependent"}},
				HadOrdinaryFailure: true,
			}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "reach planning limit"))
		if err == nil {
			t.Fatal("runTurn() error = nil, want planning-limit prompt error")
		}
	})

	if len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v, want partial result after planning-limit error", result)
	}
}

// TestRunTurnMixedFailureWithTimeoutDoesNotReplan checks timeout remains terminal for mixed batches.
func TestRunTurnMixedFailureWithTimeoutDoesNotReplan(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run mixed failures.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"slow","purpose":"Timeout","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover mixed batch.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovered mixed batch.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	calls := 0

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			calls++
			if calls == 1 {
				return commandBatchResult{
					Executions: []commandExecution{
						{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1},
						{Command: plans[1].Command, Purpose: plans[1].Purpose, ExitCode: 124},
					},
					HadOrdinaryFailure: true,
					HadTimeout:         true,
				}, nil
			}
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover mixed failures"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if calls != 1 || fake.requestCount() != 1 || result.Outcome != turnOutcomeTimeout {
		t.Fatalf("execution calls = %d, LLM requests = %d, result = %#v, want terminal timeout", calls, fake.requestCount(), result)
	}
}

// TestRunTurnInteractiveRepairUsesPlanningLimit checks interactive-prompt
// repair retains partial execution and shares the normal planning-limit path.
func TestRunTurnInteractiveRepairUsesPlanningLimit(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Run prompt-prone command.","commands":[{"command":"prompting-command","purpose":"Attempt command","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Repair prompt use.","commands":[{"command":"pwd","purpose":"Recover non-interactively","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovered from interactive prompt.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)
	calls := 0

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			calls++
			if calls == 1 {
				return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 130}}}, &interactivePromptError{Command: plans[0].Command, Prompt: "Continue? [y/N]"}
			}
			return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "repair interactive command"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if calls != 2 || len(result.Executions) != 2 || !strings.Contains(output, "planning reached the current follow-up round limit (1)") {
		t.Fatalf("calls = %d, result = %#v, output = %q", calls, result, output)
	}
}

// TestRunTurnFailureRecoveryUsesPlanningLimit checks ordinary failures share
// the existing limit extension prompt and preserve partial outcomes on decline.
func TestRunTurnFailureRecoveryUsesPlanningLimit(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		responses    []loopLLMResponse
		wantRequests int
		wantCalls    int
		wantResult   string
		wantOutcome  turnOutcome
	}{
		{
			name:  "accepted",
			input: "y\ny\n",
			responses: []loopLLMResponse{
				{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"blocked","purpose":"Skip","risk":"safe","requires_confirmation":false}]}`},
				{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
				{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovered after extension.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[3]},"commands":[]}`},
			},
			wantRequests: 3,
			wantCalls:    2,
			wantResult:   "Recovered after extension.",
			wantOutcome:  turnOutcomeCompleted,
		},
		{
			name:  "declined",
			input: "n\n",
			responses: []loopLLMResponse{
				{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"blocked","purpose":"Skip","risk":"safe","requires_confirmation":false}]}`},
			},
			wantRequests: 1,
			wantCalls:    1,
			wantResult:   "Fail first.",
			wantOutcome:  turnOutcomePlanningLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newLoopLLMClient(t, tt.responses...)
			cfg := loopTestConfig(fake.URL())
			cfg.AskConfirmPlan = false
			cfg.PlanningMaxRounds = 1
			ctxInfo := loopTestContext(t)
			calls := 0
			logger := openLoopTrace(t)

			var result turnResult
			captureMainLoopIO(t, tt.input, fake.HTTPClient(), func(deps runtimeDeps) {
				deps.Trace = logger
				deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
					calls++
					if calls == 1 {
						return commandBatchResult{
							Executions:         []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}},
							Skipped:            []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "dependent on an earlier failed command"}},
							HadOrdinaryFailure: true,
						}, nil
					}
					return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}}}, nil
				}
				var err error
				result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover within limit"))
				if err != nil {
					t.Fatalf("runTurn() error = %v", err)
				}
			})

			if fake.requestCount() != tt.wantRequests || calls != tt.wantCalls || result.Result != tt.wantResult || result.Outcome != tt.wantOutcome || len(result.Executions) != calls || len(result.Skipped) != 1 {
				t.Fatalf("requests = %d, calls = %d, result = %#v", fake.requestCount(), calls, result)
			}

			events := closeLoopTraceAndRead(t, logger)
			limitEvents := traceEventsByName(events, "shellia_decision")
			foundLimitContinuation := false
			foundFailureContinuation := false
			foundLimitDecline := false
			for _, event := range limitEvents {
				data := traceEventData(t, event)
				switch data["decision"] {
				case "planning_limit_continuation":
					if data["trigger"] == "execution_failure" {
						foundLimitContinuation = true
						foundLimitDecline = data["accepted"] == false
					}
				case "continue_after_execution_failure":
					foundFailureContinuation = true
				}
			}
			if !foundLimitContinuation {
				t.Fatalf("planning limit continuation trace missing execution_failure trigger")
			}
			if tt.name == "accepted" && (!foundFailureContinuation || foundLimitDecline) {
				t.Fatalf("accepted trace decisions: continue = %t, declined = %t", foundFailureContinuation, foundLimitDecline)
			}
			if tt.name == "declined" && (foundFailureContinuation || !foundLimitDecline) {
				t.Fatalf("declined trace decisions: continue = %t, declined = %t", foundFailureContinuation, foundLimitDecline)
			}
		})
	}
}

// TestRunTurnPreservesBatchWhenRecoveryPlanIsEmpty checks a post-execution
// final answer remains actionable and carries executions and skips.
func TestRunTurnPreservesBatchWhenRecoveryPlanIsEmpty(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect.","commands":[{"command":"pwd","purpose":"Inspect","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Nothing else to run.","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{
				Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 0}},
				Skipped:    []skippedCommand{{Command: "unused", Purpose: "Skipped action", Reason: "not needed"}},
			}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect then finish"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeCompleted || len(result.Executions) != 1 || len(result.Skipped) != 1 || result.Result != "Nothing else to run." {
		t.Fatalf("result = %#v, want actionable accumulated batch", result)
	}
}

// TestRunTurnPreservesBatchWhenRecoveryPlanIsDeclined checks declining a later
// plan does not discard outcomes already produced in the turn.
func TestRunTurnPreservesBatchWhenRecoveryPlanIsDeclined(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"blocked","purpose":"Skip","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Proposed recovery.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "y\nn\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{
				Executions:         []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}},
				Skipped:            []skippedCommand{{Command: plans[1].Command, Purpose: plans[1].Purpose, Reason: "dependent on an earlier failed command"}},
				HadOrdinaryFailure: true,
			}, nil
		}
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "decline recovery"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Outcome != turnOutcomeDeclined || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v, want accumulated actionable batch", result)
	}
	if !strings.Contains(output, "Plan not executed.") {
		t.Fatalf("output = %q, want declined recovery message", output)
	}
}

// TestRunTurnRecoveryUsesPlanAndCommandConfirmations checks both execution
// rounds traverse the same plan-level and command-level confirmation paths.
func TestRunTurnRecoveryUsesPlanAndCommandConfirmations(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover safely","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovered with confirmation.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[2]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.YesSafe = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "y\ny\ny\ny\n", fake.HTTPClient(), func(deps runtimeDeps) { //nolint:dupword // Each response confirms a distinct plan-level or command-level prompt.
		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "fail then recover"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if len(result.Executions) != 2 || result.Executions[0].ExitCode == 0 || result.Executions[1].ExitCode != 0 {
		t.Fatalf("result executions = %#v, want failed initial and successful recovery", result.Executions)
	}
	if strings.Count(output, "Execute this plan? [y/n]: yes") != 2 || strings.Count(output, "Run step 1/1? [y/e/i/n]: yes") != 2 {
		t.Fatalf("output = %q, want two accepted plan and command confirmations", output)
	}
}

// TestRunTurnSafeRecoveryHonorsYesSafe checks safe recovery commands only
// bypass their normal command prompt when yes_safe is enabled.
func TestRunTurnSafeRecoveryHonorsYesSafe(t *testing.T) {
	for _, tt := range []struct {
		name        string
		yesSafe     bool
		input       string
		wantPrompts int
	}{
		{name: "enabled", yesSafe: true, wantPrompts: 0},
		{name: "disabled", yesSafe: false, input: "y\n", wantPrompts: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := newLoopLLMClient(t,
				loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false}]}`},
				loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover safely","risk":"safe","requires_confirmation":false}]}`},
				loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recovered safely.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[2]},"commands":[]}`},
			)
			cfg := loopTestConfig(fake.URL())
			cfg.AskConfirmPlan = false
			cfg.YesSafe = tt.yesSafe
			ctxInfo := loopTestContext(t)
			calls := 0

			output := captureMainLoopIO(t, tt.input, fake.HTTPClient(), func(deps runtimeDeps) {
				deps.ExecuteCommands = func(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (commandBatchResult, error) {
					calls++
					if calls == 1 {
						return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}}, HadOrdinaryFailure: true}, nil
					}
					return executeCommands(ctx, deps, ui, cfg, ctxInfo, plans, priorExecutions)
				}
				if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover safely")); err != nil {
					t.Fatalf("runTurn() error = %v", err)
				}
			})

			if got := strings.Count(output, "Run step 1/1? [y/e/i/n]: yes"); got != tt.wantPrompts {
				t.Fatalf("accepted command prompts = %d, want %d: %q", got, tt.wantPrompts, output)
			}
		})
	}
}

// TestRunTurnRiskyRecoveryRequiresConfirmationWithYesSafe checks local safety
// classification remains authoritative for recovery commands.
func TestRunTurnRiskyRecoveryRequiresConfirmationWithYesSafe(t *testing.T) {
	ctxInfo := loopTestContext(t)
	marker := "recovered-marker"
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Recover.","commands":[{"command":"touch recovered-marker","purpose":"Create recovery marker","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Created recovery marker.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[2]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	calls := 0

	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan, priorExecutions []commandExecution) (commandBatchResult, error) {
			calls++
			if calls == 1 {
				return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}}, HadOrdinaryFailure: true}, nil
			}
			return executeCommands(ctx, deps, ui, cfg, ctxInfo, plans, priorExecutions)
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover with marker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if !strings.Contains(output, "Run step 1/1? [y/e/i/n]: yes") {
		t.Fatalf("output = %q, want risky recovery command confirmation", output)
	}
	if _, err := os.Stat(filepath.Join(ctxInfo.CWD, marker)); err != nil {
		t.Fatalf("recovery marker was not created: %v", err)
	}
}

// TestRunTurnCanContinueAfterPlanningRoundLimit checks users can approve extra planning rounds.
func TestRunTurnCanContinueAfterPlanningRoundLimit(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect the first fact.","commands":[{"command":"echo first","purpose":"Inspect first fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect the second fact.","commands":[{"command":"echo second","purpose":"Inspect second fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Done after extra planning.","completion_basis":{"type":"current_observation","evidence_revision":2,"attempt_ids":[2]},"commands":[]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 2
	ctxInfo := loopTestContext(t)
	var executed []string

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			executed = append(executed, plans[0].Command)
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: strings.TrimPrefix(plans[0].Command, "echo ")},
			}}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect twice"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if got := strings.Join(executed, ","); got != "echo first,echo second" {
		t.Fatalf("executed commands = %q, want both discovery commands", got)
	}
	if result.Result != "Done after extra planning." {
		t.Fatalf("result = %q, want final extra-round answer", result.Result)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want 3", fake.requestCount())
	}
	if !strings.Contains(output, "planning reached the current follow-up round limit (2)") {
		t.Fatalf("output missing planning limit warning: %q", output)
	}
	if !strings.Contains(output, "Continue planning? [y/n]: yes") {
		t.Fatalf("output missing continuation confirmation: %q", output)
	}
}

// TestRunTurnSummarizesWhenPlanningRoundLimitIsDeclined checks declining continuation ends cleanly.
func TestRunTurnSummarizesWhenPlanningRoundLimitIsDeclined(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect one fact.","commands":[{"command":"echo first","purpose":"Inspect first fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "n\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "first"},
			}}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "inspect once"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if result.Result != "Inspect one fact." || result.Outcome != turnOutcomePlanningLimit {
		t.Fatalf("result = %#v, want planning-limit outcome with last summary", result)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want one workflow decision", fake.requestCount())
	}
	if !strings.Contains(output, "SHELLIA_PLANNING_MAX_ROUNDS") {
		t.Fatalf("output missing env override guidance: %q", output)
	}
	if !strings.Contains(output, "Continue planning? [y/n]: no") {
		t.Fatalf("output missing declined continuation: %q", output)
	}
}

// TestRunTurnPlanOnlyDeclaresImmutableAuthority checks /plan uses the shared workflow contract with no execution authority.
func TestRunTurnPlanOnlyDeclaresImmutableAuthority(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Preparation: create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	bodies := fake.requestBodies()
	if len(bodies) != 1 {
		t.Fatalf("LLM request bodies = %d, want 1", len(bodies))
	}
	for _, snippet := range []string{"Shellia's goal-oriented planning layer", "Execution authority: plan_only; commands may be shown but must not be executed."} {
		if !strings.Contains(bodies[0], snippet) {
			t.Fatalf("plan-only request missing %q: %q", snippet, bodies[0])
		}
	}
}

// TestRunInteractiveProcessesPromptThenExit checks that the interactive loop runs one AI turn and exits cleanly.
func TestRunInteractiveProcessesPromptThenExit(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"Interactive answer.","completion_basis":{"type":"model_knowledge"},"commands":[]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "answer something\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "Interactive answer.") {
		t.Fatalf("interactive output does not contain AI answer: %q", output)
	}
	if !strings.Contains(output, "Session closed.") {
		t.Fatalf("interactive output does not contain close message: %q", output)
	}
}

// TestRunInteractiveRoutesPlainConversationThroughRenderer checks the app and
// executor boundary share one semantic turn without changing plain ordering.
func TestRunInteractiveRoutesPlainConversationThroughRenderer(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Cal consultar l'espai disponible.","commands":[{"command":"df -h /","purpose":"Mostrar l'espai lliure.","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Current disk space observed","summary":"Queden 419Gi lliures al disc arrel (/).","completion_basis":{"type":"current_observation","evidence_revision":1,"attempt_ids":[1]},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.ShowSystemOutput = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "quant d'espai queda al disc?\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, turnDeps runtimeDeps, _ bool, turnCfg config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			if turnDeps.Turn == nil {
				t.Fatal("ExecuteCommands received nil Turn")
			}
			step := turnDeps.Turn.BeginStep(turnCfg, 1, 1, plans[0])
			step.OutputLabel()
			step.OutputLine("419Gi available")
			step.Close()
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "419Gi available"},
			}}}, nil
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	position := 0
	for _, want := range []string{"test-user ›", "Shellia", "plan", "step 1/1", "system output", "Queden 419Gi lliures"} {
		next := strings.Index(output[position:], want)
		if next < 0 {
			t.Fatalf("interactive output lacks ordered value %q: %q", want, output)
		}
		position += next + len(want)
	}
}

// TestRunInteractiveModelCommandSwitchesWithoutLLM checks /model is handled locally.
func TestRunInteractiveModelCommandSwitchesWithoutLLM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"openai\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	cfg.ConfigPath = path
	cfg.ModelName = "openai"
	cfg.Models = []modelConfig{
		{Name: "openai", BaseURL: fake.URL(), Model: "openai-model", APIKey: "test-key", SupportsResponseFormat: true},
		{Name: "mlx", BaseURL: fake.URL(), Model: "mlx-model", APIKey: "test-key", SupportsResponseFormat: false},
	}
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "/model mlx\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 0 {
		t.Fatalf("LLM requests = %d, want 0", fake.requestCount())
	}
	if !strings.Contains(output, "Model switched to mlx") {
		t.Fatalf("output = %q, want model switch message", output)
	}
	if !strings.Contains(output, " · mlx-model") {
		t.Fatalf("output = %q, want selected model detail", output)
	}
	if !strings.Contains(output, "\nShellia Model switched to mlx.") {
		t.Fatalf("output = %q, want blank line before model switch message", output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `default_model = "mlx"`) {
		t.Fatalf("config after /model = %q, want persisted default", string(data))
	}
}

// TestRunInteractivePlanCommandPlansWithoutExecuting checks /plan is scoped to one prompt.
func TestRunInteractivePlanCommandPlansWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	output := captureMainLoopIO(t, "/plan create marker\nno\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan, []commandExecution) (commandBatchResult, error) {
			executed = true
			return commandBatchResult{}, nil
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if executed {
		t.Fatalf("ExecuteCommands was called for /plan")
	}
	if !strings.Contains(output, "touch marker.txt") {
		t.Fatalf("/plan output does not contain command: %q", output)
	}
	if !strings.Contains(output, "Session closed.") {
		t.Fatalf("interactive output does not contain close message: %q", output)
	}
}

// TestRunInteractiveRetryCommandRepeatsLastFailedInstruction checks /retry is a local command.
func TestRunInteractiveRetryCommandRepeatsLastFailedInstruction(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `not json`},
		loopLLMResponse{content: `still not json`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"No command needed.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "build it\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want structural attempt, repair, and retry", fake.requestCount())
	}
	for index, body := range fake.requestBodies() {
		if !strings.Contains(body, "User instruction:\\nbuild it") {
			t.Fatalf("request %d body = %q, want retried instruction", index+1, body)
		}
		if strings.Contains(body, "User instruction:\\n/retry") {
			t.Fatalf("request %d body = %q, want no /retry natural-language prompt", index+1, body)
		}
	}
	if !strings.Contains(output, "Retrying: build it") {
		t.Fatalf("output = %q, want retry status", output)
	}
}

// TestRunInteractiveCancellationRemembersPartialObservations checks a cancelled
// turn remains retryable while its real execution reaches session memory.
func TestRunInteractiveCancellationRemembersPartialObservations(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"execute","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Inspect once.","commands":[{"command":"pwd","purpose":"Inspect before cancellation","risk":"safe","requires_confirmation":false},{"command":"wait","purpose":"Cancelled step","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"observe","success_criteria":"Test objective completed","summary":"Retry received partial context.","completion_basis":{"type":"prior_session_evidence"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	calls := 0

	output := captureMainLoopIO(t, "inspect once\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan, _ []commandExecution) (commandBatchResult, error) {
			calls++
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "observed-before-cancel"},
			}}}, context.Canceled
		}
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if calls != 1 || fake.requestCount() != 2 {
		t.Fatalf("execution calls = %d, LLM requests = %d, want one cancelled execution and one retry plan", calls, fake.requestCount())
	}
	retryBody := fake.requestBodies()[1]
	for _, snippet := range []string{"last_retry_instruction: inspect once", "Recent reusable observations:", "Inspect before cancellation", "observed-before-cancel", "Prior session evidence: eligible for this same-objective retry."} {
		if !strings.Contains(retryBody, snippet) {
			t.Fatalf("retry prompt missing %q: %q", snippet, retryBody)
		}
	}
	if strings.Contains(retryBody, "Recent session context:") {
		t.Fatalf("retry prompt treated cancellation as successful history: %q", retryBody)
	}
	if !strings.Contains(output, "Request cancelled.") || !strings.Contains(output, "Retrying: inspect once") {
		t.Fatalf("output = %q, want cancellation and retry status", output)
	}
}

// TestRunInteractiveNewCommandClearsPromptContext checks /new resets conversational memory.
func TestRunInteractiveNewCommandClearsPromptContext(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"First answer.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
		loopLLMResponse{content: `{"action":"complete","objective_mode":"explain","success_criteria":"Test answer provided","summary":"Second answer.","completion_basis":{"type":"model_knowledge"},"commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "first task\n/new\nsecond task\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	bodies := fake.requestBodies()
	if len(bodies) != 2 {
		t.Fatalf("LLM requests = %d, want 2", len(bodies))
	}
	if !strings.Contains(bodies[0], "User instruction:\\nfirst task") {
		t.Fatalf("first request body = %q, want first task", bodies[0])
	}
	if !strings.Contains(bodies[1], "User instruction:\\nsecond task") {
		t.Fatalf("second request body = %q, want second task", bodies[1])
	}
	for _, snippet := range []string{"Recent session context:", "Session memory:", "first task", "First answer."} {
		if strings.Contains(bodies[1], snippet) {
			t.Fatalf("second request body contains cleared context %q: %q", snippet, bodies[1])
		}
	}
	if !strings.Contains(output, "─ new session · context cleared ") {
		t.Fatalf("output = %q, want new session separator", output)
	}
}

// TestRunInteractiveIgnoresEmptyPrompt checks Enter on an empty prompt does not start a turn.
func TestRunInteractiveIgnoresEmptyPrompt(t *testing.T) {
	fake := newLoopLLMClient(t)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 0 {
		t.Fatalf("LLM requests = %d, want 0", fake.requestCount())
	}
}
