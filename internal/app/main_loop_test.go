package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type loopLLMResponse struct {
	content           string
	stream            bool
	raw               bool
	status            int
	cancellationStart chan struct{}
}

type loopLLMRequest struct {
	stream        bool
	body          string
	authorization string
}

type loopLLMClient struct {
	responses []loopLLMResponse
	mu        sync.Mutex
	requests  []loopLLMRequest
}

type errorBodyTransport struct{}

type contextErrorTransport struct{}

type blockingContextTransport struct {
	started chan struct{}
	once    sync.Once
}

type errorReadCloser struct {
	err error
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
		stream:        request.Stream,
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
	if response.stream {
		if response.raw {
			return loopHTTPResponse(r, http.StatusOK, response.content, map[string]string{"Content-Type": "text/event-stream"}), nil
		}

		chunk := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": response.content}},
			},
		}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return nil, err
		}
		body := fmt.Sprintf("data: %s\n\ndata: [DONE]\n\n", encoded)
		return loopHTTPResponse(r, http.StatusOK, body, map[string]string{"Content-Type": "text/event-stream"}), nil
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

// RoundTrip returns a failed response whose body cannot be read.
func (transport errorBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body:       errorReadCloser{err: errors.New("broken error body")},
		Request:    r,
	}, nil
}

// RoundTrip waits for request cancellation and returns the context cause.
func (transport contextErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

// Read always fails to simulate a provider/socket error while reading the error body.
func (body errorReadCloser) Read(p []byte) (int, error) {
	return 0, body.err
}

// Close implements io.Closer for errorReadCloser.
func (body errorReadCloser) Close() error {
	return nil
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

// requestStreams returns whether each fake LLM request asked for streaming.
func (fake *loopLLMClient) requestStreams() []bool {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	streams := make([]bool, 0, len(fake.requests))
	for _, request := range fake.requests {
		streams = append(streams, request.stream)
	}
	return streams
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

// TestFilterPreviouslySuccessfulPlansKeepsFailedRetries checks only prior
// successful executions suppress matching proposals.
func TestFilterPreviouslySuccessfulPlansKeepsFailedRetries(t *testing.T) {
	plans := []commandPlan{{Command: "pwd"}, {Command: "false"}, {Command: "ls"}}
	executions := []commandExecution{{Command: "pwd", ExitCode: 0}, {Command: "false", ExitCode: 1}}

	kept, redundant := filterPreviouslySuccessfulPlans(plans, executions)
	if got := commandNames(kept); !reflect.DeepEqual(got, []string{"false", "ls"}) {
		t.Fatalf("kept = %v, want failed retry and new command", got)
	}
	if got := commandNames(redundant); !reflect.DeepEqual(got, []string{"pwd"}) {
		t.Fatalf("redundant = %v, want successful command", got)
	}
}

// TestFilterPreviouslySuccessfulPlansTrimsEffectiveCommands checks command
// identity uses trimmed effective execution and proposed command strings.
func TestFilterPreviouslySuccessfulPlansTrimsEffectiveCommands(t *testing.T) {
	plans := []commandPlan{{Command: "  pwd\t"}}
	executions := []commandExecution{{Command: "\n pwd ", ExitCode: 0}}

	kept, redundant := filterPreviouslySuccessfulPlans(plans, executions)
	if len(kept) != 0 || !reflect.DeepEqual(commandNames(redundant), []string{"  pwd\t"}) {
		t.Fatalf("kept = %#v, redundant = %#v, want trimmed successful match", kept, redundant)
	}
}

// TestFilterPreviouslySuccessfulPlansKeepsSkippedCommands checks commands
// absent from real executions remain eligible for a later attempt.
func TestFilterPreviouslySuccessfulPlansKeepsSkippedCommands(t *testing.T) {
	plans := []commandPlan{{Command: "touch blocked"}}
	executions := []commandExecution{{Command: "false", ExitCode: 1}}

	kept, redundant := filterPreviouslySuccessfulPlans(plans, executions)
	if !reflect.DeepEqual(commandNames(kept), []string{"touch blocked"}) || len(redundant) != 0 {
		t.Fatalf("kept = %#v, redundant = %#v, want skipped command retryable", kept, redundant)
	}
}

// TestSwitchInteractiveModelAppliesAndPersistsDefault checks /model changes the runtime profile and config default.
func TestSwitchInteractiveModelAppliesAndPersistsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"openai\"\n\n[[models]]\nname = \"openai\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Model switched.","commands":[]}`,
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
		content: `{"summary":"No command needed.","commands":[]}`,
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
		content: `{"summary":"No command needed.","commands":[]}`,
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
		"You are Shellia's planning layer.",
		"Decision process:",
		"user:",
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
		content: `{"summary":"No command needed.","commands":[]}`,
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

	if result.Actionable {
		t.Fatalf("runTurn() Actionable = true, want false")
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
		content: `{"summary":"No command needed.","commands":[]}`,
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
	if decisionData["decision"] != "final_answer_without_commands" {
		t.Fatalf("decision = %#v, want final_answer_without_commands", decisionData["decision"])
	}
}

// TestRunTurnExecutesSafePlanAndStreamsSummary checks planning, execution, and final summarization.
func TestRunTurnExecutesSafePlanAndStreamsSummary(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Printed shellia-loop.", stream: true},
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

	if !result.Actionable {
		t.Fatalf("runTurn() Actionable = false, want true")
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

	streams := fake.requestStreams()
	if len(streams) != 2 || streams[0] || !streams[1] {
		t.Fatalf("request streams = %#v, want []bool{false, true}", streams)
	}
}

// TestRunTurnPropagatesParentCancellationDuringSummary checks Ctrl+C is not
// converted into a successful fallback response after commands have run.
func TestRunTurnPropagatesParentCancellationDuringSummary(t *testing.T) {
	summaryStarted := make(chan struct{})
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
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
		if !outcome.result.Actionable || len(outcome.result.Executions) != 1 {
			t.Fatalf("runTurn() result = %#v, want actionable completed execution", outcome.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runTurn() did not return after cancellation")
	}
}

// TestRunTurnKeepsFallbackForRecoverableSummaryErrors checks provider failures
// still produce a result from the completed command execution.
func TestRunTurnKeepsFallbackForRecoverableSummaryErrors(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "data: {bad json}\n\n", stream: true, raw: true},
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

	if result.Result != "shellia-loop" {
		t.Fatalf("runTurn() Result = %q, want fallback answer", result.Result)
	}
}

// TestRunTurnTraceRecordsSummaryPromptAndResponse checks final responses are traced.
func TestRunTurnTraceRecordsSummaryPromptAndResponse(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Printed shellia-loop.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
	var summaryPrompts int
	var summaryResponses int
	for _, event := range events {
		if event["phase"] != "summary" {
			continue
		}
		switch event["event"] {
		case "llm_prompt":
			summaryPrompts++
		case "llm_response":
			summaryResponses++
			data := traceEventData(t, event)
			if data["raw_response"] != "Printed shellia-loop." {
				t.Fatalf("summary raw_response = %#v, want streamed answer", data["raw_response"])
			}
		}
	}
	if summaryPrompts != 1 || summaryResponses != 1 {
		t.Fatalf("summary events = prompts %d responses %d, want 1 and 1", summaryPrompts, summaryResponses)
	}
}

// TestRunTurnDeclinesPlanWithoutExecuting checks a rejected plan does not run or summarize commands.
func TestRunTurnDeclinesPlanWithoutExecuting(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Print a marker.","commands":[{"command":"echo shellia-loop","purpose":"Print marker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) (commandBatchResult, error) {
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
	if result.Actionable || len(result.Plans) != 1 || len(result.Executions) != 0 {
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
		content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	executed := false

	var result turnResult
	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) (commandBatchResult, error) {
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
	if !result.Actionable || len(result.Plans) != 1 || len(result.Executions) != 0 {
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

// TestRunTurnPlanOnlyExecutesAcceptedPlan checks /plan can run the exact accepted plan.
func TestRunTurnPlanOnlyExecutesAcceptedPlan(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Created marker file.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)
	var gotPlans []commandPlan

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
			gotPlans = append([]commandPlan{}, plans...)
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
			}}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "create marker"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if len(gotPlans) != 1 || gotPlans[0].Command != "touch marker.txt" {
		t.Fatalf("ExecuteCommands plans = %#v, want planned touch command", gotPlans)
	}
	if !result.Actionable || len(result.Executions) != 1 {
		t.Fatalf("runTurn() result = %#v, want executed plan", result)
	}
	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want 2", fake.requestCount())
	}
	if !strings.Contains(output, "Created marker file.") {
		t.Fatalf("accepted plan output does not contain streamed summary: %q", output)
	}
}

// TestRunTurnPlanOnlyExplainsObservationDependency checks unresolved follow-up plans explain the dependency.
func TestRunTurnPlanOnlyExplainsObservationDependency(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"First inspect the repo.","requires_observation":true,"observation_reason":"The next command depends on which package manager appears in the output.","commands":[{"command":"ls","purpose":"Inspect files","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "no\n", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "install deps")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if !strings.Contains(output, "The next command depends on which package manager appears in the output.") {
		t.Fatalf("plan-only output does not explain observation dependency: %q", output)
	}
	if !strings.Contains(output, "next step") {
		t.Fatalf("plan-only output does not label observation guidance as next step: %q", output)
	}
}

// TestRunTurnPlanOnlySkipsDiscoveryRepair checks /plan does not retry with discovery prompts.
func TestRunTurnPlanOnlySkipsDiscoveryRepair(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Need the target container.","requires_input":true,"input_reason":"No Docker container or image was specified.","commands":[]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Unexpected repair.","commands":[{"command":"docker ps","purpose":"Inspect Docker","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.PlanOnly = true
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "run php in docker")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if fake.requestCount() != 1 {
		t.Fatalf("LLM requests = %d, want 1", fake.requestCount())
	}
	if !strings.Contains(output, "missing detail") {
		t.Fatalf("plan-only output does not show missing detail guidance: %q", output)
	}
	if strings.Contains(output, "Unexpected repair") || strings.Contains(output, "docker ps") {
		t.Fatalf("plan-only output used discovery repair response: %q", output)
	}
}

// TestRunTurnUsesDiscoveryRepairForRecoverableEmptyPlan checks empty input responses get one discovery retry.
func TestRunTurnUsesDiscoveryRepairForRecoverableEmptyPlan(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Need the installed tool.","requires_input":true,"input_reason":"The install source is unknown.","commands":[]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Checking the local tool path.","requires_observation":false,"observation_reason":"","commands":[{"command":"command -v shellia","purpose":"Find shellia binary","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Found the shellia binary.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	var gotPlans []commandPlan
	var result turnResult
	output := captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
			gotPlans = append([]commandPlan{}, plans...)
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "/usr/local/bin/shellia"},
			}}}, nil
		}

		var err error
		result, err = runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "update shellia"))
		if err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if len(gotPlans) != 1 || gotPlans[0].Command != "command -v shellia" {
		t.Fatalf("ExecuteCommands plans = %#v, want discovery command", gotPlans)
	}
	if !result.Actionable || len(result.Executions) != 1 {
		t.Fatalf("runTurn() result = %#v, want executed discovery plan", result)
	}
	if fake.requestCount() != 3 {
		t.Fatalf("LLM requests = %d, want initial, repair, and summary requests", fake.requestCount())
	}
	bodies := fake.requestBodies()
	if len(bodies) < 2 || !strings.Contains(bodies[1], "Discovery repair mode") {
		t.Fatalf("repair request body = %#v, want discovery repair prompt", bodies)
	}
	if !strings.Contains(output, "Found the shellia binary.") {
		t.Fatalf("runTurn() output missing final summary: %q", output)
	}
}

// TestRunTurnTraceRecordsDiscoveryRepair checks repair prompts are tagged separately.
func TestRunTurnTraceRecordsDiscoveryRepair(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Need the installed tool.","requires_input":true,"input_reason":"The install source is unknown.","commands":[]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Checking the local tool path.","commands":[{"command":"command -v shellia","purpose":"Find shellia binary","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Found the shellia binary.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	captureMainLoopIO(t, "yes\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
			return commandBatchResult{Executions: []commandExecution{{
				Command:  plans[0].Command,
				Purpose:  plans[0].Purpose,
				ExitCode: 0,
				Stdout:   capturedStream{Text: "/usr/local/bin/shellia"},
			}}}, nil
		}
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "update shellia")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	events := closeLoopTraceAndRead(t, logger)
	var planningPrompt bool
	var repairPrompt bool
	var repairResponse bool
	for _, event := range events {
		if event["event"] == "llm_prompt" && event["phase"] == "planning" {
			planningPrompt = true
		}
		if event["event"] == "llm_prompt" && event["phase"] == "discovery_repair" {
			repairPrompt = true
		}
		if event["event"] == "llm_response" && event["phase"] == "discovery_repair" {
			repairResponse = true
		}
	}
	if !planningPrompt || !repairPrompt || !repairResponse {
		t.Fatalf("trace phases planning=%t repair_prompt=%t repair_response=%t", planningPrompt, repairPrompt, repairResponse)
	}
}

// TestRunTurnUsesBoundedExplicitGitObservation checks repository state reaches
// the model only through the normal command observation path.
func TestRunTurnUsesBoundedExplicitGitObservation(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{
			content: `{"summary":"Inspect current repository state.","requires_observation":true,"commands":[{"command":"git status --short","purpose":"Inspect Git status","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Repository state inspected.","commands":[]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.ObservationOutputChars = 12
	ctxInfo := loopTestContext(t)

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
		loopLLMResponse{content: `{"summary":"Run initial batch.","commands":[{"command":"false","purpose":"Trigger failure","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""},{"command":"touch blocked","purpose":"Blocked dependent step","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""},{"command":"pwd","purpose":"Independent inspection","risk":"safe","requires_confirmation":false,"independent_on_failure":true,"interactive":false,"interactive_reason":""}]}`},
		loopLLMResponse{content: `{"summary":"Run recovery.","commands":[{"command":"git status --short","purpose":"Verify repository state","risk":"safe","requires_confirmation":false,"independent_on_failure":false,"interactive":false,"interactive_reason":""}]}`},
		loopLLMResponse{content: "Recovery completed.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.ContinueOnError = true
	ctxInfo := loopTestContext(t)
	call := 0

	output := captureMainLoopIO(t, "y\ny\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
		t.Fatalf("request count = %d, want two planning requests and one summary", len(bodies))
	}
	for _, snippet := range []string{"Exit code: 7", "initial failure", "Independent inspection", "Skipped commands from the current task:", "touch blocked", "dependent on an earlier failed command"} {
		if !strings.Contains(bodies[1], snippet) {
			t.Fatalf("recovery prompt missing %q: %q", snippet, bodies[1])
		}
	}
	for _, snippet := range []string{"Skipped commands", "touch blocked", "dependent on an earlier failed command", "were not executed"} {
		if !strings.Contains(bodies[2], snippet) {
			t.Fatalf("summary prompt missing %q: %q", snippet, bodies[2])
		}
	}
	if strings.Count(output, "Execute this plan? [y/n]: yes") != 2 {
		t.Fatalf("output = %q, want confirmation for initial and recovery plans", output)
	}
}

// TestRunTurnFiltersSuccessfulCorrectionsButRetriesFailures checks repeated
// mixed proposals show, confirm, and execute only commands that have not succeeded.
func TestRunTurnFiltersSuccessfulCorrectionsButRetriesFailures(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Run failing command.","commands":[{"command":"false","purpose":"Trigger failure","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Apply correction and retry.","commands":[{"command":"touch corrected","purpose":"Apply correction","risk":"safe","requires_confirmation":false,"independent_on_failure":true},{"command":"false","purpose":"Retry failure","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"summary":"Retry the remaining failure.","commands":[{"command":"touch corrected","purpose":"Apply correction","risk":"safe","requires_confirmation":false,"independent_on_failure":true},{"command":"false","purpose":"Retry failure","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"summary":"Retries exhausted after grounded outcomes.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.PlanningMaxRounds = 4
	ctxInfo := loopTestContext(t)
	var executionBatches [][]string

	var result turnResult
	output := captureMainLoopIO(t, "y\ny\ny\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
	if len(result.Executions) != 4 || result.Result != "Retries exhausted after grounded outcomes." {
		t.Fatalf("result = %#v, want all four real executions and grounded final answer", result)
	}
	falseRuns := 0
	correctionRuns := 0
	for _, execution := range result.Executions {
		switch execution.Command {
		case "false":
			falseRuns++
		case "touch corrected":
			correctionRuns++
		}
	}
	if falseRuns != 3 || correctionRuns != 1 {
		t.Fatalf("false runs = %d, correction runs = %d, want 3 and 1", falseRuns, correctionRuns)
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
		loopLLMResponse{content: `{"summary":"Run failures.","commands":[{"command":"false","purpose":"First failure","risk":"safe","requires_confirmation":false},{"command":"exit 2","purpose":"Second failure","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"summary":"Recover once.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: "Recovered once.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	calls := 0

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
		loopLLMResponse{content: `{"summary":"Run timeout batch.","requires_observation":true,"commands":[{"command":"slow","purpose":"Timed operation","risk":"safe","requires_confirmation":false},{"command":"pwd","purpose":"Independent inspection","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: "Stopped after timeout.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if fake.requestCount() != 2 || len(result.Executions) != 2 {
		t.Fatalf("requests = %d, result = %#v, want planning/summary and both executions", fake.requestCount(), result)
	}
	events := closeLoopTraceAndRead(t, logger)
	found := false
	for _, event := range traceEventsByName(events, "shellia_decision") {
		data := traceEventData(t, event)
		if data["decision"] == "execution_failure_replan_excluded" && data["reason"] == "timeout" {
			found = true
		}
	}
	if !found {
		t.Fatal("trace missing timeout execution_failure_replan_excluded decision")
	}
}

// TestRunTurnCancellationDoesNotReplan checks cancellation returns immediately
// and records why execution failure recovery was excluded.
func TestRunTurnCancellationDoesNotReplan(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"summary":"Run commands.","commands":[{"command":"pwd","purpose":"Completed inspection","risk":"safe","requires_confirmation":false},{"command":"wait","purpose":"Cancelled step","risk":"safe","requires_confirmation":false}]}`})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	logger := openLoopTrace(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.Trace = logger
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
	if !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 {
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
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"summary":"Run batch.","commands":[{"command":"pwd","purpose":"Inspect","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	runnerErr := errors.New("runner transport failed")

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if fake.requestCount() != 1 || !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("requests = %d, result = %#v, want one plan and partial result", fake.requestCount(), result)
	}
}

// TestRunTurnLaterPlanningErrorReturnsPartialResult checks a failed follow-up
// model request retains the batch that required that request.
func TestRunTurnLaterPlanningErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Inspect first.","requires_observation":true,"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{status: http.StatusBadRequest, content: "bad follow-up request"},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if fake.requestCount() != 2 || !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("requests = %d, result = %#v, want partial result after failed follow-up", fake.requestCount(), result)
	}
}

// TestRunTurnRecoveryConfirmationErrorReturnsPartialResult checks failure to
// read a later plan confirmation does not erase earlier outcomes.
func TestRunTurnRecoveryConfirmationErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, deps runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v, want partial result after confirmation error", result)
	}
}

// TestRunTurnPlanningLimitPromptErrorReturnsPartialResult checks the existing
// limit prompt's error path preserves accumulated execution state.
func TestRunTurnPlanningLimitPromptErrorReturnsPartialResult(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"later","purpose":"Later step","risk":"safe","requires_confirmation":false}]}`})
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, deps runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v, want partial result after planning-limit error", result)
	}
}

// TestRunTurnOrdinaryFailureOverridesTimeoutExclusion checks mixed batches use
// the ordinary-failure recovery path even when a timeout also occurred.
func TestRunTurnOrdinaryFailureOverridesTimeoutExclusion(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Run mixed failures.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"slow","purpose":"Timeout","risk":"safe","requires_confirmation":false,"independent_on_failure":true}]}`},
		loopLLMResponse{content: `{"summary":"Recover mixed batch.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: "Recovered mixed batch.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	calls := 0

	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
		if _, err := runTurn(t.Context(), deps, false, loopTurnRequest(cfg, &ctxInfo, "recover mixed failures")); err != nil {
			t.Fatalf("runTurn() error = %v", err)
		}
	})

	if calls != 2 || fake.requestCount() != 3 {
		t.Fatalf("execution calls = %d, LLM requests = %d, want one recovery batch", calls, fake.requestCount())
	}
}

// TestRunTurnInteractiveRepairUsesPlanningLimit checks interactive-prompt
// repair retains partial execution and shares the normal planning-limit path.
func TestRunTurnInteractiveRepairUsesPlanningLimit(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Run prompt-prone command.","commands":[{"command":"prompting-command","purpose":"Attempt command","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Repair prompt use.","commands":[{"command":"pwd","purpose":"Recover non-interactively","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: "Recovered from interactive prompt.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)
	calls := 0

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
		wantResult   string
	}{
		{
			name:  "accepted",
			input: "y\n",
			responses: []loopLLMResponse{
				{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"blocked","purpose":"Skip","risk":"safe","requires_confirmation":false}]}`},
				{content: `{"summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
				{content: "Recovered after extension.", stream: true},
			},
			wantRequests: 3,
			wantResult:   "Recovered after extension.",
		},
		{
			name:  "declined",
			input: "n\n",
			responses: []loopLLMResponse{
				{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"blocked","purpose":"Skip","risk":"safe","requires_confirmation":false}]}`},
				{content: "Stopped with partial outcomes.", stream: true},
			},
			wantRequests: 2,
			wantResult:   "Stopped with partial outcomes.",
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

			var result turnResult
			captureMainLoopIO(t, tt.input, fake.HTTPClient(), func(deps runtimeDeps) {
				deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

			if fake.requestCount() != tt.wantRequests || result.Result != tt.wantResult || len(result.Executions) != calls || len(result.Skipped) != 1 {
				t.Fatalf("requests = %d, calls = %d, result = %#v", fake.requestCount(), calls, result)
			}
		})
	}
}

// TestRunTurnPreservesBatchWhenRecoveryPlanIsEmpty checks a post-execution
// final answer remains actionable and carries executions and skips.
func TestRunTurnPreservesBatchWhenRecoveryPlanIsEmpty(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Inspect.","requires_observation":true,"commands":[{"command":"pwd","purpose":"Inspect","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Nothing else to run.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	captureMainLoopIO(t, "", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 || result.Result != "Nothing else to run." {
		t.Fatalf("result = %#v, want actionable accumulated batch", result)
	}
}

// TestRunTurnPreservesBatchWhenRecoveryPlanIsDeclined checks declining a later
// plan does not discard outcomes already produced in the turn.
func TestRunTurnPreservesBatchWhenRecoveryPlanIsDeclined(t *testing.T) {
	fake := newLoopLLMClient(t,
		loopLLMResponse{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false},{"command":"blocked","purpose":"Skip","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Proposed recovery.","commands":[{"command":"pwd","purpose":"Recover","risk":"safe","requires_confirmation":false}]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "y\nn\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if !result.Actionable || len(result.Executions) != 1 || len(result.Skipped) != 1 {
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
		loopLLMResponse{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover safely","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: "Recovered with confirmation.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = true
	cfg.YesSafe = false
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "y\ny\ny\ny\n", fake.HTTPClient(), func(deps runtimeDeps) {
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
				loopLLMResponse{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false}]}`},
				loopLLMResponse{content: `{"summary":"Recover.","commands":[{"command":"pwd","purpose":"Recover safely","risk":"safe","requires_confirmation":false}]}`},
				loopLLMResponse{content: "Recovered safely.", stream: true},
			)
			cfg := loopTestConfig(fake.URL())
			cfg.AskConfirmPlan = false
			cfg.YesSafe = tt.yesSafe
			ctxInfo := loopTestContext(t)
			calls := 0

			output := captureMainLoopIO(t, tt.input, fake.HTTPClient(), func(deps runtimeDeps) {
				deps.ExecuteCommands = func(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan) (commandBatchResult, error) {
					calls++
					if calls == 1 {
						return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}}, HadOrdinaryFailure: true}, nil
					}
					return executeCommands(ctx, deps, ui, cfg, ctxInfo, plans)
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
		loopLLMResponse{content: `{"summary":"Fail first.","commands":[{"command":"false","purpose":"Fail","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Recover.","commands":[{"command":"touch recovered-marker","purpose":"Create recovery marker","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: "Created recovery marker.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.YesSafe = true
	calls := 0

	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(ctx context.Context, deps runtimeDeps, ui bool, cfg config, ctxInfo *contextInfo, plans []commandPlan) (commandBatchResult, error) {
			calls++
			if calls == 1 {
				return commandBatchResult{Executions: []commandExecution{{Command: plans[0].Command, Purpose: plans[0].Purpose, ExitCode: 1}}, HadOrdinaryFailure: true}, nil
			}
			return executeCommands(ctx, deps, ui, cfg, ctxInfo, plans)
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
			content: `{"summary":"Inspect the first fact.","requires_observation":true,"commands":[{"command":"echo first","purpose":"Inspect first fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Inspect the second fact.","requires_observation":true,"commands":[{"command":"echo second","purpose":"Inspect second fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{
			content: `{"summary":"Done after extra planning.","commands":[]}`,
		},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 2
	ctxInfo := loopTestContext(t)
	var executed []string

	var result turnResult
	output := captureMainLoopIO(t, "y\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
			content: `{"summary":"Inspect one fact.","requires_observation":true,"commands":[{"command":"echo first","purpose":"Inspect first fact","risk":"safe","requires_confirmation":false,"interactive":false,"interactive_reason":""}]}`,
		},
		loopLLMResponse{content: "Stopped after the observed fact.", stream: true},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	cfg.PlanningMaxRounds = 1
	ctxInfo := loopTestContext(t)

	var result turnResult
	output := captureMainLoopIO(t, "n\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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

	if result.Result != "Stopped after the observed fact." {
		t.Fatalf("result = %q, want streamed summary", result.Result)
	}
	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want planning and summary", fake.requestCount())
	}
	if !strings.Contains(output, "SHELLIA_PLANNING_MAX_ROUNDS") {
		t.Fatalf("output missing env override guidance: %q", output)
	}
	if !strings.Contains(output, "Continue planning? [y/n]: no") {
		t.Fatalf("output missing declined continuation: %q", output)
	}
}

// TestRunTurnPlanOnlyUsesDedicatedSystemPrompt checks /plan sends the plan-only prompt contract.
func TestRunTurnPlanOnlyUsesDedicatedSystemPrompt(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Preparation: create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
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
	if !strings.Contains(bodies[0], "non-executing plan mode") {
		t.Fatalf("plan-only request did not use dedicated system prompt: %q", bodies[0])
	}
}

// TestRunInteractiveProcessesPromptThenExit checks that the interactive loop runs one AI turn and exits cleanly.
func TestRunInteractiveProcessesPromptThenExit(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: `{"summary":"Interactive answer.","commands":[]}`,
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
		content: `{"summary":"Create a marker file.","commands":[{"command":"touch marker.txt","purpose":"Create marker","risk":"medium","requires_confirmation":true,"interactive":false,"interactive_reason":""}]}`,
	})
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)
	executed := false

	output := captureMainLoopIO(t, "/plan create marker\nno\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(context.Context, runtimeDeps, bool, config, *contextInfo, []commandPlan) (commandBatchResult, error) {
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
		loopLLMResponse{content: `{"summary":"No command needed.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	ctxInfo := loopTestContext(t)

	output := captureMainLoopIO(t, "build it\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		runInteractive(t.Context(), deps, false, cfg, &ctxInfo)
	})

	if fake.requestCount() != 2 {
		t.Fatalf("LLM requests = %d, want 2", fake.requestCount())
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
		loopLLMResponse{content: `{"summary":"Inspect once.","commands":[{"command":"pwd","purpose":"Inspect before cancellation","risk":"safe","requires_confirmation":false},{"command":"wait","purpose":"Cancelled step","risk":"safe","requires_confirmation":false}]}`},
		loopLLMResponse{content: `{"summary":"Retry received partial context.","commands":[]}`},
	)
	cfg := loopTestConfig(fake.URL())
	cfg.AskConfirmPlan = false
	ctxInfo := loopTestContext(t)
	calls := 0

	output := captureMainLoopIO(t, "inspect once\n/retry\n/exit\n", fake.HTTPClient(), func(deps runtimeDeps) {
		deps.ExecuteCommands = func(_ context.Context, _ runtimeDeps, _ bool, _ config, _ *contextInfo, plans []commandPlan) (commandBatchResult, error) {
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
	for _, snippet := range []string{"last_retry_instruction: inspect once", "Recent reusable observations:", "Inspect before cancellation", "observed-before-cancel"} {
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
		loopLLMResponse{content: `{"summary":"First answer.","commands":[]}`},
		loopLLMResponse{content: `{"summary":"Second answer.","commands":[]}`},
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

// TestDoLLMStreamReturnsMalformedChunkError checks corrupt SSE payloads are not skipped silently.
func TestDoLLMStreamReturnsMalformedChunkError(t *testing.T) {
	fake := newLoopLLMClient(t, loopLLMResponse{
		content: "data: {bad json}\n\n",
		stream:  true,
		raw:     true,
	})
	cfg := loopTestConfig(fake.URL())

	var output strings.Builder
	result, err := doLLMStream(t.Context(), fake.HTTPClient(), cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if err == nil {
		t.Fatalf("doLLMStream() error = nil, want malformed chunk error")
	}
	if !strings.Contains(err.Error(), "invalid llm stream chunk") {
		t.Fatalf("doLLMStream() error = %q, want invalid chunk message", err.Error())
	}
	if result != "" || output.String() != "" {
		t.Fatalf("doLLMStream() result/output = %q/%q, want both empty", result, output.String())
	}
}

// TestDoLLMStreamPropagatesContextCancellation checks streaming calls preserve cancellation identity.
func TestDoLLMStreamPropagatesContextCancellation(t *testing.T) {
	cfg := loopTestConfig("http://shellia.test")
	client := &http.Client{Transport: contextErrorTransport{}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var output strings.Builder
	_, err := doLLMStream(ctx, client, cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("doLLMStream() error = %v, want context.Canceled", err)
	}
	if output.String() != "" {
		t.Fatalf("doLLMStream() output = %q, want empty output", output.String())
	}
}

// TestDoLLMStreamReportsErrorBodyReadFailure checks failed stream diagnostics include body read errors.
func TestDoLLMStreamReportsErrorBodyReadFailure(t *testing.T) {
	cfg := loopTestConfig("http://shellia.test")
	var output strings.Builder
	client := &http.Client{Transport: errorBodyTransport{}}
	_, err := doLLMStream(t.Context(), client, cfg, chatCompletionRequest{Model: cfg.Model}, &output)
	if err == nil {
		t.Fatalf("doLLMStream() error = nil, want HTTP error")
	}

	message := err.Error()
	if !strings.Contains(message, "llm request failed with status 400") {
		t.Fatalf("doLLMStream() error = %q, want status", message)
	}
	if !strings.Contains(message, "cannot read error response body") || !strings.Contains(message, "broken error body") {
		t.Fatalf("doLLMStream() error = %q, want body read failure", message)
	}
	var statusErr *llmHTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("doLLMStream() error = %T %[1]v, want llmHTTPStatusError status 400", err)
	}
}
